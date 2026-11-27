package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/diegoparras/factorum/internal/store"
)

// GET /api/auditoria?desde&hasta&persona&accion&limite&offset
func (s *Server) verAuditoria(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	fecha := func(k string) string {
		v := q.Get(k)
		if _, err := time.Parse("2006-01-02", v); err != nil {
			return ""
		}
		return v
	}
	lim, _ := strconv.Atoi(q.Get("limite"))
	off, _ := strconv.Atoi(q.Get("offset"))
	f := store.FiltroAuditoria{Desde: fecha("desde"), Hasta: fecha("hasta"), Persona: q.Get("persona"), Accion: q.Get("accion"), Limite: lim, Offset: off}
	ev, total, err := s.App.Store.Auditoria(r.Context(), f)
	if err != nil {
		s.fallo(w, err)
		return
	}
	acciones, err := s.App.Store.AccionesAuditadas(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"eventos": ev, "total": total, "acciones": acciones})
}
