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

// Panel arma los números de la pantalla de inicio. Los días van en hora argentina.
func (s *Store) Panel(ctx context.Context, ahora time.Time) (*Panel, error) {
	p := &Panel{PorEstado: map[string]int{"ok": 0, "revisar": 0, "error": 0, "aprobado": 0}, PorDia: []DiaPanel{}, Lotes: []Lote{}}
	const tz = `'America/Argentina/Buenos_Aires'`
	q := s.Pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM documentos WHERE (creado AT TIME ZONE `+tz+`)::date = ($1 AT TIME ZONE `+tz+`)::date),
		(SELECT count(*) FROM comprobantes WHERE (creado AT TIME ZONE `+tz+`)::date = ($1 AT TIME ZONE `+tz+`)::date),
		(SELECT count(*) FROM documentos WHERE date_trunc('month', creado AT TIME ZONE `+tz+`) = date_trunc('month', $1 AT TIME ZONE `+tz+`)),
		(SELECT count(*) FROM comprobantes WHERE date_trunc('month', creado AT TIME ZONE `+tz+`) = date_trunc('month', $1 AT TIME ZONE `+tz+`)),
		(SELECT count(*) FROM comprobantes WHERE hallazgos @> '[{"codigo":"emisor_apocrifo"}]'),
		(SELECT count(*) FROM comprobantes WHERE hallazgos @> '[{"codigo":"qr_distinto_texto"}]'),
		(SELECT count(*) FROM comprobantes WHERE datos->'origenes' @> '{}' AND EXISTS (SELECT 1 FROM jsonb_each_text(datos->'origenes') o WHERE o.value = 'vision')),
		(SELECT count(*) FROM comprobantes),
		(SELECT coalesce(avg(duracion_ms), 0) FROM documentos WHERE estado = 'listo')`, ahora)
	if err := q.Scan(&p.Hoy.Documentos, &p.Hoy.Comprobantes, &p.Mes.Documentos, &p.Mes.Comprobantes,
		&p.Apocrifos, &p.Adulterados, &p.ConIA, &p.Total, &p.DuracionMS); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT estado, count(*) FROM comprobantes GROUP BY estado`)
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
		LEFT JOIN comprobantes c ON (c.creado AT TIME ZONE `+tz+`)::date = d::date
		GROUP BY d ORDER BY d`, ahora)
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
	ls, _, err := s.Lotes(ctx, 5, 0)
	if err != nil {
		return nil, err
	}
	p.Lotes = ls
	return p, nil
}
