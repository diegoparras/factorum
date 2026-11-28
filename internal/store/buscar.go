package store

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Busqueda es lo que pide el buscador de la cabecera (Ctrl+K).
type Busqueda struct {
	Q     string
	Desde string // AAAA-MM-DD: solo comprobantes con fecha desde ese día ("" = todos)
	// Alcance son los CUITs de cliente que puede ver quien busca: nil = todos. Lo pone la API según
	// la identidad, nunca el pedido.
	Alcance []string
	Limite  int // por grupo
}

// HallazgoCbte es un comprobante encontrado: lo justo para mostrarlo en la lista.
type HallazgoCbte struct {
	ID          int64  `json:"id"`
	LoteID      int64  `json:"lote_id"`
	ClienteCUIT string `json:"cliente_cuit"`
	Tipo        int    `json:"tipo"`
	PuntoVenta  int    `json:"punto_venta"`
	Numero      int64  `json:"numero"`
	Despacho    string `json:"despacho,omitempty"`
	Fecha       string `json:"fecha"`
	CUITEmisor  string `json:"cuit_emisor"`
	RazonSocial string `json:"razon_social_emisor"`
	Total       string `json:"total"`
	Moneda      string `json:"moneda"`
	Estado      string `json:"estado"`
	EsTique     bool   `json:"es_tique"`
}

// HallazgoProveedor es un emisor con comprobantes visibles.
type HallazgoProveedor struct {
	CUIT         string `json:"cuit"`
	RazonSocial  string `json:"razon_social"`
	Comprobantes int    `json:"comprobantes"`
	Ultimo       string `json:"ultimo,omitempty"` // la fecha del último comprobante
}

// HallazgoLote es un lote encontrado.
type HallazgoLote struct {
	ID           int64     `json:"id"`
	Nombre       string    `json:"nombre"`
	ClienteCUIT  string    `json:"cliente_cuit"`
	Origen       string    `json:"origen"`
	Creado       time.Time `json:"creado"`
	Comprobantes int       `json:"comprobantes"`
}

// ResultadoBusqueda junta los grupos. Los Total* cuentan todo lo que coincide (no solo lo que vuelve).
type ResultadoBusqueda struct {
	Comprobantes      []HallazgoCbte      `json:"comprobantes"`
	TotalComprobantes int                 `json:"total_comprobantes"`
	Proveedores       []HallazgoProveedor `json:"proveedores"`
	TotalProveedores  int                 `json:"total_proveedores"`
	Lotes             []HallazgoLote      `json:"lotes"`
	TotalLotes        int                 `json:"total_lotes"`
}

// LimiteBusqueda: por grupo, lo que se devuelve por defecto y el máximo.
const (
	LimiteBusqueda    = 5
	LimiteBusquedaMax = 20
)

var (
	rePVNro   = regexp.MustCompile(`^0*(\d{1,5})\s*-\s*0*(\d{1,8})$`)
	reImporte = regexp.MustCompile(`^\$?\s*[\d.,]+$`)
	reLoteID  = regexp.MustCompile(`^(?i:lote\s*)?#?(\d{1,12})$`)
)

// patronLike escapa los comodines de LIKE (el texto de quien busca no es un patrón).
func patronLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// importeCentavos entiende un importe escrito como en la pantalla (44.062,50 o $ 44062,5) o con
// punto decimal (44062.50). Un número sin coma ni decimales con punto es en pesos enteros.
func importeCentavos(q string) (int64, bool) {
	q = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "$"))
	if q == "" || !reImporte.MatchString(q) {
		return 0, false
	}
	switch {
	case strings.Contains(q, ","):
		q = strings.ReplaceAll(q, ".", "")
		q = strings.Replace(q, ",", ".", 1)
	case strings.Count(q, ".") == 1 && len(q)-strings.LastIndex(q, ".")-1 <= 2:
		// 44062.50: punto decimal.
	default:
		q = strings.ReplaceAll(q, ".", "") // 44.062: separador de miles
	}
	ent, dec, _ := strings.Cut(q, ".")
	if ent == "" || len(ent) > 13 || strings.Contains(dec, ",") || len(dec) > 2 {
		return 0, false
	}
	for len(dec) < 2 {
		dec += "0"
	}
	n, err := strconv.ParseInt(ent+dec, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func limiteBusqueda(n int) int {
	if n <= 0 {
		return LimiteBusqueda
	}
	return min(n, LimiteBusquedaMax)
}

// Buscar busca en comprobantes, proveedores (emisores) y lotes a la vez: por número (pv-nro o solo
// el número), CUIT (del emisor o del cliente), razón social, importe y nombre o número de lote. Lo
// que está en la papelera no aparece. Todo va parametrizado.
func (s *Store) Buscar(ctx context.Context, b Busqueda) (*ResultadoBusqueda, error) {
	res := &ResultadoBusqueda{Comprobantes: []HallazgoCbte{}, Proveedores: []HallazgoProveedor{}, Lotes: []HallazgoLote{}}
	q := strings.TrimSpace(b.Q)
	if len([]rune(q)) < 2 {
		return res, nil
	}
	if len(q) > 100 {
		q = q[:100]
	}
	lim := limiteBusqueda(b.Limite)
	dig := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, q)
	// Un CUIT se escribe con o sin guiones: si lo que se escribió son dígitos y guiones, se busca.
	soloCUIT := dig != "" && strings.Trim(q, "0123456789- ") == ""

	// ---- Comprobantes ----
	args := []any{b.Alcance, "%" + patronLike(q) + "%"}
	arg := func(v any) string { args = append(args, v); return "$" + strconv.Itoa(len(args)) }
	conds := []string{"c.razon_social_emisor ILIKE $2", "coalesce(c.datos->>'despacho', '') ILIKE $2"}
	if soloCUIT && len(dig) >= 3 {
		p := arg(patronLike(dig) + "%")
		conds = append(conds, "c.cuit_emisor LIKE "+p, "c.cliente_cuit LIKE "+p)
	}
	if m := rePVNro.FindStringSubmatch(q); m != nil {
		pv, _ := strconv.Atoi(m[1])
		nro, _ := strconv.ParseInt(m[2], 10, 64)
		conds = append(conds, "(c.punto_venta = "+arg(pv)+" AND c.numero = "+arg(nro)+")")
	} else if dig == q && len(dig) <= 8 {
		n, _ := strconv.ParseInt(dig, 10, 64)
		conds = append(conds, "c.numero = "+arg(n))
	}
	if cent, ok := importeCentavos(q); ok && cent > 0 {
		conds = append(conds, "c.total = "+arg(cent), "c.total = "+arg(-cent))
	}
	donde := "c.borrado_en IS NULL AND ($1::text[] IS NULL OR c.cliente_cuit = ANY($1::text[])) AND (" + strings.Join(conds, " OR ") + ")"
	if b.Desde != "" {
		donde += " AND c.fecha >= " + arg(b.Desde) + "::date"
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.id, c.lote_id, c.cliente_cuit, coalesce(c.tipo, 0), coalesce(c.punto_venta, 0), coalesce(c.numero, 0),
			coalesce(c.datos->>'despacho', ''), coalesce(to_char(c.fecha, 'YYYY-MM-DD'), ''), coalesce(c.cuit_emisor, ''), coalesce(c.razon_social_emisor, ''),
			coalesce(c.datos->>'total', ''), coalesce(c.datos->>'moneda', ''), c.estado, coalesce((c.datos->>'es_tique')::boolean, false), count(*) OVER ()
		FROM comprobantes c WHERE `+donde+` ORDER BY c.fecha DESC NULLS LAST, c.id DESC LIMIT `+arg(lim), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h HallazgoCbte
		if err := rows.Scan(&h.ID, &h.LoteID, &h.ClienteCUIT, &h.Tipo, &h.PuntoVenta, &h.Numero, &h.Despacho, &h.Fecha, &h.CUITEmisor, &h.RazonSocial,
			&h.Total, &h.Moneda, &h.Estado, &h.EsTique, &res.TotalComprobantes); err != nil {
			rows.Close()
			return nil, err
		}
		res.Comprobantes = append(res.Comprobantes, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ---- Proveedores: los emisores con comprobantes visibles, con cuántos tienen ----
	args = []any{b.Alcance, "%" + patronLike(q) + "%"}
	pconds := []string{"x.razon_social_emisor ILIKE $2"}
	if soloCUIT && len(dig) >= 3 {
		pconds = append(pconds, "x.cuit_emisor LIKE "+arg(patronLike(dig)+"%"))
	}
	rows, err = s.Pool.Query(ctx, `WITH e AS (
			SELECT c.cuit_emisor AS cuit, count(*) AS n, max(c.fecha) AS ultimo,
				(array_agg(c.razon_social_emisor ORDER BY c.fecha DESC NULLS LAST, c.id DESC) FILTER (WHERE coalesce(c.razon_social_emisor, '') <> ''))[1] AS razon
			FROM comprobantes c
			WHERE c.borrado_en IS NULL AND coalesce(c.cuit_emisor, '') <> '' AND ($1::text[] IS NULL OR c.cliente_cuit = ANY($1::text[]))
				AND c.cuit_emisor IN (SELECT DISTINCT x.cuit_emisor FROM comprobantes x WHERE x.borrado_en IS NULL
					AND ($1::text[] IS NULL OR x.cliente_cuit = ANY($1::text[])) AND (`+strings.Join(pconds, " OR ")+`))
			GROUP BY c.cuit_emisor)
		SELECT cuit, coalesce(razon, ''), n, coalesce(to_char(ultimo, 'YYYY-MM-DD'), ''), count(*) OVER () FROM e ORDER BY n DESC, cuit LIMIT `+arg(lim), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h HallazgoProveedor
		if err := rows.Scan(&h.CUIT, &h.RazonSocial, &h.Comprobantes, &h.Ultimo, &res.TotalProveedores); err != nil {
			rows.Close()
			return nil, err
		}
		res.Proveedores = append(res.Proveedores, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// ---- Lotes: por nombre, por número (12, #12, lote 12) o por el CUIT del cliente ----
	args = []any{b.Alcance, "%" + patronLike(q) + "%"}
	lconds := []string{"l.nombre ILIKE $2"}
	if m := reLoteID.FindStringSubmatch(q); m != nil {
		id, _ := strconv.ParseInt(m[1], 10, 64)
		lconds = append(lconds, "l.id = "+arg(id))
	}
	if soloCUIT && len(dig) >= 3 {
		lconds = append(lconds, "l.cliente_cuit LIKE "+arg(patronLike(dig)+"%"))
	}
	rows, err = s.Pool.Query(ctx, `SELECT l.id, l.nombre, l.cliente_cuit, l.origen, l.creado,
			(SELECT count(*) FROM comprobantes c WHERE c.lote_id = l.id AND c.borrado_en IS NULL), count(*) OVER ()
		FROM lotes l WHERE l.borrado_en IS NULL AND ($1::text[] IS NULL OR l.cliente_cuit = ANY($1::text[])) AND (`+strings.Join(lconds, " OR ")+`)
		ORDER BY l.id DESC LIMIT `+arg(lim), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var h HallazgoLote
		if err := rows.Scan(&h.ID, &h.Nombre, &h.ClienteCUIT, &h.Origen, &h.Creado, &h.Comprobantes, &res.TotalLotes); err != nil {
			return nil, err
		}
		res.Lotes = append(res.Lotes, h)
	}
	return res, rows.Err()
}
