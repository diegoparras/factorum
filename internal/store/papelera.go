package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/diegoparras/factorum/internal/arca"
)

// La papelera: borrar un lote o comprobantes sueltos los marca (borrado_en) y los saca de todo lo
// que se lista, se cuenta, se resume o se exporta. Restaurar los deja como estaban (estados,
// correcciones y constatación incluidos). El borrado definitivo es Purgar.

// ErrConflicto: lo pedido no se puede hacer así (la API contesta 409 con el mensaje).
var ErrConflicto = errors.New("conflicto")

// Tipos de lo que hay en la papelera.
const (
	PapeleraLote        = "lote"
	PapeleraComprobante = "comprobante"
)

// ItemPapelera es una fila de la papelera: un lote entero o un comprobante borrado suelto.
type ItemPapelera struct {
	Tipo        string    `json:"tipo"`
	ID          int64     `json:"id"`
	LoteID      int64     `json:"lote_id"`
	LoteNombre  string    `json:"lote_nombre"`
	LoteBorrado bool      `json:"lote_borrado,omitempty"` // un comprobante cuyo lote también está en la papelera
	ClienteCUIT string    `json:"cliente_cuit"`
	BorradoEn   time.Time `json:"borrado_en"`
	BorradoPor  string    `json:"borrado_por"`
	Vence       time.Time `json:"vence"`
	Dias        int       `json:"dias_restantes"`
	// De un lote: cuántos documentos y comprobantes se fueron con él.
	Documentos   int `json:"documentos,omitempty"`
	Comprobantes int `json:"comprobantes,omitempty"`
	// De un comprobante.
	Archivo    string `json:"archivo,omitempty"`
	CbteTipo   int    `json:"cbte_tipo,omitempty"`
	PuntoVenta int    `json:"punto_venta,omitempty"`
	Numero     int64  `json:"numero,omitempty"`
	Fecha      string `json:"fecha,omitempty"`
	Emisor     string `json:"emisor,omitempty"`
	CUITEmisor string `json:"cuit_emisor,omitempty"`
	Total      string `json:"total,omitempty"`
	Estado     string `json:"estado,omitempty"`
}

// RefPapelera señala algo de la papelera.
type RefPapelera struct {
	Tipo string `json:"tipo"`
	ID   int64  `json:"id"`
}

// Borrado es lo que se fue a la papelera (o volvió) en una operación.
type Borrado struct {
	Lotes        int     `json:"lotes,omitempty"`
	Documentos   int     `json:"documentos"`
	Comprobantes int     `json:"comprobantes"`
	Pendientes   []int64 `json:"-"` // documentos sin procesar (a medio procesar) del lote
	Lote         *Lote   `json:"-"`
}

// alcanceSQL arma la condición del alcance para una columna de CUIT de cliente.
func alcanceSQL(col string, n int) string {
	return fmt.Sprintf("($%d::text[] IS NULL OR %s = ANY($%d))", n, col, n)
}

// LoteParaBorrar trae el lote vivo (con el alcance) y lo bloquea hasta que termine la transacción.
func (s *Store) LoteParaBorrar(ctx context.Context, tx pgx.Tx, id int64, alcance []string) (*Lote, error) {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM lotes WHERE id=$1 AND borrado_en IS NULL AND `+alcanceSQL("cliente_cuit", 2)+` FOR UPDATE`, id, alcance); err != nil {
		return nil, err
	}
	return scanLote(tx.QueryRow(ctx, selLote+` WHERE l.id=$1 AND`+loteVivo+` AND `+alcanceSQL("l.cliente_cuit", 2), id, alcance))
}

// BorrarLote manda a la papelera el lote con sus documentos y comprobantes vivos (los que ya
// estaban en la papelera siguen como estaban). Devuelve los documentos que quedaron sin procesar,
// para cancelar sus trabajos de la cola. Va dentro de tx (la de cancelar esos trabajos).
func (s *Store) BorrarLote(ctx context.Context, tx pgx.Tx, id int64, quien string, alcance []string) (*Borrado, error) {
	l, err := s.LoteParaBorrar(ctx, tx, id, alcance)
	if err != nil {
		return nil, err
	}
	b := &Borrado{Lotes: 1, Lote: l}
	if _, err := tx.Exec(ctx, `UPDATE lotes SET borrado_en=now(), borrado_por=$2 WHERE id=$1`, id, quien); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `UPDATE documentos SET borrado_en=now(), borrado_por=$2, borrado_con_lote=true
		WHERE lote_id=$1 AND borrado_en IS NULL RETURNING id, estado`, id, quien)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d int64
		var est string
		if err := rows.Scan(&d, &est); err != nil {
			rows.Close()
			return nil, err
		}
		b.Documentos++
		if est == "pendiente" || est == "procesando" {
			b.Pendientes = append(b.Pendientes, d)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ct, err := tx.Exec(ctx, `UPDATE comprobantes SET borrado_en=now(), borrado_por=$2, borrado_con_lote=true
		WHERE lote_id=$1 AND borrado_en IS NULL`, id, quien)
	if err != nil {
		return nil, err
	}
	b.Comprobantes = int(ct.RowsAffected())
	return b, nil
}

// BorrarComprobantes manda esos comprobantes a la papelera. Si a un documento no le queda ningún
// comprobante vivo, se va con ellos. Todos tienen que existir, estar vivos y en el alcance; si no,
// no se borra ninguno (ErrNoEncontrado con los que faltan).
func (s *Store) BorrarComprobantes(ctx context.Context, ids []int64, quien string, alcance []string) (*Borrado, error) {
	ids = sinRepetir(ids)
	b := &Borrado{}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM comprobantes WHERE id = ANY($1) AND borrado_en IS NULL AND `+alcanceSQL("cliente_cuit", 2)+` ORDER BY id FOR UPDATE`, ids, alcance)
		if err != nil {
			return err
		}
		vivos, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(vivos) != len(ids) {
			var faltan []string
			for _, id := range ids {
				if !slices.Contains(vivos, id) {
					faltan = append(faltan, fmt.Sprint(id))
				}
			}
			return fmt.Errorf("%w: %s", ErrNoEncontrado, strings.Join(faltan, ", "))
		}
		ct, err := tx.Exec(ctx, `UPDATE comprobantes SET borrado_en=now(), borrado_por=$2, borrado_con_lote=false WHERE id = ANY($1)`, ids, quien)
		if err != nil {
			return err
		}
		b.Comprobantes = int(ct.RowsAffected())
		ct, err = tx.Exec(ctx, `UPDATE documentos d SET borrado_en=now(), borrado_por=$2, borrado_con_lote=false
			WHERE d.id IN (SELECT documento_id FROM comprobantes WHERE id = ANY($1)) AND d.borrado_en IS NULL
			  AND NOT EXISTS (SELECT 1 FROM comprobantes c WHERE c.documento_id = d.id AND c.borrado_en IS NULL)`, ids, quien)
		if err != nil {
			return err
		}
		b.Documentos = int(ct.RowsAffected())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return b, nil
}

func sinRepetir(ids []int64) []int64 {
	res := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !slices.Contains(res, id) {
			res = append(res, id)
		}
	}
	return res
}

// Papelera lista lo borrado, de lo más nuevo a lo más viejo. dias: cuántos días queda cada cosa
// antes del borrado definitivo. Un comprobante que se fue con su lote no aparece suelto: vuelve con
// el lote.
func (s *Store) Papelera(ctx context.Context, alcance []string, ahora time.Time, dias, limite, offset int) ([]ItemPapelera, int, error) {
	if limite <= 0 || limite > 500 {
		limite = 100
	}
	const desde = `FROM (
		SELECT 'lote' tipo, l.id, l.id lote_id, l.nombre lote_nombre, false lote_borrado, l.cliente_cuit, l.borrado_en, coalesce(l.borrado_por, '') borrado_por,
			(SELECT count(*) FROM documentos d WHERE d.lote_id = l.id AND d.borrado_con_lote) docs,
			(SELECT count(*) FROM comprobantes c WHERE c.lote_id = l.id AND c.borrado_con_lote) cbtes,
			'' archivo, 0 cbte_tipo, 0 punto_venta, 0::bigint numero, '' fecha, '' emisor, '' cuit_emisor, '' total, '' estado
		FROM lotes l WHERE l.borrado_en IS NOT NULL AND ($1::text[] IS NULL OR l.cliente_cuit = ANY($1))
		UNION ALL
		SELECT 'comprobante', c.id, c.lote_id, l.nombre, l.borrado_en IS NOT NULL, c.cliente_cuit, c.borrado_en, coalesce(c.borrado_por, ''),
			0, 0, d.nombre, coalesce(c.tipo, 0), coalesce(c.punto_venta, 0), coalesce(c.numero, 0), coalesce(to_char(c.fecha, 'YYYY-MM-DD'), ''),
			coalesce(c.razon_social_emisor, ''), coalesce(c.cuit_emisor, ''), coalesce(c.datos->>'total', ''), c.estado
		FROM comprobantes c JOIN documentos d ON d.id = c.documento_id JOIN lotes l ON l.id = c.lote_id
		WHERE c.borrado_en IS NOT NULL AND NOT c.borrado_con_lote AND ($1::text[] IS NULL OR c.cliente_cuit = ANY($1))
	) x`
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) `+desde, alcance).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT * `+desde+` ORDER BY borrado_en DESC, tipo DESC, id DESC LIMIT $2 OFFSET $3`, alcance, limite, max(offset, 0))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := []ItemPapelera{}
	for rows.Next() {
		var it ItemPapelera
		if err := rows.Scan(&it.Tipo, &it.ID, &it.LoteID, &it.LoteNombre, &it.LoteBorrado, &it.ClienteCUIT, &it.BorradoEn, &it.BorradoPor,
			&it.Documentos, &it.Comprobantes, &it.Archivo, &it.CbteTipo, &it.PuntoVenta, &it.Numero, &it.Fecha, &it.Emisor, &it.CUITEmisor, &it.Total, &it.Estado); err != nil {
			return nil, 0, err
		}
		it.Vence, it.Dias = Vencimiento(it.BorradoEn, ahora, dias)
		res = append(res, it)
	}
	return res, total, rows.Err()
}

// Vencimiento dice cuándo se borra para siempre lo que se borró en borrado y cuántos días faltan
// (redondeando para arriba: lo que vence en unas horas dice 1; lo vencido, 0).
func Vencimiento(borrado, ahora time.Time, dias int) (time.Time, int) {
	vence := borrado.Add(time.Duration(dias) * 24 * time.Hour)
	falta := vence.Sub(ahora)
	if falta <= 0 {
		return vence, 0
	}
	return vence, int(math.Ceil(falta.Hours() / 24))
}

// Restaurado es lo que volvió de la papelera.
type Restaurado struct {
	Borrado
	LoteID  int64  `json:"lote_id"`
	Cliente string `json:"-"`
}

// conflictoHash controla que restaurar esos documentos no deje dos documentos vivos con el mismo
// archivo (procesados, o por procesarse): pasa si, mientras estaba en la papelera, se volvió a subir
// el mismo archivo y se procesó como nuevo.
func conflictoHash(ctx context.Context, tx pgx.Tx, docs []int64) error {
	var nombre string
	var lote, id int64
	err := tx.QueryRow(ctx, `SELECT o.id, o.nombre, o.lote_id FROM documentos d JOIN documentos o ON o.sha256 = d.sha256 AND o.id <> d.id
		WHERE d.id = ANY($1) AND d.estado NOT IN ('repetido','error') AND o.borrado_en IS NULL AND o.estado NOT IN ('repetido','error')
		ORDER BY o.id LIMIT 1`, docs).Scan(&id, &nombre, &lote)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: ya hay uno igual cargado después (%s, en el lote %d); borrá ese primero", ErrConflicto, nombre, lote)
}

// Restaurar saca de la papelera un lote (con lo que se fue con él) o un comprobante (con su
// documento, si se había ido con él). Devuelve los documentos que habían quedado sin procesar, para
// volver a encolarlos (RestaurarTx, en la misma transacción).
func (s *Store) Restaurar(ctx context.Context, ref RefPapelera, alcance []string) (*Restaurado, error) {
	var r *Restaurado
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		var err error
		r, err = s.RestaurarTx(ctx, tx, ref, alcance)
		return err
	})
	return r, err
}

// RestaurarTx es Restaurar dentro de tx.
func (s *Store) RestaurarTx(ctx context.Context, tx pgx.Tx, ref RefPapelera, alcance []string) (*Restaurado, error) {
	r := &Restaurado{}
	err := func() error {
		switch ref.Tipo {
		case PapeleraLote:
			if err := tx.QueryRow(ctx, `SELECT id, cliente_cuit FROM lotes WHERE id=$1 AND borrado_en IS NOT NULL AND `+alcanceSQL("cliente_cuit", 2)+` FOR UPDATE`,
				ref.ID, alcance).Scan(&r.LoteID, &r.Cliente); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrNoEncontrado
				}
				return err
			}
			rows, err := tx.Query(ctx, `SELECT id FROM documentos WHERE lote_id=$1 AND borrado_con_lote ORDER BY id FOR UPDATE`, ref.ID)
			if err != nil {
				return err
			}
			docs, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			if err := conflictoHash(ctx, tx, docs); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE lotes SET borrado_en=NULL, borrado_por=NULL WHERE id=$1`, ref.ID); err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `UPDATE documentos SET borrado_en=NULL, borrado_por=NULL, borrado_con_lote=false WHERE id = ANY($1) RETURNING id, estado`, docs)
			if err != nil {
				return err
			}
			for rows.Next() {
				var d int64
				var est string
				if err := rows.Scan(&d, &est); err != nil {
					rows.Close()
					return err
				}
				r.Documentos++
				if est == "pendiente" || est == "procesando" {
					r.Pendientes = append(r.Pendientes, d)
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			rows, err = tx.Query(ctx, `UPDATE comprobantes SET borrado_en=NULL, borrado_por=NULL, borrado_con_lote=false WHERE lote_id=$1 AND borrado_con_lote RETURNING id`, ref.ID)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			r.Lotes, r.Comprobantes = 1, len(ids)
			// Lo que vuelve y es de un período ya presentado va al primer período abierto.
			return reasignarPeriodos(ctx, tx, ids)
		case PapeleraComprobante:
			var doc int64
			var loteBorrado, docBorrado bool
			var loteNombre string
			err := tx.QueryRow(ctx, `SELECT c.lote_id, c.documento_id, c.cliente_cuit, l.borrado_en IS NOT NULL, l.nombre, d.borrado_en IS NOT NULL
				FROM comprobantes c JOIN lotes l ON l.id = c.lote_id JOIN documentos d ON d.id = c.documento_id
				WHERE c.id=$1 AND c.borrado_en IS NOT NULL AND NOT c.borrado_con_lote AND `+alcanceSQL("c.cliente_cuit", 2)+` FOR UPDATE OF c`,
				ref.ID, alcance).Scan(&r.LoteID, &doc, &r.Cliente, &loteBorrado, &loteNombre, &docBorrado)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoEncontrado
			}
			if err != nil {
				return err
			}
			if loteBorrado {
				if loteNombre == "" {
					loteNombre = fmt.Sprintf("lote %d", r.LoteID)
				}
				return fmt.Errorf("%w: su lote («%s») también está en la papelera; restaurá primero el lote", ErrConflicto, loteNombre)
			}
			if docBorrado {
				if err := conflictoHash(ctx, tx, []int64{doc}); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE documentos SET borrado_en=NULL, borrado_por=NULL, borrado_con_lote=false WHERE id=$1`, doc); err != nil {
					return err
				}
				r.Documentos = 1
			}
			if _, err := tx.Exec(ctx, `UPDATE comprobantes SET borrado_en=NULL, borrado_por=NULL WHERE id=$1`, ref.ID); err != nil {
				return err
			}
			r.Comprobantes = 1
			return reasignarPeriodos(ctx, tx, []int64{ref.ID})
		}
		return fmt.Errorf("%w: tipo %q (puede ser lote o comprobante)", ErrFiltro, ref.Tipo)
	}()
	if err != nil {
		return nil, err
	}
	return r, nil
}

// ---------- Borrado definitivo ----------

// FiltroPurga dice qué se borra de verdad. Antes: lo que se borró antes de ese momento (lo vencido);
// Items: esos (Borrar ya); Todo: toda la papelera (Vaciar). Alcance limita Items y Todo.
type FiltroPurga struct {
	Antes   time.Time
	Items   []RefPapelera
	Todo    bool
	Alcance []string
}

// Purga cuenta lo borrado de verdad.
type Purga struct {
	Lotes        int `json:"lotes"`
	Documentos   int `json:"documentos"`
	Comprobantes int `json:"comprobantes"`
	Archivos     int `json:"archivos"`
}

// Vacia dice si no se borró nada.
func (p Purga) Vacia() bool { return p.Lotes+p.Documentos+p.Comprobantes+p.Archivos == 0 }

// Purgar borra de verdad (filas) lo que diga el filtro: los lotes con todo lo suyo, los
// comprobantes sueltos (con sus correcciones), los documentos que quedaron sin ningún comprobante y
// los lotes que se quedaron sin ningún documento. Los archivos de esos documentos quedan anotados en
// almacen_por_borrar, en la misma transacción, para LimpiarAlmacen.
func (s *Store) Purgar(ctx context.Context, f FiltroPurga) (Purga, error) {
	var p Purga
	var lotesIDs, cbtesIDs []int64
	for _, it := range f.Items {
		switch it.Tipo {
		case PapeleraLote:
			lotesIDs = append(lotesIDs, it.ID)
		case PapeleraComprobante:
			cbtesIDs = append(cbtesIDs, it.ID)
		default:
			return p, fmt.Errorf("%w: tipo %q (puede ser lote o comprobante)", ErrFiltro, it.Tipo)
		}
	}
	explicito := !f.Todo && f.Antes.IsZero()
	if explicito && len(f.Items) == 0 {
		return p, nil
	}
	var antes any
	if !f.Antes.IsZero() {
		antes = f.Antes
	}
	alcance := f.Alcance
	if !f.Antes.IsZero() {
		alcance = nil // el automático es de toda la instalación
	}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		// 1. Lotes enteros.
		rows, err := tx.Query(ctx, `SELECT id FROM lotes WHERE borrado_en IS NOT NULL AND `+alcanceSQL("cliente_cuit", 1)+`
			AND ($2::timestamptz IS NULL OR borrado_en < $2) AND ($3::bigint[] IS NULL OR id = ANY($3))
			ORDER BY id FOR UPDATE`, alcance, antes, nulos(lotesIDs, explicito))
		if err != nil {
			return err
		}
		lotes, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(lotes) > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO almacen_por_borrar (sha256) SELECT DISTINCT sha256 FROM documentos WHERE lote_id = ANY($1) ON CONFLICT DO NOTHING`, lotes); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM documentos WHERE lote_id = ANY($1)), (SELECT count(*) FROM comprobantes WHERE lote_id = ANY($1))`, lotes).
				Scan(&p.Documentos, &p.Comprobantes); err != nil {
				return err
			}
			// Los repetidos de otros lotes que apuntaban a estos documentos quedan sin referencia.
			if _, err := tx.Exec(ctx, `DELETE FROM lotes WHERE id = ANY($1)`, lotes); err != nil {
				return err
			}
			p.Lotes = len(lotes)
		}
		// 2. Comprobantes sueltos (las correcciones se van con ellos).
		rows, err = tx.Query(ctx, `DELETE FROM comprobantes WHERE borrado_en IS NOT NULL AND NOT borrado_con_lote AND `+alcanceSQL("cliente_cuit", 1)+`
			AND ($2::timestamptz IS NULL OR borrado_en < $2) AND ($3::bigint[] IS NULL OR id = ANY($3))
			RETURNING documento_id`, alcance, antes, nulos(cbtesIDs, explicito))
		if err != nil {
			return err
		}
		docsDeCbtes, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		p.Comprobantes += len(docsDeCbtes)
		// 3. Documentos en la papelera que se quedaron sin ningún comprobante. Con Borrar ya, solo
		// los de esos comprobantes.
		rows, err = tx.Query(ctx, `DELETE FROM documentos d USING lotes l WHERE l.id = d.lote_id AND d.borrado_en IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM comprobantes c WHERE c.documento_id = d.id)
			AND `+alcanceSQL("l.cliente_cuit", 1)+` AND ($2::timestamptz IS NULL OR d.borrado_en < $2) AND (NOT $4 OR d.id = ANY($3))
			RETURNING d.sha256, d.lote_id`, alcance, antes, docsDeCbtes, explicito)
		if err != nil {
			return err
		}
		var shas []string
		var lotesTocados []int64
		for rows.Next() {
			var sha string
			var l int64
			if err := rows.Scan(&sha, &l); err != nil {
				rows.Close()
				return err
			}
			shas = append(shas, sha)
			lotesTocados = append(lotesTocados, l)
			p.Documentos++
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(shas) > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO almacen_por_borrar (sha256) SELECT DISTINCT x FROM unnest($1::text[]) x ON CONFLICT DO NOTHING`, shas); err != nil {
				return err
			}
		}
		// 4. Lotes que se quedaron sin ningún documento (les borraron todo de a uno).
		if len(lotesTocados) > 0 {
			ct, err := tx.Exec(ctx, `DELETE FROM lotes l WHERE l.id = ANY($1) AND NOT EXISTS (SELECT 1 FROM documentos d WHERE d.lote_id = l.id)`, sinRepetir(lotesTocados))
			if err != nil {
				return err
			}
			p.Lotes += int(ct.RowsAffected())
		}
		return nil
	})
	return p, err
}

// nulos: con un filtro explícito, la lista (vacía = ninguno); si no, NULL (= cualquiera).
func nulos(ids []int64, explicito bool) any {
	if !explicito {
		return nil
	}
	if ids == nil {
		return []int64{}
	}
	return ids
}

// PorBorrarDelAlmacen lista los archivos anotados para sacar del almacén.
func (s *Store) PorBorrarDelAlmacen(ctx context.Context, limite int) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `SELECT sha256 FROM almacen_por_borrar ORDER BY creado, sha256 LIMIT $1`, limite)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// CandadoArchivo es el par de claves del candado de Postgres (pg_advisory_lock) de un archivo del
// almacén: la carga lo toma compartido desde antes de guardar el archivo hasta que su documento
// queda en la base; el borrado definitivo, exclusivo, para mirar que nadie lo use y borrarlo.
const CandadoArchivo = 84131

// BorrarArchivoSiNadieLoUsa saca un archivo de almacen_por_borrar y, si no hay ningún documento con
// ese SHA-256 (vivo o en la papelera), llama a borrar con él antes de confirmar. Con el candado del
// archivo tomado, una carga del mismo archivo espera a que termine (y lo vuelve a guardar).
func (s *Store) BorrarArchivoSiNadieLoUsa(ctx context.Context, sha string, borrar func(context.Context) error) (bool, error) {
	borrado := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1, hashtext($2))`, CandadoArchivo, sha); err != nil {
			return err
		}
		var usado bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM documentos WHERE sha256=$1)`, sha).Scan(&usado); err != nil {
			return err
		}
		if !usado {
			if err := borrar(ctx); err != nil {
				return err
			}
			borrado = true
			// El pase a R2 ya no tiene que mirar estos objetos.
			if _, err := tx.Exec(ctx, `DELETE FROM r2_objetos WHERE clave = ANY($1)`, ClavesArchivo(sha)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `DELETE FROM almacen_por_borrar WHERE sha256=$1`, sha)
		return err
	})
	return borrado, err
}

// ClavesArchivo son las claves del almacén de un documento: el original y el resultado.
func ClavesArchivo(sha string) []string {
	return []string{"originales/" + sha, "resultados/" + sha + ".json"}
}

// PaseR2Trabajando dice si el pase a R2 o la liberación del disco están corriendo (mientras tanto,
// el borrado definitivo no toca el almacén).
func (s *Store) PaseR2Trabajando(ctx context.Context) (bool, error) {
	var est string
	err := s.Pool.QueryRow(ctx, `SELECT estado FROM r2_migracion WHERE id=1`).Scan(&est)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return est == "corriendo" || est == "liberando", err
}

// ---------- Cambiar el cliente de un lote ----------

// CambioCliente es lo que hizo CambiarClienteLote.
type CambioCliente struct {
	Antes        string `json:"antes"`
	Despues      string `json:"despues"`
	Comprobantes int    `json:"comprobantes"`
	Lote         *Lote  `json:"-"`
}

// CambiarClienteLote pasa el lote y todos sus comprobantes (también los de la papelera) a otro
// cliente. El lote tiene que estar vivo y en el alcance de quien pide.
func (s *Store) CambiarClienteLote(ctx context.Context, id int64, nuevo string, alcance []string) (*CambioCliente, error) {
	nuevo = arca.SoloDigitos(nuevo)
	c := &CambioCliente{Despues: nuevo}
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		l, err := s.LoteParaBorrar(ctx, tx, id, alcance)
		if err != nil {
			return err
		}
		c.Antes, c.Lote = l.ClienteCUIT, l
		if l.ClienteCUIT == nuevo {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE lotes SET cliente_cuit=$2, actualizado=now() WHERE id=$1`, id, nuevo); err != nil {
			return err
		}
		ct, err := tx.Exec(ctx, `UPDATE comprobantes SET cliente_cuit=$2, actualizado=now() WHERE lote_id=$1`, id, nuevo)
		if err != nil {
			return err
		}
		c.Comprobantes = int(ct.RowsAffected())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ConstatadosDeLote trae los comprobantes vivos del lote que ya tienen constatación.
func (s *Store) ConstatadosDeLote(ctx context.Context, lote int64) ([]Cbte, error) {
	rows, err := s.Pool.Query(ctx, selCbte+` WHERE c.lote_id=$1 AND c.borrado_en IS NULL AND c.constatacion IS NOT NULL ORDER BY c.id`, lote)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Cbte
	for rows.Next() {
		c, err := scanCbte(rows)
		if err != nil {
			return nil, err
		}
		res = append(res, *c)
	}
	return res, rows.Err()
}
