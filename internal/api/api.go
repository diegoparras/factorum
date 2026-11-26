// Package api expone la API REST, el webhook de entrada, MCP y la UI.
package api

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/store"
)

type Rol int

const (
	RolNinguno Rol = iota
	RolLectura
	RolOperador
)

// Webhook es lo que la API necesita del canal webhook.
type Webhook interface {
	Autorizado(token string) bool
	Decodificar(body []byte) (canal.Mensaje, error)
}

type Server struct {
	App     *app.App
	Cfg     *config.Config
	Log     *slog.Logger
	Web     fs.FS   // UI estática
	Webhook Webhook // nil = sin webhook
	MCP     http.Handler
}

type ctxRol struct{}

// RolDe devuelve el rol autenticado del pedido.
func RolDe(ctx context.Context) Rol {
	r, _ := ctx.Value(ctxRol{}).(Rol)
	return r
}

// Rol autentica un pedido (lo usa también MCP).
func (s *Server) Rol(r *http.Request) Rol { return s.rol(r) }

func (s *Server) rol(r *http.Request) Rol {
	k := r.Header.Get("X-API-Key")
	if k == "" {
		if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
			k = strings.TrimPrefix(a, "Bearer ")
		}
	}
	if k == "" {
		return RolNinguno
	}
	if s.Cfg.APIKey != "" && subtle.ConstantTimeCompare([]byte(k), []byte(s.Cfg.APIKey)) == 1 {
		return RolOperador
	}
	if s.Cfg.APIKeyLect != "" && subtle.ConstantTimeCompare([]byte(k), []byte(s.Cfg.APIKeyLect)) == 1 {
		return RolLectura
	}
	return RolNinguno
}

// auth exige clave. Las claves de lectura solo pueden hacer GET (y verificar, que no guarda).
func (s *Server) auth(min Rol, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rol := s.rol(r)
		if rol == RolNinguno {
			jsonErr(w, http.StatusUnauthorized, "Falta la clave de API o no es válida (X-API-Key o Authorization: Bearer)")
			return
		}
		if rol < min {
			jsonErr(w, http.StatusForbidden, "Esta clave es de solo lectura")
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxRol{}, rol)))
	}
}

// Handler arma el ruteo completo.
func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", s.salud)
	m.HandleFunc("GET /metrics", s.auth(RolLectura, s.metricas))

	m.HandleFunc("POST /api/verificar", s.auth(RolLectura, s.verificar))
	m.HandleFunc("POST /api/lotes", s.auth(RolOperador, s.crearLote))
	m.HandleFunc("GET /api/lotes", s.auth(RolLectura, s.lotes))
	m.HandleFunc("GET /api/lotes/{id}", s.auth(RolLectura, s.lote))
	m.HandleFunc("GET /api/comprobantes", s.auth(RolLectura, s.comprobantes))
	m.HandleFunc("GET /api/comprobantes/{id}", s.auth(RolLectura, s.comprobante))
	m.HandleFunc("PATCH /api/comprobantes/{id}", s.auth(RolOperador, s.corregir))
	m.HandleFunc("POST /api/comprobantes/{id}/constatar", s.auth(RolOperador, s.constatar))
	m.HandleFunc("GET /api/documentos/{id}/archivo", s.auth(RolLectura, s.archivo))
	m.HandleFunc("GET /api/documentos/{id}/pagina/{n}", s.auth(RolLectura, s.pagina))
	m.HandleFunc("GET /api/export.xlsx", s.auth(RolLectura, s.exportar("xlsx")))
	m.HandleFunc("GET /api/export.csv", s.auth(RolLectura, s.exportar("csv")))
	m.HandleFunc("GET /api/resumen", s.auth(RolLectura, s.resumen))
	m.HandleFunc("GET /api/panel", s.auth(RolLectura, s.panel))
	m.HandleFunc("GET /api/novedades", s.novedades)
	m.HandleFunc("GET /api/canales", s.auth(RolLectura, s.canales))
	m.HandleFunc("GET /api/canales/whatsapp/qr.png", s.auth(RolOperador, s.qrWhatsApp))
	m.HandleFunc("POST /api/canales/whatsapp/vincular", s.auth(RolOperador, s.vincularWhatsApp))
	m.HandleFunc("POST /api/canales/whatsapp/desvincular", s.auth(RolOperador, s.desvincularWhatsApp))
	m.HandleFunc("POST /api/canales/webhook", s.webhook)
	if s.MCP != nil {
		m.Handle("/mcp", s.MCP)
	}
	if s.Web != nil {
		est := http.FileServerFS(s.Web)
		m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				jsonErr(w, http.StatusMethodNotAllowed, "Método no permitido")
				return
			}
			est.ServeHTTP(w, r)
		})
	}
	return s.envolver(m)
}

// envolver agrega cabeceras de seguridad, recuperación de pánicos y log de acceso (sin cuerpos).
func (s *Server) envolver(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/mcp" {
			w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-src 'self' blob:; object-src 'self' blob:")
		}
		rw := &registro{ResponseWriter: w, status: 200}
		defer func() {
			if p := recover(); p != nil {
				s.Log.Error("pánico atendiendo un pedido", "ruta", r.URL.Path, "error", p)
				jsonErr(rw, http.StatusInternalServerError, "Error interno")
			}
			if r.URL.Path != "/healthz" {
				s.Log.Info("pedido", "metodo", r.Method, "ruta", r.URL.Path, "status", rw.status, "ms", time.Since(t).Milliseconds())
			}
		}()
		h.ServeHTTP(rw, r)
	})
}

type registro struct {
	http.ResponseWriter
	status int
}

func (r *registro) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }
func (r *registro) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// ---------- helpers ----------

func jsonOK(w http.ResponseWriter, status int, v map[string]any) {
	v["ok"] = true
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func jsonErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func (s *Server) fallo(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNoEncontrado):
		jsonErr(w, http.StatusNotFound, "No encontrado")
	default:
		s.Log.Error("error", "error", err)
		jsonErr(w, http.StatusInternalServerError, err.Error())
	}
}

func idDe(r *http.Request) (int64, error) {
	return strconv.ParseInt(r.PathValue("id"), 10, 64)
}

func filtroDe(r *http.Request) store.Filtro {
	q := r.URL.Query()
	n := func(k string) int { v, _ := strconv.Atoi(q.Get(k)); return v }
	return store.Filtro{
		Lote: int64(n("lote")), Estado: q.Get("estado"), Cliente: arca.SoloDigitos(q.Get("cliente")),
		Emisor: arca.SoloDigitos(q.Get("emisor")), Desde: q.Get("desde"), Hasta: q.Get("hasta"), Q: q.Get("q"),
		Limite: min(max(n("limite"), 0), 500), Offset: max(n("offset"), 0),
	}
}

func verdadero(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "si", "sí", "on":
		return true
	}
	return false
}

// ---------- handlers ----------

func (s *Server) salud(w http.ResponseWriter, r *http.Request) {
	ia := map[string]any{"habilitada": false}
	if v := s.App.Pipeline.Vision; v != nil {
		ia = map[string]any{"habilitada": true, "modelo": v.Modelo, "local": v.Local()}
	}
	base := s.App.Store.Pool.Ping(r.Context()) == nil
	jsonOK(w, http.StatusOK, map[string]any{
		"version": config.Version, "base": base, "ocr": s.App.Pipeline.OCR != nil, "ia": ia,
		"arcanum": s.App.Arcanum != nil, "almacen": s.App.Almacen.Backend(), "cifrado": s.App.Almacen.Cifrado(),
	})
}

func (s *Server) metricas(w http.ResponseWriter, r *http.Request) {
	m, err := s.App.Store.Estadisticas(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP factorum_info Versión de Factorum\n# TYPE factorum_info gauge\nfactorum_info{version=%q} 1\n", config.Version)
	for k, v := range m {
		tipo, estado, _ := strings.Cut(k, "_")
		fmt.Fprintf(w, "factorum_%s{estado=%q} %d\n", tipo, estado, v)
	}
}

// leerArchivo saca un archivo de un multipart ("archivo") o de un JSON {nombre, contenido_base64}.
func (s *Server) leerArchivo(w http.ResponseWriter, r *http.Request) (nombre string, datos []byte, campos map[string]string, err error) {
	campos = map[string]string{}
	r.Body = http.MaxBytesReader(w, r.Body, s.Cfg.MaxArchivo*4/3+1<<20)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err = r.ParseMultipartForm(32 << 20); err != nil {
			return "", nil, nil, fmt.Errorf("formulario inválido: %w", err)
		}
		for k, v := range r.MultipartForm.Value {
			if len(v) > 0 {
				campos[k] = v[0]
			}
		}
		f, h, err := r.FormFile("archivo")
		if err != nil {
			return "", nil, nil, errors.New("falta el campo \"archivo\"")
		}
		defer f.Close()
		datos, err = io.ReadAll(io.LimitReader(f, s.Cfg.MaxArchivo+1))
		if int64(len(datos)) > s.Cfg.MaxArchivo {
			return "", nil, nil, fmt.Errorf("el archivo supera los %d MB", s.Cfg.MaxArchivo>>20)
		}
		return h.Filename, datos, campos, err
	}
	var b map[string]any
	if err = json.NewDecoder(r.Body).Decode(&b); err != nil {
		return "", nil, nil, errors.New("mandá un multipart con \"archivo\" o un JSON con nombre y contenido_base64")
	}
	for k, v := range b {
		if k != "contenido_base64" {
			campos[k] = fmt.Sprint(v)
		}
	}
	nombre, _ = b["nombre"].(string)
	c64, _ := b["contenido_base64"].(string)
	datos, err = base64.StdEncoding.DecodeString(c64)
	if err != nil || len(datos) == 0 {
		return "", nil, nil, errors.New("contenido_base64 vacío o inválido")
	}
	if nombre == "" {
		nombre = "archivo"
	}
	return nombre, datos, campos, nil
}

func (s *Server) verificar(w http.ResponseWriter, r *http.Request) {
	nombre, datos, campos, err := s.leerArchivo(w, r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ia := s.Cfg.IAPorDefecto
	if v, ok := campos["ia"]; ok {
		ia = verdadero(v)
	}
	guardar := verdadero(campos["guardar"]) && RolDe(r.Context()) >= RolOperador
	v, err := s.App.Verificar(r.Context(), nombre, datos, arca.SoloDigitos(campos["cuit_consultante"]), ia, guardar)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"veredicto": v.Veredicto, "mensaje": v.Mensaje, "resultado": v.Resultado,
		"constatacion": v.Constatacion, "comprobantes": v.Comprobantes, "lote": v.Lote})
}

func (s *Server) crearLote(w http.ResponseWriter, r *http.Request) {
	maxLote := int64(envMB("FACTORUM_MAX_LOTE_MB", 1024)) << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxLote)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		jsonErr(w, http.StatusBadRequest, "Formulario inválido o demasiado grande: "+err.Error())
		return
	}
	defer r.MultipartForm.RemoveAll()
	var arch []app.Archivo
	for _, k := range []string{"archivos", "archivo", "archivos[]"} {
		for _, h := range r.MultipartForm.File[k] {
			f, err := h.Open()
			if err != nil {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(f, s.Cfg.MaxArchivo*8+1)) // un ZIP puede ser más grande que un archivo suelto
			f.Close()
			if err != nil {
				continue
			}
			arch = append(arch, app.Archivo{Nombre: h.Filename, Mime: h.Header.Get("Content-Type"), Datos: b})
		}
	}
	ia := s.Cfg.IAPorDefecto
	if v := r.FormValue("ia"); v != "" {
		ia = verdadero(v)
	}
	constatar := s.App.Arcanum != nil
	if v := r.FormValue("constatar"); v != "" {
		constatar = verdadero(v) && s.App.Arcanum != nil
	}
	origen := r.FormValue("origen")
	if origen != "arcanum" && origen != "mcp" {
		origen = "web"
	}
	res, err := s.App.CrearLote(r.Context(), store.LoteNuevo{
		Nombre: strings.TrimSpace(r.FormValue("nombre")), ClienteCUIT: arca.SoloDigitos(r.FormValue("cliente_cuit")),
		Origen: origen, IA: ia, Constatar: constatar,
	}, arch, 0)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusAccepted, map[string]any{"lote": res.Lote, "recibidos": res.Recibidos, "repetidos": res.Repetidos, "descartados": res.Descartados})
}

func envMB(k string, def int) int {
	v, err := strconv.Atoi(strings.TrimSpace(getenv(k)))
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func (s *Server) lotes(w http.ResponseWriter, r *http.Request) {
	f := filtroDe(r)
	ls, total, err := s.App.Store.Lotes(r.Context(), f.Limite, f.Offset)
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"lotes": ls, "total": total})
}

func (s *Server) lote(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	l, err := s.App.Store.Lote(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"lote": l})
}

func (s *Server) comprobantes(w http.ResponseWriter, r *http.Request) {
	cbs, total, err := s.App.Store.Comprobantes(r.Context(), filtroDe(r))
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"comprobantes": cbs, "total": total})
}

func (s *Server) comprobante(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	c, err := s.App.Store.Comprobante(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	corr, _ := s.App.Store.Correcciones(r.Context(), id)
	jsonOK(w, http.StatusOK, map[string]any{"comprobante": c, "correcciones": corr})
}

func (s *Server) corregir(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	var b struct {
		Campos  map[string]any `json:"campos"`
		Aprobar bool           `json:"aprobar"`
		Usuario string         `json:"usuario"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	campos := map[string]string{}
	for k, v := range b.Campos {
		campos[k] = fmt.Sprint(v)
	}
	if len(b.Usuario) > 100 {
		b.Usuario = b.Usuario[:100]
	}
	c, err := s.App.Corregir(r.Context(), id, campos, b.Aprobar, b.Usuario)
	if err != nil {
		if errors.Is(err, store.ErrNoEncontrado) {
			s.fallo(w, err)
			return
		}
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"comprobante": c})
}

func (s *Server) constatar(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	c, err := s.App.ConstatarUno(r.Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNoEncontrado) {
			s.fallo(w, err)
			return
		}
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"comprobante": c, "constatacion": c.Constatacion})
}

func (s *Server) archivo(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	d, b, err := s.App.Original(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	mime := d.Mime
	if mime == "" {
		mime = http.DetectContentType(b)
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", strings.ReplaceAll(d.Nombre, `"`, "")))
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write(b)
}

// pagina devuelve una página del documento como PNG: se ve igual en cualquier navegador (los
// visores de PDF embebidos fallan en celulares) y no expone el PDF entero.
func (s *Server) pagina(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	n, err2 := strconv.Atoi(strings.TrimSuffix(r.PathValue("n"), ".png"))
	if err != nil || err2 != nil || n < 1 {
		jsonErr(w, http.StatusBadRequest, "id o página inválidos")
		return
	}
	d, b, err := s.App.Original(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if !bytes.HasPrefix(b, []byte("%PDF")) {
		w.Header().Set("Content-Type", http.DetectContentType(b))
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(b)
		return
	}
	doc, err := s.App.Pipeline.Motor.Abrir(r.Context(), b)
	if err != nil {
		jsonErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	defer doc.Cerrar()
	if n > doc.Paginas() {
		jsonErr(w, http.StatusNotFound, fmt.Sprintf("%s tiene %d páginas", d.Nombre, doc.Paginas()))
		return
	}
	_, png, err := doc.Rasterizar(r.Context(), n, 110, false)
	if err != nil {
		s.fallo(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Paginas", strconv.Itoa(doc.Paginas()))
	_, _ = w.Write(png)
}

func (s *Server) exportar(formato string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filas, err := s.App.Filas(r.Context(), filtroDe(r))
		if err != nil {
			s.fallo(w, err)
			return
		}
		nombre := "factorum-" + time.Now().Format("20060102-1504")
		var buf bytes.Buffer
		tipo := "text/csv; charset=utf-8"
		if formato == "xlsx" {
			tipo = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			err = export.Excel(&buf, filas)
		} else {
			err = export.CSV(&buf, filas)
		}
		if err != nil {
			s.fallo(w, err)
			return
		}
		w.Header().Set("Content-Type", tipo)
		w.Header().Set("Content-Disposition", `attachment; filename="`+nombre+`.`+formato+`"`)
		_, _ = w.Write(buf.Bytes())
	}
}

func (s *Server) resumen(w http.ResponseWriter, r *http.Request) {
	filas, err := s.App.Filas(r.Context(), filtroDe(r))
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"resumen": export.Resumir(filas)})
}

func (s *Server) canales(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, http.StatusOK, map[string]any{"canales": s.App.Canales()})
}

type vinculable interface {
	VincularConCodigo(ctx context.Context, telefono string) (string, error)
	Desvincular(ctx context.Context) error
}

func (s *Server) qrWhatsApp(w http.ResponseWriter, r *http.Request) {
	c := s.App.Canal("whatsapp")
	if c == nil || c.Estado().QR == "" {
		jsonErr(w, http.StatusNotFound, "No hay un código de vinculación pendiente")
		return
	}
	png, err := qrcode.Encode(c.Estado().QR, qrcode.Medium, 320)
	if err != nil {
		s.fallo(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

func (s *Server) vincularWhatsApp(w http.ResponseWriter, r *http.Request) {
	v, ok := s.App.Canal("whatsapp").(vinculable)
	if !ok {
		jsonErr(w, http.StatusNotFound, "WhatsApp no está habilitado (FACTORUM_WHATSAPP=true)")
		return
	}
	var b struct {
		Telefono string `json:"telefono"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
	cod, err := v.VincularConCodigo(r.Context(), arca.SoloDigitos(b.Telefono))
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"codigo": cod})
}

func (s *Server) desvincularWhatsApp(w http.ResponseWriter, r *http.Request) {
	v, ok := s.App.Canal("whatsapp").(vinculable)
	if !ok {
		jsonErr(w, http.StatusNotFound, "WhatsApp no está habilitado")
		return
	}
	if err := v.Desvincular(r.Context()); err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{})
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if s.Webhook == nil {
		jsonErr(w, http.StatusNotFound, "El webhook no está habilitado (FACTORUM_WEBHOOK_TOKEN)")
		return
	}
	if !s.Webhook.Autorizado(r.Header.Get("X-Factorum-Token")) {
		jsonErr(w, http.StatusUnauthorized, "Token inválido")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 35<<20))
	if err != nil {
		jsonErr(w, http.StatusRequestEntityTooLarge, "Cuerpo demasiado grande")
		return
	}
	m, err := s.Webhook.Decodificar(body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"respuesta": s.App.ManejarMensaje(r.Context(), m)})
}
