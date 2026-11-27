package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/store"
)

// Cuánto vale cada link.
const (
	VidaRestablecer = 30 * time.Minute
	VidaInvitacion  = 72 * time.Hour
)

// RespuestaOlvide es siempre la misma, exista o no la cuenta.
const RespuestaOlvide = "Si ese email es de una cuenta de Factorum, te mandamos un link para elegir una contraseña nueva. Vence en 30 minutos."

// cuentasPorMail dice si se pueden mandar links por mail (hace falta el correo saliente y la URL
// pública, para armar el link).
func (s *Server) cuentasPorMail() bool {
	c := s.App.Config()
	return c.SMTPConfigurado() && c.URLPublica != ""
}

func (s *Server) faltaParaMail() string {
	c := s.App.Config()
	switch {
	case !c.SMTPConfigurado():
		return "Falta configurar el correo saliente (Configuración, Correo saliente)"
	case c.URLPublica == "":
		return "Falta la URL pública (Configuración, General): el link tiene que apuntar a ella"
	}
	return ""
}

func (s *Server) linkCuenta(token string) string {
	return strings.TrimRight(s.App.Config().URLPublica, "/") + "/#restablecer?t=" + token
}

// mandarLink crea el link de un solo uso y lo manda por mail.
func (s *Server) mandarLink(ctx context.Context, u *store.Usuario, tipo, quien, ip string) error {
	tok := auth.NuevoToken()
	vida := VidaRestablecer
	if tipo == store.TokenInvitacion {
		vida = VidaInvitacion
	}
	if err := s.App.Store.CrearTokenCuenta(ctx, auth.HashToken(tok), u.ID, tipo, s.ahora().Add(vida), ip); err != nil {
		return err
	}
	var m correo.Mail
	if tipo == store.TokenInvitacion {
		m = correo.Invitacion(s.App.Marca(), u.Nombre, quien, s.linkCuenta(tok), int(vida.Hours()))
	} else {
		m = correo.Restablecer(s.App.Marca(), u.Nombre, s.linkCuenta(tok), int(vida.Minutes()))
	}
	m.Para = []string{u.Email}
	_, err := s.App.EnviarCorreo(ctx, m)
	return err
}

// POST /api/sesion/olvide {email}: manda el link para restablecer. Contesta siempre lo mismo y
// tarda lo mismo, exista o no la cuenta (el mail sale aparte).
func (s *Server) olvideClave(w http.ResponseWriter, r *http.Request) {
	var b struct{ Email string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	if falta := s.faltaParaMail(); falta != "" {
		jsonErr(w, http.StatusConflict, falta)
		return
	}
	email := strings.ToLower(strings.TrimSpace(b.Email))
	if _, err := validarEmail(email); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.cupoIP.Tomar("ip:" + ipDe(r)) {
		jsonErr(w, http.StatusTooManyRequests, "Demasiados pedidos. Probá de nuevo en una hora.")
		return
	}
	ip := ipDe(r)
	// El cupo por email no se dice: contestar distinto revelaría que la cuenta existe.
	if s.cupoEmail.Tomar("email:" + email) {
		go func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
			defer cancel()
			u, err := s.App.Store.UsuarioPorEmail(ctx, email)
			if err != nil || !u.Activo {
				return
			}
			if err := s.mandarLink(ctx, u, store.TokenRestablecer, "", ip); err != nil {
				s.Log.Warn("no se pudo mandar el link para restablecer la contraseña", "error", err)
				return
			}
			uid := u.ID
			if err := s.App.Store.Auditar(ctx, store.Evento{Actor: email, UsuarioID: &uid, Accion: "usuario.pedir_restablecer", Objeto: u.Email, IP: ip}); err != nil {
				s.Log.Error("no se pudo anotar en la auditoría", "accion", "usuario.pedir_restablecer", "error", err)
			}
		}()
	}
	jsonOK(w, http.StatusOK, map[string]any{"mensaje": RespuestaOlvide})
}

func (s *Server) leerToken(w http.ResponseWriter, r *http.Request, dst any) (string, bool) {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(dst); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return "", false
	}
	if d := s.limite.Espera("link:" + ipDe(r)); d > 0 {
		jsonErr(w, http.StatusTooManyRequests, esperaTexto(d))
		return "", false
	}
	return "link:" + ipDe(r), true
}

const linkVencido = "El link venció o ya se usó. Pedí uno nuevo con «Olvidé mi contraseña» o pedile a un admin que te lo reenvíe."

// POST /api/sesion/link {token}: para qué es el link y de quién (sin usarlo).
func (s *Server) verLinkCuenta(w http.ResponseWriter, r *http.Request) {
	var b struct{ Token string }
	clave, ok := s.leerToken(w, r, &b)
	if !ok {
		return
	}
	u, tipo, err := s.App.Store.TokenCuenta(r.Context(), auth.HashToken(strings.TrimSpace(b.Token)), s.ahora())
	if errors.Is(err, store.ErrPaseInvalido) {
		s.limite.Fallo(clave)
		jsonErr(w, http.StatusGone, linkVencido)
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"tipo": tipo, "email": u.Email, "nombre": u.Nombre})
}

// POST /api/sesion/restablecer {token, clave}: pone la contraseña nueva, cierra todas las sesiones
// de esa persona y abre una nueva.
func (s *Server) usarLinkCuenta(w http.ResponseWriter, r *http.Request) {
	var b struct{ Token, Clave string }
	clave, ok := s.leerToken(w, r, &b)
	if !ok {
		return
	}
	if err := auth.ValidarClave(b.Clave); err != nil {
		jsonErr(w, http.StatusBadRequest, capital(err.Error()))
		return
	}
	hash, err := auth.HashClave(b.Clave)
	if err != nil {
		s.fallo(w, err)
		return
	}
	u, tipo, err := s.App.Store.CanjearTokenCuenta(r.Context(), auth.HashToken(strings.TrimSpace(b.Token)), hash, s.ahora())
	if errors.Is(err, store.ErrPaseInvalido) {
		s.limite.Fallo(clave)
		jsonErr(w, http.StatusGone, linkVencido)
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.limite.Exito(clave)
	if err := s.abrirSesion(w, r, u); err != nil {
		s.fallo(w, err)
		return
	}
	accion := "usuario.restablecer_por_mail"
	if tipo == store.TokenInvitacion {
		accion = "usuario.aceptar_invitacion"
	}
	id := identidadDeUsuario(u)
	s.auditarComo(r, id, accion, u.Email, nil)
	s.auditarComo(r, id, "sesion.entrar", u.Email, map[string]any{"por": tipo})
	jsonOK(w, http.StatusOK, map[string]any{"usuario": u})
}

// POST /api/usuarios/{id}/link (admin): manda por mail la invitación (si todavía no eligió
// contraseña) o el link para restablecerla. Cierra sus sesiones solo cuando use el link.
func (s *Server) mandarLinkUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	if falta := s.faltaParaMail(); falta != "" {
		jsonErr(w, http.StatusConflict, falta)
		return
	}
	actor := IdentidadDe(r.Context())
	u, err := s.App.Store.Usuario(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if err := puedeTocar(actor, u); err != nil {
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if !u.Activo {
		jsonErr(w, http.StatusConflict, "Esa persona está desactivada: reactivala primero")
		return
	}
	tipo := store.TokenRestablecer
	if !u.TieneClave {
		tipo = store.TokenInvitacion
	}
	quien := actor.Nombre
	if quien == "" {
		quien = actor.Email
	}
	if err := s.mandarLink(r.Context(), u, tipo, quien, ipDe(r)); err != nil {
		jsonErr(w, http.StatusBadGateway, "No se pudo mandar el mail: "+err.Error())
		return
	}
	s.auditar(r, "usuario.link_por_mail", u.Email, map[string]any{"tipo": tipo})
	jsonOK(w, http.StatusOK, map[string]any{"tipo": tipo})
}
