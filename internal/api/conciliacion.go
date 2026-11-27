package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/store"
)

// errFaltaCliente: falta elegir el cliente (400) o no es de los que ve quien pide (403).
var errFaltaCliente = errors.New("Elegí el cliente (cliente=CUIT)")

// ClienteConciliacion limpia el CUIT del cliente y controla que quien pide lo pueda ver. Sin CUIT y
// con un solo cliente asignado, ese.
func ClienteConciliacion(id *auth.Identidad, cuit string) (string, error) {
	c, err := cuitEnAlcance(id, cuit)
	if err != nil {
		return "", err
	}
	if c == "" {
		return "", errFaltaCliente
	}
	if len(c) != 11 {
		return "", fmt.Errorf("El CUIT del cliente tiene que tener 11 dígitos")
	}
	return c, nil
}

// conciliacionDe lee cliente, desde y hasta del pedido y concilia. Escribe el error si lo hay.
func (s *Server) conciliacionDe(w http.ResponseWriter, r *http.Request) (*app.Conciliacion, bool) {
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
	desde, hasta := strings.TrimSpace(q.Get("desde")), strings.TrimSpace(q.Get("hasta"))
	if err := app.ValidarPeriodo(desde, hasta); err != nil {
		jsonErr(w, http.StatusBadRequest, mayuscula(err.Error()))
		return nil, false
	}
	c, err := s.App.Conciliar(r.Context(), cliente, desde, hasta, verdadero(q.Get("refrescar")))
	if err != nil {
		s.fallo(w, err)
		return nil, false
	}
	return c, true
}

func mayuscula(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// GET /api/conciliacion?cliente=CUIT&desde=AAAA-MM-DD&hasta=AAAA-MM-DD[&refrescar=1]
func (s *Server) conciliacion(w http.ResponseWriter, r *http.Request) {
	c, ok := s.conciliacionDe(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		OK bool `json:"ok"`
		*app.Conciliacion
	}{true, c})
}

// GET /api/conciliacion/clientes: los clientes que ve quien pide, con cuántos comprobantes tienen.
// Con clientes asignados, están todos aunque todavía no tengan nada cargado.
func (s *Server) clientesConciliacion(w http.ResponseWriter, r *http.Request) {
	id := IdentidadDe(r.Context())
	cs, err := s.App.Store.ClientesConComprobantes(r.Context(), id.Alcance())
	if err != nil {
		s.fallo(w, err)
		return
	}
	if id.Restringido() {
		hay := map[string]bool{}
		for _, c := range cs {
			hay[c.CUIT] = true
		}
		for _, c := range id.CUITs {
			if !hay[c] {
				cs = append(cs, store.ClienteConComprobantes{CUIT: c})
			}
		}
	}
	jsonOK(w, http.StatusOK, map[string]any{"clientes": cs, "arcanum": s.App.Arcanum() != nil})
}

// GET /api/conciliacion.xlsx y .csv, con los mismos parámetros y, si se quiere, grupo=falta_cargar
// (o cualquier otro grupo) para bajar solo ese.
func (s *Server) exportarConciliacion(formato string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, ok := s.conciliacionDe(w, r)
		if !ok {
			return
		}
		if c.Estado != app.ConcLista || c.Resultado == nil {
			jsonErr(w, http.StatusConflict, c.Mensaje)
			return
		}
		grupo := strings.TrimSpace(r.URL.Query().Get("grupo"))
		if grupo != "" && conciliar.NombreGrupo[grupo] == "" {
			jsonErr(w, http.StatusBadRequest, "grupo: coinciden, diferencias, falta_cargar o no_figura")
			return
		}
		var buf bytes.Buffer
		var err error
		tipo := "text/csv; charset=utf-8"
		if formato == "xlsx" {
			tipo = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
			err = export.ConciliacionExcel(&buf, c.Resultado, InfoConciliacion(c, s.nombreCliente(r, c.Cliente)), grupo)
		} else {
			err = export.ConciliacionCSV(&buf, c.Resultado, grupo)
		}
		if err != nil {
			s.fallo(w, err)
			return
		}
		s.auditar(r, "conciliacion.exportar", formato, map[string]any{"cliente": c.Cliente, "desde": c.Desde, "hasta": c.Hasta, "grupo": grupo})
		nombre := "conciliacion-" + c.Cliente + "-" + c.Desde + "-a-" + c.Hasta
		if grupo != "" {
			nombre += "-" + strings.ReplaceAll(grupo, "_", "-")
		}
		w.Header().Set("Content-Type", tipo)
		w.Header().Set("Content-Disposition", `attachment; filename="`+nombre+`.`+formato+`"`)
		_, _ = w.Write(buf.Bytes())
	}
}

// nombreCliente busca la razón social del cliente entre sus comprobantes (vacío si no hay).
func (s *Server) nombreCliente(r *http.Request, cuit string) string {
	cs, err := s.App.Store.ClientesConComprobantes(r.Context(), []string{cuit})
	if err != nil || len(cs) == 0 {
		return ""
	}
	return cs[0].Nombre
}

// InfoConciliacion arma el encabezado del Excel.
func InfoConciliacion(c *app.Conciliacion, nombre string) export.InfoConciliacion {
	info := export.InfoConciliacion{Cliente: c.Cliente, Nombre: nombre, Desde: c.Desde, Hasta: c.Hasta, SinSincronizar: c.SinSincronizar, Aviso: c.Mensaje}
	info.Sincronizacion = TextoSincronizacion(c)
	return info
}

// TextoSincronizacion resume el estado de Mis Comprobantes en Arcanum para una persona.
func TextoSincronizacion(c *app.Conciliacion) string {
	k := c.Cobertura
	if k == nil {
		return ""
	}
	if !k.Sincronizado {
		return "Arcanum todavía no bajó Mis Comprobantes de este CUIT."
	}
	t := ""
	if u := k.UltimaSincronizacion; u != nil {
		cuando := u.Terminada
		if x, err := time.Parse(time.RFC3339, u.Terminada); err == nil {
			cuando = x.In(zonaAR).Format("02/01/2006 15:04")
		}
		t = "Última sincronización: " + cuando + "."
	}
	var rs []string
	for _, x := range k.Rangos {
		rs = append(rs, fechaCorta(x.Desde)+" al "+fechaCorta(x.Hasta))
	}
	if len(rs) > 0 {
		t += " Bajado: " + strings.Join(rs, ", ") + "."
	}
	if k.EnCurso != nil {
		t += " Hay una sincronización en curso."
	}
	return strings.TrimSpace(t)
}

func fechaCorta(f string) string {
	if len(f) != 10 {
		return f
	}
	return f[8:10] + "/" + f[5:7] + "/" + f[0:4]
}
