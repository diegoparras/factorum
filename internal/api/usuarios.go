package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

// GET /api/usuarios
func (s *Server) usuarios(w http.ResponseWriter, r *http.Request) {
	us, err := s.App.Store.Usuarios(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"usuarios": us})
}

// leerCUITs lee "cuits" de un pedido: null, [] o ausente = todos; una lista; o un texto separado por
// comas o renglones.
func leerCUITs(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var l []string
	if err := json.Unmarshal(raw, &l); err != nil {
		var t string
		if err := json.Unmarshal(raw, &t); err != nil {
			return nil, errors.New("cuits tiene que ser una lista de CUITs")
		}
		l = strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == '\n' || r == ';' || r == ' ' })
	}
	return auth.NormalizarCUITs(l)
}

// controlarAlcance: lo que se le da a otro (persona o clave) no puede ser más que lo propio.
func controlarAlcance(actor *auth.Identidad, rol auth.Rol, cuits []string) error {
	if rol > actor.Rol {
		return fmt.Errorf("No podés dar un rol mayor al tuyo (%s)", actor.Rol)
	}
	if !auth.Contenido(cuits, actor.CUITs) {
		if cuits == nil {
			return errors.New("Tenés clientes asignados: elegí CUITs de entre los tuyos (no podés dar acceso a todos)")
		}
		return errors.New("Hay CUITs que no están entre tus clientes")
	}
	return nil
}

// POST /api/usuarios: alta con contraseña temporal (se muestra una vez).
func (s *Server) crearUsuario(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Email   string          `json:"email"`
		Nombre  string          `json:"nombre"`
		Rol     string          `json:"rol"`
		CUITs   json.RawMessage `json:"cuits"`
		Invitar bool            `json:"invitar"` // mandarle un link por mail en lugar de la contraseña temporal
		// Lockatus: entra con Lockatus y no tiene contraseña acá (con «Solo se entra con Lockatus», siempre).
		Lockatus    bool `json:"lockatus"`
		SinClientes bool `json:"sin_clientes"` // ningún cliente todavía
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	actor := IdentidadDe(r.Context())
	cfg := s.App.Config()
	if cfg.SoloLockatus() {
		b.Lockatus = true
	}
	if b.Lockatus {
		if !cfg.OIDCListo() {
			jsonErr(w, http.StatusConflict, "Lockatus no está configurado (Configuración, Lockatus)")
			return
		}
		b.Invitar = false
	}
	if b.Invitar {
		if falta := s.faltaParaMail(); falta != "" {
			jsonErr(w, http.StatusConflict, "No se puede invitar por mail: "+strings.ToLower(falta[:1])+falta[1:])
			return
		}
	}
	email, err := validarEmail(b.Email)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rol, ok := auth.ParsearRol(b.Rol)
	if !ok || rol == auth.Ninguno {
		jsonErr(w, http.StatusBadRequest, "Rol desconocido (puede ser lectura, usuario, admin o superadmin)")
		return
	}
	cuits, err := leerCUITs(b.CUITs)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, capital(err.Error()))
		return
	}
	if b.SinClientes {
		cuits = []string{}
	}
	if err := controlarAlcance(actor, rol, cuits); err != nil {
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	if b.Lockatus {
		u, err := s.App.Store.CrearUsuario(r.Context(), store.UsuarioNuevo{Email: email, Nombre: nombreLimpio(b.Nombre, email), Rol: rol, CUITs: cuits,
			Origen: OrigenLockatus})
		if errors.Is(err, store.ErrEmailUsado) {
			jsonErr(w, http.StatusConflict, "Ya hay una persona con ese email")
			return
		}
		if err != nil {
			s.fallo(w, err)
			return
		}
		s.auditar(r, "usuario.crear", u.Email, map[string]any{"rol": u.Rol.String(), "cuits": u.CUITs, "origen": OrigenLockatus})
		jsonOK(w, http.StatusCreated, map[string]any{"usuario": u, "lockatus": true})
		return
	}
	temp, hash := "", ""
	if !b.Invitar {
		temp = auth.ClaveTemporal()
		if hash, err = auth.HashClave(temp); err != nil {
			s.fallo(w, err)
			return
		}
	}
	u, err := s.App.Store.CrearUsuario(r.Context(), store.UsuarioNuevo{Email: email, Nombre: nombreLimpio(b.Nombre, email), Rol: rol, CUITs: cuits,
		ClaveHash: hash, DebeCambiar: !b.Invitar})
	if errors.Is(err, store.ErrEmailUsado) {
		jsonErr(w, http.StatusConflict, "Ya hay una persona con ese email")
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "usuario.crear", u.Email, map[string]any{"rol": u.Rol.String(), "cuits": u.CUITs, "invitado": b.Invitar})
	if b.Invitar {
		quien := actor.Nombre
		if quien == "" {
			quien = actor.Email
		}
		res := map[string]any{"usuario": u, "invitado": true}
		if err := s.mandarLink(r.Context(), u, store.TokenInvitacion, quien, ipDe(r)); err != nil {
			res["invitado"] = false
			res["aviso"] = "La persona quedó creada, pero no se pudo mandar la invitación: " + err.Error() + ". Probá con «Mandar link» cuando el correo ande."
		} else {
			s.auditar(r, "usuario.link_por_mail", u.Email, map[string]any{"tipo": store.TokenInvitacion})
		}
		jsonOK(w, http.StatusCreated, res)
		return
	}
	jsonOK(w, http.StatusCreated, map[string]any{"usuario": u, "clave_temporal": temp})
}

// sinContrasena: a quien entra con Lockatus (o a todos, con «Solo se entra con Lockatus») no se le
// da contraseña ni invitación. Devuelve el motivo, o "" si se puede.
func sinContrasena(s *Server, u *store.Usuario) string {
	switch {
	case u.Origen == OrigenLockatus:
		return "Esa persona entra con Lockatus: no tiene contraseña en Factorum"
	case s.App.Config().SoloLockatus():
		return "Solo se entra con Lockatus: no hace falta una contraseña de Factorum"
	}
	return ""
}

// puedeTocar: un admin no toca superadmins.
func puedeTocar(actor *auth.Identidad, u *store.Usuario) error {
	if u.Rol > actor.Rol {
		return fmt.Errorf("No podés modificar a alguien con rol %s", u.Rol)
	}
	if !auth.Contenido(u.CUITs, actor.CUITs) {
		return errors.New("Esa persona ve clientes que no están entre los tuyos")
	}
	return nil
}

// PATCH /api/usuarios/{id}: nombre, rol, CUITs o activo.
func (s *Server) editarUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	var b map[string]json.RawMessage
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
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
	var c store.CambiosUsuario
	detalle := map[string]any{}
	nuevoRol, nuevoActivo := u.Rol, u.Activo
	if raw, ok := b["nombre"]; ok {
		var n string
		if json.Unmarshal(raw, &n) != nil || strings.TrimSpace(n) == "" {
			jsonErr(w, http.StatusBadRequest, "El nombre no puede quedar vacío")
			return
		}
		n = nombreLimpio(n, u.Email)
		c.Nombre = &n
		detalle["nombre"] = n
	}
	if raw, ok := b["rol"]; ok {
		var t string
		_ = json.Unmarshal(raw, &t)
		rol, ok := auth.ParsearRol(t)
		if !ok || rol == auth.Ninguno {
			jsonErr(w, http.StatusBadRequest, "Rol desconocido (puede ser lectura, usuario, admin o superadmin)")
			return
		}
		if rol > actor.Rol {
			jsonErr(w, http.StatusForbidden, fmt.Sprintf("No podés dar un rol mayor al tuyo (%s)", actor.Rol))
			return
		}
		if rol != u.Rol {
			c.Rol, nuevoRol = &rol, rol
			detalle["rol"] = map[string]string{"antes": u.Rol.String(), "despues": rol.String()}
		}
	}
	_, conCUITs := b["cuits"]
	var sinClientes bool
	if raw, ok := b["sin_clientes"]; ok {
		_ = json.Unmarshal(raw, &sinClientes)
	}
	if conCUITs || sinClientes {
		cuits, err := leerCUITs(b["cuits"])
		if err != nil {
			jsonErr(w, http.StatusBadRequest, capital(err.Error()))
			return
		}
		if sinClientes {
			cuits = []string{}
		}
		if err := controlarAlcance(actor, auth.Ninguno, cuits); err != nil {
			jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
		c.CUITs = &cuits
		detalle["cuits"] = cuits
	}
	if raw, ok := b["activo"]; ok {
		var a bool
		if json.Unmarshal(raw, &a) != nil {
			jsonErr(w, http.StatusBadRequest, "activo tiene que ser true o false")
			return
		}
		if !a && u.ID == actor.UsuarioID {
			jsonErr(w, http.StatusForbidden, "No podés desactivarte a vos mismo")
			return
		}
		if a != u.Activo {
			c.Activo, nuevoActivo = &a, a
			detalle["activo"] = a
		}
	}
	// Siempre tiene que quedar un superadmin activo.
	if u.Rol == auth.Superadmin && u.Activo && (nuevoRol != auth.Superadmin || !nuevoActivo) {
		n, err := s.App.Store.ContarSuperadmins(r.Context())
		if err != nil {
			s.fallo(w, err)
			return
		}
		if n <= 1 {
			jsonErr(w, http.StatusConflict, "Es el último superadmin: antes de cambiarlo, nombrá a otro")
			return
		}
	}
	u, err = s.App.Store.ActualizarUsuario(r.Context(), id, c, "")
	if err != nil {
		s.fallo(w, err)
		return
	}
	accion := "usuario.editar"
	if v, ok := detalle["activo"]; ok && len(detalle) == 1 {
		accion = map[bool]string{true: "usuario.reactivar", false: "usuario.desactivar"}[v.(bool)]
	}
	if len(detalle) > 0 {
		s.auditar(r, accion, u.Email, detalle)
	}
	jsonOK(w, http.StatusOK, map[string]any{"usuario": u})
}

// POST /api/usuarios/{id}/restablecer: contraseña temporal nueva (se muestra una vez). Cierra las
// sesiones de esa persona.
func (s *Server) restablecerUsuario(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
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
	if err := sinContrasena(s, u); err != "" {
		jsonErr(w, http.StatusConflict, err)
		return
	}
	temp := auth.ClaveTemporal()
	hash, err := auth.HashClave(temp)
	if err != nil {
		s.fallo(w, err)
		return
	}
	si := true
	if u, err = s.App.Store.ActualizarUsuario(r.Context(), id, store.CambiosUsuario{ClaveHash: &hash, DebeCambia: &si}, ""); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "usuario.restablecer", u.Email, nil)
	jsonOK(w, http.StatusOK, map[string]any{"usuario": u, "clave_temporal": temp})
}
