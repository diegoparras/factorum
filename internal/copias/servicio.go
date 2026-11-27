package copias

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/config"
)

// Estados de una copia.
const (
	Haciendo = "haciendo"
	OK       = "ok"
	Error    = "error"
)

// Destinos.
const (
	DestinoR2    = "r2"
	DestinoDisco = "disco"
)

// Carpeta de las copias, en R2 (bajo el prefijo de la instalación) y en el disco (FACTORUM_DATOS).
const Carpeta = "copias"

// Extension de los archivos de copia.
const Extension = ".copia"

// candado serializa las copias (pg_advisory_lock): nunca dos a la vez.
const candado = 84121

// Zona es la hora de Buenos Aires: la copia diaria y los nombres de los archivos van en esa hora.
var Zona = func() *time.Location {
	if z, err := time.LoadLocation("America/Argentina/Buenos_Aires"); err == nil {
		return z
	}
	return time.FixedZone("ART", -3*3600)
}()

// Copia es una fila del registro.
type Copia struct {
	ID         int64      `json:"id"`
	Creado     time.Time  `json:"creado"`
	Terminado  *time.Time `json:"terminado,omitempty"`
	Estado     string     `json:"estado"`
	Manual     bool       `json:"manual"`
	Programada string     `json:"programada,omitempty"` // AAAA-MM-DD
	Por        string     `json:"por"`
	Destino    string     `json:"destino"`
	Clave      string     `json:"clave,omitempty"`
	Tamano     *int64     `json:"tamano,omitempty"`
	Filas      *int64     `json:"filas,omitempty"`
	Tablas     *int       `json:"tablas,omitempty"`
	SHA256     string     `json:"sha256,omitempty"`
	Version    string     `json:"version,omitempty"`
	Error      string     `json:"error,omitempty"`
	DuracionMS *int64     `json:"duracion_ms,omitempty"`
	// Disponible: se puede descargar desde acá (está en el disco o en el R2 activo).
	Disponible bool `json:"disponible"`
}

// Servicio hace, lista, poda y programa las copias.
type Servicio struct {
	Pool   *pgxpool.Pool
	Sello  *almacen.Almacen      // la clave maestra (cifra las copias)
	Config func() *config.Config // la vigente: activa, hora y cuántas guardar
	R2     func() *almacen.S3    // el R2 vigente (nil = las copias van al disco)
	Dir    string                // FACTORUM_DATOS/copias
	Log    *slog.Logger
	// Encolar arranca una copia en segundo plano (el trabajo de River). Sin Encolar, Programar no
	// hace nada.
	Encolar func(ctx context.Context, a Args, opts *river.InsertOpts) error
	// Auditar anota cada copia (bien o mal).
	Auditar func(ctx context.Context, accion, objeto, actor string, detalle map[string]any)
	Ahora   func() time.Time

	mu     sync.Mutex
	avisos []func(context.Context, Copia)
}

func (s *Servicio) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Servicio) ahora() time.Time {
	if s.Ahora != nil {
		return s.Ahora()
	}
	return time.Now()
}

func (s *Servicio) r2() *almacen.S3 {
	if s.R2 == nil {
		return nil
	}
	return s.R2()
}

// AlTerminar registra una función que se llama después de cada copia, haya salido bien
// (c.Estado == copias.OK) o mal (copias.Error, con c.Error). Es el enganche para avisar por mail
// cuando una copia falla: el que manda los avisos hace, al arrancar,
//
//	copias.AlTerminar(func(ctx context.Context, c copias.Copia) {
//		if c.Estado == copias.Error { avisarPorMail(ctx, "Falló la copia de la base", c.Error) }
//	})
//
// Las funciones corren en el trabajo de la copia, de a una y en orden; no tienen que tardar (si
// mandan un mail, que lo encolen). El contexto no se cancela al terminar la copia.
func (s *Servicio) AlTerminar(f func(context.Context, Copia)) {
	s.mu.Lock()
	s.avisos = append(s.avisos, f)
	s.mu.Unlock()
}

func (s *Servicio) avisar(ctx context.Context, c Copia) {
	s.mu.Lock()
	fs := append([]func(context.Context, Copia){}, s.avisos...)
	s.mu.Unlock()
	for _, f := range fs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					s.log().Error("un aviso de copia falló", "error", r)
				}
			}()
			f(context.WithoutCancel(ctx), c)
		}()
	}
}

func (s *Servicio) auditar(ctx context.Context, accion, objeto, actor string, d map[string]any) {
	if s.Auditar != nil {
		s.Auditar(context.WithoutCancel(ctx), accion, objeto, actor, d)
	}
}

const selCopia = `SELECT id, creado, terminado, estado, manual, coalesce(to_char(programada,'YYYY-MM-DD'),''), por, destino, coalesce(clave,''),
	tamano, filas, tablas, coalesce(sha256,''), version, error, duracion_ms FROM copias`

func scanCopia(r pgx.Row) (Copia, error) {
	var c Copia
	err := r.Scan(&c.ID, &c.Creado, &c.Terminado, &c.Estado, &c.Manual, &c.Programada, &c.Por, &c.Destino, &c.Clave,
		&c.Tamano, &c.Filas, &c.Tablas, &c.SHA256, &c.Version, &c.Error, &c.DuracionMS)
	return c, err
}

// ErrNoEncontrada: no hay una copia con ese id (o no se puede bajar).
var ErrNoEncontrada = errors.New("no hay una copia con ese número")

// nombre arma el nombre del archivo: factorum-AAAAMMDD-HHMMSS.copia, en hora de Buenos Aires.
func nombre(t time.Time) string {
	return "factorum-" + t.In(Zona).Format("20060102-150405") + Extension
}

// Hacer hace una copia ahora (la espera si hay otra en curso) y la deja en R2 o en el disco.
// programada es el día de la copia diaria (vacío si es a pedido).
func (s *Servicio) Hacer(ctx context.Context, por string, manual bool, programada string) (*Copia, error) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(candado)); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(candado)) }()
	// Con el candado tomado, nada está haciendo una copia: lo que figura «haciendo» se cortó.
	if _, err := s.Pool.Exec(ctx, `UPDATE copias SET estado='error', error='se cortó (se apagó o se reinició el servidor)', terminado=now() WHERE estado='haciendo'`); err != nil {
		return nil, err
	}

	destino := DestinoDisco
	r2 := s.r2()
	if r2 != nil {
		destino = DestinoR2
	}
	// El nombre lleva los segundos: si ya hay una de este mismo segundo, se espera al siguiente.
	inicio := s.ahora()
	for range 3 {
		var hay bool
		if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM copias WHERE destino=$1 AND clave=$2)`, destino, Carpeta+"/"+nombre(inicio)).Scan(&hay); err != nil {
			return nil, err
		}
		if !hay {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Until(inicio.Truncate(time.Second).Add(time.Second))):
		}
		inicio = s.ahora()
	}
	base := nombre(inicio)
	clave := Carpeta + "/" + base
	var prog any
	if programada != "" {
		prog = programada
	}
	var id int64
	err = s.Pool.QueryRow(ctx, `INSERT INTO copias (estado, manual, programada, por, destino, clave, version, creado)
		VALUES ('haciendo', $1, $2, $3, $4, $5, $6, $7) RETURNING id`, manual, prog, por, destino, clave, config.Version, inicio).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "copias_clave") {
			return nil, errors.New("ya hay una copia de este mismo segundo")
		}
		return nil, err
	}
	m, tam, suma, err := s.volcarA(ctx, r2, clave, base)
	dur := s.ahora().Sub(inicio).Milliseconds()
	c := context.WithoutCancel(ctx)
	if err != nil {
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		_, _ = s.Pool.Exec(c, `UPDATE copias SET estado='error', error=$2, terminado=now(), duracion_ms=$3 WHERE id=$1`, id, msg, dur)
		s.log().Error("falló la copia de la base", "destino", destino, "error", msg)
		s.auditar(ctx, "copia.error", base, quien(por), map[string]any{"destino": destino, "error": msg, "manual": manual})
		cp, _ := s.Copia(c, id)
		if cp != nil {
			s.avisar(ctx, *cp)
		}
		return cp, err
	}
	_, err = s.Pool.Exec(c, `UPDATE copias SET estado='ok', terminado=now(), tamano=$2, filas=$3, tablas=$4, sha256=$5, duracion_ms=$6 WHERE id=$1`,
		id, tam, m.Filas, len(m.Tablas), suma, dur)
	if err != nil {
		return nil, err
	}
	s.log().Info("copia de la base hecha", "destino", destino, "clave", clave, "bytes", tam, "filas", m.Filas, "ms", dur)
	s.auditar(ctx, "copia.ok", base, quien(por), map[string]any{"destino": destino, "bytes": tam, "filas": m.Filas, "tablas": len(m.Tablas), "manual": manual})
	cp, err := s.Copia(c, id)
	if err != nil {
		return nil, err
	}
	s.avisar(ctx, *cp)
	if err := s.Podar(c); err != nil {
		s.log().Warn("no se pudieron borrar las copias viejas", "error", err)
	}
	return cp, nil
}

func quien(por string) string {
	if por == "" {
		return "copia diaria"
	}
	return por
}

// volcarA escribe la copia en un archivo de paso y la deja en su destino.
func (s *Servicio) volcarA(ctx context.Context, r2 *almacen.S3, clave, base string) (*Manifiesto, int64, string, error) {
	tmp := filepath.Join(s.Dir, ".tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return nil, 0, "", fmt.Errorf("no se pudo crear %s: %w", tmp, err)
	}
	f, err := os.CreateTemp(tmp, "copia-*"+Extension)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	h := sha256.New()
	cuenta := &contadorBytes{}
	m, err := Volcar(ctx, s.Pool, s.Sello, io.MultiWriter(f, h, cuenta), tmp)
	if err != nil {
		return nil, 0, "", err
	}
	if err := f.Sync(); err != nil {
		return nil, 0, "", err
	}
	if err := f.Close(); err != nil {
		return nil, 0, "", err
	}
	suma := hex.EncodeToString(h.Sum(nil))
	if r2 != nil {
		if err := r2.SubirArchivo(ctx, clave, f.Name()); err != nil {
			return nil, 0, "", fmt.Errorf("subiendo a R2: %w", err)
		}
		n, err := r2.Tamano(ctx, clave)
		if err != nil || n != cuenta.n {
			return nil, 0, "", fmt.Errorf("la copia no quedó bien en R2 (tamaño %d de %d)", n, cuenta.n)
		}
		return m, cuenta.n, suma, nil
	}
	if err := os.Rename(f.Name(), filepath.Join(s.Dir, base)); err != nil {
		return nil, 0, "", err
	}
	return m, cuenta.n, suma, nil
}

type contadorBytes struct{ n int64 }

func (c *contadorBytes) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// Copia trae una fila del registro.
func (s *Servicio) Copia(ctx context.Context, id int64) (*Copia, error) {
	c, err := scanCopia(s.Pool.QueryRow(ctx, selCopia+` WHERE id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoEncontrada
	}
	if err != nil {
		return nil, err
	}
	s.disponible(&c)
	return &c, nil
}

func (s *Servicio) disponible(c *Copia) {
	if c.Estado != OK || c.Clave == "" {
		return
	}
	switch c.Destino {
	case DestinoR2:
		c.Disponible = s.r2() != nil
	case DestinoDisco:
		_, err := os.Stat(filepath.Join(s.Dir, filepath.Base(c.Clave)))
		c.Disponible = err == nil
	}
}

// Lista devuelve las copias, de la más nueva a la más vieja. Antes suma al registro las que están
// en el destino y no figuran (por ejemplo, después de restaurar en una base nueva).
func (s *Servicio) Lista(ctx context.Context, limite int) ([]Copia, error) {
	if err := s.sincronizar(ctx); err != nil {
		s.log().Warn("no se pudo mirar qué copias hay en el destino", "error", err)
	}
	if limite <= 0 || limite > 500 {
		limite = 100
	}
	rows, err := s.Pool.Query(ctx, selCopia+` ORDER BY creado DESC, id DESC LIMIT $1`, limite)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []Copia{}
	for rows.Next() {
		c, err := scanCopia(rows)
		if err != nil {
			return nil, err
		}
		s.disponible(&c)
		res = append(res, c)
	}
	return res, rows.Err()
}

// Ultima es la última copia terminada (bien o mal); nil si no hay ninguna.
func (s *Servicio) Ultima(ctx context.Context) (*Copia, error) {
	c, err := scanCopia(s.Pool.QueryRow(ctx, selCopia+` WHERE estado<>'haciendo' ORDER BY creado DESC, id DESC LIMIT 1`))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// creadaDe saca la fecha del nombre del archivo.
func creadaDe(base string) (time.Time, bool) {
	t, err := time.ParseInLocation("factorum-20060102-150405"+Extension, base, Zona)
	return t, err == nil
}

type archivo struct {
	destino, clave string
	tamano         int64
	creada         time.Time
}

func (s *Servicio) enDestinos(ctx context.Context) ([]archivo, error) {
	var res []archivo
	if ents, err := os.ReadDir(s.Dir); err == nil {
		for _, e := range ents {
			if t, ok := creadaDe(e.Name()); ok && !e.IsDir() {
				info, err := e.Info()
				if err != nil {
					continue
				}
				res = append(res, archivo{DestinoDisco, Carpeta + "/" + e.Name(), info.Size(), t})
			}
		}
	}
	if r2 := s.r2(); r2 != nil {
		objs, err := r2.Listar(ctx, Carpeta+"/")
		if err != nil {
			return res, err
		}
		for _, o := range objs {
			if t, ok := creadaDe(strings.TrimPrefix(o.Clave, Carpeta+"/")); ok {
				res = append(res, archivo{DestinoR2, o.Clave, o.Tamano, t})
			}
		}
	}
	return res, nil
}

func (s *Servicio) sincronizar(ctx context.Context) error {
	arch, err := s.enDestinos(ctx)
	for _, a := range arch {
		if _, e := s.Pool.Exec(ctx, `INSERT INTO copias (estado, destino, clave, tamano, creado, terminado, por, error)
			VALUES ('ok', $1, $2, $3, $4, $4, '', 'estaba en el destino y no figuraba en el registro') ON CONFLICT (destino, clave) WHERE clave IS NOT NULL DO NOTHING`,
			a.destino, a.clave, a.tamano, a.creada); e != nil {
			return e
		}
	}
	return err
}

// Abrir devuelve el archivo cifrado de una copia, para descargarlo.
func (s *Servicio) Abrir(ctx context.Context, id int64) (io.ReadCloser, int64, *Copia, error) {
	c, err := s.Copia(ctx, id)
	if err != nil {
		return nil, 0, nil, err
	}
	if !c.Disponible {
		return nil, 0, c, ErrNoEncontrada
	}
	if c.Destino == DestinoR2 {
		r, n, err := s.r2().Abrir(ctx, c.Clave)
		return r, n, c, err
	}
	f, err := os.Open(filepath.Join(s.Dir, filepath.Base(c.Clave)))
	if err != nil {
		return nil, 0, c, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, c, err
	}
	return f, info.Size(), c, nil
}

// Podar borra las copias que sobran: se guardan las últimas «diarias» y, de cada uno de los
// últimos «mensuales» meses, la primera del mes. Las que no se pueden borrar desde acá (en un R2
// que ya no está activo) quedan. Los errores de más de 60 días se sacan del registro.
func (s *Servicio) Podar(ctx context.Context) error {
	cfg := s.Config()
	diarias, mensuales := cfg.CopiasAGuardar()
	rows, err := s.Pool.Query(ctx, selCopia+` WHERE estado='ok' ORDER BY creado DESC, id DESC`)
	if err != nil {
		return err
	}
	var oks []Copia
	for rows.Next() {
		c, err := scanCopia(rows)
		if err != nil {
			rows.Close()
			return err
		}
		oks = append(oks, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	queda := map[int64]bool{}
	for i, c := range oks {
		if i < diarias {
			queda[c.ID] = true
		}
	}
	// La primera de cada mes, para los últimos meses con copias.
	primera := map[string]Copia{}
	var meses []string
	for _, c := range oks {
		mes := c.Creado.In(Zona).Format("2006-01")
		if p, ok := primera[mes]; !ok {
			meses = append(meses, mes)
			primera[mes] = c
		} else if c.Creado.Before(p.Creado) {
			primera[mes] = c
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(meses)))
	for i, mes := range meses {
		if i < mensuales {
			queda[primera[mes].ID] = true
		}
	}
	for _, c := range oks {
		if queda[c.ID] {
			continue
		}
		if err := s.borrarArchivo(ctx, c); err != nil {
			if errors.Is(err, errNoSePuedeBorrar) {
				continue
			}
			return err
		}
		if _, err := s.Pool.Exec(ctx, `DELETE FROM copias WHERE id=$1`, c.ID); err != nil {
			return err
		}
		s.log().Info("copia vieja borrada", "clave", c.Clave, "destino", c.Destino)
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM copias WHERE estado='error' AND creado < now() - interval '60 days'`)
	return err
}

var errNoSePuedeBorrar = errors.New("no se puede borrar desde acá")

func (s *Servicio) borrarArchivo(ctx context.Context, c Copia) error {
	switch c.Destino {
	case DestinoR2:
		r2 := s.r2()
		if r2 == nil {
			return errNoSePuedeBorrar
		}
		return r2.Borrar(ctx, c.Clave)
	case DestinoDisco:
		err := os.Remove(filepath.Join(s.Dir, filepath.Base(c.Clave)))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return errNoSePuedeBorrar
}

// limpiarCortadas marca con error las copias que figuran «haciendo» si nadie está haciendo una
// (se cortaron al apagarse el servidor).
func (s *Servicio) limpiarCortadas(ctx context.Context) {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(candado)).Scan(&ok); err != nil || !ok {
		return
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(candado)) }()
	_, _ = conn.Exec(ctx, `UPDATE copias SET estado='error', error='se cortó (se apagó o se reinició el servidor)', terminado=now() WHERE estado='haciendo'`)
}

// HoyToca dice si ya es hora de la copia de hoy y no se hizo (ni se intentó). Devuelve el día.
func (s *Servicio) HoyToca(ctx context.Context) (string, bool, error) {
	cfg := s.Config()
	if !cfg.CopiasActivas {
		return "", false, nil
	}
	ahora := s.ahora().In(Zona)
	h, m := cfg.HoraDeCopias()
	hora := time.Date(ahora.Year(), ahora.Month(), ahora.Day(), h, m, 0, 0, Zona)
	dia := ahora.Format("2006-01-02")
	if ahora.Before(hora) {
		return dia, false, nil
	}
	var hay bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM copias WHERE programada=$1)`, dia).Scan(&hay); err != nil {
		return dia, false, err
	}
	return dia, !hay, nil
}

// Programar mira cada minuto si toca la copia diaria y, si toca, la encola (una por día aunque
// haya varios procesos: el trabajo es único por día). Vuelve cuando ctx se cancela.
func (s *Servicio) Programar(ctx context.Context) {
	if s.Encolar == nil {
		return
	}
	s.limpiarCortadas(ctx)
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if dia, toca, err := s.HoyToca(ctx); err != nil {
			if ctx.Err() == nil {
				s.log().Warn("no se pudo ver si toca la copia de hoy", "error", err)
			}
		} else if toca {
			if err := s.Encolar(ctx, Args{Programada: dia}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil && ctx.Err() == nil {
				s.log().Warn("no se pudo encolar la copia de hoy", "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// ---------- Trabajo de River ----------

// Args es el trabajo de una copia. Las diarias son únicas por día.
type Args struct {
	Programada string `json:"programada,omitempty" river:"unique"`
	Por        string `json:"por,omitempty"`
}

func (Args) Kind() string { return "copia_base" }

func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "mantenimiento", MaxAttempts: 3}
}

// Worker hace la copia.
type Worker struct {
	river.WorkerDefaults[Args]
	S *Servicio
}

func (w *Worker) Timeout(*river.Job[Args]) time.Duration { return 3 * time.Hour }

func (w *Worker) Work(ctx context.Context, j *river.Job[Args]) error {
	_, err := w.S.Hacer(ctx, j.Args.Por, j.Args.Programada == "", j.Args.Programada)
	return err
}
