package api

import (
	"net/http"
	"time"

	factorum "github.com/diegoparras/factorum"
)

func (s *Server) panel(w http.ResponseWriter, r *http.Request) {
	p, err := s.App.Store.Panel(r.Context(), time.Now(), IdentidadDe(r.Context()).Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"panel": p})
}

// novedades es pública (como /healthz): el CHANGELOG no tiene datos de nadie.
func (s *Server) novedades(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "public, max-age=300")
	jsonOK(w, http.StatusOK, map[string]any{"novedades": factorum.Novedades()})
}
