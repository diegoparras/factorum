// Package copias hace las copias de seguridad de la base y las restaura.
//
// La copia es lógica y portable: en Go, con COPY ... TO STDOUT (formato de texto) de cada tabla,
// todo dentro de una transacción REPEATABLE READ de solo lectura, así que es una foto consistente
// de un instante y no depende de que haya un pg_dump de la misma versión que el servidor. Se arma
// un tar con una entrada por tabla (en el orden de las claves foráneas) y un manifiesto al final
// (versión de Factorum y del esquema, fecha, columnas, filas, secuencias y SHA-256 de cada tabla),
// se comprime con zstd y se cifra con la clave maestra (AES-256-GCM en tramos, almacen.CifrarFlujo).
//
// No se copian las tablas que describen la cola (river_*), las migraciones aplicadas (van en el
// manifiesto), el pase a R2 (r2_*) ni el registro de las copias mismas (copias): son del servidor
// y del almacenamiento, no datos. Restaurar no las pisa, salvo la cola, que se vacía y se vuelve a
// llenar con lo que quedó a medio procesar.
package copias

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/klauspost/compress/zstd"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/config"
)

// Formato de la copia. Si cambia, Restaurar sabe leer los anteriores.
const Formato = 1

// ArchivoManifiesto es la última entrada del tar.
const ArchivoManifiesto = "manifiesto.json"

// Columna de una tabla, con su tipo como lo escribe Postgres (format_type).
type Columna struct {
	Nombre string `json:"nombre"`
	Tipo   string `json:"tipo"`
}

// Secuencia de una columna (bigserial o identity), con su valor al momento de la copia.
type Secuencia struct {
	Columna string `json:"columna"`
	Valor   int64  `json:"valor"`
	Llamado bool   `json:"llamado"`
}

// Tabla copiada.
type Tabla struct {
	Nombre     string      `json:"nombre"`
	Archivo    string      `json:"archivo"` // entrada del tar
	Columnas   []Columna   `json:"columnas"`
	Filas      int64       `json:"filas"`
	Bytes      int64       `json:"bytes"`
	SHA256     string      `json:"sha256"` // del texto de COPY
	Secuencias []Secuencia `json:"secuencias,omitempty"`
}

// Esquema dice en qué versión estaba la base.
type Esquema struct {
	Migraciones []string `json:"migraciones"`         // las de Factorum (001_inicial.sql, ...)
	River       int      `json:"river,omitempty"`     // versión de las tablas de la cola
	WhatsApp    int      `json:"whatsapp,omitempty"`  // versión de las tablas de la sesión de WhatsApp
	Postgres    string   `json:"postgres,omitempty"`  // del servidor donde se hizo (informativo)
	Esquema     string   `json:"esquema,omitempty"`   // el schema de Postgres (public)
	Excluidas   []string `json:"excluidas,omitempty"` // tablas que no se copiaron (a propósito)
}

// Manifiesto describe la copia.
type Manifiesto struct {
	Formato  int       `json:"formato"`
	Factorum string    `json:"factorum"`
	Creada   time.Time `json:"creada"`
	Esquema  Esquema   `json:"esquema"`
	Tablas   []Tabla   `json:"tablas"`
	Filas    int64     `json:"filas"`
}

// Excluida dice si una tabla queda afuera de las copias (y de la restauración).
func Excluida(nombre string) bool {
	return strings.HasPrefix(nombre, "river_") || strings.HasPrefix(nombre, "r2_") ||
		nombre == "factorum_migraciones" || nombre == "whatsmeow_version" || nombre == "copias"
}

type consultor interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type tablaBase struct {
	oid      uint32
	nombre   string
	columnas []Columna
	seqs     []string // columnas con secuencia
}

func ident(s string) string { return pgx.Identifier{s}.Sanitize() }

// tablasDeDatos lista las tablas del schema actual que se copian, en un orden en que cada tabla
// va después de las que referencia (las claves foráneas a sí misma no importan: COPY las chequea
// al final de cada tabla).
func tablasDeDatos(ctx context.Context, q consultor) ([]tablaBase, []string, error) {
	rows, err := q.Query(ctx, `SELECT c.oid, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind IN ('r','p') AND NOT c.relispartition ORDER BY c.relname`)
	if err != nil {
		return nil, nil, err
	}
	var todas []tablaBase
	var excluidas []string
	for rows.Next() {
		var t tablaBase
		if err := rows.Scan(&t.oid, &t.nombre); err != nil {
			rows.Close()
			return nil, nil, err
		}
		if Excluida(t.nombre) {
			excluidas = append(excluidas, t.nombre)
			continue
		}
		todas = append(todas, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	por := map[uint32]int{}
	for i, t := range todas {
		por[t.oid] = i
	}
	deps := map[uint32]map[uint32]bool{}
	rows, err = q.Query(ctx, `SELECT conrelid, confrelid FROM pg_constraint WHERE contype='f' AND conrelid <> confrelid`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var de, a uint32
		if err := rows.Scan(&de, &a); err != nil {
			rows.Close()
			return nil, nil, err
		}
		_, ok1 := por[de]
		_, ok2 := por[a]
		if ok1 && ok2 {
			if deps[de] == nil {
				deps[de] = map[uint32]bool{}
			}
			deps[de][a] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	// Orden topológico, alfabético entre las que están listas.
	var orden []tablaBase
	hecha := map[uint32]bool{}
	for len(orden) < len(todas) {
		avanzo := false
		for _, t := range todas {
			if hecha[t.oid] {
				continue
			}
			lista := true
			for d := range deps[t.oid] {
				if !hecha[d] {
					lista = false
					break
				}
			}
			if lista {
				orden = append(orden, t)
				hecha[t.oid] = true
				avanzo = true
			}
		}
		if !avanzo {
			var ciclo []string
			for _, t := range todas {
				if !hecha[t.oid] {
					ciclo = append(ciclo, t.nombre)
				}
			}
			return nil, nil, fmt.Errorf("hay un ciclo de claves foráneas entre %s", strings.Join(ciclo, ", "))
		}
	}
	for i := range orden {
		t := &orden[i]
		rs, err := q.Query(ctx, `SELECT attname, format_type(atttypid, atttypmod), pg_get_serial_sequence($2, attname) IS NOT NULL
			FROM pg_attribute WHERE attrelid=$1 AND attnum>0 AND NOT attisdropped AND attgenerated='' ORDER BY attnum`, t.oid, ident(t.nombre))
		if err != nil {
			return nil, nil, err
		}
		for rs.Next() {
			var c Columna
			var seq bool
			if err := rs.Scan(&c.Nombre, &c.Tipo, &seq); err != nil {
				rs.Close()
				return nil, nil, err
			}
			t.columnas = append(t.columnas, c)
			if seq {
				t.seqs = append(t.seqs, c.Nombre)
			}
		}
		rs.Close()
		if err := rs.Err(); err != nil {
			return nil, nil, err
		}
	}
	return orden, excluidas, nil
}

func listaColumnas(cs []Columna) string {
	n := make([]string, len(cs))
	for i, c := range cs {
		n[i] = ident(c.Nombre)
	}
	return strings.Join(n, ", ")
}

// esquemaActual lee la versión del esquema.
func esquemaActual(ctx context.Context, q consultor) (Esquema, error) {
	var e Esquema
	rows, err := q.Query(ctx, `SELECT nombre FROM factorum_migraciones ORDER BY nombre`)
	if err != nil {
		return e, fmt.Errorf("la base no tiene las tablas de Factorum: %w", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return e, err
		}
		e.Migraciones = append(e.Migraciones, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return e, err
	}
	var hay bool
	if err := q.QueryRow(ctx, `SELECT to_regclass('river_migration') IS NOT NULL`).Scan(&hay); err == nil && hay {
		_ = q.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM river_migration`).Scan(&e.River)
	}
	if err := q.QueryRow(ctx, `SELECT to_regclass('whatsmeow_version') IS NOT NULL`).Scan(&hay); err == nil && hay {
		_ = q.QueryRow(ctx, `SELECT coalesce(max(version),0) FROM whatsmeow_version`).Scan(&e.WhatsApp)
	}
	_ = q.QueryRow(ctx, `SHOW server_version`).Scan(&e.Postgres)
	_ = q.QueryRow(ctx, `SELECT current_schema()`).Scan(&e.Esquema)
	return e, nil
}

// contador cuenta líneas (en el formato de texto de COPY, una por fila) y calcula el SHA-256.
type contador struct {
	h      hashEscritor
	n      int64
	lineas int64
}

type hashEscritor interface {
	io.Writer
	Sum([]byte) []byte
}

func nuevoContador() *contador { return &contador{h: sha256.New()} }

func (c *contador) Write(p []byte) (int, error) {
	c.h.Write(p)
	c.n += int64(len(p))
	for _, b := range p {
		if b == '\n' {
			c.lineas++
		}
	}
	return len(p), nil
}

func (c *contador) suma() string { return hex.EncodeToString(c.h.Sum(nil)) }

// Volcar escribe en w la copia cifrada de la base. tmp es una carpeta para los archivos de paso
// (una tabla por vez: el tar necesita saber el tamaño antes de escribirla).
func Volcar(ctx context.Context, db interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
}, sello *almacen.Almacen, w io.Writer, tmp string) (*Manifiesto, error) {
	if sello == nil || !sello.Cifrado() {
		return nil, errors.New("hace falta FACTORUM_MASTER_KEY para cifrar la copia")
	}
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	m := &Manifiesto{Formato: Formato, Factorum: config.Version, Creada: time.Now()}
	if m.Esquema, err = esquemaActual(ctx, tx); err != nil {
		return nil, err
	}
	tablas, excluidas, err := tablasDeDatos(ctx, tx)
	if err != nil {
		return nil, err
	}
	m.Esquema.Excluidas = excluidas

	cif, err := sello.CifrarFlujo(w)
	if err != nil {
		return nil, err
	}
	zw, err := zstd.NewWriter(cif, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(zw)

	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, err
	}
	for i, t := range tablas {
		tab := Tabla{Nombre: t.nombre, Archivo: fmt.Sprintf("tablas/%03d-%s.copy", i+1, t.nombre), Columnas: t.columnas}
		if err := volcarTabla(ctx, tx, tw, &tab, tmp, m.Creada); err != nil {
			return nil, fmt.Errorf("tabla %s: %w", t.nombre, err)
		}
		for _, col := range t.seqs {
			var s Secuencia
			s.Columna = col
			var seq string
			if err := tx.QueryRow(ctx, `SELECT pg_get_serial_sequence($1, $2)`, ident(t.nombre), col).Scan(&seq); err != nil {
				return nil, err
			}
			// La secuencia no es transaccional: puede estar más adelante que la foto, nunca atrás.
			if err := tx.QueryRow(ctx, `SELECT last_value, is_called FROM `+seq).Scan(&s.Valor, &s.Llamado); err != nil {
				return nil, fmt.Errorf("secuencia de %s.%s: %w", t.nombre, col, err)
			}
			tab.Secuencias = append(tab.Secuencias, s)
		}
		m.Tablas = append(m.Tablas, tab)
		m.Filas += tab.Filas
	}
	man, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := tw.WriteHeader(&tar.Header{Name: ArchivoManifiesto, Mode: 0o600, Size: int64(len(man)), ModTime: m.Creada, Typeflag: tar.TypeReg}); err != nil {
		return nil, err
	}
	if _, err := tw.Write(man); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := cif.Close(); err != nil {
		return nil, err
	}
	return m, nil
}

func volcarTabla(ctx context.Context, tx pgx.Tx, tw *tar.Writer, tab *Tabla, tmp string, cuando time.Time) error {
	f, err := os.CreateTemp(tmp, "tabla-*.copy")
	if err != nil {
		return err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	c := nuevoContador()
	if _, err := tx.Conn().PgConn().CopyTo(ctx, io.MultiWriter(f, c), "COPY "+ident(tab.Nombre)+" ("+listaColumnas(tab.Columnas)+") TO STDOUT"); err != nil {
		return err
	}
	tab.Filas, tab.Bytes, tab.SHA256 = c.lineas, c.n, c.suma()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: tab.Archivo, Mode: 0o600, Size: c.n, ModTime: cuando, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, c.n)
	return err
}

// Leer abre una copia y la recorre entera: descifra, descomprime, calcula el SHA-256 y cuenta las
// filas de cada tabla, y controla todo contra el manifiesto. No toca ninguna base.
func Leer(r io.Reader, sello *almacen.Almacen) (*Manifiesto, error) {
	return recorrer(r, sello, nil)
}

// recorrer lee la copia y, si cargar no es nil, se lo pasa a cada tabla (en el orden del tar).
// Con cargar, el manifiesto tiene que venir de antes (esperado) porque está al final.
func recorrer(r io.Reader, sello *almacen.Almacen, cargar func(archivo string, datos io.Reader) error) (*Manifiesto, error) {
	if sello == nil || !sello.Cifrado() {
		return nil, errors.New("hace falta FACTORUM_MASTER_KEY para abrir la copia")
	}
	plano, err := sello.DescifrarFlujo(r)
	if err != nil {
		return nil, err
	}
	zr, err := zstd.NewReader(plano, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	vistas := map[string]*contador{}
	var orden []string
	var m *Manifiesto
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, roto(err)
		}
		if h.Name == ArchivoManifiesto {
			b, err := io.ReadAll(io.LimitReader(tr, 64<<20))
			if err != nil {
				return nil, roto(err)
			}
			m = &Manifiesto{}
			if err := json.Unmarshal(b, m); err != nil {
				return nil, fmt.Errorf("el manifiesto de la copia no se entiende: %w", err)
			}
			continue
		}
		if !strings.HasPrefix(h.Name, "tablas/") || vistas[h.Name] != nil {
			return nil, fmt.Errorf("la copia tiene una entrada que no corresponde: %q", h.Name)
		}
		c := nuevoContador()
		vistas[h.Name] = c
		orden = append(orden, h.Name)
		src := io.TeeReader(tr, c)
		if cargar != nil {
			if err := cargar(h.Name, src); err != nil {
				return nil, err
			}
		}
		if _, err := io.Copy(io.Discard, src); err != nil {
			return nil, roto(err)
		}
	}
	if m == nil {
		return nil, errors.New("la copia no tiene manifiesto: está incompleta")
	}
	if m.Formato < 1 || m.Formato > Formato {
		return nil, fmt.Errorf("la copia tiene el formato %d y este Factorum (%s) lee hasta el %d: restaurala con un Factorum más nuevo", m.Formato, config.Version, Formato)
	}
	if len(m.Tablas) != len(vistas) {
		return nil, fmt.Errorf("la copia tiene %d tablas y el manifiesto dice %d", len(vistas), len(m.Tablas))
	}
	var filas int64
	for i, t := range m.Tablas {
		c := vistas[t.Archivo]
		if c == nil || orden[i] != t.Archivo {
			return nil, fmt.Errorf("falta la tabla %s (o está fuera de orden)", t.Nombre)
		}
		if c.suma() != t.SHA256 || c.n != t.Bytes || c.lineas != t.Filas {
			return nil, fmt.Errorf("la tabla %s no coincide con el manifiesto (SHA-256, tamaño o filas)", t.Nombre)
		}
		filas += t.Filas
	}
	if filas != m.Filas {
		return nil, errors.New("el total de filas no coincide con el manifiesto")
	}
	return m, nil
}

func roto(err error) error {
	if errors.Is(err, almacen.ErrFlujoRoto) {
		return err
	}
	return fmt.Errorf("la copia está dañada: %w", err)
}

// Tablas devuelve los nombres de las tablas de la copia, en orden.
func (m *Manifiesto) NombresTablas() []string {
	n := make([]string, len(m.Tablas))
	for i, t := range m.Tablas {
		n[i] = t.Nombre
	}
	return n
}

// MigracionesDesconocidas son las migraciones de la copia que este Factorum no tiene.
func (m *Manifiesto) MigracionesDesconocidas(conocidas []string) []string {
	var res []string
	for _, x := range m.Esquema.Migraciones {
		if !slices.Contains(conocidas, x) {
			res = append(res, x)
		}
	}
	return res
}
