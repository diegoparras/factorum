// Package api expone la API REST, el webhook de entrada, MCP y la UI.
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/mudanza"
	"github.com/diegoparras/factorum/internal/rubro"
	"github.com/diegoparras/factorum/internal/store"
)

// Webhook es lo que la API necesita del canal webhook.
type Webhook interface {
	Autorizado(token string) bool
	Decodificar(body []byte) (canal.Mensaje, error)
}

type Server struct {
	App     *app.App
	Cfg     *config.Config // la del arranque: de acá salen las claves del entorno
	Log     *slog.Logger
	Web     fs.FS             // UI estática
	Webhook func() Webhook    // el webhook vigente (nil o que devuelve nil = sin webhook)
	MCP     http.Handler      // nil = sin MCP
	Ajustes *ajustes.Servicio // configuración desde la pantalla (nil = solo la del entorno)
	Ahora   func() time.Time  // para las pruebas
	HTTP    *http.Client      // para hablar con las otras apps y las pruebas de la configuración
	Probar  map[string]Prueba // pruebas de la configuración por sección (se pueden cambiar en las pruebas)
	// EstadoCorreo dice cómo viene el correo entrante que se revisa (IMAP o Gmail); nil = sin datos.
	EstadoCorreo func() EstadoCorreo
	Mudanza      *mudanza.Servicio // pasar lo del disco a R2 (nil = no disponible)
	Copias       *copias.Servicio  // copias de seguridad de la base (nil = no disponibles)

	limite         *auth.Limitador
	cupoEmail      *auth.Cupo // pedidos de «Olvidé mi contraseña» por email
	cupoIP         *auth.Cupo // y por IP
	repetidos      correo.Repetidos
	stats          correoStats
	oauthMu        sync.Mutex
	oauth          map[string]estadoOAuth
	arranqueMu     sync.Mutex
	codigoArranque string
	suiteMu        sync.Mutex
	suiteSalud     map[string]saludApp
	oidcMu         sync.Mutex
	oidcPend       map[string]pendienteOIDC // inicios con Lockatus a medias, por el SHA-256 del state
	oidcProv       *proveedorOIDC
	cupoOIDC       *auth.Cupo // inicios con Lockatus por IP
}

func (s *Server) cliente() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// cuitEnAlcance limpia un CUIT de cliente que viene en el pedido y controla que quien pide lo pueda
// usar. Vacío con un solo CUIT asignado: ese.
func cuitEnAlcance(id *auth.Identidad, cuit string) (string, error) {
	c := arca.SoloDigitos(cuit)
	if c == "" {
		if id.Restringido() && len(id.CUITs) == 1 {
			return id.CUITs[0], nil
		}
		return "", nil
	}
	if !id.Permite(c) {
		return "", fmt.Errorf("El CUIT %s no está entre tus clientes", arca.FormatearCUIT(c))
	}
	return c, nil
}

// cbteVisible trae un comprobante si quien pide lo puede ver (si no, «no encontrado»).
func (s *Server) cbteVisible(r *http.Request, id int64) (*store.Cbte, error) {
	c, err := s.App.Store.Comprobante(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if !IdentidadDe(r.Context()).Permite(c.ClienteCUIT) {
		return nil, store.ErrNoEncontrado
	}
	return c, nil
}

// original trae el archivo de un documento si quien pide lo puede ver.
func (s *Server) original(r *http.Request, id int64) (*store.Documento, []byte, error) {
	d, err := s.App.Store.Documento(r.Context(), id)
	if err != nil {
		return nil, nil, err
	}
	if !IdentidadDe(r.Context()).Permite(d.ClienteCUIT) {
		return nil, nil, store.ErrNoEncontrado
	}
	return s.App.Original(r.Context(), id)
}

// Handler arma el ruteo completo.
func (s *Server) Handler() http.Handler {
	if s.limite == nil {
		s.limite = auth.NuevoLimitador()
	}
	if s.cupoEmail == nil {
		s.cupoEmail = &auth.Cupo{N: 3, Ventana: time.Hour}
		s.cupoIP = &auth.Cupo{N: 10, Ventana: time.Hour}
	}
	if s.cupoOIDC == nil {
		s.cupoOIDC = &auth.Cupo{N: 30, Ventana: 10 * time.Minute}
	}
	if s.Probar == nil {
		s.Probar = s.pruebasPorDefecto()
	}
	lect, usu, adm, sup := auth.Lectura, auth.Usuario, auth.Admin, auth.Superadmin
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", s.salud)
	m.HandleFunc("GET /metrics", s.auth(lect, s.metricas))

	// Sesión, arranque y cuentas.
	m.HandleFunc("GET /api/arranque", s.verArranque)
	m.HandleFunc("POST /api/arranque", s.publicoCSRF(s.crearArranque))
	m.HandleFunc("POST /api/sesion", s.publicoCSRF(s.entrar))
	m.HandleFunc("GET /api/sesion", s.auth(lect, s.yo))
	m.HandleFunc("DELETE /api/sesion", s.soloSesion(lect, s.salirSesion))
	m.HandleFunc("POST /api/sesion/clave", s.soloSesion(lect, s.cambiarMiClave))
	m.HandleFunc("POST /api/sesion/olvide", s.publicoCSRF(s.olvideClave))
	m.HandleFunc("POST /api/sesion/link", s.publicoCSRF(s.verLinkCuenta))
	m.HandleFunc("POST /api/sesion/restablecer", s.publicoCSRF(s.usarLinkCuenta))
	m.HandleFunc("GET "+RutaOIDCLogin, s.loginOIDC)
	m.HandleFunc("GET "+RutaOIDCCallback, s.callbackOIDC)
	m.HandleFunc("GET /api/usuarios", s.soloSesion(adm, s.usuarios))
	m.HandleFunc("POST /api/usuarios", s.soloSesion(adm, s.crearUsuario))
	m.HandleFunc("PATCH /api/usuarios/{id}", s.soloSesion(adm, s.editarUsuario))
	m.HandleFunc("POST /api/usuarios/{id}/restablecer", s.soloSesion(adm, s.restablecerUsuario))
	m.HandleFunc("POST /api/usuarios/{id}/link", s.soloSesion(adm, s.mandarLinkUsuario))
	m.HandleFunc("GET /api/claves", s.soloSesion(adm, s.claves))
	m.HandleFunc("POST /api/claves", s.soloSesion(adm, s.crearClave))
	m.HandleFunc("DELETE /api/claves/{id}", s.soloSesion(adm, s.revocarClave))
	m.HandleFunc("GET /api/configuracion", s.soloSesion(adm, s.verConfiguracion))
	m.HandleFunc("PUT /api/configuracion", s.soloSesion(adm, s.guardarConfiguracion))
	m.HandleFunc("POST /api/configuracion/probar/{seccion}", s.soloSesion(sup, s.probarConfiguracion))
	m.HandleFunc("GET /api/auditoria", s.soloSesion(adm, s.verAuditoria))
	m.HandleFunc("GET /api/r2/pase", s.soloSesion(sup, s.verPaseR2))
	m.HandleFunc("POST /api/r2/pase", s.soloSesion(sup, s.accionPaseR2))
	m.HandleFunc("GET /api/copias", s.soloSesion(sup, s.verCopias))
	m.HandleFunc("POST /api/copias", s.soloSesion(sup, s.hacerCopia))
	m.HandleFunc("GET /api/copias/{id}/archivo", s.soloSesion(sup, s.descargarCopia))

	// Correo: lo que entra (cada forma con su firma), lo retenido y la conexión con Google.
	m.HandleFunc("POST "+rutaCloudflare, s.entranteCloudflare)
	m.HandleFunc("POST "+rutaProveedor+"{proveedor}", s.entranteProveedor)
	m.HandleFunc("GET /api/correo/retenido", s.soloSesion(adm, s.retenidos))
	m.HandleFunc("POST /api/correo/retenido/{id}/aceptar", s.soloSesion(adm, s.aceptarRetenido))
	m.HandleFunc("POST /api/correo/retenido/{id}/descartar", s.soloSesion(adm, s.descartarRetenido))
	m.HandleFunc("POST /api/correo/google/conectar", s.soloSesion(sup, s.conectarGoogle))
	m.HandleFunc("GET "+rutaCallback, s.callbackGoogle)
	m.HandleFunc("POST /api/correo/google/desconectar", s.soloSesion(sup, s.desconectarGoogle))

	// Pase entre apps de la suite (docs/suite-pase.md).
	m.HandleFunc("POST /api/suite/pase", s.auth(lect, s.emitirPase))
	m.HandleFunc("GET /suite/entrar", s.entrarConPase)
	m.HandleFunc("GET /api/suite/apps", s.soloSesion(lect, s.appsSuite))
	m.HandleFunc("POST /api/suite/abrir/{app}", s.soloSesion(lect, s.abrirApp))

	m.HandleFunc("POST /api/verificar", s.auth(lect, s.verificar))
	m.HandleFunc("POST /api/lotes", s.auth(usu, s.crearLote))
	m.HandleFunc("GET /api/lotes", s.auth(lect, s.lotes))
	m.HandleFunc("GET /api/lotes/{id}", s.auth(lect, s.lote))
	m.HandleFunc("GET /api/comprobantes", s.auth(lect, s.comprobantes))
	m.HandleFunc("GET /api/comprobantes/{id}", s.auth(lect, s.comprobante))
	m.HandleFunc("PATCH /api/comprobantes/{id}", s.auth(usu, s.corregir))
	m.HandleFunc("POST /api/comprobantes/{id}/constatar", s.auth(usu, s.constatar))
	m.HandleFunc("GET /api/documentos/{id}/archivo", s.auth(lect, s.archivo))
	m.HandleFunc("GET /api/documentos/{id}/pagina/{n}", s.auth(lect, s.pagina))
	m.HandleFunc("GET /api/export.xlsx", s.auth(lect, s.exportar("xlsx")))
	m.HandleFunc("GET /api/export.csv", s.auth(lect, s.exportar("csv")))
	m.HandleFunc("GET /api/resumen", s.auth(lect, s.resumen))
	m.HandleFunc("GET /api/panel", s.auth(lect, s.panel))
	m.HandleFunc("GET /api/novedades", s.novedades)
	m.HandleFunc("GET /api/rubros", s.auth(lect, s.rubros))
	m.HandleFunc("GET /api/canales", s.auth(lect, s.canales))
	m.HandleFunc("GET /api/canales/whatsapp/qr.png", s.soloSesion(sup, s.qrWhatsApp))
	m.HandleFunc("POST /api/canales/whatsapp/vincular", s.soloSesion(sup, s.vincularWhatsApp))
	m.HandleFunc("POST /api/canales/whatsapp/desvincular", s.soloSesion(sup, s.desvincularWhatsApp))
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

// jsonCrudo escribe el mapa tal cual (sin forzar ok).
func jsonCrudo(w http.ResponseWriter, status int, v map[string]any) {
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
	case errors.Is(err, store.ErrFiltro):
		jsonErr(w, http.StatusBadRequest, err.Error())
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

// filtroDe lee el filtro de la query y le pone el alcance de quien pide.
func filtroDe(r *http.Request) store.Filtro {
	q := r.URL.Query()
	n := func(k string) int { v, _ := strconv.Atoi(q.Get(k)); return v }
	return store.Filtro{
		Lote: int64(n("lote")), Estado: q.Get("estado"), Cliente: arca.SoloDigitos(q.Get("cliente")),
		Emisor: arca.SoloDigitos(q.Get("emisor")), Desde: q.Get("desde"), Hasta: q.Get("hasta"), Q: q.Get("q"),
		Clase: strings.ToLower(strings.TrimSpace(q.Get("clase"))), Rubro: strings.ToLower(strings.TrimSpace(q.Get("rubro"))),
		Limite: min(max(n("limite"), 0), 500), Offset: max(n("offset"), 0),
		Alcance: IdentidadDe(r.Context()).Alcance(),
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
	if v := s.App.Pipeline.Vision(); v != nil {
		ia = map[string]any{"habilitada": true, "modelo": v.Modelo, "local": v.Local()}
	}
	base := s.App.Store.Pool.Ping(r.Context()) == nil
	cfg := s.App.Config()
	jsonOK(w, http.StatusOK, map[string]any{
		"version": config.Version, "base": base, "ocr": s.App.Pipeline.OCR != nil, "ia": ia,
		"arcanum": s.App.Arcanum() != nil, "almacen": s.App.Almacen.Backend(), "cifrado": s.App.Almacen.Cifrado(),
		"nombre": cfg.Nombre, "ia_por_defecto": cfg.IAPorDefecto, "constatar_por_defecto": cfg.ConstatarPorDefecto,
		"arranque": s.arranquePendiente(),
	})
}

func (s *Server) metricas(w http.ResponseWriter, r *http.Request) {
	if IdentidadDe(r.Context()).Restringido() {
		jsonErr(w, http.StatusForbidden, "Las métricas son de toda la instalación: hace falta una clave sin restricción de clientes")
		return
	}
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
	maxArchivo := s.App.Config().MaxArchivo
	r.Body = http.MaxBytesReader(w, r.Body, maxArchivo*4/3+1<<20)
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
		datos, err = io.ReadAll(io.LimitReader(f, maxArchivo+1))
		if int64(len(datos)) > maxArchivo {
			return "", nil, nil, fmt.Errorf("el archivo supera los %d MB", maxArchivo>>20)
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
	id := IdentidadDe(r.Context())
	ia := s.App.Config().IAPorDefecto
	if v, ok := campos["ia"]; ok {
		ia = verdadero(v)
	}
	consultante, err := cuitEnAlcance(id, campos["cuit_consultante"])
	if err != nil {
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	guardar := verdadero(campos["guardar"]) && id.Rol >= auth.Usuario
	if guardar && id.Restringido() && consultante == "" {
		jsonErr(w, http.StatusBadRequest, "Para guardar, elegí el CUIT del cliente (cuit_consultante)")
		return
	}
	v, err := s.App.Verificar(r.Context(), nombre, datos, consultante, ia, guardar)
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
			b, err := io.ReadAll(io.LimitReader(f, s.App.Config().MaxArchivo*8+1)) // un ZIP puede ser más grande que un archivo suelto
			f.Close()
			if err != nil {
				continue
			}
			arch = append(arch, app.Archivo{Nombre: h.Filename, Mime: h.Header.Get("Content-Type"), Datos: b})
		}
	}
	id := IdentidadDe(r.Context())
	cliente, err := cuitEnAlcance(id, r.FormValue("cliente_cuit"))
	if err != nil {
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if id.Restringido() && cliente == "" {
		jsonErr(w, http.StatusBadRequest, "Elegí el CUIT del cliente: tu usuario tiene más de un cliente asignado")
		return
	}
	cfg, hayArcanum := s.App.Config(), s.App.Arcanum() != nil
	ia := cfg.IAPorDefecto
	if v := r.FormValue("ia"); v != "" {
		ia = verdadero(v)
	}
	constatar := cfg.ConstatarPorDefecto && hayArcanum
	if v := r.FormValue("constatar"); v != "" {
		constatar = verdadero(v) && hayArcanum
	}
	origen := r.FormValue("origen")
	if origen != "arcanum" && origen != "mcp" {
		origen = "web"
	}
	res, err := s.App.CrearLote(r.Context(), store.LoteNuevo{
		Nombre: strings.TrimSpace(r.FormValue("nombre")), ClienteCUIT: cliente,
		Origen: origen, IA: ia, Constatar: constatar, CreadoPor: id.UsuarioID,
	}, arch, 0)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.auditar(r, "lote.crear", fmt.Sprintf("lote %d", res.Lote.ID), map[string]any{"cliente_cuit": cliente, "archivos": res.Recibidos, "origen": origen})
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
	ls, total, err := s.App.Store.Lotes(r.Context(), f.Limite, f.Offset, f.Alcance)
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
	if err == nil && !IdentidadDe(r.Context()).Permite(l.ClienteCUIT) {
		err = store.ErrNoEncontrado
	}
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
	c, err := s.cbteVisible(r, id)
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
	if _, err := s.cbteVisible(r, id); err != nil {
		s.fallo(w, err)
		return
	}
	campos := map[string]string{}
	nombres := []string{}
	for k, v := range b.Campos {
		campos[k] = fmt.Sprint(v)
		nombres = append(nombres, k)
	}
	sort.Strings(nombres)
	// Con sesión, el papel de trabajo dice quién es la persona (no lo que mande el pedido). Con una
	// clave, la app que llama (Arcanum) puede decir en nombre de quién corrige.
	quien := IdentidadDe(r.Context()).Quien()
	if IdentidadDe(r.Context()).Tipo != auth.TipoSesion && strings.TrimSpace(b.Usuario) != "" {
		quien = strings.TrimSpace(b.Usuario)
	}
	if len(quien) > 100 {
		quien = quien[:100]
	}
	c, err := s.App.Corregir(r.Context(), id, campos, b.Aprobar, quien)
	if err != nil {
		if errors.Is(err, store.ErrNoEncontrado) {
			s.fallo(w, err)
			return
		}
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(nombres) > 0 {
		s.auditar(r, "comprobante.corregir", fmt.Sprintf("comprobante %d", id), map[string]any{"campos": nombres, "en_nombre_de": quien})
	}
	if b.Aprobar {
		s.auditar(r, "comprobante.aprobar", fmt.Sprintf("comprobante %d", id), map[string]any{"en_nombre_de": quien})
	}
	jsonOK(w, http.StatusOK, map[string]any{"comprobante": c})
}

// rubros devuelve los rubros que se recuerdan por CUIT del emisor (lo que corrigió una persona) y la
// lista de rubros válidos.
func (s *Server) rubros(w http.ResponseWriter, r *http.Request) {
	rs, err := s.App.Store.Rubros(r.Context(), IdentidadDe(r.Context()).Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"rubros": rubro.Todos, "por_cuit": rs})
}

func (s *Server) constatar(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	if _, err := s.cbteVisible(r, id); err != nil {
		s.fallo(w, err)
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
	d, b, err := s.original(r, id)
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
	d, b, err := s.original(r, id)
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
		f := filtroDe(r)
		filas, err := s.App.Filas(r.Context(), f)
		if err != nil {
			s.fallo(w, err)
			return
		}
		s.auditar(r, "exportar", formato, map[string]any{"filas": len(filas), "filtro": r.URL.RawQuery})
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
	jsonOK(w, http.StatusOK, map[string]any{"canales": s.canalesPara(IdentidadDe(r.Context()))})
}

// canalesPara: el QR de vinculación de WhatsApp lo ve solo un superadmin (con él se vincula un
// teléfono a este servidor).
func (s *Server) canalesPara(id *auth.Identidad) []canal.Estado {
	cs := s.App.Canales()
	if id == nil || id.Rol < auth.Superadmin {
		for i := range cs {
			cs[i].QR = ""
		}
	}
	return cs
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
		jsonErr(w, http.StatusNotFound, "WhatsApp no está activado (Configuración, WhatsApp)")
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
	s.auditar(r, "config.whatsapp_desvincular", "whatsapp", nil)
	jsonOK(w, http.StatusOK, map[string]any{})
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	var wh Webhook
	if s.Webhook != nil {
		wh = s.Webhook()
	}
	if wh == nil {
		jsonErr(w, http.StatusNotFound, "El webhook no está habilitado (Configuración, Webhook)")
		return
	}
	if !wh.Autorizado(r.Header.Get("X-Factorum-Token")) {
		jsonErr(w, http.StatusUnauthorized, "Token inválido")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 35<<20))
	if err != nil {
		jsonErr(w, http.StatusRequestEntityTooLarge, "Cuerpo demasiado grande")
		return
	}
	m, err := wh.Decodificar(body)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"respuesta": s.App.ManejarMensaje(r.Context(), m)})
}
