package store

import (
	"context"
	"time"
)

// Panel es lo que muestra la pantalla de inicio.
type Panel struct {
	Hoy         Contadores     `json:"hoy"`
	Mes         Contadores     `json:"mes"`
	PorEstado   map[string]int `json:"por_estado"`
	Apocrifos   int            `json:"apocrifos"`
	Adulterados int            `json:"adulterados"`
	ConIA       int            `json:"con_ia"`
	TicketsMes  int            `json:"tickets_mes"` // tiques de controlador fiscal cargados este mes
	Total       int            `json:"total"`
	DuracionMS  float64        `json:"duracion_promedio_ms"`
	PorDia      []DiaPanel     `json:"por_dia"`
	Lotes       []Lote         `json:"lotes"`
}

type Contadores struct {
	Documentos   int `json:"documentos"`
	Comprobantes int `json:"comprobantes"`
}

type DiaPanel struct {
	Dia     string `json:"dia"`
	OK      int    `json:"ok"`
	Revisar int    `json:"revisar"`
	Error   int    `json:"error"`
}

// Panel arma los números de la pantalla de inicio. Los días van en hora argentina. alcance: los CUITs
// de cliente que puede ver quien pide (nil = todos). Lo de la papelera no cuenta.
func (s *Store) Panel(ctx context.Context, ahora time.Time, alcance []string) (*Panel, error) {
	p := &Panel{PorEstado: map[string]int{"ok": 0, "revisar": 0, "error": 0, "aprobado": 0}, PorDia: []DiaPanel{}, Lotes: []Lote{}}
	const tz = `'America/Argentina/Buenos_Aires'`
	q := s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM documentos WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR lote_id IN (SELECT id FROM lotes WHERE cliente_cuit = ANY($3))) AND (creado AT TIME ZONE `+tz+`)::date = ($1 AT TIME ZONE `+tz+`)::date),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND (creado AT TIME ZONE `+tz+`)::date = ($1 AT TIME ZONE `+tz+`)::date),
		(SELECT count(*) FROM documentos WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR lote_id IN (SELECT id FROM lotes WHERE cliente_cuit = ANY($3))) AND date_trunc('month', creado AT TIME ZONE `+tz+`) = date_trunc('month', $1 AT TIME ZONE `+tz+`)),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND date_trunc('month', creado AT TIME ZONE `+tz+`) = date_trunc('month', $1 AT TIME ZONE `+tz+`)),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND hallazgos @> '[{"codigo":"emisor_apocrifo"}]'),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND hallazgos @> '[{"codigo":"qr_distinto_texto"}]'),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND datos->'origenes' @> '{}' AND EXISTS (SELECT 1 FROM jsonb_each_text(datos->'origenes') o WHERE o.value = 'vision')),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3))),
		(SELECT coalesce(avg(duracion_ms), 0) FROM documentos WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR lote_id IN (SELECT id FROM lotes WHERE cliente_cuit = ANY($3))) AND estado = 'listo'),
		(SELECT count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($3::text[] IS NULL OR cliente_cuit = ANY($3)) AND tipo = ANY($2::int[]) AND date_trunc('month', creado AT TIME ZONE `+tz+`) = date_trunc('month', $1 AT TIME ZONE `+tz+`))`, ahora, codigosTique(), alcance)
	if err := q.Scan(&p.Hoy.Documentos, &p.Hoy.Comprobantes, &p.Mes.Documentos, &p.Mes.Comprobantes,
		&p.Apocrifos, &p.Adulterados, &p.ConIA, &p.Total, &p.DuracionMS, &p.TicketsMes); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT estado, count(*) FROM comprobantes WHERE borrado_en IS NULL AND ($1::text[] IS NULL OR cliente_cuit = ANY($1)) GROUP BY estado`, alcance)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var e string
		var n int
		if err := rows.Scan(&e, &n); err != nil {
			rows.Close()
			return nil, err
		}
		p.PorEstado[e] = n
	}
	rows.Close()

	// Últimos 14 días, con los días sin movimiento en cero.
	rows, err = s.Pool.Query(ctx, `SELECT to_char(d, 'YYYY-MM-DD'),
			count(c.id) FILTER (WHERE c.estado IN ('ok','aprobado')),
			count(c.id) FILTER (WHERE c.estado = 'revisar'),
			count(c.id) FILTER (WHERE c.estado = 'error')
		FROM generate_series(($1 AT TIME ZONE `+tz+`)::date - 13, ($1 AT TIME ZONE `+tz+`)::date, interval '1 day') d
		LEFT JOIN comprobantes c ON (c.creado AT TIME ZONE `+tz+`)::date = d::date AND c.borrado_en IS NULL AND ($2::text[] IS NULL OR c.cliente_cuit = ANY($2))
		GROUP BY d ORDER BY d`, ahora, alcance)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d DiaPanel
		if err := rows.Scan(&d.Dia, &d.OK, &d.Revisar, &d.Error); err != nil {
			rows.Close()
			return nil, err
		}
		p.PorDia = append(p.PorDia, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ls, _, err := s.Lotes(ctx, 5, 0, alcance)
	if err != nil {
		return nil, err
	}
	p.Lotes = ls
	return p, nil
}
