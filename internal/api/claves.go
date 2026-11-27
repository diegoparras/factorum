package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

var zonaAR = func() *time.Location {
	l, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		return time.FixedZone("ART", -3*3600)
	}
	return l
}()

// GET /api/claves
func (s *Server) claves(w http.ResponseWriter, r *http.Request) {
	ks, err := s.App.Store.Claves(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"claves": ks})
}

// POST /api/claves: la clave completa se devuelve una sola vez.
func (s *Server) crearClave(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Nombre    string          `json:"nombre"`
		Rol       string          `json:"rol"`
		CUITs     json.RawMessage `json:"cuits"`
		Vence     string          `json:"vence"` // AAAA-MM-DD (vence al terminar ese día) o vacío
		Suite     bool            `json:"suite"`
		SuiteTope string          `json:"suite_tope"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	actor := IdentidadDe(r.Context())
	nombre := strings.TrimSpace(b.Nombre)
	if nombre == "" || len(nombre) > 120 {
		jsonErr(w, http.StatusBadRequest, "Poné un nombre que diga para qué es la clave (por ejemplo «Claude Desktop de Diego»)")
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
	if err := controlarAlcance(actor, rol, cuits); err != nil {
		jsonErr(w, http.StatusForbidden, err.Error())
		return
	}
	tope := auth.Usuario
	if b.Suite && strings.TrimSpace(b.SuiteTope) != "" {
		if tope, ok = auth.ParsearRol(b.SuiteTope); !ok || tope == auth.Ninguno {
			jsonErr(w, http.StatusBadRequest, "Rol tope de la suite desconocido")
			return
		}
	}
	if b.Suite {
		if tope >= auth.Admin && actor.Rol < auth.Superadmin {
			jsonErr(w, http.StatusForbidden, "Solo un superadmin puede habilitar la suite con tope admin o superadmin")
			return
		}
		if tope > actor.Rol {
			jsonErr(w, http.StatusForbidden, fmt.Sprintf("El tope de la suite no puede ser mayor a tu rol (%s)", actor.Rol))
			return
		}
	}
	var vence *time.Time
	if v := strings.TrimSpace(b.Vence); v != "" {
		d, err := time.ParseInLocation("2006-01-02", v, zonaAR)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "El vencimiento tiene que ser una fecha (AAAA-MM-DD)")
			return
		}
		fin := d.AddDate(0, 0, 1)
		if !fin.After(s.ahora()) {
			jsonErr(w, http.StatusBadRequest, "El vencimiento ya pasó")
			return
		}
		vence = &fin
	}
	clave, prefijo := auth.NuevaClaveAPI()
	k, err := s.App.Store.CrearClave(r.Context(), store.ClaveNueva{Nombre: nombre, Prefijo: prefijo, Hash: auth.HashToken(clave), Rol: rol, CUITs: cuits,
		UsuarioID: actor.UsuarioID, Vence: vence, Suite: b.Suite, SuiteTope: tope})
	if err != nil {
		s.fallo(w, err)
		return
	}
	det := map[string]any{"nombre": k.Nombre, "rol": k.Rol.String(), "cuits": k.CUITs, "suite": k.Suite}
	if k.Suite {
		det["suite_tope"] = k.SuiteTope.String()
	}
	s.auditar(r, "clave.crear", k.Prefijo, det)
	jsonOK(w, http.StatusCreated, map[string]any{"clave": clave, "datos": k})
}

// DELETE /api/claves/{id}: revocar.
func (s *Server) revocarClave(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	actor := IdentidadDe(r.Context())
	k, err := s.App.Store.Clave(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if k.Rol > actor.Rol || (k.Suite && k.SuiteTope > actor.Rol) {
		jsonErr(w, http.StatusForbidden, fmt.Sprintf("No podés revocar una clave de rol %s", max(k.Rol, k.SuiteTope)))
		return
	}
	if err := s.App.Store.RevocarClave(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNoEncontrado) {
			jsonErr(w, http.StatusConflict, "La clave ya estaba revocada")
			return
		}
		s.fallo(w, err)
		return
	}
	s.auditar(r, "clave.revocar", k.Prefijo, map[string]any{"nombre": k.Nombre})
	jsonOK(w, http.StatusOK, map[string]any{})
}
