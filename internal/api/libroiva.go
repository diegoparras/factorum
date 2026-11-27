package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/libroiva"
)

// libroDe lee cliente, periodo y solo_aprobados del pedido y arma el libro. Escribe el error si lo hay.
func (s *Server) libroDe(w http.ResponseWriter, r *http.Request, conConciliacion bool) (*app.LibroIVA, bool) {
	q := r.URL.Query()
	cliente, err := ClienteConciliacion(IdentidadDe(r.Context()), q.Get("cliente"))
	if err != nil {
		st := http.StatusForbidden
		if errors.Is(err, errFaltaCliente) || strings.HasPrefix(err.Error(), "El CUIT del cliente") {
			st = http.StatusBadRequest
		}
		jsonErr(w, st, err.Error())
		return nil, false
	}
	periodo := strings.TrimSpace(q.Get("periodo"))
	if _, _, err := libroiva.ValidarPeriodo(periodo); err != nil {
		jsonErr(w, http.StatusBadRequest, mayuscula(err.Error()))
		return nil, false
	}
	l, err := s.App.LibroIVA(r.Context(), cliente, periodo, verdadero(q.Get("solo_aprobados")), conConciliacion)
	if err != nil {
		s.fallo(w, err)
		return nil, false
	}
	return l, true
}

// GET /api/libro-iva?cliente=CUIT&periodo=AAAA-MM[&solo_aprobados=1][&conciliar=0][&registros=1]:
// el resumen y los problemas, sin los archivos. Con registros=1 trae también lo que va al archivo.
func (s *Server) libroIVA(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	l, ok := s.libroDe(w, r, q.Get("conciliar") == "" || verdadero(q.Get("conciliar")))
	if !ok {
		return
	}
	var regs []libroiva.Registro
	if verdadero(q.Get("registros")) {
		regs = l.Registros
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		OK     bool   `json:"ok"`
		Nombre string `json:"nombre"`
		*app.LibroIVA
		Registros []libroiva.Registro `json:"registros,omitempty"` // tapa al de Resultado
	}{true, s.nombreCliente(r, l.Cliente), l, regs})
}

// GET /api/libro-iva/archivos.zip con los mismos parámetros: los dos archivos de ARCA y el
// resumen.csv. No concilia: la conciliación solo avisa y no cambia lo que va.
func (s *Server) libroIVAZip(w http.ResponseWriter, r *http.Request) {
	l, ok := s.libroDe(w, r, false)
	if !ok {
		return
	}
	if len(l.Registros) == 0 {
		msg := "No hay comprobantes para exportar en ese período."
		if l.Excluidos > 0 {
			msg = "Ninguno de los comprobantes del período se puede exportar: resolvé los problemas en Revisar."
		}
		jsonErr(w, http.StatusConflict, msg)
		return
	}
	var buf bytes.Buffer
	if err := l.Zip(&buf, s.nombreCliente(r, l.Cliente), time.Now().In(zonaAR)); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "libro_iva.exportar", l.Cliente, map[string]any{"cliente": l.Cliente, "periodo": l.Periodo, "solo_aprobados": l.SoloAprobados,
		"comprobantes": len(l.Registros), "alicuotas": l.Resumen.LineasAlicuotas, "excluidos": l.Excluidos})
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="libro-iva-compras-`+l.Cliente+`-`+l.Periodo+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}
