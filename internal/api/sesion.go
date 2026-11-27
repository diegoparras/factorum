package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

// ArchivoArranque guarda el código de arranque en el directorio de datos, para `factorum salud`.
const ArchivoArranque = "codigo-arranque.txt"

// PrepararArranque: si todavía no hay nadie, genera el código de un solo uso para crear el primer
// superadmin, lo escribe en el log y en el directorio de datos.
func (s *Server) PrepararArranque(ctx context.Context) error {
	n, err := s.App.Store.ContarUsuarios(ctx)
	if err != nil {
		return err
	}
	s.arranqueMu.Lock()
	defer s.arranqueMu.Unlock()
	if n > 0 {
		s.codigoArranque = ""
		s.borrarArchivoArranque()
		return nil
	}
	s.codigoArranque = auth.CodigoArranque()
	s.Log.Warn("Todavía no hay usuarios: entrá a Factorum y creá el primer superadmin con este código de un solo uso",
		"codigo_arranque", s.codigoArranque)
	if dir := s.App.Config().DirDatos; dir != "" {
		ruta := filepath.Join(dir, ArchivoArranque)
		if err := os.MkdirAll(dir, 0o700); err == nil {
			if err := os.WriteFile(ruta, []byte(s.codigoArranque+"\n"), 0o600); err != nil {
				s.Log.Warn("no se pudo escribir el código de arranque en el directorio de datos", "error", err)
			}
		}
	}
	return nil
}

func (s *Server) borrarArchivoArranque() {
	if dir := s.App.Config().DirDatos; dir != "" {
		_ = os.Remove(filepath.Join(dir, ArchivoArranque))
	}
}

func (s *Server) arranquePendiente() bool {
	s.arranqueMu.Lock()
	defer s.arranqueMu.Unlock()
	return s.codigoArranque != ""
}

// GET /api/arranque: si hay que crear el primer superadmin.
func (s *Server) verArranque(w http.ResponseWriter, r *http.Request) {
	cfg := s.App.Config()
	jsonOK(w, http.StatusOK, map[string]any{"necesita": s.arranquePendiente(), "nombre": cfg.Nombre, "olvide": s.cuentasPorMail() && !cfg.SoloLockatus(),
		"lockatus": cfg.OIDCListo(), "solo_lockatus": cfg.SoloLockatus()})
}

// POST /api/arranque: crea el primer superadmin con el código del log.
func (s *Server) crearArranque(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Codigo, Email, Nombre, Clave string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	ip := "arranque:" + ipDe(r)
	if d := s.limite.Espera(ip); d > 0 {
		jsonErr(w, http.StatusTooManyRequests, esperaTexto(d))
		return
	}
	s.arranqueMu.Lock()
	codigo := s.codigoArranque
	s.arranqueMu.Unlock()
	if codigo == "" {
		jsonErr(w, http.StatusConflict, "Ya hay personas cargadas: entrá con tu email y contraseña")
		return
	}
	if !auth.Iguales(strings.ToUpper(strings.TrimSpace(b.Codigo)), codigo) {
		s.limite.Fallo(ip)
		jsonErr(w, http.StatusUnauthorized, "El código no es el del log del servidor")
		return
	}
	email, err := validarEmail(b.Email)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
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
	u, err := s.App.Store.CrearPrimerSuperadmin(r.Context(), store.UsuarioNuevo{Email: email, Nombre: nombreLimpio(b.Nombre, email), ClaveHash: hash})
	if err != nil {
		jsonErr(w, http.StatusConflict, err.Error())
		return
	}
	s.limite.Exito(ip)
	s.arranqueMu.Lock()
	s.codigoArranque = ""
	s.arranqueMu.Unlock()
	s.borrarArchivoArranque()
	if err := s.abrirSesion(w, r, u); err != nil {
		s.fallo(w, err)
		return
	}
	id := identidadDeUsuario(u)
	s.auditarComo(r, id, "usuario.crear", u.Email, map[string]any{"rol": u.Rol.String(), "primer_superadmin": true})
	s.auditarComo(r, id, "sesion.entrar", u.Email, nil)
	s.Log.Info("primer superadmin creado")
	jsonOK(w, http.StatusCreated, map[string]any{"usuario": u})
}

func identidadDeUsuario(u *store.Usuario) *auth.Identidad {
	return &auth.Identidad{Tipo: auth.TipoSesion, UsuarioID: u.ID, Email: u.Email, Nombre: u.Nombre, Rol: u.Rol, CUITs: u.CUITs}
}

func validarEmail(e string) (string, error) {
	e = strings.TrimSpace(e)
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || !strings.Contains(e, ".") || len(e) > 200 {
		return "", errors.New("El email no es válido")
	}
	return e, nil
}

func nombreLimpio(n, email string) string {
	n = strings.TrimSpace(n)
	if n == "" {
		n, _, _ = strings.Cut(email, "@")
	}
	if len(n) > 120 {
		n = n[:120]
	}
	return n
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func esperaTexto(d time.Duration) string {
	seg := int(d.Round(time.Second).Seconds())
	if seg < 1 {
		seg = 1
	}
	if seg >= 120 {
		return fmt.Sprintf("Demasiados intentos. Probá de nuevo en %d minutos.", (seg+59)/60)
	}
	return fmt.Sprintf("Demasiados intentos. Probá de nuevo en %d segundos.", seg)
}

// POST /api/sesion: entrar con email y contraseña.
func (s *Server) entrar(w http.ResponseWriter, r *http.Request) {
	var b struct{ Email, Clave string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	email := strings.ToLower(strings.TrimSpace(b.Email))
	if s.App.Config().SoloLockatus() {
		// No alcanza con esconder el formulario: el servidor tampoco deja (salvo FACTORUM_LOGIN_LOCAL=1).
		s.auditarComo(r, &auth.Identidad{Email: email}, "sesion.fallida", email, map[string]any{"motivo": "solo se entra con Lockatus"})
		jsonErr(w, http.StatusForbidden, msgSoloLockatus)
		return
	}
	claves := []string{"ip:" + ipDe(r), "email:" + email}
	if d := s.limite.Espera(claves...); d > 0 {
		jsonErr(w, http.StatusTooManyRequests, esperaTexto(d))
		return
	}
	u, err := s.App.Store.UsuarioPorEmail(r.Context(), email)
	if err != nil && !errors.Is(err, store.ErrNoEncontrado) {
		s.fallo(w, err)
		return
	}
	ok := false
	if u != nil && u.TieneClave {
		ok = auth.VerificarClave(u.ClaveHash, b.Clave)
	} else {
		auth.GastarTiempo(b.Clave)
	}
	if !ok || !u.Activo {
		s.limite.Fallo(claves...)
		s.auditarComo(r, &auth.Identidad{Email: email}, "sesion.fallida", email, map[string]any{"motivo": motivoFallo(u, ok)})
		if ok && !u.Activo {
			jsonErr(w, http.StatusForbidden, "Tu cuenta está desactivada: pedile a un admin que la reactive")
			return
		}
		jsonErr(w, http.StatusUnauthorized, "Email o contraseña incorrectos")
		return
	}
	s.limite.Exito(claves...)
	if err := s.abrirSesion(w, r, u); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditarComo(r, identidadDeUsuario(u), "sesion.entrar", u.Email, nil)
	jsonOK(w, http.StatusOK, map[string]any{"usuario": u})
}

func motivoFallo(u *store.Usuario, ok bool) string {
	switch {
	case u == nil:
		return "email desconocido"
	case !u.TieneClave:
		return "la cuenta no tiene contraseña"
	case !ok:
		return "contraseña incorrecta"
	}
	return "cuenta desactivada"
}

// GET /api/sesion: quién soy y qué puedo hacer.
func (s *Server) yo(w http.ResponseWriter, r *http.Request) {
	id := IdentidadDe(r.Context())
	cfg := s.App.Config()
	res := map[string]any{"identidad": id, "permisos": permisos(id), "instalacion": cfg.Nombre, "correo_saliente": s.cuentasPorMail(),
		"lockatus": cfg.OIDCListo(), "solo_lockatus": cfg.SoloLockatus()}
	if id.Tipo == auth.TipoSesion {
		// De dónde salió la cuenta y si tiene contraseña: quien entra con Lockatus no la cambia acá.
		if u, err := s.App.Store.Usuario(r.Context(), id.UsuarioID); err == nil {
			res["cuenta"] = map[string]any{"origen": u.Origen, "tiene_clave": u.TieneClave}
		}
	}
	jsonOK(w, http.StatusOK, res)
}

func permisos(id *auth.Identidad) map[string]bool {
	return map[string]bool{
		"cargar":        id.Rol >= auth.Usuario,
		"corregir":      id.Rol >= auth.Usuario,
		"usuarios":      id.Rol >= auth.Admin,
		"claves":        id.Rol >= auth.Admin,
		"auditoria":     id.Rol >= auth.Admin,
		"clientes":      id.Rol >= auth.Admin,
		"retenido":      id.Rol >= auth.Admin,
		"configuracion": id.Rol >= auth.Superadmin,
		// La papelera: borrar comprobantes y restaurar, usuario o más; borrar lotes, admin o más;
		// el borrado definitivo a mano, superadmin con sesión.
		"borrar":          id.Rol >= auth.Usuario,
		"borrar_lotes":    id.Rol >= auth.Admin,
		"papelera":        id.Rol >= auth.Usuario,
		"vaciar_papelera": id.Rol >= auth.Superadmin && id.Tipo == auth.TipoSesion,
		"sin_clientes":    id.CUITs != nil && len(id.CUITs) == 0,
	}
}

// DELETE /api/sesion: salir.
func (s *Server) salirSesion(w http.ResponseWriter, r *http.Request) {
	id := IdentidadDe(r.Context())
	if err := s.App.Store.BorrarSesion(r.Context(), id.SesionID); err != nil {
		s.fallo(w, err)
		return
	}
	s.borrarCookie(w)
	s.auditar(r, "sesion.salir", id.Email, nil)
	jsonOK(w, http.StatusOK, map[string]any{})
}

// POST /api/sesion/clave: cambiar mi contraseña. Cierra mis otras sesiones.
func (s *Server) cambiarMiClave(w http.ResponseWriter, r *http.Request) {
	var b struct{ Actual, Nueva string }
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	id := IdentidadDe(r.Context())
	clave := "cambio:" + id.Email
	if d := s.limite.Espera(clave); d > 0 {
		jsonErr(w, http.StatusTooManyRequests, esperaTexto(d))
		return
	}
	u, err := s.App.Store.Usuario(r.Context(), id.UsuarioID)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if u.Origen == OrigenLockatus {
		jsonErr(w, http.StatusConflict, "Tu cuenta entra con Lockatus: la contraseña se cambia en Lockatus")
		return
	}
	if u.TieneClave && !auth.VerificarClave(u.ClaveHash, b.Actual) {
		s.limite.Fallo(clave)
		jsonErr(w, http.StatusUnauthorized, "La contraseña actual no es correcta")
		return
	}
	if err := auth.ValidarClave(b.Nueva); err != nil {
		jsonErr(w, http.StatusBadRequest, capital(err.Error()))
		return
	}
	if b.Nueva == b.Actual {
		jsonErr(w, http.StatusBadRequest, "La contraseña nueva tiene que ser distinta de la actual")
		return
	}
	hash, err := auth.HashClave(b.Nueva)
	if err != nil {
		s.fallo(w, err)
		return
	}
	no := false
	if _, err := s.App.Store.ActualizarUsuario(r.Context(), u.ID, store.CambiosUsuario{ClaveHash: &hash, DebeCambia: &no}, id.SesionID); err != nil {
		s.fallo(w, err)
		return
	}
	s.limite.Exito(clave)
	s.auditar(r, "usuario.cambiar_clave", u.Email, map[string]any{"propia": true})
	jsonOK(w, http.StatusOK, map[string]any{})
}
