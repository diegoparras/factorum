// Package app es el orquestador: recibe archivos (web, chat, MCP), los guarda en el almacén, los
// encola, los procesa con el pipeline, los constata con Arcanum y avisa por el canal de origen.
package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/pipeline"
	"github.com/diegoparras/factorum/internal/rubro"
	"github.com/diegoparras/factorum/internal/store"
	"github.com/diegoparras/factorum/internal/validar"
)

type App struct {
	Cfg      *config.Config // la configuración del arranque; la vigente la da Config()
	Store    *store.Store
	Almacen  *almacen.Almacen
	Pipeline *pipeline.Pipeline
	River    *river.Client[pgx.Tx]
	Log      *slog.Logger
	// Trabajos registra los trabajos de otros paquetes (el pase a R2 y las copias de la base) antes
	// de arrancar la cola. Van a la cola ColaMantenimiento.
	Trabajos func(ws *river.Workers)

	Reloj     func() time.Time // para las pruebas (nil = la hora de verdad)
	TLSCorreo *tls.Config      // para las pruebas: en qué certificados confiar al hablar con el correo
	// GoogleURLs cambia las direcciones de Google (OAuth y la Gmail API) en las pruebas.
	GoogleURLs correo.Google
	// Calma es cuánto se espera sin archivos nuevos antes de mandar los avisos por mail de un lote
	// (10 segundos; en las pruebas, menos).
	Calma time.Duration

	cfg atomic.Pointer[config.Config]   // la vigente, si se cambió desde la pantalla
	arc atomic.Pointer[arcanum.Cliente] // nil = sin Arcanum

	mu      sync.RWMutex
	canales map[string]canal.Canal

	padron sync.Map // cuit -> razón social

	recibidos cacheRecibidos // lo que trajo Arcanum para conciliar, unos minutos por CUIT y rango
}

// Config es la configuración vigente. Se cambia en caliente (Configuración): cada llamado puede
// devolver otra, así que conviene tomarla una vez por operación.
func (a *App) Config() *config.Config {
	if c := a.cfg.Load(); c != nil {
		return c
	}
	return a.Cfg
}

// SetConfig reemplaza la configuración vigente.
func (a *App) SetConfig(c *config.Config) { a.cfg.Store(c) }

// Arcanum es el cliente de Arcanum vigente (nil = sin Arcanum).
func (a *App) Arcanum() *arcanum.Cliente { return a.arc.Load() }

// SetArcanum cambia el cliente de Arcanum (nil lo apaga).
func (a *App) SetArcanum(c *arcanum.Cliente) { a.arc.Store(c) }

// ---------- Trabajos de la cola ----------

type ProcesarArgs struct {
	DocumentoID int64 `json:"documento_id"`
}

func (ProcesarArgs) Kind() string { return "procesar_documento" }

type ConstatarArgs struct {
	LoteID int64 `json:"lote_id"`
	// Comprobantes, si viene, limita a esos (se cambió el cliente de un lote que no se constata
	// entero: se vuelven a constatar solo los que ya se habían constatado).
	Comprobantes []int64 `json:"comprobantes,omitempty"`
}

func (ConstatarArgs) Kind() string { return "constatar_lote" }
func (ConstatarArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "arcanum"}
}

type NotificarArgs struct {
	LoteID int64 `json:"lote_id"`
}

func (NotificarArgs) Kind() string { return "notificar_lote" }

type procesarWorker struct {
	river.WorkerDefaults[ProcesarArgs]
	a *App
}

func (w *procesarWorker) Timeout(*river.Job[ProcesarArgs]) time.Duration { return 15 * time.Minute }
func (w *procesarWorker) Work(ctx context.Context, j *river.Job[ProcesarArgs]) error {
	return w.a.procesar(ctx, j.Args.DocumentoID, j.Attempt >= j.MaxAttempts)
}

type constatarWorker struct {
	river.WorkerDefaults[ConstatarArgs]
	a *App
}

func (w *constatarWorker) Timeout(*river.Job[ConstatarArgs]) time.Duration { return 30 * time.Minute }
func (w *constatarWorker) Work(ctx context.Context, j *river.Job[ConstatarArgs]) error {
	return w.a.constatarLote(ctx, j.Args.LoteID, j.Args.Comprobantes)
}

type notificarWorker struct {
	river.WorkerDefaults[NotificarArgs]
	a *App
}

func (w *notificarWorker) Work(ctx context.Context, j *river.Job[NotificarArgs]) error {
	return w.a.notificar(ctx, j.Args.LoteID)
}

// ColaMantenimiento es la cola del pase a R2 y de las copias de la base.
const ColaMantenimiento = "mantenimiento"

// IniciarCola migra las tablas de River, registra los trabajos y arranca los workers.
func (a *App) IniciarCola(ctx context.Context) error {
	drv := riverpgxv5.New(a.Store.Pool)
	mig, err := rivermigrate.New(drv, nil)
	if err != nil {
		return err
	}
	if _, err := mig.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrando la cola: %w", err)
	}
	ws := river.NewWorkers()
	river.AddWorker(ws, &procesarWorker{a: a})
	river.AddWorker(ws, &constatarWorker{a: a})
	river.AddWorker(ws, &notificarWorker{a: a})
	river.AddWorker(ws, &avisoCorreoWorker{a: a})
	river.AddWorker(ws, &semanalWorker{a: a})
	river.AddWorker(ws, &purgarWorker{a: a})
	if a.Trabajos != nil {
		a.Trabajos(ws)
	}
	a.River, err = river.NewClient(drv, &river.Config{
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(15*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) { return SemanalArgs{}, nil }, &river.PeriodicJobOpts{ID: "resumen_semanal", RunOnStart: true}),
			// El borrado definitivo de lo vencido en la papelera.
			river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return PurgarArgs{}, nil }, &river.PeriodicJobOpts{ID: "purgar_papelera", RunOnStart: true}),
		},
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: a.Config().Workers},
			"arcanum":          {MaxWorkers: 1}, // WSCDC no es para ráfagas
			ColaMantenimiento:  {MaxWorkers: 2}, // el pase a R2 y las copias, sin frenar las facturas
		},
		Workers:     ws,
		MaxAttempts: 5,
		Logger:      a.Log.With("componente", "cola"),
	})
	if err != nil {
		return err
	}
	return a.River.Start(ctx)
}

// ---------- Canales ----------

func (a *App) RegistrarCanal(c canal.Canal) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.canales == nil {
		a.canales = map[string]canal.Canal{}
	}
	a.canales[c.Nombre()] = c
}

// QuitarCanal saca un canal (se apagó desde la configuración).
func (a *App) QuitarCanal(nombre string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.canales, nombre)
}

func (a *App) Canal(nombre string) canal.Canal {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.canales[nombre]
}

func (a *App) Canales() []canal.Estado {
	a.mu.RLock()
	defer a.mu.RUnlock()
	res := []canal.Estado{}
	for _, n := range []string{"telegram", "whatsapp", "webhook"} {
		if c := a.canales[n]; c != nil {
			res = append(res, c.Estado())
		} else {
			res = append(res, canal.Estado{Canal: n})
		}
	}
	return res
}

// ---------- Lotes ----------

type Archivo = canal.Archivo

// Expandir abre los ZIP y descarta lo que no es PDF ni imagen. Devuelve también los descartados.
func Expandir(arch []Archivo, maxBytes int64) (ok []Archivo, descartados []string) {
	for _, f := range arch {
		if int64(len(f.Datos)) > maxBytes {
			descartados = append(descartados, f.Nombre+" (muy grande)")
			continue
		}
		if esZip(f) {
			zr, err := zip.NewReader(bytes.NewReader(f.Datos), int64(len(f.Datos)))
			if err != nil {
				descartados = append(descartados, f.Nombre+" (ZIP ilegible)")
				continue
			}
			var total int64
			for _, zf := range zr.File {
				if zf.FileInfo().IsDir() || strings.HasPrefix(path.Base(zf.Name), ".") || strings.Contains(zf.Name, "__MACOSX") {
					continue
				}
				if int64(zf.UncompressedSize64) > maxBytes {
					descartados = append(descartados, zf.Name+" (muy grande)")
					continue
				}
				total += int64(zf.UncompressedSize64)
				if total > 50*maxBytes { // defensa contra bombas ZIP
					descartados = append(descartados, f.Nombre+" (el ZIP descomprimido es demasiado grande)")
					break
				}
				rc, err := zf.Open()
				if err != nil {
					continue
				}
				b, err := io.ReadAll(io.LimitReader(rc, maxBytes+1))
				rc.Close()
				if err != nil || int64(len(b)) > maxBytes {
					continue
				}
				sub := Archivo{Nombre: path.Base(zf.Name), Datos: b}
				if soportado(sub) {
					ok = append(ok, sub)
				} else {
					descartados = append(descartados, zf.Name)
				}
			}
			continue
		}
		if soportado(f) {
			ok = append(ok, f)
		} else {
			descartados = append(descartados, f.Nombre)
		}
	}
	return ok, descartados
}

func esZip(f Archivo) bool {
	return bytes.HasPrefix(f.Datos, []byte("PK\x03\x04")) || strings.HasSuffix(strings.ToLower(f.Nombre), ".zip")
}

func soportado(f Archivo) bool {
	if bytes.HasPrefix(bytes.TrimLeft(f.Datos[:min(len(f.Datos), 1024)], "\x00\t\r\n "), []byte("%PDF")) {
		return true
	}
	t := http.DetectContentType(f.Datos[:min(len(f.Datos), 512)])
	return t == "image/jpeg" || t == "image/png" || t == "image/webp"
}

func mimeDe(f Archivo) string {
	if f.Mime != "" {
		return f.Mime
	}
	if bytes.HasPrefix(f.Datos, []byte("%PDF")) {
		return "application/pdf"
	}
	return http.DetectContentType(f.Datos[:min(len(f.Datos), 512)])
}

// ResultadoLote es lo que devuelve CrearLote.
type ResultadoLote struct {
	Lote        *store.Lote `json:"lote"`
	Recibidos   int         `json:"recibidos"`
	Repetidos   int         `json:"repetidos"`
	Descartados []string    `json:"descartados"`
}

// CrearLote guarda los archivos y encola su procesamiento. Con ventana > 0 y un chat, reutiliza el
// lote abierto de ese chat (WhatsApp manda los archivos de a uno).
func (a *App) CrearLote(ctx context.Context, n store.LoteNuevo, archivos []Archivo, ventana time.Duration) (*ResultadoLote, error) {
	ok, desc := Expandir(archivos, a.Config().MaxArchivo)
	if len(ok) == 0 {
		if len(desc) > 0 {
			return nil, fmt.Errorf("ningún archivo es un PDF o una imagen: %s", strings.Join(desc, ", "))
		}
		return nil, errors.New("no llegó ningún archivo")
	}
	type guardado struct {
		f   Archivo
		sha string
	}
	var gs []guardado
	var shas []string
	for _, f := range ok {
		s := sha256.Sum256(f.Datos)
		gs = append(gs, guardado{f, hex.EncodeToString(s[:])})
		shas = append(shas, gs[len(gs)-1].sha)
	}
	// El candado de cada archivo, compartido, desde antes de guardarlo hasta que su documento queda en
	// la base: así el borrado definitivo de la papelera no puede borrar del almacén un archivo que se
	// está volviendo a subir (si ya lo borró, Guardar lo vuelve a poner).
	conn, err := a.Store.Pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared($1, hashtext(x)) FROM unnest($2::text[]) x`, store.CandadoArchivo, shas); err != nil {
		return nil, err
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock_all()`) }()
	for _, g := range gs {
		if _, err := a.Almacen.Guardar(ctx, "originales/"+g.sha, g.f.Datos); err != nil {
			return nil, fmt.Errorf("no se pudo guardar %s: %w", g.f.Nombre, err)
		}
	}
	res := &ResultadoLote{Recibidos: len(gs), Descartados: desc}
	var loteID int64
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		var err error
		if ventana > 0 && n.Canal != "" && n.Chat != "" {
			loteID, err = a.Store.LoteAbiertoDeChat(ctx, tx, n.Canal, n.Chat, ventana)
			if err != nil {
				return err
			}
		}
		if loteID == 0 {
			if loteID, err = a.Store.CrearLote(ctx, tx, n); err != nil {
				return err
			}
		}
		var jobs []river.InsertManyParams
		for _, g := range gs {
			id, rep, err := a.Store.CrearDocumento(ctx, tx, loteID, g.f.Nombre, mimeDe(g.f), int64(len(g.f.Datos)), g.sha)
			if err != nil {
				return err
			}
			if rep {
				res.Repetidos++
				continue
			}
			jobs = append(jobs, river.InsertManyParams{Args: ProcesarArgs{DocumentoID: id}})
		}
		if len(jobs) > 0 {
			if _, err := a.River.InsertManyTx(ctx, tx, jobs); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Si todos eran repetidos, el lote ya terminó.
	if cerrado, _ := a.Store.CerrarLoteSiTermino(ctx, loteID); cerrado {
		a.alTerminarLote(ctx, loteID)
	}
	res.Lote, err = a.Store.Lote(ctx, loteID)
	return res, err
}

func (a *App) opciones(l *store.Lote) pipeline.Opciones {
	return pipeline.Opciones{MaxPaginas: a.Config().MaxPaginas, IA: l.IA}
}

// procesar es el trabajo de la cola para un documento.
func (a *App) procesar(ctx context.Context, docID int64, ultimoIntento bool) error {
	doc, err := a.Store.Documento(ctx, docID)
	if errors.Is(err, store.ErrNoEncontrado) {
		return nil // se borró el lote
	}
	if err != nil {
		return err
	}
	if ok, err := a.Store.TomarDocumento(ctx, docID); err != nil || !ok {
		return err
	}
	lote, err := a.Store.Lote(ctx, doc.LoteID)
	if err != nil {
		return err
	}
	datos, err := a.Almacen.Leer(ctx, "originales/"+doc.SHA256)
	if err != nil {
		if ultimoIntento {
			_ = a.Store.ErrorDocumento(ctx, docID, "no se pudo leer el archivo del almacén: "+err.Error())
			a.verificarFinDeLote(ctx, doc.LoteID)
			return nil
		}
		return err
	}
	r := a.Pipeline.Procesar(ctx, doc.Nombre, datos, a.opciones(lote))
	a.enriquecerConPadron(ctx, &r)
	a.aplicarRubrosGuardados(ctx, &r)
	if j, err := json.Marshal(r); err == nil {
		if _, err := a.Almacen.Guardar(ctx, "resultados/"+doc.SHA256+".json", j); err != nil {
			a.Log.Warn("no se pudo guardar el resultado en el almacén", "documento", docID, "error", err)
		}
	}
	if _, err := a.Store.GuardarResultado(ctx, doc, &r); err != nil {
		if errors.Is(err, store.ErrNoEncontrado) {
			return nil // se fue a la papelera mientras se leía: si se restaura, se vuelve a procesar
		}
		return err
	}
	a.verificarFinDeLote(ctx, doc.LoteID)
	return nil
}

func (a *App) verificarFinDeLote(ctx context.Context, loteID int64) {
	if cerrado, err := a.Store.CerrarLoteSiTermino(ctx, loteID); err == nil && cerrado {
		a.alTerminarLote(ctx, loteID)
	}
}

func (a *App) alTerminarLote(ctx context.Context, loteID int64) {
	l, err := a.Store.Lote(ctx, loteID)
	if err != nil {
		return
	}
	if l.Constatar && a.Arcanum() != nil {
		if _, err := a.River.Insert(ctx, ConstatarArgs{LoteID: loteID}, nil); err != nil {
			a.Log.Error("no se pudo encolar la constatación", "lote", loteID, "error", err)
		}
		return // avisa la constatación al terminar
	}
	a.encolarAviso(ctx, l)
}

func (a *App) encolarAviso(ctx context.Context, l *store.Lote) {
	if a.hayAvisosDeLote(l) {
		if _, err := a.River.Insert(ctx, AvisoCorreoArgs{LoteID: l.ID}, &river.InsertOpts{ScheduledAt: time.Now().Add(a.calma() + 2*time.Second)}); err != nil {
			a.Log.Error("no se pudo encolar el aviso por mail", "lote", l.ID, "error", err)
		}
	}
	if l.Canal == "" || l.Chat == "" || l.Canal == "correo" {
		return
	}
	_, err := a.River.Insert(ctx, NotificarArgs{LoteID: l.ID}, &river.InsertOpts{ScheduledAt: time.Now().Add(12 * time.Second)})
	if err != nil {
		a.Log.Error("no se pudo encolar el aviso", "lote", l.ID, "error", err)
	}
}

// enriquecerConPadron completa con el padrón A5 de ARCA (vía Arcanum) la razón social y la condición
// frente al IVA del emisor, si faltan, y el rubro desde su actividad principal (capa 2 del rubro:
// le gana a las palabras clave, pero no a lo que eligió una persona). El padrón se guarda en
// memoria por CUIT.
func (a *App) enriquecerConPadron(ctx context.Context, r *modelo.Resultado) {
	arc := a.Arcanum()
	if arc == nil || arc.Consultante == "" {
		return
	}
	for i := range r.Comprobantes {
		rc := &r.Comprobantes[i]
		c := &rc.Comprobante
		if !arca.CUITValido(c.CUITEmisor) {
			continue
		}
		var p arcanum.DatosPadron
		if v, ok := a.padron.Load(c.CUITEmisor); ok {
			p = v.(arcanum.DatosPadron)
		} else {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			var err error
			p, err = arc.Padron(cctx, "", c.CUITEmisor)
			cancel()
			if err != nil {
				a.Log.Info("padrón no disponible", "error", err)
				return // si Arcanum no responde, no insistimos con el resto
			}
			a.padron.Store(c.CUITEmisor, p)
		}
		if c.CondicionIVAEmisor == "" && p.Condicion != "" {
			c.CondicionIVAEmisor = p.Condicion
			c.Marcar("condicion_iva_emisor", modelo.OrigenArcanum)
		}
		if p.Actividad != nil && c.Origen("rubro") != modelo.OrigenManual {
			if rb := rubro.DesdeActividad(p.Actividad.Codigo, p.Actividad.Descripcion); rb != "" {
				c.Rubro = rb
				c.Marcar("rubro", modelo.OrigenArcanum)
			}
		}
		if c.RazonSocialEmisor != "" || p.Razon == "" {
			continue
		}
		c.RazonSocialEmisor = p.Razon
		c.Marcar("razon_social_emisor", modelo.OrigenArcanum)
		var h []modelo.Hallazgo
		for _, x := range rc.Hallazgos {
			if x.Codigo != "falta_razon_social_emisor" {
				h = append(h, x)
			}
		}
		rc.Hallazgos = h
		rc.Estado = validar.Estado(c, h)
		rc.Confianza = validar.Confianza(c, h)
	}
}

// aplicarRubrosGuardados es la capa 3 del rubro: si una persona ya eligió el rubro de un emisor, ese
// gana (con origen manual).
func (a *App) aplicarRubrosGuardados(ctx context.Context, r *modelo.Resultado) {
	if a.Store == nil {
		return
	}
	var cuits []string
	for i := range r.Comprobantes {
		if c := &r.Comprobantes[i].Comprobante; c.CUITEmisor != "" {
			cuits = append(cuits, c.CUITEmisor)
		}
	}
	guardados, err := a.Store.RubrosDeCUITs(ctx, cuits)
	if err != nil {
		a.Log.Warn("no se pudieron leer los rubros guardados", "error", err)
		return
	}
	for i := range r.Comprobantes {
		c := &r.Comprobantes[i].Comprobante
		if rb, ok := guardados[c.CUITEmisor]; ok && rubro.Valido(rb) {
			c.Rubro = rb
			c.Marcar("rubro", modelo.OrigenManual)
		}
	}
}

// ---------- Constatación ----------

func hallazgosConstatacion(k *arcanum.Constatacion) []modelo.Hallazgo {
	if k == nil {
		return nil
	}
	if k.Apoc != nil && k.Apoc.EsApocrifo != nil && *k.Apoc.EsApocrifo {
		return []modelo.Hallazgo{{Codigo: "emisor_apocrifo", Nivel: modelo.NivelError, Campo: "cuit_emisor",
			Mensaje: "El emisor figura en la base de apócrifos de ARCA."}}
	}
	if k.Valido != nil && !*k.Valido {
		msg := "ARCA no reconoce el comprobante con estos datos (constatación WSCDC rechazada)."
		for _, o := range k.Observaciones {
			msg += " " + o.Message
		}
		return []modelo.Hallazgo{{Codigo: "constatacion_rechazada", Nivel: modelo.NivelError, Mensaje: msg}}
	}
	if k.Valido != nil && *k.Valido {
		return []modelo.Hallazgo{{Codigo: "constatado", Nivel: modelo.NivelInfo, Mensaje: "ARCA confirma el comprobante (WSCDC)."}}
	}
	if k.Apoc != nil && k.Apoc.Consultado && k.Apoc.EsApocrifo != nil {
		// Un tique: WSCDC no lo cubre, pero el emisor no está en la base de apócrifos.
		return []modelo.Hallazgo{{Codigo: "apoc_controlado", Nivel: modelo.NivelInfo, Campo: "cuit_emisor",
			Mensaje: "El emisor no figura en la base de apócrifos de ARCA."}}
	}
	if k.Error != "" {
		return []modelo.Hallazgo{{Codigo: "constatacion_fallo", Nivel: modelo.NivelInfo, Mensaje: "No se pudo constatar: " + k.Error}}
	}
	return nil
}

var codigosConstatacion = map[string]bool{"emisor_apocrifo": true, "apoc_controlado": true, "constatacion_rechazada": true, "constatado": true, "constatacion_fallo": true,
	codigoReconstatar: true}

// aplicarConstatacion guarda el resultado en el comprobante y recalcula el estado.
func (a *App) aplicarConstatacion(ctx context.Context, c *store.Cbte, k *arcanum.Constatacion) error {
	var h []modelo.Hallazgo
	for _, x := range c.Hallazgos {
		if !codigosConstatacion[x.Codigo] {
			h = append(h, x)
		}
	}
	h = append(h, hallazgosConstatacion(k)...)
	c.Hallazgos = h
	if c.Estado != modelo.EstadoAprobado {
		c.Estado = validar.Estado(&c.Datos, h)
	}
	c.Confianza = validar.Confianza(&c.Datos, h)
	b, _ := json.Marshal(k)
	c.Constatacion = b
	return a.Store.ActualizarCbte(ctx, c, nil, "")
}

func (a *App) constatarLote(ctx context.Context, loteID int64, solo []int64) error {
	arc := a.Arcanum()
	if arc == nil {
		return nil
	}
	l, err := a.Store.Lote(ctx, loteID)
	if errors.Is(err, store.ErrNoEncontrado) {
		return nil // se fue a la papelera
	}
	if err != nil {
		return err
	}
	for {
		cbs, err := a.Store.SinConstatar(ctx, loteID, solo, 100)
		if err != nil {
			return err
		}
		if len(cbs) == 0 {
			break
		}
		// El cliente se lee en cada tanda: si alguien lo cambió, se constata con el nuevo.
		if l, err = a.Store.Lote(ctx, loteID); errors.Is(err, store.ErrNoEncontrado) {
			return nil
		} else if err != nil {
			return err
		}
		ptrs := make([]*modelo.Comprobante, len(cbs))
		for i := range cbs {
			ptrs[i] = &cbs[i].Datos
		}
		consultante := ""
		if arca.CUITValido(l.ClienteCUIT) {
			consultante = l.ClienteCUIT
		}
		ks, err := arc.ConstatarTodo(ctx, consultante, ptrs)
		if err != nil {
			return err // la cola reintenta con espera creciente
		}
		// Si mientras se constataba se borró el lote o le cambiaron el cliente, esta tanda no vale.
		if ahora, err := a.Store.Lote(ctx, loteID); errors.Is(err, store.ErrNoEncontrado) {
			return nil
		} else if err != nil {
			return err
		} else if ahora.ClienteCUIT != l.ClienteCUIT {
			continue
		}
		for i := range cbs {
			if err := a.aplicarConstatacion(ctx, &cbs[i], &ks[i]); err != nil && !errors.Is(err, store.ErrNoEncontrado) {
				return err
			}
		}
	}
	a.encolarAviso(ctx, l)
	return nil
}

// ConstatarUno constata un comprobante en el momento.
func (a *App) ConstatarUno(ctx context.Context, id int64) (*store.Cbte, error) {
	arc := a.Arcanum()
	if arc == nil {
		return nil, errors.New("Arcanum no está configurado (Configuración, Arcanum)")
	}
	c, err := a.Store.Comprobante(ctx, id)
	if err != nil {
		return nil, err
	}
	consultante := ""
	if arca.CUITValido(c.ClienteCUIT) {
		consultante = c.ClienteCUIT
	}
	ks, err := arc.ConstatarTodo(ctx, consultante, []*modelo.Comprobante{&c.Datos})
	if err != nil {
		return nil, err
	}
	if err := a.aplicarConstatacion(ctx, c, &ks[0]); err != nil {
		return nil, err
	}
	return a.Store.Comprobante(ctx, id)
}

// ---------- Verificar ----------

type Verificacion struct {
	Veredicto    string                 `json:"veredicto"`
	Mensaje      string                 `json:"mensaje"`
	Resultado    modelo.Resultado       `json:"resultado"`
	Constatacion []arcanum.Constatacion `json:"constatacion"`
	Comprobantes []VeredictoComprobante `json:"comprobantes"`
	Lote         *store.Lote            `json:"lote,omitempty"`
}

type VeredictoComprobante struct {
	Veredicto string `json:"veredicto"`
	Mensaje   string `json:"mensaje"`
}

// orden de gravedad para resumir varios comprobantes en un veredicto.
var gravedad = map[string]int{"valida": 0, "tique_controlado": 1, "no_verificable": 2, "con_errores": 3, "invalida": 4, "apocrifa": 5}

// Verificar lee un archivo y lo constata, sin guardarlo (salvo que se pida).
func (a *App) Verificar(ctx context.Context, nombre string, datos []byte, consultante string, ia, guardar bool) (*Verificacion, error) {
	cfg, arc := a.Config(), a.Arcanum()
	if int64(len(datos)) > cfg.MaxArchivo {
		return nil, fmt.Errorf("el archivo supera los %d MB", cfg.MaxArchivo>>20)
	}
	r := a.Pipeline.Procesar(ctx, nombre, datos, pipeline.Opciones{MaxPaginas: cfg.MaxPaginas, IA: ia})
	v := &Verificacion{Resultado: r, Constatacion: []arcanum.Constatacion{}, Comprobantes: []VeredictoComprobante{}}
	if r.Error != "" {
		v.Veredicto, v.Mensaje = "no_verificable", "No se pudo leer el archivo: "+r.Error
		return v, nil
	}
	if len(r.Comprobantes) == 0 {
		v.Veredicto, v.Mensaje = "no_verificable", "No se encontró ningún comprobante en el archivo."
		return v, nil
	}
	a.enriquecerConPadron(ctx, &r)
	a.aplicarRubrosGuardados(ctx, &r)
	v.Resultado = r
	ks := make([]arcanum.Constatacion, len(r.Comprobantes))
	if arc != nil {
		ptrs := make([]*modelo.Comprobante, len(r.Comprobantes))
		for i := range r.Comprobantes {
			ptrs[i] = &r.Comprobantes[i].Comprobante
		}
		res, err := arc.ConstatarTodo(ctx, consultante, ptrs)
		if err != nil {
			for i := range ks {
				ks[i] = arcanum.Constatacion{Error: err.Error()}
			}
		} else {
			ks = res
		}
	}
	peor := -1
	for i := range r.Comprobantes {
		var k *arcanum.Constatacion
		if arc != nil {
			k = &ks[i]
		}
		ver, msg := arcanum.Veredicto(&r.Comprobantes[i], k)
		v.Comprobantes = append(v.Comprobantes, VeredictoComprobante{ver, msg})
		if gravedad[ver] > peor {
			peor = gravedad[ver]
			v.Veredicto, v.Mensaje = ver, msg
		}
	}
	if arc != nil {
		v.Constatacion = ks
	}
	if guardar {
		rl, err := a.CrearLote(ctx, store.LoteNuevo{Nombre: "Verificación " + nombre, ClienteCUIT: arca.SoloDigitos(consultante), Origen: "verificar", IA: ia},
			[]Archivo{{Nombre: nombre, Datos: datos}}, 0)
		if err == nil {
			v.Lote = rl.Lote
		}
	}
	return v, nil
}

// ---------- Correcciones ----------

// CamposEditables: nombre JSON -> tipo ("s" texto, "i" entero, "m" importe, "f" decimal, "d" fecha, "c" CUIT).
var CamposEditables = map[string]string{
	"tipo": "i", "letra": "s", "punto_venta": "i", "numero": "i", "fecha": "d",
	"cuit_emisor": "c", "razon_social_emisor": "s", "condicion_iva_emisor": "s",
	"doc_tipo_receptor": "i", "doc_nro_receptor": "c", "razon_social_receptor": "s",
	"moneda": "s", "cotizacion": "f",
	"neto_gravado": "m", "no_gravado": "m", "exento": "m", "iva_27": "m", "iva_21": "m", "iva_10_5": "m",
	"iva_5": "m", "iva_2_5": "m", "otros_tributos": "m", "subtotal": "m", "iva_contenido": "m", "total": "m",
	"cae": "c", "vto_cae": "d",
	"hora": "s", "rubro": "s",
	// Libro IVA Digital (0.10.0): el período del libro (AAAA-MM; vacío vuelve al automático), el
	// despacho de importación y el detalle de los otros tributos.
	"periodo_iva": "p", "despacho": "s",
	"percepcion_iva": "m", "percepcion_nacionales": "m", "percepcion_iibb": "m", "percepcion_municipal": "m", "impuestos_internos": "m",
}

var rePeriodo = regexp.MustCompile(`^(19|20)\d\d-(0[1-9]|1[0-2])$`)

var reHora = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d)?$`)

var codigosPersistentes = map[string]bool{"qr_distinto_texto": true, "duplicado": true, "ia_contradice_qr": true, "ia_fallo": true, "ocr_fallo": true,
	store.CodigoLlegoTarde: true, "despacho_leido": true}

// Corregir aplica cambios hechos por una persona, los registra y recalcula los controles.
func (a *App) Corregir(ctx context.Context, id int64, campos map[string]string, aprobar bool, usuario string) (*store.Cbte, error) {
	c, err := a.Store.Comprobante(ctx, id)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	b, _ := json.Marshal(c.Datos)
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber() // "21000.00" queda igual que como se formatea el valor nuevo
	_ = dec.Decode(&m)
	var cambios []store.Correccion
	for k, v := range campos {
		tipo, ok := CamposEditables[k]
		if !ok {
			return nil, fmt.Errorf("el campo %q no se puede editar", k)
		}
		v = strings.TrimSpace(v)
		var nuevo any
		switch tipo {
		case "s":
			nuevo = v
			switch k {
			case "letra", "moneda":
				nuevo = strings.ToUpper(v)
			case "rubro":
				v = strings.ToLower(v)
				if !rubro.Valido(v) {
					return nil, fmt.Errorf("rubro: %q no es válido (puede ser %s)", v, strings.Join(rubro.Todos, ", "))
				}
				nuevo = v
			case "hora":
				if v != "" && !reHora.MatchString(v) {
					return nil, fmt.Errorf("hora: %q no es una hora válida (HH:MM o HH:MM:SS)", v)
				}
			case "despacho":
				v = arca.NormalizarDespacho(v)
				if v != "" && !arca.DespachoValido(v) {
					return nil, fmt.Errorf("despacho: %q no tiene los 16 caracteres (letras y números) que pide ARCA", v)
				}
				nuevo = v
			}
		case "p":
			if v != "" && !rePeriodo.MatchString(v) {
				return nil, fmt.Errorf("período del libro: %q no es un mes con el formato AAAA-MM", v)
			}
			nuevo = v
		case "c":
			nuevo = arca.SoloDigitos(v)
		case "d":
			f := arca.NormalizarFecha(v)
			if _, err := time.Parse("2006-01-02", f); err != nil && v != "" {
				return nil, fmt.Errorf("%s: fecha inválida %q", k, v)
			}
			nuevo = f
		case "i":
			var n int64
			if _, err := fmt.Sscan(arca.SoloDigitos(v), &n); err != nil && v != "" {
				return nil, fmt.Errorf("%s: número inválido %q", k, v)
			}
			nuevo = n
		case "m":
			if v == "" {
				v = "0"
			}
			imp, err := modelo.ParsearImporte(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", k, err)
			}
			nuevo = imp.String()
		case "f":
			imp, err := modelo.ParsearImporte(v)
			if err != nil {
				return nil, fmt.Errorf("%s: %v", k, err)
			}
			nuevo = imp.Float()
		}
		antes := ""
		if m[k] != nil {
			antes = fmt.Sprint(m[k])
		}
		if antes == fmt.Sprint(nuevo) {
			continue
		}
		m[k] = nuevo
		cambios = append(cambios, store.Correccion{Campo: k, Antes: antes, Despues: fmt.Sprint(nuevo), Usuario: usuario})
	}
	b, _ = json.Marshal(m)
	var d modelo.Comprobante
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, err
	}
	cambio := map[string]bool{}
	for _, k := range cambios {
		d.Marcar(k.Campo, modelo.OrigenManual)
		cambio[k.Campo] = true
	}
	if d.Tipo != 0 {
		d.EsTique = arca.EsTique(d.Tipo)
	}
	var h []modelo.Hallazgo
	for _, x := range c.Hallazgos {
		if codigosPersistentes[x.Codigo] || codigosConstatacion[x.Codigo] {
			h = append(h, x)
		}
	}
	// El período del libro: el que elige una persona (no puede ser anterior al mes de la fecha), o
	// el automático, que se vuelve a decidir si cambió la fecha o el tipo (o si se borró el elegido).
	switch {
	case cambio["periodo_iva"] && d.PeriodoIVA != "":
		if len(d.Fecha) >= 7 && d.PeriodoIVA < d.Fecha[:7] {
			return nil, fmt.Errorf("período del libro: %s es anterior a la fecha del comprobante (%s)", d.PeriodoIVA, d.Fecha)
		}
		var sin []modelo.Hallazgo
		for _, x := range h {
			if x.Codigo != store.CodigoLlegoTarde {
				sin = append(sin, x)
			}
		}
		h = sin
	case cambio["periodo_iva"] || ((cambio["fecha"] || cambio["tipo"]) && d.Origen("periodo_iva") != modelo.OrigenManual):
		delete(d.Origenes, "periodo_iva")
		if _, err := a.Store.AsignarPeriodo(ctx, c.ClienteCUIT, &d, &h, c.ID); err != nil {
			return nil, err
		}
	}
	c.Datos = d
	h = append(h, validar.Comprobante(&d, time.Now())...)
	c.Hallazgos = h
	c.Confianza = validar.Confianza(&d, h)
	switch {
	case aprobar:
		c.Estado = modelo.EstadoAprobado
	case len(cambios) > 0 || c.Estado != modelo.EstadoAprobado:
		c.Estado = validar.Estado(&d, h)
	}
	aprobadoPor := ""
	if aprobar {
		aprobadoPor = usuario
		if aprobadoPor == "" {
			aprobadoPor = "api"
		}
	}
	if err := a.Store.ActualizarCbte(ctx, c, cambios, aprobadoPor); err != nil {
		return nil, err
	}
	// Capa 3 del rubro: lo que corrige una persona se recuerda para ese emisor y pasa a sus otros
	// comprobantes (salvo a los que una persona les corrigió el rubro a ellos en particular).
	for _, k := range cambios {
		if k.Campo == "rubro" && d.CUITEmisor != "" {
			n, err := a.Store.GuardarRubroCUIT(ctx, d.CUITEmisor, d.Rubro, usuario)
			if err != nil {
				return nil, fmt.Errorf("el comprobante se guardó, pero no se pudo recordar el rubro del emisor: %w", err)
			}
			a.Log.Info("rubro recordado para el emisor", "cuit", d.CUITEmisor, "rubro", d.Rubro, "comprobantes", n)
		}
	}
	return a.Store.Comprobante(ctx, id)
}

// Original devuelve el archivo original de un documento.
func (a *App) Original(ctx context.Context, docID int64) (*store.Documento, []byte, error) {
	d, err := a.Store.Documento(ctx, docID)
	if err != nil {
		return nil, nil, err
	}
	b, err := a.Almacen.Leer(ctx, "originales/"+d.SHA256)
	return d, b, err
}
