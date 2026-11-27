package api

import (
	"net/http"
	"time"

	factorum "github.com/diegoparras/factorum"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/copias"
)

func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	p, err := s.App.Store.Panel(r.Context(), time.Now(), IdentidadDe(r.Context()).Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	res := map[string]any{"panel": p}
	// Para un superadmin: si la última copia de la base falló, Inicio lo avisa.
	if id := IdentidadDe(r.Context()); id.Rol >= auth.Superadmin && s.Copias != nil {
		if u, err := s.Copias.Ultima(r.Context()); err == nil && u != nil && u.Estado == copias.Error {
			res["copia_fallida"] = map[string]any{"cuando": u.Creado, "error": u.Error}
		}
	}
	jsonOK(w, http.StatusOK, res)
}

// novedades es pública (como /healthz): el CHANGELOG no tiene datos de nadie.
func (s *Server) novedades(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	jsonOK(w, http.StatusOK, map[string]any{"novedades": factorum.Novedades()})
}
