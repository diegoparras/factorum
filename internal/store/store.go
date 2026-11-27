// Package store es la persistencia en Postgres: lotes, documentos, comprobantes y el registro de
// correcciones (el "papel de trabajo"). El comprobante completo va en JSON; las columnas que se
// filtran o se suman van aparte e indexadas.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/rubro"
)

//go:embed migraciones/*.sql
var migraciones embed.FS

type Store struct{ Pool *pgxpool.Pool }

var ErrNoEncontrado = errors.New("no encontrado")

// ErrFiltro: un filtro con un valor que no se entiende (la API contesta 400).
var ErrFiltro = errors.New("filtro inválido")

// Abrir conecta y corre las migraciones.
func Abrir(ctx context.Context, url string, maxConns int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("FACTORUM_DATABASE_URL inválida: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var ultimo error
	for i := 0; i < 30; i++ { // la base puede tardar en levantar (docker compose)
		if ultimo = pool.Ping(ctx); ultimo == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if ultimo != nil {
		return nil, fmt.Errorf("no se pudo conectar a Postgres: %w", ultimo)
	}
	s := &Store{Pool: pool}
	return s, s.migrar(ctx)
}

func (s *Store) Cerrar() { s.Pool.Close() }

// Migraciones son los nombres de las migraciones que trae esta versión, en orden.
func Migraciones() []string {
	ents, _ := migraciones.ReadDir("migraciones")
	res := make([]string, 0, len(ents))
	for _, e := range ents {
		res = append(res, e.Name())
	}
	return res
}

func (s *Store) migrar(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS factorum_migraciones (nombre text PRIMARY KEY, aplicada timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	ents, err := migraciones.ReadDir("migraciones")
	if err != nil {
		return err
	}
	for _, e := range ents {
		var ya bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM factorum_migraciones WHERE nombre=$1)`, e.Name()).Scan(&ya); err != nil {
			return err
		}
		if ya {
			continue
		}
		sqlb, _ := migraciones.ReadFile("migraciones/" + e.Name())
		err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
			// Un candado para que dos réplicas no migren a la vez.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(84112)`); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, string(sqlb)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO factorum_migraciones (nombre) VALUES ($1) ON CONFLICT DO NOTHING`, e.Name())
			return err
		})
		if err != nil {
			return fmt.Errorf("migración %s: %w", e.Name(), err)
		}
	}
	return nil
}

// ---------- Tipos ----------

type Lote struct {
	ID           int64          `json:"id"`
	Nombre       string         `json:"nombre"`
	ClienteCUIT  string         `json:"cliente_cuit"`
	Origen       string         `json:"origen"`
	Canal        string         `json:"canal,omitempty"`
	Chat         string         `json:"-"`
	IA           bool           `json:"ia"`
	Constatar    bool           `json:"constatar"`
	Creado       time.Time      `json:"creado"`
	Actualizado  time.Time      `json:"actualizado"`
	Terminado    *time.Time     `json:"terminado"`
	Estado       string         `json:"estado"`
	Documentos   int            `json:"documentos"`
	Procesados   int            `json:"procesados"`
	ConError     int            `json:"con_error"`
	Comprobantes map[string]int `json:"comprobantes"`
	CreadoPor    *int64         `json:"-"`
	// SinConstatar: el lote se constata y le falta algún comprobante; Constatado: alguno ya se
	// constató (con el CUIT del cliente como consultante). Los usa «Cambiar cliente».
	SinConstatar bool `json:"constatacion_pendiente"`
	Constatado   bool `json:"constatado"`
}

type Documento struct {
	ID         int64     `json:"id"`
	LoteID     int64     `json:"lote_id"`
	Nombre     string    `json:"nombre"`
	Mime       string    `json:"mime"`
	Tamano     int64     `json:"tamano"`
	SHA256     string    `json:"sha256"`
	Estado     string    `json:"estado"`
	Error      string    `json:"error,omitempty"`
	RepetidoDe *int64    `json:"repetido_de,omitempty"`
	Creado     time.Time `json:"creado"`
	// ClienteCUIT es el del lote: decide quién puede ver el archivo.
	ClienteCUIT string `json:"cliente_cuit"`
}

// Cbte es un comprobante guardado: los datos leídos más su estado en Factorum.
type Cbte struct {
	ID           int64              `json:"id"`
	LoteID       int64              `json:"lote_id"`
	DocumentoID  int64              `json:"documento_id"`
	ClienteCUIT  string             `json:"cliente_cuit"`
	Archivo      string             `json:"archivo"`
	Datos        modelo.Comprobante `json:"-"`
	Hallazgos    []modelo.Hallazgo  `json:"hallazgos"`
	Estado       modelo.Estado      `json:"estado"`
	Confianza    float64            `json:"confianza"`
	Constatacion json.RawMessage    `json:"constatacion"`
	DuplicadoDe  *int64             `json:"duplicado_de"`
	Creado       time.Time          `json:"creado"`
	RevisadoPor  *string            `json:"revisado_por"`
	RevisadoEn   *time.Time         `json:"revisado_en"`
}

// MarshalJSON aplana los datos del comprobante junto con los de Factorum.
func (c Cbte) MarshalJSON() ([]byte, error) {
	type alias Cbte
	a, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}
	d, err := json.Marshal(c.Datos)
	if err != nil {
		return nil, err
	}
	// {datos..., propios...}: los propios pisan (no hay choque de nombres salvo "origenes", que es de datos).
	return append(append(d[:len(d)-1], ','), a[1:]...), nil
}

type Correccion struct {
	Campo   string    `json:"campo"`
	Antes   string    `json:"antes"`
	Despues string    `json:"despues"`
	Usuario string    `json:"usuario"`
	Creado  time.Time `json:"creado"`
}

// Filtro de comprobantes (lo usan la API, MCP y la exportación).
type Filtro struct {
	Lote    int64
	Estado  string
	Cliente string
	Emisor  string
	Desde   string
	Hasta   string
	Q       string
	Clase   string // "", "facturas" (todo lo que no es tique) o "tickets" (tiques de controlador fiscal)
	Rubro   string // supermercado, combustible...; "sin_rubro" trae los que no tienen
	// Alcance son los CUITs de cliente que puede ver quien pide: nil = todos. Lo pone la API según
	// la identidad, nunca el pedido.
	Alcance []string
	Limite  int
	Offset  int
}

// Clases válidas del filtro.
const (
	ClaseFacturas = "facturas"
	ClaseTickets  = "tickets"
)

// ClaseValida dice si el valor del filtro clase se entiende.
func ClaseValida(c string) bool { return c == "" || c == ClaseFacturas || c == ClaseTickets }

func codigosTique() []int32 {
	var res []int32
	for _, c := range arca.CodigosTique() {
		res = append(res, int32(c))
	}
	return res
}

// ---------- Lotes ----------

type LoteNuevo struct {
	Nombre, ClienteCUIT, Origen, Canal, Chat string
	IA, Constatar                            bool
	CreadoPor                                int64 // la persona que lo cargó desde la pantalla (0 = nadie: chat, correo, API)
}

func (s *Store) CrearLote(ctx context.Context, tx pgx.Tx, n LoteNuevo) (int64, error) {
	var id int64
	var por any
	if n.CreadoPor > 0 {
		por = n.CreadoPor
	}
	err := tx.QueryRow(ctx, `INSERT INTO lotes (nombre, cliente_cuit, origen, canal, chat, ia, constatar, creado_por)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, n.Nombre, n.ClienteCUIT, n.Origen, n.Canal, n.Chat, n.IA, n.Constatar, por).Scan(&id)
	return id, err
}

// LoteAbiertoDeChat devuelve el lote de un chat con actividad en los últimos minutos (para juntar
// los archivos que llegan de a uno por WhatsApp) y lo reabre.
func (s *Store) LoteAbiertoDeChat(ctx context.Context, tx pgx.Tx, canal, chat string, ventana time.Duration) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `UPDATE lotes SET terminado=NULL, notificado=false, actualizado=now()
		WHERE id = (SELECT id FROM lotes WHERE canal=$1 AND chat=$2 AND borrado_en IS NULL AND actualizado > now() - $3::interval ORDER BY id DESC LIMIT 1)
		RETURNING id`, canal, chat, fmt.Sprintf("%d seconds", int(ventana.Seconds()))).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

const selLote = `SELECT l.id, l.nombre, l.cliente_cuit, l.origen, coalesce(l.canal,''), coalesce(l.chat,''), l.ia, l.constatar, l.creado, l.actualizado, l.terminado, l.creado_por,
	(SELECT count(*) FROM documentos d WHERE d.lote_id=l.id AND d.borrado_en IS NULL),
	(SELECT count(*) FROM documentos d WHERE d.lote_id=l.id AND d.borrado_en IS NULL AND d.estado NOT IN ('pendiente','procesando')),
	(SELECT count(*) FROM documentos d WHERE d.lote_id=l.id AND d.borrado_en IS NULL AND d.estado='error'),
	(SELECT coalesce(json_object_agg(estado, n), '{}') FROM (SELECT estado, count(*) n FROM comprobantes c WHERE c.lote_id=l.id AND c.borrado_en IS NULL GROUP BY estado) x),
	l.constatar AND EXISTS (SELECT 1 FROM comprobantes c WHERE c.lote_id=l.id AND c.borrado_en IS NULL AND c.constatacion IS NULL),
	EXISTS (SELECT 1 FROM comprobantes c WHERE c.lote_id=l.id AND c.borrado_en IS NULL AND c.constatacion IS NOT NULL)
	FROM lotes l`

// vivo: lo que no está en la papelera.
const loteVivo = ` l.borrado_en IS NULL`

func scanLote(r pgx.Row) (*Lote, error) {
	var l Lote
	var cb []byte
	if err := r.Scan(&l.ID, &l.Nombre, &l.ClienteCUIT, &l.Origen, &l.Canal, &l.Chat, &l.IA, &l.Constatar, &l.Creado, &l.Actualizado, &l.Terminado, &l.CreadoPor,
		&l.Documentos, &l.Procesados, &l.ConError, &cb, &l.SinConstatar, &l.Constatado); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoEncontrado
		}
		return nil, err
	}
	l.Comprobantes = map[string]int{"ok": 0, "revisar": 0, "error": 0, "aprobado": 0}
	_ = json.Unmarshal(cb, &l.Comprobantes)
	l.Estado = "procesando"
	if l.Terminado != nil {
		l.Estado = "terminado"
	}
	return &l, nil
}

func (s *Store) Lote(ctx context.Context, id int64) (*Lote, error) {
	return scanLote(s.Pool.QueryRow(ctx, selLote+` WHERE l.id=$1 AND`+loteVivo, id))
}

// Lotes lista los lotes, del más nuevo al más viejo. alcance: CUITs de cliente visibles (nil = todos).
func (s *Store) Lotes(ctx context.Context, limite, offset int, alcance []string) ([]Lote, int, error) {
	if limite <= 0 || limite > 500 {
		limite = 50
	}
	rows, err := s.Pool.Query(ctx, selLote+` WHERE`+loteVivo+` AND ($3::text[] IS NULL OR l.cliente_cuit = ANY($3)) ORDER BY l.id DESC LIMIT $1 OFFSET $2`, limite, offset, alcance)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := []Lote{}
	for rows.Next() {
		l, err := scanLote(rows)
		if err != nil {
			return nil, 0, err
		}
		res = append(res, *l)
	}
	var total int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM lotes l WHERE`+loteVivo+` AND ($1::text[] IS NULL OR l.cliente_cuit = ANY($1))`, alcance).Scan(&total)
	return res, total, rows.Err()
}

// UltimoLoteDeChat para el comando "estado" de los canales.
func (s *Store) UltimoLoteDeChat(ctx context.Context, canal, chat string) (*Lote, error) {
	return scanLote(s.Pool.QueryRow(ctx, selLote+` WHERE l.canal=$1 AND l.chat=$2 AND`+loteVivo+` ORDER BY l.id DESC LIMIT 1`, canal, chat))
}

// CerrarLoteSiTermino marca el lote como terminado si no le quedan documentos pendientes.
// Devuelve true solo a quien lo cerró (si dos workers terminan a la vez, uno solo gana).
func (s *Store) CerrarLoteSiTermino(ctx context.Context, id int64) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE lotes SET terminado=now() WHERE id=$1 AND terminado IS NULL AND borrado_en IS NULL
		AND NOT EXISTS (SELECT 1 FROM documentos WHERE lote_id=$1 AND estado IN ('pendiente','procesando'))`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// MarcarNotificado devuelve true si este llamado fue el que lo marcó.
func (s *Store) MarcarNotificado(ctx context.Context, id int64) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE lotes SET notificado=true WHERE id=$1 AND NOT notificado AND terminado IS NOT NULL
		AND actualizado < now() - interval '10 seconds'`, id)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() == 1, nil
}

// ---------- Documentos ----------

// CrearDocumento registra un archivo. Si el mismo contenido ya se procesó bien, queda como repetido.
// Lo que está en la papelera no cuenta: el mismo archivo se vuelve a procesar como nuevo.
func (s *Store) CrearDocumento(ctx context.Context, tx pgx.Tx, lote int64, nombre, mime string, tamano int64, sha string) (id int64, repetido bool, err error) {
	var previo *int64
	_ = tx.QueryRow(ctx, `SELECT id FROM documentos WHERE sha256=$1 AND estado='listo' AND borrado_en IS NULL ORDER BY id LIMIT 1`, sha).Scan(&previo)
	estado := "pendiente"
	if previo != nil {
		estado = "repetido"
	}
	err = tx.QueryRow(ctx, `INSERT INTO documentos (lote_id, nombre, mime, tamano, sha256, estado, repetido_de) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		lote, nombre, mime, tamano, sha, estado, previo).Scan(&id)
	return id, previo != nil, err
}

func (s *Store) Documento(ctx context.Context, id int64) (*Documento, error) {
	var d Documento
	var e *string
	err := s.Pool.QueryRow(ctx, `SELECT d.id, d.lote_id, d.nombre, d.mime, d.tamano, d.sha256, d.estado, d.error, d.repetido_de, d.creado, l.cliente_cuit
		FROM documentos d JOIN lotes l ON l.id = d.lote_id WHERE d.id=$1 AND d.borrado_en IS NULL`, id).
		Scan(&d.ID, &d.LoteID, &d.Nombre, &d.Mime, &d.Tamano, &d.SHA256, &d.Estado, &e, &d.RepetidoDe, &d.Creado, &d.ClienteCUIT)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoEncontrado
	}
	if e != nil {
		d.Error = *e
	}
	return &d, err
}

// TomarDocumento lo pasa a "procesando". Devuelve false si ya no estaba pendiente (reintento de un
// trabajo que ya había terminado) o si se fue a la papelera.
func (s *Store) TomarDocumento(ctx context.Context, id int64) (bool, error) {
	ct, err := s.Pool.Exec(ctx, `UPDATE documentos SET estado='procesando' WHERE id=$1 AND estado IN ('pendiente','procesando') AND borrado_en IS NULL`, id)
	return ct.RowsAffected() == 1, err
}

func (s *Store) ErrorDocumento(ctx context.Context, id int64, msg string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE documentos SET estado='error', error=$2, procesado=now() WHERE id=$1 AND borrado_en IS NULL`, id, msg)
	return err
}

// GuardarResultado reemplaza los comprobantes del documento (es idempotente ante reintentos) y
// detecta duplicados: mismo emisor, tipo, punto de venta y número para el mismo cliente (sin contar
// lo que está en la papelera). El cliente sale del lote en ese momento (se puede haber cambiado
// mientras se procesaba). Si el documento se fue a la papelera mientras se leía, no guarda nada y
// devuelve ErrNoEncontrado: queda como estaba, para procesarlo de nuevo si se restaura.
func (s *Store) GuardarResultado(ctx context.Context, doc *Documento, r *modelo.Resultado) ([]int64, error) {
	var ids []int64
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		// Primero el lote y después el documento, el mismo orden que borrar y cambiar el cliente. El
		// candado del lote es el mismo que toma el UPDATE del final (dos documentos del mismo lote se
		// guardan de a uno: así la detección de duplicados entre ellos no depende de la suerte).
		var cliente string
		if err := tx.QueryRow(ctx, `SELECT cliente_cuit FROM lotes WHERE id=$1 AND borrado_en IS NULL FOR NO KEY UPDATE`, doc.LoteID).Scan(&cliente); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoEncontrado
			}
			return err
		}
		var vivo bool
		if err := tx.QueryRow(ctx, `SELECT borrado_en IS NULL FROM documentos WHERE id=$1 FOR UPDATE`, doc.ID).Scan(&vivo); err != nil || !vivo {
			if err == nil || errors.Is(err, pgx.ErrNoRows) {
				return ErrNoEncontrado
			}
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM comprobantes WHERE documento_id=$1`, doc.ID); err != nil {
			return err
		}
		det, _ := json.Marshal(r.Detalle)
		estado, msg := "listo", (*string)(nil)
		if r.Error != "" {
			estado, msg = "error", &r.Error
		}
		if _, err := tx.Exec(ctx, `UPDATE documentos SET estado=$2, error=$3, paginas=$4, extractores=$5, duracion_ms=$6, detalle=$7, procesado=now() WHERE id=$1`,
			doc.ID, estado, msg, r.Paginas, r.Extractores, r.DuracionMS, det); err != nil {
			return err
		}
		for i := range r.Comprobantes {
			rc := &r.Comprobantes[i]
			c := &rc.Comprobante
			var dup *int64
			if c.Clave() != "" {
				_ = tx.QueryRow(ctx, `SELECT id FROM comprobantes WHERE cliente_cuit=$1 AND cuit_emisor=$2 AND tipo=$3 AND punto_venta=$4 AND numero=$5 AND documento_id<>$6 AND borrado_en IS NULL ORDER BY id LIMIT 1`,
					cliente, c.CUITEmisor, c.Tipo, c.PuntoVenta, c.Numero, doc.ID).Scan(&dup)
			}
			if dup != nil {
				rc.Hallazgos = append(rc.Hallazgos, modelo.Hallazgo{Codigo: "duplicado", Nivel: modelo.NivelAviso,
					Mensaje: fmt.Sprintf("Este comprobante ya se cargó antes (comprobante %d).", *dup)})
				if rc.Estado == modelo.EstadoOK {
					rc.Estado = modelo.EstadoRevisar
				}
			}
			id, err := insertarCbte(ctx, tx, doc.LoteID, doc.ID, cliente, i, rc, dup)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		_, err := tx.Exec(ctx, `UPDATE lotes SET actualizado=now() WHERE id=$1`, doc.LoteID)
		return err
	})
	return ids, err
}

func fechaONull(s string) any {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return nil
	}
	return s
}

func insertarCbte(ctx context.Context, tx pgx.Tx, lote, doc int64, cliente string, orden int, rc *modelo.ResultadoCbte, dup *int64) (int64, error) {
	c := &rc.Comprobante
	datos, _ := json.Marshal(c)
	hall, _ := json.Marshal(rc.Hallazgos)
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO comprobantes (documento_id, lote_id, cliente_cuit, orden, tipo, punto_venta, numero, fecha, cuit_emisor,
		razon_social_emisor, total, datos, hallazgos, estado, confianza, duplicado_de)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
		doc, lote, cliente, orden, c.Tipo, c.PuntoVenta, c.Numero, fechaONull(c.Fecha), c.CUITEmisor, c.RazonSocialEmisor,
		int64(c.Total), datos, hall, string(rc.Estado), rc.Confianza, dup).Scan(&id)
	return id, err
}

// ---------- Comprobantes ----------

const selCbte = `SELECT c.id, c.lote_id, c.documento_id, c.cliente_cuit, d.nombre, c.datos, c.hallazgos, c.estado, c.confianza,
	c.constatacion, c.duplicado_de, c.creado, c.revisado_por, c.revisado_en
	FROM comprobantes c JOIN documentos d ON d.id=c.documento_id`

func scanCbte(r pgx.Row) (*Cbte, error) {
	var c Cbte
	var datos, hall []byte
	var est string
	if err := r.Scan(&c.ID, &c.LoteID, &c.DocumentoID, &c.ClienteCUIT, &c.Archivo, &datos, &hall, &est, &c.Confianza,
		&c.Constatacion, &c.DuplicadoDe, &c.Creado, &c.RevisadoPor, &c.RevisadoEn); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoEncontrado
		}
		return nil, err
	}
	c.Estado = modelo.Estado(est)
	if err := json.Unmarshal(datos, &c.Datos); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(hall, &c.Hallazgos)
	if c.Hallazgos == nil {
		c.Hallazgos = []modelo.Hallazgo{}
	}
	if len(c.Constatacion) == 0 {
		c.Constatacion = json.RawMessage("null")
	}
	return &c, nil
}

func (f Filtro) where() (string, []any) {
	w := []string{"c.borrado_en IS NULL"} // lo de la papelera no se ve en ningún listado
	var a []any
	add := func(cond string, v any) {
		a = append(a, v)
		w = append(w, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(a))))
	}
	if f.Alcance != nil {
		add("c.cliente_cuit = ANY(?::text[])", f.Alcance)
	}
	if f.Lote > 0 {
		add("c.lote_id = ?", f.Lote)
	}
	if f.Estado != "" {
		add("c.estado = ?", f.Estado)
	}
	if f.Cliente != "" {
		add("c.cliente_cuit = ?", f.Cliente)
	}
	if f.Emisor != "" {
		add("c.cuit_emisor = ?", f.Emisor)
	}
	if f.Desde != "" {
		add("c.fecha >= ?::date", f.Desde)
	}
	if f.Hasta != "" {
		add("c.fecha <= ?::date", f.Hasta)
	}
	switch f.Clase {
	case ClaseTickets:
		add("c.tipo = ANY(?::int[])", codigosTique())
	case ClaseFacturas:
		add("(c.tipo IS NULL OR NOT c.tipo = ANY(?::int[]))", codigosTique())
	}
	switch f.Rubro {
	case "":
	case "sin_rubro":
		w = append(w, "coalesce(c.datos->>'rubro', '') = ''")
	default:
		add("c.datos->>'rubro' = ?", f.Rubro)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		a = append(a, q)
		n := "$" + strconv.Itoa(len(a))
		w = append(w, "(c.razon_social_emisor ILIKE '%' || "+n+" || '%' OR c.cuit_emisor LIKE '%' || "+n+" || '%' OR c.numero::text = "+n+" OR d.nombre ILIKE '%' || "+n+" || '%')")
	}
	return " WHERE " + strings.Join(w, " AND "), a
}

func (s *Store) Comprobantes(ctx context.Context, f Filtro) ([]Cbte, int, error) {
	if f.Limite <= 0 {
		f.Limite = 100
	}
	if f.Limite > 5000 {
		f.Limite = 5000
	}
	if !ClaseValida(f.Clase) {
		return nil, 0, fmt.Errorf("%w: clase %q (puede ser facturas o tickets)", ErrFiltro, f.Clase)
	}
	if f.Rubro != "" && f.Rubro != "sin_rubro" && !rubro.Valido(f.Rubro) {
		return nil, 0, fmt.Errorf("%w: rubro %q (puede ser %s o sin_rubro)", ErrFiltro, f.Rubro, strings.Join(rubro.Todos, ", "))
	}
	w, a := f.where()
	var total int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM comprobantes c JOIN documentos d ON d.id=c.documento_id`+w, a...).Scan(&total); err != nil {
		return nil, 0, err
	}
	a = append(a, f.Limite, f.Offset)
	rows, err := s.Pool.Query(ctx, selCbte+w+fmt.Sprintf(` ORDER BY c.fecha DESC NULLS LAST, c.id DESC LIMIT $%d OFFSET $%d`, len(a)-1, len(a)), a...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	res := []Cbte{}
	for rows.Next() {
		c, err := scanCbte(rows)
		if err != nil {
			return nil, 0, err
		}
		res = append(res, *c)
	}
	return res, total, rows.Err()
}

func (s *Store) Comprobante(ctx context.Context, id int64) (*Cbte, error) {
	return scanCbte(s.Pool.QueryRow(ctx, selCbte+` WHERE c.id=$1 AND c.borrado_en IS NULL`, id))
}

func (s *Store) ComprobantesDeDocumento(ctx context.Context, doc int64) ([]Cbte, error) {
	rows, err := s.Pool.Query(ctx, selCbte+` WHERE c.documento_id=$1 AND c.borrado_en IS NULL ORDER BY c.orden`, doc)
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

// SinConstatar devuelve los comprobantes de un lote que todavía no se constataron (sin los de la
// papelera). solo, si no es nil, limita a esos comprobantes.
func (s *Store) SinConstatar(ctx context.Context, lote int64, solo []int64, limite int) ([]Cbte, error) {
	rows, err := s.Pool.Query(ctx, selCbte+` WHERE c.lote_id=$1 AND c.constatacion IS NULL AND c.borrado_en IS NULL
		AND ($3::bigint[] IS NULL OR c.id = ANY($3)) ORDER BY c.id LIMIT $2`, lote, limite, solo)
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

// ActualizarCbte guarda datos, hallazgos, estado y (opcional) constatación, y registra las correcciones.
// Lo que está en la papelera no se toca (ErrNoEncontrado): al restaurarlo vuelve como estaba.
func (s *Store) ActualizarCbte(ctx context.Context, c *Cbte, correcciones []Correccion, aprobadoPor string) error {
	return pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		datos, _ := json.Marshal(c.Datos)
		hall, _ := json.Marshal(c.Hallazgos)
		var cons any
		if len(c.Constatacion) > 0 && string(c.Constatacion) != "null" {
			cons = []byte(c.Constatacion)
		}
		var revPor any
		if aprobadoPor != "" {
			revPor = aprobadoPor
		}
		ct, err := tx.Exec(ctx, `UPDATE comprobantes SET datos=$2, hallazgos=$3, estado=$4, confianza=$5, constatacion=$6,
			tipo=$7, punto_venta=$8, numero=$9, fecha=$10, cuit_emisor=$11, razon_social_emisor=$12, total=$13, actualizado=now(),
			revisado_por=coalesce($14, revisado_por), revisado_en=CASE WHEN $14::text IS NULL THEN revisado_en ELSE now() END
			WHERE id=$1 AND borrado_en IS NULL`,
			c.ID, datos, hall, string(c.Estado), c.Confianza, cons, c.Datos.Tipo, c.Datos.PuntoVenta, c.Datos.Numero, fechaONull(c.Datos.Fecha),
			c.Datos.CUITEmisor, c.Datos.RazonSocialEmisor, int64(c.Datos.Total), revPor)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return ErrNoEncontrado
		}
		for _, k := range correcciones {
			if _, err := tx.Exec(ctx, `INSERT INTO correcciones (comprobante_id, campo, antes, despues, usuario) VALUES ($1,$2,$3,$4,$5)`,
				c.ID, k.Campo, k.Antes, k.Despues, k.Usuario); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) Correcciones(ctx context.Context, id int64) ([]Correccion, error) {
	rows, err := s.Pool.Query(ctx, `SELECT campo, antes, despues, usuario, creado FROM correcciones WHERE comprobante_id=$1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Correccion{}
	for rows.Next() {
		var k Correccion
		if err := rows.Scan(&k.Campo, &k.Antes, &k.Despues, &k.Usuario, &k.Creado); err != nil {
			return nil, err
		}
		res = append(res, k)
	}
	return res, rows.Err()
}

// ---------- Rubros por CUIT (capa 3) ----------

// RubroCUIT es el rubro que una persona eligió para un emisor.
type RubroCUIT struct {
	CUITEmisor  string    `json:"cuit_emisor"`
	Rubro       string    `json:"rubro"`
	Usuario     string    `json:"usuario"`
	Actualizado time.Time `json:"actualizado"`
}

// RubrosDeCUITs devuelve los rubros guardados para esos emisores (cuit -> rubro).
func (s *Store) RubrosDeCUITs(ctx context.Context, cuits []string) (map[string]string, error) {
	res := map[string]string{}
	if len(cuits) == 0 {
		return res, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT cuit_emisor, rubro FROM rubros_cuit WHERE cuit_emisor = ANY($1)`, cuits)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c, r string
		if err := rows.Scan(&c, &r); err != nil {
			return nil, err
		}
		res[c] = r
	}
	return res, rows.Err()
}

// GuardarRubroCUIT recuerda el rubro de un emisor y lo pasa a sus comprobantes ya cargados, salvo
// a los que una persona les corrigió el rubro a ellos en particular (tienen una corrección de rubro).
// Los que tomaron el rubro de una corrección anterior a este emisor lo siguen: si alguien cambia de
// idea, cambian todos. Devuelve cuántos actualizó.
func (s *Store) GuardarRubroCUIT(ctx context.Context, cuit, rubro, usuario string) (int64, error) {
	var n int64
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO rubros_cuit (cuit_emisor, rubro, usuario, actualizado) VALUES ($1, $2, $3, now())
			ON CONFLICT (cuit_emisor) DO UPDATE SET rubro=EXCLUDED.rubro, usuario=EXCLUDED.usuario, actualizado=now()`, cuit, rubro, usuario); err != nil {
			return err
		}
		ct, err := tx.Exec(ctx, `UPDATE comprobantes SET
				datos = jsonb_set(datos || jsonb_build_object('rubro', $2::text), '{origenes}',
					coalesce(nullif(datos->'origenes', 'null'::jsonb), '{}'::jsonb) || '{"rubro":"manual"}'::jsonb),
				actualizado = now()
			WHERE cuit_emisor = $1 AND borrado_en IS NULL
			  AND NOT EXISTS (SELECT 1 FROM correcciones k WHERE k.comprobante_id = comprobantes.id AND k.campo = 'rubro')`, cuit, rubro)
		n = ct.RowsAffected()
		return err
	})
	return n, err
}

// Rubros lista los rubros guardados por CUIT, del más reciente al más viejo. Con alcance, solo los
// de emisores que aparecen en comprobantes de esos clientes.
func (s *Store) Rubros(ctx context.Context, alcance []string) ([]RubroCUIT, error) {
	rows, err := s.Pool.Query(ctx, `SELECT cuit_emisor, rubro, usuario, actualizado FROM rubros_cuit r
		WHERE $1::text[] IS NULL OR EXISTS (SELECT 1 FROM comprobantes c WHERE c.cuit_emisor = r.cuit_emisor AND c.cliente_cuit = ANY($1) AND c.borrado_en IS NULL)
		ORDER BY actualizado DESC LIMIT 5000`, alcance)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []RubroCUIT{}
	for rows.Next() {
		var r RubroCUIT
		if err := rows.Scan(&r.CUITEmisor, &r.Rubro, &r.Usuario, &r.Actualizado); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, rows.Err()
}

// Estadisticas para /metrics.
func (s *Store) Estadisticas(ctx context.Context) (map[string]int64, error) {
	m := map[string]int64{}
	rows, err := s.Pool.Query(ctx, `SELECT 'documentos_'||estado, count(*) FROM documentos WHERE borrado_en IS NULL GROUP BY estado
		UNION ALL SELECT 'comprobantes_'||estado, count(*) FROM comprobantes WHERE borrado_en IS NULL GROUP BY estado
		UNION ALL SELECT 'papelera_lotes', count(*) FROM lotes WHERE borrado_en IS NOT NULL
		UNION ALL SELECT 'papelera_comprobantes', count(*) FROM comprobantes WHERE borrado_en IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, rows.Err()
}
