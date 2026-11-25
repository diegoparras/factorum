// Package app es el orquestador: recibe archivos (web, chat, MCP), los guarda en el almacén, los
// encola, los procesa con el pipeline, los constata con Arcanum y avisa por el canal de origen.
package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/diegoparras/contadorfacturas/internal/almacen"
	"github.com/diegoparras/contadorfacturas/internal/arca"
	"github.com/diegoparras/contadorfacturas/internal/arcanum"
	"github.com/diegoparras/contadorfacturas/internal/canal"
	"github.com/diegoparras/contadorfacturas/internal/config"
	"github.com/diegoparras/contadorfacturas/internal/modelo"
	"github.com/diegoparras/contadorfacturas/internal/pipeline"
	"github.com/diegoparras/contadorfacturas/internal/store"
	"github.com/diegoparras/contadorfacturas/internal/validar"
)

type App struct {
	Cfg      *config.Config
	Store    *store.Store
	Almacen  *almacen.Almacen
	Pipeline *pipeline.Pipeline
	Arcanum  *arcanum.Cliente // nil = sin Arcanum
	River    *river.Client[pgx.Tx]
	Log      *slog.Logger

	mu      sync.RWMutex
	canales map[string]canal.Canal

	padron sync.Map // cuit -> razón social
}

// ---------- Trabajos de la cola ----------

type ProcesarArgs struct {
	DocumentoID int64 `json:"documento_id"`
}

func (ProcesarArgs) Kind() string { return "procesar_documento" }

type ConstatarArgs struct {
	LoteID int64 `json:"lote_id"`
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
	return w.a.constatarLote(ctx, j.Args.LoteID)
}

type notificarWorker struct {
	river.WorkerDefaults[NotificarArgs]
	a *App
}

func (w *notificarWorker) Work(ctx context.Context, j *river.Job[NotificarArgs]) error {
	return w.a.notificar(ctx, j.Args.LoteID)
}

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
	a.River, err = river.NewClient(drv, &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: a.Cfg.Workers},
			"arcanum":          {MaxWorkers: 1}, // WSCDC no es para ráfagas
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
	ok, desc := Expandir(archivos, a.Cfg.MaxArchivo)
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
	for _, f := range ok {
		s := sha256.Sum256(f.Datos)
		sha := hex.EncodeToString(s[:])
		if _, err := a.Almacen.Guardar(ctx, "originales/"+sha, f.Datos); err != nil {
			return nil, fmt.Errorf("no se pudo guardar %s: %w", f.Nombre, err)
		}
		gs = append(gs, guardado{f, sha})
	}
	res := &ResultadoLote{Recibidos: len(gs), Descartados: desc}
	var loteID int64
	err := pgx.BeginFunc(ctx, a.Store.Pool, func(tx pgx.Tx) error {
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
	return pipeline.Opciones{MaxPaginas: a.Cfg.MaxPaginas, IA: l.IA}
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
	if j, err := json.Marshal(r); err == nil {
		if _, err := a.Almacen.Guardar(ctx, "resultados/"+doc.SHA256+".json", j); err != nil {
			a.Log.Warn("no se pudo guardar el resultado en el almacén", "documento", docID, "error", err)
		}
	}
	if _, err := a.Store.GuardarResultado(ctx, doc, lote.ClienteCUIT, &r); err != nil {
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
	if l.Constatar && a.Arcanum != nil {
		if _, err := a.River.Insert(ctx, ConstatarArgs{LoteID: loteID}, nil); err != nil {
			a.Log.Error("no se pudo encolar la constatación", "lote", loteID, "error", err)
		}
		return // avisa la constatación al terminar
	}
	a.encolarAviso(ctx, l)
}

func (a *App) encolarAviso(ctx context.Context, l *store.Lote) {
	if l.Canal == "" || l.Chat == "" {
		return
	}
	_, err := a.River.Insert(ctx, NotificarArgs{LoteID: l.ID}, &river.InsertOpts{ScheduledAt: time.Now().Add(12 * time.Second)})
	if err != nil {
		a.Log.Error("no se pudo encolar el aviso", "lote", l.ID, "error", err)
	}
}

// enriquecerConPadron completa la razón social del emisor con el padrón de ARCA (vía Arcanum).
func (a *App) enriquecerConPadron(ctx context.Context, r *modelo.Resultado) {
	if a.Arcanum == nil || a.Arcanum.Consultante == "" {
		return
	}
	for i := range r.Comprobantes {
		rc := &r.Comprobantes[i]
		c := &rc.Comprobante
		if c.RazonSocialEmisor != "" || !arca.CUITValido(c.CUITEmisor) {
			continue
		}
		razon := ""
		if v, ok := a.padron.Load(c.CUITEmisor); ok {
			razon = v.(string)
		} else {
			cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			rz, cond, err := a.Arcanum.Padron(cctx, "", c.CUITEmisor)
			cancel()
			if err != nil {
				a.Log.Info("padrón no disponible", "error", err)
				return // si Arcanum no responde, no insistimos con el resto
			}
			razon = rz
			a.padron.Store(c.CUITEmisor, rz)
			if c.CondicionIVAEmisor == "" && cond != "" {
				c.CondicionIVAEmisor = cond
				c.Marcar("condicion_iva_emisor", modelo.OrigenArcanum)
			}
		}
		if razon == "" {
			continue
		}
		c.RazonSocialEmisor = razon
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
	if k.Error != "" {
		return []modelo.Hallazgo{{Codigo: "constatacion_fallo", Nivel: modelo.NivelInfo, Mensaje: "No se pudo constatar: " + k.Error}}
	}
	return nil
}

var codigosConstatacion = map[string]bool{"emisor_apocrifo": true, "constatacion_rechazada": true, "constatado": true, "constatacion_fallo": true}

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

func (a *App) constatarLote(ctx context.Context, loteID int64) error {
	if a.Arcanum == nil {
		return nil
	}
	l, err := a.Store.Lote(ctx, loteID)
	if err != nil {
		return err
	}
	for {
		cbs, err := a.Store.SinConstatar(ctx, loteID, 100)
		if err != nil {
			return err
		}
		if len(cbs) == 0 {
			break
		}
		ptrs := make([]*modelo.Comprobante, len(cbs))
		for i := range cbs {
			ptrs[i] = &cbs[i].Datos
		}
		consultante := ""
		if arca.CUITValido(l.ClienteCUIT) {
			consultante = l.ClienteCUIT
		}
		ks, err := a.Arcanum.Constatar(ctx, consultante, ptrs)
		if err != nil {
			return err // la cola reintenta con espera creciente
		}
		for i := range cbs {
			if err := a.aplicarConstatacion(ctx, &cbs[i], &ks[i]); err != nil {
				return err
			}
		}
	}
	a.encolarAviso(ctx, l)
	return nil
}

// ConstatarUno constata un comprobante en el momento.
func (a *App) ConstatarUno(ctx context.Context, id int64) (*store.Cbte, error) {
	if a.Arcanum == nil {
		return nil, errors.New("Arcanum no está configurado (ARCANUM_URL y ARCANUM_API_KEY)")
	}
	c, err := a.Store.Comprobante(ctx, id)
	if err != nil {
		return nil, err
	}
	consultante := ""
	if arca.CUITValido(c.ClienteCUIT) {
		consultante = c.ClienteCUIT
	}
	ks, err := a.Arcanum.Constatar(ctx, consultante, []*modelo.Comprobante{&c.Datos})
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
var gravedad = map[string]int{"valida": 0, "no_verificable": 1, "con_errores": 2, "invalida": 3, "apocrifa": 4}

// Verificar lee un archivo y lo constata, sin guardarlo (salvo que se pida).
func (a *App) Verificar(ctx context.Context, nombre string, datos []byte, consultante string, ia, guardar bool) (*Verificacion, error) {
	if int64(len(datos)) > a.Cfg.MaxArchivo {
		return nil, fmt.Errorf("el archivo supera los %d MB", a.Cfg.MaxArchivo>>20)
	}
	r := a.Pipeline.Procesar(ctx, nombre, datos, pipeline.Opciones{MaxPaginas: a.Cfg.MaxPaginas, IA: ia})
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
	v.Resultado = r
	ks := make([]arcanum.Constatacion, len(r.Comprobantes))
	if a.Arcanum != nil {
		ptrs := make([]*modelo.Comprobante, len(r.Comprobantes))
		for i := range r.Comprobantes {
			ptrs[i] = &r.Comprobantes[i].Comprobante
		}
		res, err := a.Arcanum.Constatar(ctx, consultante, ptrs)
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
		if a.Arcanum != nil {
			k = &ks[i]
		}
		ver, msg := arcanum.Veredicto(&r.Comprobantes[i], k)
		v.Comprobantes = append(v.Comprobantes, VeredictoComprobante{ver, msg})
		if gravedad[ver] > peor {
			peor = gravedad[ver]
			v.Veredicto, v.Mensaje = ver, msg
		}
	}
	if a.Arcanum != nil {
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
}

var codigosPersistentes = map[string]bool{"qr_distinto_texto": true, "duplicado": true, "ia_contradice_qr": true, "ia_fallo": true, "ocr_fallo": true}

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
			if k == "letra" || k == "moneda" {
				nuevo = strings.ToUpper(v)
			}
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
		antes := fmt.Sprint(m[k])
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
	for _, k := range cambios {
		d.Marcar(k.Campo, modelo.OrigenManual)
	}
	c.Datos = d
	var h []modelo.Hallazgo
	for _, x := range c.Hallazgos {
		if codigosPersistentes[x.Codigo] || codigosConstatacion[x.Codigo] {
			h = append(h, x)
		}
	}
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
