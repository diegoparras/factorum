package api

// Pase entre apps de la suite Escriba: el contrato está en docs/suite-pase.md.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

// VidaPase es cuánto dura un pase sin usar.
const VidaPase = 60 * time.Second

// AvisoPaseVencido es el mensaje del login cuando el pase no sirve.
const AvisoPaseVencido = "El pase para entrar venció o ya se usó. Volvé a tocar el botón en la otra app."

type saludApp struct {
	cuando time.Time
	ok     bool
}

// ---------- Receptora ----------

// POST /api/suite/pase: solo claves de API habilitadas para la suite (una creada en la pantalla con
// la marca, o FACTORUM_API_KEY si FACTORUM_API_KEY_SUITE_TOPE tiene un rol).
func (s *Server) emitirPase(w http.ResponseWriter, r *http.Request) {
	id := IdentidadDe(r.Context())
	if (id.Tipo != auth.TipoClave && id.Tipo != auth.TipoEnv) || !id.Suite {
		jsonErr(w, http.StatusForbidden, "Esta clave no está habilitada para abrir sesiones de la suite")
		return
	}
	var b struct {
		App    string          `json:"app"`
		Email  string          `json:"email"`
		Nombre string          `json:"nombre"`
		Rol    string          `json:"rol"`
		CUITs  json.RawMessage `json:"cuits"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	app := strings.ToLower(strings.TrimSpace(b.App))
	if app != "arcanum" && app != "factorum" {
		jsonErr(w, http.StatusBadRequest, "app tiene que ser arcanum o factorum")
		return
	}
	email, err := validarEmail(b.Email)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := s.App.Config()
	if cfg.URLPublica == "" {
		jsonErr(w, http.StatusConflict, "Falta la URL pública de Factorum")
		return
	}
	ctx := r.Context()
	// 1. Traducir el rol que trae de la otra app.
	rolOrigen, rolOK := auth.RolDeApp(app, b.Rol)
	cuitsOrigen, err := leerCUITs(b.CUITs)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, capital(err.Error()))
		return
	}
	// 2. Si ya existe, manda lo de acá. 3. Si no, se crea (si está permitido).
	u, err := s.App.Store.UsuarioPorEmail(ctx, email)
	creado := false
	var rol auth.Rol
	var cuits []string
	switch {
	case errors.Is(err, store.ErrNoEncontrado):
		if !cfg.SuiteCrearUsuarios {
			jsonErr(w, http.StatusForbidden, "Esa persona no tiene cuenta en Factorum y la creación automática está apagada")
			return
		}
		if !rolOK {
			jsonErr(w, http.StatusForbidden, fmt.Sprintf("El rol %q de %s no tiene equivalente en Factorum", b.Rol, app))
			return
		}
		// Se crea con el rol topeado: nadie nace superadmin por venir de otra app.
		u, err = s.App.Store.CrearUsuario(ctx, store.UsuarioNuevo{Email: email, Nombre: nombreLimpio(b.Nombre, email),
			Rol: min(rolOrigen, id.SuiteTope), CUITs: cuitsOrigen, Origen: app})
		if errors.Is(err, store.ErrEmailUsado) { // se creó en paralelo
			u, err = s.App.Store.UsuarioPorEmail(ctx, email)
		} else if err == nil {
			creado = true
		}
		if err != nil {
			s.fallo(w, err)
			return
		}
		rol, cuits = u.Rol, u.CUITs
	case err != nil:
		s.fallo(w, err)
		return
	default:
		if !u.Activo {
			jsonErr(w, http.StatusForbidden, "La cuenta de esa persona en Factorum está desactivada")
			return
		}
		rol, cuits = u.Rol, u.CUITs
	}
	// 4. Tope de la clave y CUITs de la clave.
	rol = min(rol, id.SuiteTope)
	cuits = auth.Intersecar(cuits, id.CUITs)
	// 5. Que quede algo.
	if rol == auth.Ninguno {
		jsonErr(w, http.StatusForbidden, "Con el tope de la clave no queda ningún rol válido")
		return
	}
	if cuits != nil && len(cuits) == 0 {
		jsonErr(w, http.StatusForbidden, "Esa persona y la clave no tienen ningún CUIT de cliente en común")
		return
	}
	token := auth.NuevoToken()
	vence := s.ahora().Add(VidaPase)
	if err := s.App.Store.CrearPase(ctx, store.Pase{Hash: auth.HashToken(token), UsuarioID: u.ID, App: app, Rol: rol, CUITs: cuits,
		ClaveID: id.ClaveID, Vence: vence}); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditarComo(r, &auth.Identidad{Tipo: "suite", Email: u.Email, UsuarioID: u.ID}, "suite.pase_emitido", u.Email,
		map[string]any{"app": app, "clave": id.ClavePrefijo, "rol": rol.String(), "rol_en_origen": b.Rol, "cuenta_creada": creado})
	if creado {
		s.auditarComo(r, id, "usuario.crear", u.Email, map[string]any{"rol": u.Rol.String(), "cuits": u.CUITs, "origen": app})
	}
	jsonOK(w, http.StatusOK, map[string]any{"url": cfg.URLPublica + "/suite/entrar?pase=" + token, "vence": vence.UTC().Format(time.RFC3339)})
}

// GET /suite/entrar?pase=...: canjea el pase por una sesión y lleva a la portada.
func (s *Server) entrarConPase(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	token := r.URL.Query().Get("pase")
	if token == "" || len(token) > 100 {
		http.Redirect(w, r, "/?aviso=pase", http.StatusSeeOther)
		return
	}
	nuevo := auth.NuevoToken()
	u, p, err := s.App.Store.CanjearPase(r.Context(), auth.HashToken(token), store.Sesion{ID: auth.HashToken(nuevo),
		Vence: s.ahora().Add(Inactividad), IP: ipDe(r), UserAgent: r.UserAgent()})
	if err != nil {
		if !errors.Is(err, store.ErrPaseInvalido) && !errors.Is(err, store.ErrNoEncontrado) {
			s.Log.Error("no se pudo canjear un pase de la suite", "error", err)
		}
		s.auditarComo(r, &auth.Identidad{Tipo: "suite"}, "suite.pase_rechazado", "", nil)
		http.Redirect(w, r, "/?aviso=pase", http.StatusSeeOther)
		return
	}
	s.ponerCookie(w, nuevo)
	s.auditarComo(r, &auth.Identidad{Tipo: auth.TipoSesion, Email: u.Email, UsuarioID: u.ID}, "suite.pase_usado", u.Email,
		map[string]any{"app": p.App, "rol": p.Rol.String()})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// ---------- Origen ----------

type appSuite struct {
	App        string `json:"app"`
	Nombre     string `json:"nombre"`
	Disponible bool   `json:"disponible"`
}

// GET /api/suite/apps: las apps conectadas (por ahora, Arcanum).
func (s *Server) appsSuite(w http.ResponseWriter, r *http.Request) {
	apps := []appSuite{}
	cfg := s.App.Config()
	if cfg.ArcanumURL != "" && cfg.ArcanumAPIKey != "" {
		apps = append(apps, appSuite{App: "arcanum", Nombre: "Arcanum", Disponible: s.disponible(r, "arcanum", cfg.ArcanumURL+"/api/health")})
	}
	jsonOK(w, http.StatusOK, map[string]any{"apps": apps})
}

// disponible: la otra app respondió su healthz en los últimos 60 segundos (se guarda el resultado).
func (s *Server) disponible(r *http.Request, app, urlSalud string) bool {
	s.suiteMu.Lock()
	if s.suiteSalud == nil {
		s.suiteSalud = map[string]saludApp{}
	}
	x, ok := s.suiteSalud[app+" "+urlSalud]
	s.suiteMu.Unlock()
	if ok && time.Since(x.cuando) < 60*time.Second {
		return x.ok
	}
	ctx, cancel := contextoCorto(r, 3*time.Second)
	defer cancel()
	var b struct {
		OK bool `json:"ok"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, urlSalud, nil, nil, &b)
	bien := err == nil && st == 200 && b.OK
	s.suiteMu.Lock()
	s.suiteSalud[app+" "+urlSalud] = saludApp{cuando: time.Now(), ok: bien}
	s.suiteMu.Unlock()
	return bien
}

// POST /api/suite/abrir/{app}: pide un pase a la otra app con la credencial guardada y devuelve la URL.
func (s *Server) abrirApp(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	cfg := s.App.Config()
	if app != "arcanum" {
		jsonErr(w, http.StatusNotFound, "Esa app no está conectada")
		return
	}
	if cfg.ArcanumURL == "" || cfg.ArcanumAPIKey == "" {
		jsonErr(w, http.StatusNotFound, "Arcanum no está conectado (Configuración, Arcanum)")
		return
	}
	id := IdentidadDe(r.Context())
	if id.Email == "" {
		jsonErr(w, http.StatusBadRequest, "Tu sesión no tiene email: no se puede pedir un pase")
		return
	}
	// El rol va con el nombre de Factorum (Arcanum traduce usuario a operador).
	cuerpo, _ := json.Marshal(map[string]any{"app": "factorum", "email": id.Email, "nombre": id.Nombre,
		"rol": id.Rol.String(), "cuits": id.CUITs})
	var b struct {
		OK    bool   `json:"ok"`
		URL   string `json:"url"`
		Error string `json:"error"`
	}
	ctx, cancel := contextoCorto(r, 15*time.Second)
	defer cancel()
	st, err := s.pedirJSON(ctx, http.MethodPost, cfg.ArcanumURL+"/api/suite/pase", map[string]string{"Authorization": "Bearer " + cfg.ArcanumAPIKey}, cuerpo, &b)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, "Arcanum "+err.Error())
		return
	}
	if st != 200 || !b.OK {
		msg := b.Error
		if msg == "" {
			msg = fmt.Sprintf("respondió %d", st)
		}
		if st < 400 || st >= 500 {
			st = http.StatusBadGateway
		}
		s.auditar(r, "suite.abrir_fallido", app, map[string]any{"status": st, "error": msg})
		jsonErr(w, st, "Arcanum: "+msg)
		return
	}
	u, err := url.Parse(b.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		jsonErr(w, http.StatusBadGateway, "Arcanum devolvió una dirección inválida")
		return
	}
	if pub := cfg.ArcanumURLPublica; pub != "" && !strings.HasPrefix(b.URL, pub+"/") {
		jsonErr(w, http.StatusBadGateway, "Arcanum devolvió una dirección que no es su URL pública ("+pub+"): revisá la configuración de las dos apps")
		return
	}
	s.auditar(r, "suite.abrir", app, map[string]any{"rol": id.Rol.String()})
	jsonOK(w, http.StatusOK, map[string]any{"url": b.URL})
}
