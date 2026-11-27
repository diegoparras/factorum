package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

// CookieSesion es el nombre de la cookie de sesión.
const CookieSesion = "factorum_sesion"

// CabeceraCSRF la manda la UI en todo pedido que modifica: un sitio ajeno no puede ponerla sin
// que el navegador pregunte antes (CORS), y además se controla el Origin.
const CabeceraCSRF = "X-Factorum"

// Inactividad es cuánto dura una sesión sin uso.
const Inactividad = 12 * time.Hour

type ctxIdentidad struct{}

// IdentidadDe devuelve quién hace el pedido (nil si nadie).
func IdentidadDe(ctx context.Context) *auth.Identidad {
	i, _ := ctx.Value(ctxIdentidad{}).(*auth.Identidad)
	return i
}

// ConIdentidad guarda la identidad en el contexto.
func ConIdentidad(ctx context.Context, i *auth.Identidad) context.Context {
	return context.WithValue(ctx, ctxIdentidad{}, i)
}

func (s *Server) ahora() time.Time {
	if s.Ahora != nil {
		return s.Ahora()
	}
	return time.Now()
}

// claveDe saca la clave de API del pedido (X-API-Key o Authorization: Bearer).
func claveDe(r *http.Request) string {
	k := strings.TrimSpace(r.Header.Get("X-API-Key"))
	if k == "" {
		if a := r.Header.Get("Authorization"); len(a) > 7 && strings.EqualFold(a[:7], "Bearer ") {
			k = strings.TrimSpace(a[7:])
		}
	}
	return k
}

var errNoAutenticado = errors.New("no autenticado")

// Identificar resuelve quién hace el pedido: una clave de API (en la cabecera) o, si no viene
// ninguna, la cookie de sesión. Lo usa también MCP (que solo acepta claves).
func (s *Server) Identificar(r *http.Request) (*auth.Identidad, error) {
	if k := claveDe(r); k != "" {
		return s.identificarClave(r.Context(), k)
	}
	c, err := r.Cookie(CookieSesion)
	if err != nil || c.Value == "" {
		return nil, errNoAutenticado
	}
	return s.identificarSesion(r.Context(), c.Value)
}

// IdentificarClave resuelve solo claves de API (para MCP: nunca la cookie del navegador).
func (s *Server) IdentificarClave(r *http.Request) (*auth.Identidad, error) {
	k := claveDe(r)
	if k == "" {
		return nil, errNoAutenticado
	}
	return s.identificarClave(r.Context(), k)
}

func (s *Server) identificarClave(ctx context.Context, k string) (*auth.Identidad, error) {
	if auth.Iguales(k, s.Cfg.APIKey) {
		// FACTORUM_API_KEY_SUITE_TOPE la habilita para la suite (Arcanum la usa como FACTORUM_TOKEN).
		tope, _ := auth.ParsearRol(s.Cfg.APIKeySuiteTope)
		return &auth.Identidad{Tipo: auth.TipoEnv, Rol: auth.Usuario, ClavePrefijo: "FACTORUM_API_KEY",
			Suite: tope != auth.Ninguno, SuiteTope: tope}, nil
	}
	if auth.Iguales(k, s.Cfg.APIKeyLect) {
		return &auth.Identidad{Tipo: auth.TipoEnv, Rol: auth.Lectura}, nil
	}
	if !strings.HasPrefix(k, auth.PrefijoClave) {
		return nil, errNoAutenticado
	}
	c, err := s.App.Store.ClavePorHash(ctx, auth.HashToken(k), s.ahora())
	if errors.Is(err, store.ErrNoEncontrado) {
		return nil, errNoAutenticado
	}
	if err != nil {
		return nil, err
	}
	if !c.DuenoActivo {
		return nil, errNoAutenticado // si se desactiva a la persona, sus claves dejan de andar
	}
	if err := s.App.Store.TocarClave(ctx, c.ID); err != nil {
		s.Log.Warn("no se pudo anotar el uso de una clave", "clave", c.Prefijo, "error", err)
	}
	return &auth.Identidad{
		Tipo: auth.TipoClave, Rol: min(c.Rol, c.DuenoRol), CUITs: auth.Intersecar(c.CUITs, c.DuenoCUITs),
		ClaveID: c.ID, ClavePrefijo: c.Prefijo, Suite: c.Suite, SuiteTope: c.SuiteTope,
	}, nil
}

func (s *Server) identificarSesion(ctx context.Context, token string) (*auth.Identidad, error) {
	ahora := s.ahora()
	q, u, err := s.App.Store.SesionValida(ctx, auth.HashToken(token), ahora)
	if errors.Is(err, store.ErrNoEncontrado) {
		return nil, errNoAutenticado
	}
	if err != nil {
		return nil, err
	}
	if !u.Activo {
		return nil, errNoAutenticado
	}
	rol, cuits := u.Rol, u.CUITs
	if q.RolTope != auth.Ninguno {
		rol = min(rol, q.RolTope)
	}
	if q.TieneCUIT {
		cuits = auth.Intersecar(cuits, q.CUITs)
	}
	if ahora.Sub(q.UltimoUso) > time.Minute {
		if err := s.App.Store.TocarSesion(ctx, q.ID, ahora.Add(Inactividad)); err != nil {
			s.Log.Warn("no se pudo extender la sesión", "error", err)
		}
	}
	// Quien entró con Lockatus no usa la contraseña de Factorum: una temporal pendiente no lo frena.
	debe := u.DebeCambiar && q.Origen != OrigenLockatus
	return &auth.Identidad{Tipo: auth.TipoSesion, UsuarioID: u.ID, Email: u.Email, Nombre: u.Nombre, Rol: rol, CUITs: cuits,
		DebeCambiar: debe, Origen: q.Origen, SesionID: q.ID}, nil
}

// Rutas que puede usar alguien con la contraseña temporal sin cambiar.
var rutasConClaveTemporal = map[string]bool{"/api/sesion": true, "/api/sesion/clave": true}

// Rutas que puede usar alguien sin ningún cliente asignado (entró con Lockatus y todavía nadie le
// asignó clientes): ver quién es, cambiar su contraseña y salir. Todo lo demás le da 403.
var rutasSinClientes = map[string]bool{"/api/sesion": true, "/api/sesion/clave": true}

// auth exige una identidad con al menos ese rol. Con cookie, los pedidos que modifican tienen que
// traer la cabecera X-Factorum y un Origin propio (CSRF); con clave de API no hace falta.
func (s *Server) auth(min auth.Rol, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := s.Identificar(r)
		if err != nil {
			if !errors.Is(err, errNoAutenticado) {
				s.Log.Error("no se pudo autenticar", "error", err)
			}
			jsonErr(w, http.StatusUnauthorized, "Iniciá sesión o mandá una clave de API válida (X-API-Key o Authorization: Bearer)")
			return
		}
		if id.Tipo == auth.TipoSesion && modifica(r) && !s.csrfOK(r) {
			jsonErr(w, http.StatusForbidden, "Pedido rechazado: falta la cabecera X-Factorum o el origen no coincide")
			return
		}
		if id.DebeCambiar && !rutasConClaveTemporal[r.URL.Path] {
			jsonCrudo(w, http.StatusForbidden, map[string]any{"ok": false, "debe_cambiar": true, "error": "Cambiá la contraseña temporal para seguir"})
			return
		}
		if id.Rol < min {
			jsonErr(w, http.StatusForbidden, fmt.Sprintf("Tu rol (%s) no alcanza para esto: hace falta %s", id.Rol, min))
			return
		}
		if id.CUITs != nil && len(id.CUITs) == 0 && !rutasSinClientes[r.URL.Path] {
			jsonErr(w, http.StatusForbidden, "No tenés ningún cliente asignado")
			return
		}
		h(w, r.WithContext(ConIdentidad(r.Context(), id)))
	}
}

// soloSesion es como auth, pero no acepta claves de API (lo que tiene sentido solo en el navegador).
func (s *Server) soloSesion(min auth.Rol, h http.HandlerFunc) http.HandlerFunc {
	return s.auth(min, func(w http.ResponseWriter, r *http.Request) {
		if IdentidadDe(r.Context()).Tipo != auth.TipoSesion {
			jsonErr(w, http.StatusForbidden, "Esto se hace desde el navegador, con una sesión iniciada")
			return
		}
		h(w, r)
	})
}

// publicoCSRF protege los pedidos sin sesión que vienen de la UI (entrar, crear el primer superadmin).
func (s *Server) publicoCSRF(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.csrfOK(r) {
			jsonErr(w, http.StatusForbidden, "Pedido rechazado: falta la cabecera X-Factorum o el origen no coincide")
			return
		}
		h(w, r)
	}
}

func modifica(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

// csrfOK: la cabecera propia y un Origin (o Referer) del mismo sitio o de la URL pública.
func (s *Server) csrfOK(r *http.Request) bool {
	if r.Header.Get(CabeceraCSRF) != "1" {
		return false
	}
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		if ref, err := url.Parse(r.Header.Get("Referer")); err == nil && ref.Host != "" {
			o = ref.Scheme + "://" + ref.Host
		}
	}
	u, err := url.Parse(o)
	if o == "" || err != nil || u.Host == "" {
		return false
	}
	if pub := s.App.Config().URLPublica; pub != "" {
		if p, err := url.Parse(pub); err == nil && strings.EqualFold(p.Host, u.Host) && p.Scheme == u.Scheme {
			return true
		}
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && detrasDeProxy(r) {
		host = strings.TrimSpace(strings.Split(fh, ",")[0])
	}
	return strings.EqualFold(u.Host, host)
}

// detrasDeProxy: el pedido llegó desde una IP privada o local (un proxy inverso del mismo servidor
// o de la red interna), así que sus cabeceras X-Forwarded-* son creíbles.
func detrasDeProxy(r *http.Request) bool {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

// ipDe es la IP de quien pide (la del proxy si el pedido no pasó por uno de confianza).
func ipDe(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		h = r.RemoteAddr
	}
	if detrasDeProxy(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if p := strings.TrimSpace(strings.Split(xff, ",")[0]); net.ParseIP(p) != nil {
				return p
			}
		}
		if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(xr) != nil {
			return xr
		}
	}
	return h
}

// ponerCookie abre la sesión en el navegador.
func (s *Server) ponerCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: CookieSesion, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.App.Config().EsHTTPS()})
}

func (s *Server) borrarCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: CookieSesion, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: s.App.Config().EsHTTPS(), MaxAge: -1})
}

// abrirSesion crea la sesión de una persona y pone la cookie.
func (s *Server) abrirSesion(w http.ResponseWriter, r *http.Request, u *store.Usuario) error {
	return s.abrirSesionCon(w, r, u, "local")
}

// abrirSesionCon es abrirSesion con el origen de la sesión (local o lockatus).
func (s *Server) abrirSesionCon(w http.ResponseWriter, r *http.Request, u *store.Usuario, origen string) error {
	token := auth.NuevoToken()
	err := s.App.Store.CrearSesion(r.Context(), store.Sesion{ID: auth.HashToken(token), UsuarioID: u.ID, Vence: s.ahora().Add(Inactividad),
		IP: ipDe(r), UserAgent: r.UserAgent(), Origen: origen})
	if err != nil {
		return err
	}
	if err := s.App.Store.MarcarIngreso(r.Context(), u.ID); err != nil {
		s.Log.Warn("no se pudo anotar el ingreso", "error", err)
	}
	s.ponerCookie(w, token)
	return nil
}

// auditar anota un evento con la identidad del pedido. Nunca lleva secretos.
func (s *Server) auditar(r *http.Request, accion, objeto string, detalle map[string]any) {
	id := IdentidadDe(r.Context())
	s.auditarComo(r, id, accion, objeto, detalle)
}

func (s *Server) auditarComo(r *http.Request, id *auth.Identidad, accion, objeto string, detalle map[string]any) {
	e := store.Evento{Accion: accion, Objeto: objeto, Detalle: detalle, IP: ipDe(r)}
	if id != nil {
		e.Actor, e.ActorTipo = id.Quien(), id.Tipo
		if id.UsuarioID > 0 {
			uid := id.UsuarioID
			e.UsuarioID = &uid
		}
	}
	if err := s.App.Store.Auditar(context.WithoutCancel(r.Context()), e); err != nil {
		s.Log.Error("no se pudo anotar en la auditoría", "accion", accion, "error", err)
	}
}
