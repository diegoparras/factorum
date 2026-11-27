package api

// Inicio de sesión único con Lockatus (OIDC): Authorization Code con PKCE S256, state y nonce. El
// state, el nonce y el verificador quedan del lado del servidor, sirven una sola vez y vencen a los
// 10 minutos; el navegador lleva el state en una cookie HttpOnly y SameSite=Lax de vida corta, así
// un callback que no empezó en este navegador no sirve. La persona se reconoce por su email
// verificado y entra con su rol y sus clientes de Factorum.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/oidc"
	"github.com/diegoparras/factorum/internal/store"
)

const (
	// RutaOIDCLogin manda al navegador a Lockatus.
	RutaOIDCLogin = "/api/auth/oidc/login"
	// RutaOIDCCallback es a donde vuelve (el redirect que se registra en Lockatus).
	RutaOIDCCallback = "/api/auth/oidc/callback"
	// OrigenLockatus es el origen de las cuentas creadas y de las sesiones abiertas con Lockatus.
	OrigenLockatus = "lockatus"

	cookieOIDC      = "factorum_oidc"
	vidaOIDC        = 10 * time.Minute
	maxPendientes   = 10000
	vidaProveedor   = time.Hour // cada cuánto se vuelve a leer el discovery
	msgSoloLockatus = "El ingreso con email y contraseña está apagado: entrá con Lockatus."
)

type pendienteOIDC struct {
	verificador, nonce string
	config             string // con qué configuración se pidió
	vence              time.Time
}

type proveedorOIDC struct {
	clave  string
	prov   *oidc.Proveedor
	armado time.Time
}

// claveOIDC resume la configuración de Lockatus: si cambia, se vuelve a leer el discovery y los
// inicios a medias dejan de servir.
func claveOIDC(c *config.Config) string {
	return strings.Join([]string{c.OIDCEmisor, c.OIDCClienteID, auth.HashToken(c.OIDCClienteSecreto), c.OIDCRedirect(),
		strings.Join(c.OIDCScopes(), " ")}, "|")
}

// proveedor devuelve el emisor ya descubierto (lo vuelve a leer si cambió la configuración o pasó
// una hora).
func (s *Server) proveedor(ctx context.Context, c *config.Config) (*oidc.Proveedor, error) {
	clave := claveOIDC(c)
	s.oidcMu.Lock()
	p := s.oidcProv
	s.oidcMu.Unlock()
	if p != nil && p.clave == clave && time.Since(p.armado) < vidaProveedor {
		return p.prov, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	prov, err := oidc.Nuevo(ctx, oidc.Config{Emisor: c.OIDCEmisor, ClienteID: c.OIDCClienteID, ClienteSecreto: c.OIDCClienteSecreto,
		Redirect: c.OIDCRedirect(), Scopes: c.OIDCScopes(), HTTP: s.cliente(), Ahora: s.ahora})
	if err != nil {
		return nil, err
	}
	s.oidcMu.Lock()
	s.oidcProv = &proveedorOIDC{clave: clave, prov: prov, armado: time.Now()}
	s.oidcMu.Unlock()
	return prov, nil
}

func (s *Server) guardarPendiente(state string, p pendienteOIDC) bool {
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	if s.oidcPend == nil {
		s.oidcPend = map[string]pendienteOIDC{}
	}
	if len(s.oidcPend) >= maxPendientes/2 {
		ahora := time.Now()
		for k, v := range s.oidcPend {
			if ahora.After(v.vence) {
				delete(s.oidcPend, k)
			}
		}
	}
	if len(s.oidcPend) >= maxPendientes {
		return false
	}
	s.oidcPend[auth.HashToken(state)] = p
	return true
}

// tomarPendiente saca el inicio pendiente de ese state (sirve una sola vez, aunque falle).
func (s *Server) tomarPendiente(state string) (pendienteOIDC, bool) {
	if state == "" || len(state) > 200 {
		return pendienteOIDC{}, false
	}
	s.oidcMu.Lock()
	defer s.oidcMu.Unlock()
	k := auth.HashToken(state)
	p, ok := s.oidcPend[k]
	delete(s.oidcPend, k)
	return p, ok && time.Now().Before(p.vence)
}

func (s *Server) cookieOIDC(w http.ResponseWriter, valor string, vida int) {
	http.SetCookie(w, &http.Cookie{Name: cookieOIDC, Value: valor, Path: "/api/auth/oidc/", MaxAge: vida, HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: s.App.Config().EsHTTPS()})
}

// volverAlLogin lleva a la portada con un código que la pantalla traduce (nunca un texto ni una URL
// que venga de afuera).
func volverAlLogin(w http.ResponseWriter, r *http.Request, codigo string) {
	http.Redirect(w, r, "/?lockatus="+codigo, http.StatusSeeOther)
}

// GET /api/auth/oidc/login: arma el pedido a Lockatus y manda al navegador.
func (s *Server) loginOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cfg := s.App.Config()
	if !cfg.OIDCListo() {
		volverAlLogin(w, r, "no_config")
		return
	}
	if !s.cupoOIDC.Tomar("ip:" + ipDe(r)) {
		volverAlLogin(w, r, "demasiados")
		return
	}
	prov, err := s.proveedor(r.Context(), cfg)
	if err != nil {
		s.Log.Warn("no se pudo hablar con Lockatus para iniciar sesión", "error", err)
		volverAlLogin(w, r, "caido")
		return
	}
	state, nonce, ver := auth.NuevoToken(), auth.NuevoToken(), oidc.NuevoVerificador()
	if !s.guardarPendiente(state, pendienteOIDC{verificador: ver, nonce: nonce, config: claveOIDC(cfg), vence: time.Now().Add(vidaOIDC)}) {
		volverAlLogin(w, r, "demasiados")
		return
	}
	s.cookieOIDC(w, state, int(vidaOIDC.Seconds()))
	http.Redirect(w, r, prov.URLAutorizacion(state, nonce, ver), http.StatusFound)
}

// GET /api/auth/oidc/callback: Lockatus vuelve con el código. Se verifica todo, se abre la sesión
// y se va siempre a la portada.
func (s *Server) callbackOIDC(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	s.cookieOIDC(w, "", -1)
	ip := ipDe(r)
	lim := "oidc:" + ip
	if d := s.limite.Espera(lim); d > 0 {
		volverAlLogin(w, r, "demasiados")
		return
	}
	rechazar := func(motivo, codigo, email string) {
		s.limite.Fallo(lim)
		s.auditarComo(r, &auth.Identidad{Tipo: OrigenLockatus, Email: email}, "sesion.rechazo_lockatus", email, map[string]any{"motivo": motivo})
		s.Log.Warn("entrada con Lockatus rechazada", "motivo", motivo)
		volverAlLogin(w, r, codigo)
	}
	cfg := s.App.Config()
	if !cfg.OIDCListo() {
		volverAlLogin(w, r, "no_config")
		return
	}
	q := r.URL.Query()
	state := q.Get("state")
	pend, ok := s.tomarPendiente(state)
	c, errC := r.Cookie(cookieOIDC)
	if !ok || errC != nil || !auth.Iguales(c.Value, state) {
		rechazar("state inválido, vencido, ya usado o de otro navegador", "estado", "")
		return
	}
	if e := q.Get("error"); e != "" {
		rechazar("Lockatus devolvió "+recortarTexto(e, 60), "cancelado", "")
		return
	}
	codigo := q.Get("code")
	if codigo == "" || len(codigo) > 4096 {
		rechazar("sin código", "estado", "")
		return
	}
	if pend.config != claveOIDC(cfg) {
		rechazar("la configuración de Lockatus cambió durante el inicio de sesión", "estado", "")
		return
	}
	prov, err := s.proveedor(r.Context(), cfg)
	if err != nil {
		s.Log.Warn("no se pudo hablar con Lockatus para terminar el inicio de sesión", "error", err)
		volverAlLogin(w, r, "caido")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	per, err := prov.Canjear(ctx, codigo, pend.verificador, pend.nonce)
	if err != nil {
		rechazar(err.Error(), "token", "") // los errores de oidc no llevan ni el código ni el token
		return
	}
	email := strings.ToLower(per.Email)
	if !per.EmailVerificado {
		rechazar("Lockatus no mandó un email verificado", "email", email)
		return
	}
	if _, err := validarEmail(email); err != nil {
		rechazar("el email que mandó Lockatus no es válido", "email", "")
		return
	}
	u, err := s.App.Store.UsuarioPorEmail(r.Context(), email)
	creado := false
	switch {
	case errors.Is(err, store.ErrNoEncontrado):
		if !cfg.OIDCCrearUsuarios {
			rechazar("no tiene cuenta y la creación automática está apagada", "sin_cuenta", email)
			return
		}
		rol, deClaim := rolNuevoLockatus(cfg, per)
		var cuits []string // admin y superadmin: todos los clientes
		if rol < auth.Admin {
			cuits = []string{} // usuario y lectura: ninguno hasta que un admin se los asigne
		}
		u, err = s.App.Store.CrearUsuario(r.Context(), store.UsuarioNuevo{Email: email, Nombre: nombreLimpio(per.Nombre, email), Rol: rol,
			CUITs: cuits, Origen: OrigenLockatus})
		if errors.Is(err, store.ErrEmailUsado) { // entró dos veces a la vez
			u, err = s.App.Store.UsuarioPorEmail(r.Context(), email)
		} else if err == nil {
			creado = true
			s.auditarComo(r, &auth.Identidad{Tipo: OrigenLockatus, Email: email, UsuarioID: u.ID}, "usuario.crear", u.Email,
				map[string]any{"rol": u.Rol.String(), "cuits": u.CUITs, "origen": OrigenLockatus, "rol_del_claim": deClaim})
		}
		if err != nil {
			s.Log.Error("no se pudo crear la cuenta de quien entró con Lockatus", "error", err)
			volverAlLogin(w, r, "error")
			return
		}
	case err != nil:
		s.Log.Error("no se pudo buscar a quien entró con Lockatus", "error", err)
		volverAlLogin(w, r, "error")
		return
	}
	if !u.Activo {
		rechazar("la cuenta está desactivada", "desactivado", u.Email)
		return
	}
	if err := s.abrirSesionCon(w, r, u, OrigenLockatus); err != nil {
		s.Log.Error("no se pudo abrir la sesión de Lockatus", "error", err)
		volverAlLogin(w, r, "error")
		return
	}
	s.limite.Exito(lim)
	s.auditarComo(r, identidadDeUsuario(u), "sesion.entrar_lockatus", u.Email, map[string]any{"cuenta_creada": creado, "rol": u.Rol.String()})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// rolNuevoLockatus: el rol con el que se crea a quien entra por primera vez. El del claim si es uno
// conocido; si no, el rol por defecto (lectura si no se eligió otro).
func rolNuevoLockatus(cfg *config.Config, per *oidc.Persona) (auth.Rol, bool) {
	if r := auth.RolDeLockatus(oidc.Claim(per.Claims, cfg.OIDCClaim())); r != auth.Ninguno {
		return r, true
	}
	if r, ok := auth.ParsearRol(cfg.OIDCRolPorDefecto); ok && r != auth.Ninguno {
		return r, false
	}
	return auth.Lectura, false
}

// probarLockatus lee el discovery y el JWKS del emisor con lo que está en la pantalla.
func (s *Server) probarLockatus(ctx context.Context, c *config.Config) (string, error) {
	if c.OIDCEmisor == "" {
		return "", errors.New("falta el emisor")
	}
	d, err := oidc.Descubrir(ctx, s.cliente(), c.OIDCEmisor)
	if err != nil {
		return "", errors.New(strings.TrimPrefix(err.Error(), oidc.ErrDiscovery.Error()+": "))
	}
	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
		} `json:"keys"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, d.JWKSURI, nil, nil, &jwks)
	if err != nil {
		return "", fmt.Errorf("el discovery anda, pero las claves (jwks_uri) %v", err)
	}
	if st != 200 || len(jwks.Keys) == 0 {
		return "", fmt.Errorf("el discovery anda, pero jwks_uri respondió %d sin claves", st)
	}
	msg := fmt.Sprintf("Lockatus responde: %s tiene los endpoints de autorización y de token y %d clave(s) para firmar.", d.Issuer, len(jwks.Keys))
	var avisos []string
	if len(d.PKCE) == 0 {
		avisos = append(avisos, "no dice si acepta PKCE (Factorum lo manda igual, con S256)")
	}
	if !strings.HasPrefix(c.OIDCEmisor, "https://") {
		avisos = append(avisos, "el emisor no es https: en producción tiene que serlo")
	}
	if len(d.Scopes) > 0 && !slices.Contains(d.Scopes, "email") {
		avisos = append(avisos, "no publica el scope email, y sin email verificado no se entra")
	}
	switch {
	case c.OIDCClienteID == "" || c.OIDCClienteSecreto == "":
		avisos = append(avisos, "faltan el client ID y el client secret")
	case c.URLPublica == "":
		avisos = append(avisos, "falta la URL pública (General): Lockatus vuelve a ella")
	}
	if len(avisos) > 0 {
		msg += " Ojo: " + strings.Join(avisos, "; ") + "."
	}
	return msg, nil
}
