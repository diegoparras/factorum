package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/store"
)

// ConfirmacionBorrar es lo que hay que escribir para vaciar la papelera o borrar algo ya.
const ConfirmacionBorrar = "borrar"

// Topes de lo que se manda de una vez.
const (
	maxBorrar    = 5000
	maxRestaurar = 1000
)

// sinPrefijo saca el «conflicto: » o el «no encontrado: » del mensaje de un error del store.
func sinPrefijo(err error) string {
	m := err.Error()
	for _, p := range []error{store.ErrConflicto, store.ErrNoEncontrado, store.ErrFiltro} {
		m = strings.TrimPrefix(m, p.Error()+": ")
	}
	if m != "" {
		m = strings.ToUpper(m[:1]) + m[1:]
	}
	return m
}

func leerJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return false
	}
	return true
}

// DELETE /api/lotes/{id}: el lote entero a la papelera (admin o más). Si todavía se está
// procesando, se cancela lo que le falta en la cola.
func (s *Server) borrarLote(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	idn := IdentidadDe(r.Context())
	res, err := s.App.BorrarLote(r.Context(), id, idn.Quien(), idn.Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	l := res.Lote
	s.auditar(r, "lote.borrar", fmt.Sprintf("lote %d", id), map[string]any{"nombre": l.Nombre, "cliente_cuit": l.ClienteCUIT,
		"documentos": res.Documentos, "comprobantes": res.Comprobantes, "en_proceso": l.Terminado == nil, "trabajos_cancelados": res.Cancelados})
	jsonOK(w, http.StatusOK, map[string]any{"borrado": res, "dias": s.App.Config().DiasDePapelera()})
}

// PATCH /api/lotes/{id}: cambiar el cliente (cliente_cuit). Quien pide tiene que poder ver el
// cliente de antes y el nuevo.
func (s *Server) editarLote(w http.ResponseWriter, r *http.Request) {
	id, err := idDe(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "id inválido")
		return
	}
	var b struct {
		ClienteCUIT *string `json:"cliente_cuit"`
	}
	if !leerJSON(w, r, &b) {
		return
	}
	if b.ClienteCUIT == nil {
		jsonErr(w, http.StatusBadRequest, "Mandá el cliente_cuit nuevo")
		return
	}
	nuevo := arca.SoloDigitos(*b.ClienteCUIT)
	if !arca.CUITValido(nuevo) {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("El CUIT %s no es válido: revisá el dígito verificador", strings.TrimSpace(*b.ClienteCUIT)))
		return
	}
	idn := IdentidadDe(r.Context())
	if !idn.Permite(nuevo) {
		jsonErr(w, http.StatusForbidden, fmt.Sprintf("El CUIT %s no está entre tus clientes", arca.FormatearCUIT(nuevo)))
		return
	}
	res, err := s.App.CambiarCliente(r.Context(), id, nuevo, idn.Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if res.Antes != res.Despues {
		s.auditar(r, "lote.cambiar_cliente", fmt.Sprintf("lote %d", id), map[string]any{
			"cliente_cuit": map[string]string{"antes": res.Antes, "despues": res.Despues},
			"comprobantes": res.Comprobantes, "reconstatar": res.Reconstatar})
	}
	l, err := s.App.Store.Lote(r.Context(), id)
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"lote": l, "cambio": res})
}

// POST /api/comprobantes/borrar: comprobantes sueltos a la papelera (usuario o más, en su alcance).
func (s *Server) borrarComprobantes(w http.ResponseWriter, r *http.Request) {
	var b struct {
		IDs []int64 `json:"ids"`
	}
	if !leerJSON(w, r, &b) {
		return
	}
	if len(b.IDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "Mandá los ids de los comprobantes a borrar")
		return
	}
	if len(b.IDs) > maxBorrar {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("Se pueden borrar hasta %d comprobantes de una vez", maxBorrar))
		return
	}
	idn := IdentidadDe(r.Context())
	res, err := s.App.BorrarComprobantes(r.Context(), b.IDs, idn.Quien(), idn.Alcance())
	if errors.Is(err, store.ErrNoEncontrado) {
		jsonErr(w, http.StatusNotFound, "No se encontraron (o ya están en la papelera): "+strings.TrimPrefix(err.Error(), store.ErrNoEncontrado.Error()+": ")+". No se borró ninguno")
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	ids := b.IDs
	if len(ids) > 200 {
		ids = ids[:200]
	}
	objeto := "1 comprobante"
	if res.Comprobantes != 1 {
		objeto = fmt.Sprintf("%d comprobantes", res.Comprobantes)
	}
	s.auditar(r, "comprobante.borrar", objeto, map[string]any{"ids": ids, "documentos": res.Documentos})
	jsonOK(w, http.StatusOK, map[string]any{"borrado": res, "dias": s.App.Config().DiasDePapelera()})
}

// GET /api/papelera: lo borrado que puede ver quien pide.
func (s *Server) papelera(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lim, _ := strconv.Atoi(q.Get("limite"))
	off, _ := strconv.Atoi(q.Get("offset"))
	idn := IdentidadDe(r.Context())
	items, total, dias, err := s.App.Papelera(r.Context(), idn.Alcance(), lim, off)
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"items": items, "total": total, "dias": dias})
}

type pedidoPapelera struct {
	Items        []store.RefPapelera `json:"items"`
	Confirmacion string              `json:"confirmacion"`
}

func objetoPapelera(it store.RefPapelera) string { return fmt.Sprintf("%s %d", it.Tipo, it.ID) }

// POST /api/papelera/restaurar: vuelve como estaba. Cada cosa por separado: lo que no se puede
// (un archivo igual cargado después, su lote en la papelera) vuelve en rechazados con el motivo.
func (s *Server) restaurarPapelera(w http.ResponseWriter, r *http.Request) {
	var b pedidoPapelera
	if !leerJSON(w, r, &b) {
		return
	}
	if len(b.Items) == 0 || len(b.Items) > maxRestaurar {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("Mandá qué restaurar (items, hasta %d)", maxRestaurar))
		return
	}
	idn := IdentidadDe(r.Context())
	type rechazo struct {
		store.RefPapelera
		Error string `json:"error"`
	}
	rechazados := []rechazo{}
	restaurados, conflicto := 0, false
	for _, it := range b.Items {
		res, err := s.App.Restaurar(r.Context(), it, idn.Alcance())
		switch {
		case err == nil:
			restaurados++
			s.auditar(r, "papelera.restaurar", objetoPapelera(it), map[string]any{"documentos": res.Documentos, "comprobantes": res.Comprobantes,
				"reencolados": len(res.Pendientes)})
		case errors.Is(err, store.ErrConflicto), errors.Is(err, store.ErrFiltro):
			conflicto = conflicto || errors.Is(err, store.ErrConflicto)
			rechazados = append(rechazados, rechazo{it, sinPrefijo(err)})
		case errors.Is(err, store.ErrNoEncontrado):
			rechazados = append(rechazados, rechazo{it, "No está en la papelera (o no es de tus clientes)"})
		default:
			s.fallo(w, err)
			return
		}
	}
	if restaurados == 0 {
		st := http.StatusNotFound
		if conflicto {
			st = http.StatusConflict
		}
		jsonCrudo(w, st, map[string]any{"ok": false, "error": rechazados[0].Error, "rechazados": rechazados})
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"restaurados": restaurados, "rechazados": rechazados})
}

// DELETE /api/papelera: vaciar (solo superadmin, con sesión, escribiendo «borrar»).
func (s *Server) vaciarPapelera(w http.ResponseWriter, r *http.Request) {
	var b pedidoPapelera
	if !leerJSON(w, r, &b) {
		return
	}
	if strings.ToLower(strings.TrimSpace(b.Confirmacion)) != ConfirmacionBorrar {
		jsonErr(w, http.StatusBadRequest, "Para vaciar la papelera hay que escribir «"+ConfirmacionBorrar+"»")
		return
	}
	p, err := s.App.BorrarDeLaPapelera(r.Context(), store.FiltroPurga{Todo: true, Alcance: IdentidadDe(r.Context()).Alcance()})
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "papelera.vaciar", "papelera", map[string]any{"lotes": p.Lotes, "documentos": p.Documentos, "comprobantes": p.Comprobantes, "archivos": p.Archivos})
	jsonOK(w, http.StatusOK, map[string]any{"borrado": p})
}

// POST /api/papelera/borrar: Borrar ya, de a uno o varios (solo superadmin, con sesión, escribiendo
// «borrar»).
func (s *Server) borrarYa(w http.ResponseWriter, r *http.Request) {
	var b pedidoPapelera
	if !leerJSON(w, r, &b) {
		return
	}
	if strings.ToLower(strings.TrimSpace(b.Confirmacion)) != ConfirmacionBorrar {
		jsonErr(w, http.StatusBadRequest, "Para borrar para siempre hay que escribir «"+ConfirmacionBorrar+"»")
		return
	}
	if len(b.Items) == 0 || len(b.Items) > maxRestaurar {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("Mandá qué borrar (items, hasta %d)", maxRestaurar))
		return
	}
	p, err := s.App.BorrarDeLaPapelera(r.Context(), store.FiltroPurga{Items: b.Items, Alcance: IdentidadDe(r.Context()).Alcance()})
	if err != nil {
		s.fallo(w, err)
		return
	}
	if p.Lotes+p.Comprobantes == 0 {
		jsonErr(w, http.StatusNotFound, "No está en la papelera (o no es de tus clientes)")
		return
	}
	objs := make([]string, 0, len(b.Items))
	for _, it := range b.Items {
		objs = append(objs, objetoPapelera(it))
	}
	s.auditar(r, "papelera.borrar_ya", strings.Join(objs, ", "), map[string]any{"lotes": p.Lotes, "documentos": p.Documentos, "comprobantes": p.Comprobantes, "archivos": p.Archivos})
	jsonOK(w, http.StatusOK, map[string]any{"borrado": p})
}
