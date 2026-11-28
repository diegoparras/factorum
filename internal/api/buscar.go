package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

// GET /api/buscar?q=&limite=&desde=: el buscador de la cabecera (Ctrl+K). Busca a la vez en los
// comprobantes, los proveedores (emisores), los lotes y, para quien puede configurar, los ajustes.
// Respeta el alcance de quien busca (solo sus clientes) y deja afuera lo de la papelera. Las
// acciones (ir a Cargar facturas, al Libro IVA...) las arma la pantalla con sus permisos.
func (s *Server) buscar(w http.ResponseWriter, r *http.Request) {
	id := IdentidadDe(r.Context())
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	lim, _ := strconv.Atoi(r.URL.Query().Get("limite"))
	desde := strings.TrimSpace(r.URL.Query().Get("desde"))
	if desde != "" {
		if _, err := time.Parse("2006-01-02", desde); err != nil {
			jsonErr(w, http.StatusBadRequest, "desde tiene que ser una fecha AAAA-MM-DD")
			return
		}
	}
	res, err := s.App.Store.Buscar(r.Context(), store.Busqueda{Q: q, Desde: desde, Alcance: id.Alcance(), Limite: lim})
	if err != nil {
		s.fallo(w, err)
		return
	}
	// Los ajustes, solo a quien entra a Configuración (con sesión: una clave de API no configura) y
	// solo de las secciones que ve (un admin, Clientes y canales; un superadmin, todas).
	aj := []ajustes.HallazgoAjuste{}
	if id.Tipo == auth.TipoSesion && id.Rol >= auth.Admin && s.Ajustes != nil {
		aj = ajustes.BuscarAjustes(q, seccionesDe(id), max(limiteDe(lim), 8))
	}
	jsonOK(w, http.StatusOK, map[string]any{"q": q, "comprobantes": res.Comprobantes, "total_comprobantes": res.TotalComprobantes,
		"proveedores": res.Proveedores, "total_proveedores": res.TotalProveedores, "lotes": res.Lotes, "total_lotes": res.TotalLotes, "ajustes": aj})
}

func limiteDe(n int) int {
	if n <= 0 {
		return store.LimiteBusqueda
	}
	return min(n, store.LimiteBusquedaMax)
}
