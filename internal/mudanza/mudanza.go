// Package mudanza pasa a R2 los objetos que quedaron guardados en el disco (los de antes de activar
// R2). Los objetos ya están comprimidos y cifrados: se suben tal cual, sin abrirlos.
//
// Cada objeto se anota en r2_objetos y se marca verificado recién cuando R2 devuelve el mismo
// tamaño y el mismo SHA-256 que el archivo del disco. Por eso es reanudable e idempotente: si se
// corta, lo verificado no se vuelve a subir y lo que quedó a medias se vuelve a mirar (si ya está
// en R2 y coincide, no se sube de nuevo). Nada se borra del disco hasta que alguien pide «Liberar el
// disco», y entonces solo lo verificado, después de volver a mirar que siga en R2.
//
// Lo corre un trabajo de River (del servidor) o «factorum r2 migrar» (por consola). Un candado de
// Postgres asegura que haya uno solo a la vez, aunque haya varios procesos.
package mudanza

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/diegoparras/factorum/internal/almacen"
)

// Estados del pase (r2_migracion.estado). "" = nunca se empezó.
const (
	Corriendo = "corriendo"
	Pausada   = "pausada"
	Terminada = "terminada"
	Liberando = "liberando"
	Liberada  = "liberada"
	ConError  = "error"
)

// ConfirmacionLiberar es lo que hay que escribir para liberar el disco.
const ConfirmacionLiberar = "liberar"

// candado es la clave del candado de Postgres (pg_advisory_lock) del pase.
const candado = 84120

var (
	// ErrOcupado: otro proceso ya está trabajando (el servidor o la consola).
	ErrOcupado = errors.New("ya hay otro proceso pasando los objetos a R2")
	// ErrSigue: se cumplió el tramo; hay que seguir en otro trabajo.
	ErrSigue = errors.New("se cumplió el tramo: sigue en otro trabajo")
	// ErrSinR2: R2 no está activo.
	ErrSinR2 = errors.New("R2 no está activo: configuralo y guardá antes de pasar lo del disco")
	// ErrNoSePuede: lo pedido no tiene sentido en el estado actual (el mensaje dice por qué).
	ErrNoSePuede = errors.New("no se puede")
)

var errNoEnDisco = errors.New("ya no está en el disco")

// Servicio pasa lo del disco a R2.
type Servicio struct {
	Pool  *pgxpool.Pool
	Disco *almacen.Disco
	R2    func() *almacen.S3 // el R2 vigente (nil = no está activo)
	// Encolar arranca el trabajo en segundo plano (nil = lo corre quien llama a Correr, la consola).
	Encolar func(ctx context.Context) error
	// Auditar anota el fin del pase y de la liberación (lo de las personas lo anota la API).
	Auditar func(ctx context.Context, accion string, detalle map[string]any)
	Log     *slog.Logger

	Concurrencia int           // objetos a la vez (por defecto 4)
	Reintentos   int           // intentos por objeto (por defecto 3)
	Espera       time.Duration // espera antes del segundo intento; se duplica en cada uno (por defecto 1 s)
	Tramo        time.Duration // cuánto corre un trabajo antes de pasarle la posta a otro (0 = sin límite)
	Intervalo    time.Duration // cada cuánto se fija si lo pausaron (por defecto 1 s)
}

func (s *Servicio) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Servicio) auditar(ctx context.Context, accion string, d map[string]any) {
	if s.Auditar != nil {
		s.Auditar(context.WithoutCancel(ctx), accion, d)
	}
}

// Cuenta son objetos y bytes.
type Cuenta struct {
	Objetos int64 `json:"objetos"`
	Bytes   int64 `json:"bytes"`
}

// ErrorObjeto es un objeto que no se pudo pasar.
type ErrorObjeto struct {
	Clave    string `json:"clave"`
	Error    string `json:"error"`
	Intentos int    `json:"intentos"`
}

// Estado es lo que muestra la pantalla (y la consola).
type Estado struct {
	Estado      string     `json:"estado"`
	Por         string     `json:"por,omitempty"`
	Iniciada    *time.Time `json:"iniciada,omitempty"`
	Terminada   *time.Time `json:"terminada,omitempty"`
	Actualizado *time.Time `json:"actualizado,omitempty"`
	Mensaje     string     `json:"mensaje,omitempty"`
	Destino     string     `json:"destino,omitempty"`

	Total      Cuenta `json:"total"`
	Hechos     Cuenta `json:"hechos"` // verificados (incluidos los liberados)
	Subidos    Cuenta `json:"subidos"`
	Pendientes Cuenta `json:"pendientes"`
	Errores    Cuenta `json:"errores"`
	Liberados  Cuenta `json:"liberados"`
	// Verificados que siguen en el disco: lo que liberaría «Liberar el disco».
	ParaLiberar Cuenta `json:"para_liberar"`

	EstimadoSeg  *int64        `json:"estimado_seg,omitempty"` // tiempo estimado para terminar
	Trabajando   bool          `json:"trabajando"`             // hay un proceso con el candado
	R2Activo     bool          `json:"r2_activo"`
	PuedeLiberar bool          `json:"puede_liberar"`
	UltErrores   []ErrorObjeto `json:"ultimos_errores,omitempty"`
}

type fila struct {
	estado, destino, por, mensaje string
	iniciada, actualizado         time.Time
	terminada                     *time.Time
	tramoInicio                   time.Time
	tramoBytes                    int64
}

func (s *Servicio) leer(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (*fila, error) {
	var f fila
	err := q.QueryRow(ctx, `SELECT estado, destino, por, mensaje, iniciada, actualizado, terminada, tramo_inicio, tramo_bytes FROM r2_migracion WHERE id=1`).
		Scan(&f.estado, &f.destino, &f.por, &f.mensaje, &f.iniciada, &f.actualizado, &f.terminada, &f.tramoInicio, &f.tramoBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		return &fila{}, nil
	}
	return &f, err
}

// Estado arma el avance.
func (s *Servicio) Estado(ctx context.Context) (*Estado, error) {
	f, err := s.leer(ctx, s.Pool)
	if err != nil {
		return nil, err
	}
	e := &Estado{Estado: f.estado, Por: f.por, Mensaje: f.mensaje, Destino: f.destino}
	if f.estado != "" {
		e.Iniciada, e.Actualizado, e.Terminada = &f.iniciada, &f.actualizado, f.terminada
	}
	rows, err := s.Pool.Query(ctx, `SELECT estado, subido, count(*), coalesce(sum(tamano),0) FROM r2_objetos GROUP BY estado, subido`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var est string
		var sub bool
		var c Cuenta
		if err := rows.Scan(&est, &sub, &c.Objetos, &c.Bytes); err != nil {
			rows.Close()
			return nil, err
		}
		sumar := func(d *Cuenta) { d.Objetos += c.Objetos; d.Bytes += c.Bytes }
		sumar(&e.Total)
		switch est {
		case "verificado":
			sumar(&e.Hechos)
			sumar(&e.ParaLiberar)
		case "liberado":
			sumar(&e.Hechos)
			sumar(&e.Liberados)
		case "pendiente":
			sumar(&e.Pendientes)
		case "error":
			sumar(&e.Errores)
		}
		if sub && (est == "verificado" || est == "liberado") {
			sumar(&e.Subidos)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if e.Errores.Objetos > 0 {
		rs, err := s.Pool.Query(ctx, `SELECT clave, error, intentos FROM r2_objetos WHERE estado='error' ORDER BY actualizado DESC LIMIT 5`)
		if err != nil {
			return nil, err
		}
		for rs.Next() {
			var x ErrorObjeto
			if err := rs.Scan(&x.Clave, &x.Error, &x.Intentos); err != nil {
				rs.Close()
				return nil, err
			}
			e.UltErrores = append(e.UltErrores, x)
		}
		rs.Close()
	}
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid=$1 AND objsubid=1 AND granted)`, candado).Scan(&e.Trabajando); err != nil {
		return nil, err
	}
	r2 := s.r2()
	e.R2Activo = r2 != nil
	e.PuedeLiberar = e.R2Activo && (f.estado == Terminada || f.estado == Liberada) && e.ParaLiberar.Objetos > 0 && f.destino == r2.Destino()
	// Tiempo estimado: lo que falta al ritmo de este tramo (desde que se empezó o se siguió).
	if f.estado == Corriendo && e.Pendientes.Bytes > 0 {
		dur := time.Since(f.tramoInicio).Seconds()
		hecho := float64(e.Hechos.Bytes - f.tramoBytes)
		if dur > 2 && hecho > 0 {
			seg := int64(float64(e.Pendientes.Bytes)/(hecho/dur)) + 1
			e.EstimadoSeg = &seg
		}
	}
	return e, nil
}

func (s *Servicio) r2() *almacen.S3 {
	if s.R2 == nil {
		return nil
	}
	return s.R2()
}

// Iniciar empieza el pase (o lo vuelve a empezar si ya había terminado: lo nuevo del disco se suma y
// lo verificado no se vuelve a subir).
func (s *Servicio) Iniciar(ctx context.Context, quien string) error {
	return s.arrancar(ctx, quien, true)
}

// Seguir sigue un pase pausado, con error o terminado con objetos que fallaron (se reintentan).
func (s *Servicio) Seguir(ctx context.Context, quien string) error {
	return s.arrancar(ctx, quien, false)
}

func (s *Servicio) arrancar(ctx context.Context, quien string, nuevo bool) error {
	r2 := s.r2()
	if r2 == nil {
		return ErrSinR2
	}
	destino := r2.Destino()
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE r2_migracion IN EXCLUSIVE MODE`); err != nil {
			return err
		}
		f, err := s.leer(ctx, tx)
		if err != nil {
			return err
		}
		switch f.estado {
		case Corriendo:
			return fmt.Errorf("%w: ya está corriendo", ErrNoSePuede)
		case Liberando:
			return fmt.Errorf("%w: se está liberando el disco; esperá a que termine", ErrNoSePuede)
		}
		if f.estado != "" && f.destino != destino {
			var liberados int64
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM r2_objetos WHERE estado='liberado'`).Scan(&liberados); err != nil {
				return err
			}
			if liberados > 0 {
				return fmt.Errorf("%w: R2 apunta a otro bucket o prefijo que cuando se liberó el disco (%d objetos están solo en %s). Volvé a poner ese destino", ErrNoSePuede, liberados, f.destino)
			}
			// Otro destino: lo verificado era en el otro bucket.
			if _, err := tx.Exec(ctx, `UPDATE r2_objetos SET estado='pendiente', intentos=0, error='', subido=false, actualizado=now() WHERE estado<>'pendiente'`); err != nil {
				return err
			}
		}
		// Lo que falló se vuelve a intentar.
		if _, err := tx.Exec(ctx, `UPDATE r2_objetos SET estado='pendiente', intentos=0, error='', actualizado=now() WHERE estado='error'`); err != nil {
			return err
		}
		var hechos int64
		if err := tx.QueryRow(ctx, `SELECT coalesce(sum(tamano),0) FROM r2_objetos WHERE estado IN ('verificado','liberado')`).Scan(&hechos); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO r2_migracion (id, estado, destino, por, iniciada, tramo_inicio, tramo_bytes, mensaje, actualizado)
			VALUES (1, 'corriendo', $1, $2, now(), now(), $3, '', now())
			ON CONFLICT (id) DO UPDATE SET estado='corriendo', destino=$1, tramo_inicio=now(), tramo_bytes=$3, mensaje='', actualizado=now(),
				terminada=NULL, por=CASE WHEN $4 THEN $2 ELSE r2_migracion.por END, iniciada=CASE WHEN $4 THEN now() ELSE r2_migracion.iniciada END`,
			destino, quien, hechos, nuevo || f.estado == "")
		return err
	})
	if err != nil {
		return err
	}
	return s.encolar(ctx)
}

func (s *Servicio) encolar(ctx context.Context) error {
	if s.Encolar == nil {
		return nil
	}
	return s.Encolar(ctx)
}

// Pausar frena el pase: el trabajo termina el objeto que está pasando y se detiene.
func (s *Servicio) Pausar(ctx context.Context) error {
	t, err := s.Pool.Exec(ctx, `UPDATE r2_migracion SET estado='pausada', mensaje='', actualizado=now() WHERE id=1 AND estado='corriendo'`)
	if err != nil {
		return err
	}
	if t.RowsAffected() == 0 {
		return fmt.Errorf("%w: no está corriendo", ErrNoSePuede)
	}
	return nil
}

// Liberar borra del disco lo verificado en R2. confirmacion tiene que ser «liberar».
func (s *Servicio) Liberar(ctx context.Context, confirmacion string) error {
	if confirmacion != ConfirmacionLiberar {
		return fmt.Errorf("%w: para liberar el disco hay que escribir «%s»", ErrNoSePuede, ConfirmacionLiberar)
	}
	e, err := s.Estado(ctx)
	if err != nil {
		return err
	}
	if !e.R2Activo {
		return ErrSinR2
	}
	if !e.PuedeLiberar {
		if e.Estado != Terminada && e.Estado != Liberada {
			return fmt.Errorf("%w: primero tiene que terminar el pase a R2", ErrNoSePuede)
		}
		if e.Destino != s.r2().Destino() {
			return fmt.Errorf("%w: R2 apunta a otro bucket o prefijo que el del pase: volvé a pasar lo del disco", ErrNoSePuede)
		}
		return fmt.Errorf("%w: no hay nada verificado en R2 que siga en el disco", ErrNoSePuede)
	}
	t, err := s.Pool.Exec(ctx, `UPDATE r2_migracion SET estado='liberando', mensaje='', tramo_inicio=now(), actualizado=now() WHERE id=1 AND estado IN ('terminada','liberada')`)
	if err != nil {
		return err
	}
	if t.RowsAffected() == 0 {
		return fmt.Errorf("%w: cambió el estado del pase; recargá", ErrNoSePuede)
	}
	return s.encolar(ctx)
}

// Reanudar encola el trabajo si el pase quedó corriendo (se reinició el servidor en el medio).
func (s *Servicio) Reanudar(ctx context.Context) error {
	f, err := s.leer(ctx, s.Pool)
	if err != nil {
		return err
	}
	if f.estado == Corriendo || f.estado == Liberando {
		s.log().Info("se sigue con el pase a R2 que quedó a medias", "estado", f.estado)
		return s.encolar(ctx)
	}
	return nil
}

func (s *Servicio) marcar(ctx context.Context, estado, mensaje string) error {
	terminada := estado == Terminada || estado == Liberada
	_, err := s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE r2_migracion SET estado=$1, mensaje=$2, actualizado=now(), terminada=CASE WHEN $3 THEN now() ELSE terminada END WHERE id=1`, estado, mensaje, terminada)
	return err
}

// Correr hace lo que diga el estado: pasar (corriendo) o liberar (liberando). Vuelve con nil cuando
// termina o lo pausan, con ErrSigue si se cumplió el tramo, con ErrOcupado si otro proceso tiene el
// candado y con el error del contexto si lo cortan. avance (opcional) recibe el estado cada tanto.
func (s *Servicio) Correr(ctx context.Context, avance func(*Estado)) error {
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	var ok bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, int64(candado)).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrOcupado
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, int64(candado))
	}()
	f, err := s.leer(ctx, s.Pool)
	if err != nil {
		return err
	}
	switch f.estado {
	case Corriendo:
		return s.pasar(ctx, f, avance)
	case Liberando:
		return s.liberar(ctx, f, avance)
	}
	return nil
}

// vigilar cancela el contexto si el estado deja de ser el esperado (alguien tocó Pausar).
func (s *Servicio) vigilar(ctx context.Context, esperado string) (context.Context, func()) {
	cctx, cancel := context.WithCancel(ctx)
	iv := s.Intervalo
	if iv <= 0 {
		iv = time.Second
	}
	go func() {
		t := time.NewTicker(iv)
		defer t.Stop()
		for {
			select {
			case <-cctx.Done():
				return
			case <-t.C:
				if f, err := s.leer(cctx, s.Pool); err == nil && f.estado != esperado {
					cancel()
					return
				}
			}
		}
	}()
	return cctx, cancel
}

func (s *Servicio) destinoVigente(ctx context.Context, f *fila) (*almacen.S3, bool) {
	r2 := s.r2()
	if r2 == nil {
		_ = s.marcar(ctx, ConError, "R2 dejó de estar activo: volvé a activarlo y tocá Seguir")
		return nil, false
	}
	if r2.Destino() != f.destino {
		_ = s.marcar(ctx, ConError, "R2 apunta a otro bucket o prefijo que cuando se empezó: tocá Seguir para pasar todo al nuevo")
		return nil, false
	}
	return r2, true
}

// inventario anota lo que hay en el disco. Lo que ya estaba anotado no cambia, salvo lo liberado que
// volvió a aparecer (se guardó en disco con R2 apagado): se vuelve a pasar.
func (s *Servicio) inventario(ctx context.Context) error {
	var claves []string
	var tams []int64
	volcar := func() error {
		if len(claves) == 0 {
			return nil
		}
		_, err := s.Pool.Exec(ctx, `INSERT INTO r2_objetos (clave, tamano) SELECT * FROM unnest($1::text[], $2::bigint[])
			ON CONFLICT (clave) DO UPDATE SET estado='pendiente', tamano=EXCLUDED.tamano, intentos=0, error='', subido=false, actualizado=now()
			WHERE r2_objetos.estado='liberado'`, claves, tams)
		claves, tams = claves[:0], tams[:0]
		return err
	}
	err := s.Disco.Recorrer(func(clave string, n int64) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		claves, tams = append(claves, clave), append(tams, n)
		if len(claves) == 1000 {
			return volcar()
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("recorriendo el disco: %w", err)
	}
	return volcar()
}

type resultado struct {
	clave  string
	sha    string
	subido bool
	err    error
}

func (s *Servicio) pasar(ctx context.Context, f *fila, avance func(*Estado)) error {
	r2, ok := s.destinoVigente(ctx, f)
	if !ok {
		return nil
	}
	padre := ctx
	ctx, cancel := s.vigilar(ctx, Corriendo)
	defer cancel()
	if err := s.inventario(ctx); err != nil {
		if padre.Err() != nil {
			return padre.Err()
		}
		if ctx.Err() != nil {
			return nil // pausado mientras se contaba
		}
		return err
	}
	inicio := time.Now()
	conc := max(1, s.Concurrencia)
	if s.Concurrencia == 0 {
		conc = 4
	}
	for {
		if padre.Err() != nil {
			return padre.Err()
		}
		if ctx.Err() != nil {
			return nil // pausado
		}
		if s.Tramo > 0 && time.Since(inicio) > s.Tramo {
			return ErrSigue
		}
		rows, err := s.Pool.Query(ctx, `SELECT clave, tamano FROM r2_objetos WHERE estado='pendiente' ORDER BY clave LIMIT 100`)
		if err != nil {
			if ctx.Err() != nil {
				continue
			}
			return err
		}
		type obj struct {
			clave string
			tam   int64
		}
		var lote []obj
		for rows.Next() {
			var o obj
			if err := rows.Scan(&o.clave, &o.tam); err != nil {
				rows.Close()
				return err
			}
			lote = append(lote, o)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			if ctx.Err() != nil {
				continue
			}
			return err
		}
		if len(lote) == 0 {
			return s.terminarPase(ctx, avance)
		}
		res := make(chan resultado, len(lote))
		sem := make(chan struct{}, conc)
		var wg sync.WaitGroup
		for _, o := range lote {
			if ctx.Err() != nil {
				break
			}
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				sha, subido, err := s.moverConReintentos(ctx, r2, o.clave)
				res <- resultado{clave: o.clave, sha: sha, subido: subido, err: err}
			}()
		}
		wg.Wait()
		close(res)
		for r := range res {
			if err := s.anotar(padre, r); err != nil {
				return err
			}
		}
		if avance != nil {
			if e, err := s.Estado(padre); err == nil {
				avance(e)
			}
		}
	}
}

func (s *Servicio) terminarPase(ctx context.Context, avance func(*Estado)) error {
	e, err := s.Estado(ctx)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Listo: %d objetos verificados en R2 (%d subidos, %d ya estaban).", e.Hechos.Objetos, e.Subidos.Objetos, e.Hechos.Objetos-e.Subidos.Objetos)
	if e.Errores.Objetos > 0 {
		msg = fmt.Sprintf("Terminó con %d objetos que no se pudieron pasar; los demás (%d) están verificados en R2. Tocá Seguir para reintentar los que fallaron.", e.Errores.Objetos, e.Hechos.Objetos)
	}
	// Solo si nadie lo pausó justo ahora.
	t, err := s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE r2_migracion SET estado='terminada', mensaje=$1, terminada=now(), actualizado=now() WHERE id=1 AND estado='corriendo'`, msg)
	if err != nil {
		return err
	}
	if t.RowsAffected() > 0 {
		s.auditar(ctx, "r2.migrar_fin", map[string]any{"objetos": e.Hechos.Objetos, "bytes": e.Hechos.Bytes, "subidos": e.Subidos.Objetos, "errores": e.Errores.Objetos})
		s.log().Info("pase a R2 terminado", "objetos", e.Hechos.Objetos, "subidos", e.Subidos.Objetos, "errores", e.Errores.Objetos)
	}
	if avance != nil {
		if e, err := s.Estado(ctx); err == nil {
			avance(e)
		}
	}
	return nil
}

// anotar guarda el resultado de un objeto. Si se cortó en el medio (pausa o apagado), queda pendiente.
func (s *Servicio) anotar(ctx context.Context, r resultado) error {
	c := context.WithoutCancel(ctx)
	switch {
	case r.err == nil:
		_, err := s.Pool.Exec(c, `UPDATE r2_objetos SET estado='verificado', sha256=$2, subido=$3, error='', actualizado=now() WHERE clave=$1`, r.clave, r.sha, r.subido)
		return err
	case errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded):
		return nil
	default:
		msg := r.err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		_, err := s.Pool.Exec(c, `UPDATE r2_objetos SET estado='error', error=$2, intentos=intentos+1, actualizado=now() WHERE clave=$1`, r.clave, msg)
		return err
	}
}

func (s *Servicio) moverConReintentos(ctx context.Context, r2 *almacen.S3, clave string) (string, bool, error) {
	n := s.Reintentos
	if n <= 0 {
		n = 3
	}
	espera := s.Espera
	if espera <= 0 {
		espera = time.Second
	}
	var err error
	for i := 0; i < n; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return "", false, ctx.Err()
			case <-time.After(espera << (i - 1)):
			}
		}
		var sha string
		var subido bool
		if sha, subido, err = s.mover(ctx, r2, clave); err == nil {
			return sha, subido, nil
		}
		if ctx.Err() != nil {
			return "", false, ctx.Err()
		}
		if errors.Is(err, errNoEnDisco) {
			break
		}
		s.log().Warn("no se pudo pasar un objeto a R2: se reintenta", "clave", clave, "intento", i+1, "error", err)
	}
	return "", false, err
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// mover sube un objeto si hace falta y lo verifica: el tamaño (HEAD) y el SHA-256 de lo que quedó.
func (s *Servicio) mover(ctx context.Context, r2 *almacen.S3, clave string) (string, bool, error) {
	datos, err := s.Disco.Traer(ctx, clave)
	if errors.Is(err, almacen.ErrNoExiste) {
		return "", false, errNoEnDisco
	}
	if err != nil {
		return "", false, fmt.Errorf("leyendo del disco: %w", err)
	}
	suma := sha(datos)
	coincide := func() (bool, error) {
		n, err := r2.Tamano(ctx, clave)
		if errors.Is(err, almacen.ErrNoExiste) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if n != int64(len(datos)) {
			return false, nil
		}
		remoto, err := r2.Traer(ctx, clave)
		if err != nil {
			return false, err
		}
		return bytes.Equal(remoto, datos) || sha(remoto) == suma, nil
	}
	// ¿Ya estaba (de una corrida anterior que se cortó antes de anotarlo)?
	if ok, err := coincide(); err != nil {
		return "", false, fmt.Errorf("mirando R2: %w", err)
	} else if ok {
		return suma, false, nil
	}
	if err := r2.Poner(ctx, clave, datos); err != nil {
		return "", false, fmt.Errorf("subiendo: %w", err)
	}
	ok, err := coincide()
	if err != nil {
		return "", false, fmt.Errorf("verificando: %w", err)
	}
	if !ok {
		return "", false, errors.New("lo que quedó en R2 no coincide con el disco (tamaño o SHA-256 distinto)")
	}
	return suma, true, nil
}

func (s *Servicio) liberar(ctx context.Context, f *fila, avance func(*Estado)) error {
	r2, ok := s.destinoVigente(ctx, f)
	if !ok {
		return nil
	}
	conc := max(1, s.Concurrencia)
	if s.Concurrencia == 0 {
		conc = 4
	}
	inicio := time.Now()
	var faltan int64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if s.Tramo > 0 && time.Since(inicio) > s.Tramo {
			return ErrSigue
		}
		rows, err := s.Pool.Query(ctx, `SELECT clave, tamano FROM r2_objetos WHERE estado='verificado' ORDER BY clave LIMIT 200`)
		if err != nil {
			return err
		}
		var claves []string
		var tams []int64
		for rows.Next() {
			var k string
			var n int64
			if err := rows.Scan(&k, &n); err != nil {
				rows.Close()
				return err
			}
			claves, tams = append(claves, k), append(tams, n)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(claves) == 0 {
			break
		}
		var mu sync.Mutex
		var primerErr error
		sem := make(chan struct{}, conc)
		var wg sync.WaitGroup
		for i := range claves {
			sem <- struct{}{}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				err := s.liberarUno(ctx, r2, claves[i], tams[i])
				mu.Lock()
				defer mu.Unlock()
				switch {
				case errors.Is(err, errNoEnR2):
					faltan++
				case err != nil && primerErr == nil:
					primerErr = err
				}
			}()
		}
		wg.Wait()
		if primerErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			_ = s.marcar(ctx, ConError, "No se pudo terminar de liberar el disco: "+primerErr.Error()+". Tocá Seguir y después Liberar otra vez.")
			return nil
		}
		if avance != nil {
			if e, err := s.Estado(ctx); err == nil {
				avance(e)
			}
		}
	}
	e, err := s.Estado(ctx)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("Disco liberado: %d objetos están solo en R2.", e.Liberados.Objetos)
	if faltan > 0 {
		msg += fmt.Sprintf(" %d no estaban bien en R2 y quedaron en el disco: tocá Seguir para volver a pasarlos.", faltan)
	}
	if err := s.marcar(ctx, Liberada, msg); err != nil {
		return err
	}
	s.auditar(ctx, "r2.liberar_fin", map[string]any{"objetos": e.Liberados.Objetos, "bytes": e.Liberados.Bytes, "no_estaban": faltan})
	s.log().Info("disco liberado", "objetos", e.Liberados.Objetos, "no_estaban", faltan)
	if avance != nil {
		if e, err := s.Estado(ctx); err == nil {
			avance(e)
		}
	}
	return nil
}

var errNoEnR2 = errors.New("no está en R2")

// liberarUno vuelve a mirar que el objeto esté en R2 con su tamaño y recién ahí lo borra del disco.
func (s *Servicio) liberarUno(ctx context.Context, r2 *almacen.S3, clave string, tam int64) error {
	n, err := r2.Tamano(ctx, clave)
	if errors.Is(err, almacen.ErrNoExiste) || (err == nil && n != tam) {
		_, err := s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE r2_objetos SET estado='pendiente', error='al liberar no estaba bien en R2', actualizado=now() WHERE clave=$1`, clave)
		if err != nil {
			return err
		}
		return errNoEnR2
	}
	if err != nil {
		return fmt.Errorf("mirando R2: %w", err)
	}
	if err := s.Disco.Borrar(ctx, clave); err != nil {
		return fmt.Errorf("borrando del disco: %w", err)
	}
	_, err = s.Pool.Exec(context.WithoutCancel(ctx), `UPDATE r2_objetos SET estado='liberado', actualizado=now() WHERE clave=$1`, clave)
	return err
}
