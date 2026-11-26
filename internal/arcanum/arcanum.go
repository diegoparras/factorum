// Package arcanum habla con Arcanum (el gateway de ARCA de la suite) para lo que solo ARCA sabe:
// si un comprobante existe (WSCDC), si el emisor es apócrifo (WSAPOC) y los datos del padrón.
package arcanum

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

type Cliente struct {
	URL         string
	APIKey      string
	Consultante string // CUIT con certificado y WSCDC habilitado
	HTTP        *http.Client
}

func Nuevo(url, key, consultante string, timeout time.Duration) *Cliente {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Cliente{URL: strings.TrimRight(url, "/"), APIKey: key, Consultante: arca.SoloDigitos(consultante), HTTP: &http.Client{Timeout: timeout}}
}

// Apoc es el resultado de la base de apócrifos.
type Apoc struct {
	Consultado     bool    `json:"consultado"`
	EsApocrifo     *bool   `json:"esApocrifo"`
	FechaCondicion *string `json:"fechaCondicion,omitempty"`
}

// Constatacion es la respuesta de WSCDC más la de apócrifos, como la devuelve Arcanum.
type Constatacion struct {
	Valido        *bool         `json:"valido"`
	Resultado     *string       `json:"resultado"` // A aprobado, R rechazado, O observado
	Observaciones []Observacion `json:"observaciones"`
	Error         string        `json:"error,omitempty"`
	Apoc          *Apoc         `json:"apoc,omitempty"`
	Fecha         string        `json:"fecha,omitempty"`
}

type Observacion struct {
	Code    any    `json:"code"`
	Message string `json:"message"`
}

type errHTTP struct {
	status int
	msg    string
}

func (e *errHTTP) Error() string { return fmt.Sprintf("Arcanum respondió %d: %s", e.status, e.msg) }

// errConexion: no se pudo hablar con Arcanum (red, timeout). Vale la pena reintentar.
type errConexion struct{ err error }

func (e *errConexion) Error() string { return "no se pudo contactar a Arcanum: " + e.err.Error() }
func (e *errConexion) Unwrap() error { return e.err }

func (c *Cliente) pedir(ctx context.Context, metodo, ruta string, cuerpo any, dst any) error {
	var r io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, c.URL+ruta, r)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.APIKey)
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return &errConexion{err}
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		if e.Error == "" {
			e.Error = http.StatusText(res.StatusCode)
		}
		return &errHTTP{res.StatusCode, e.Error}
	}
	return json.Unmarshal(b, dst)
}

// Salud consulta /api/health.
func (c *Cliente) Salud(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.URL+"/api/health", nil)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("Arcanum respondió %d", res.StatusCode)
	}
	return nil
}

// ItemConstatacion arma el pedido para WSCDC desde un comprobante leído.
func ItemConstatacion(c *modelo.Comprobante) (map[string]any, error) {
	if c.CUITEmisor == "" || c.Tipo == 0 || c.Numero == 0 || c.Fecha == "" || c.CAE == "" || c.Total == 0 {
		return nil, errors.New("faltan datos para constatar (CUIT del emisor, tipo, número, fecha, CAE o total)")
	}
	docTipo, docNro := c.DocTipoReceptor, c.DocNroReceptor
	if docTipo == 0 {
		docTipo = 99
	}
	if docNro == "" {
		docNro = "0"
	}
	return map[string]any{
		"cuitEmisor":      c.CUITEmisor,
		"puntoVenta":      c.PuntoVenta,
		"tipoComprobante": c.Tipo,
		"numero":          c.Numero,
		"fecha":           c.Fecha,
		"importeTotal":    c.Total.Float(),
		"cae":             c.CAE,
		"tipoCodAut":      c.TipoCodAut,
		"docTipo":         docTipo,
		"docNro":          docNro,
	}, nil
}

// Constatar manda hasta 500 comprobantes a POST /api/constatacion de Arcanum (WSCDC + apócrifos).
// Devuelve una constatación por comprobante, en el mismo orden. consultante "" usa el por defecto.
func (c *Cliente) Constatar(ctx context.Context, consultante string, cbtes []*modelo.Comprobante) ([]Constatacion, error) {
	if consultante == "" {
		consultante = c.Consultante
	}
	consultante = arca.SoloDigitos(consultante)
	if len(consultante) != 11 {
		return nil, errors.New("falta el CUIT consultante (con certificado y WSCDC en Arcanum): ARCANUM_CUIT_CONSULTANTE")
	}
	res := make([]Constatacion, len(cbtes))
	var items []map[string]any
	var idx []int
	for i, cb := range cbtes {
		it, err := ItemConstatacion(cb)
		if err != nil {
			res[i] = Constatacion{Error: err.Error()}
			continue
		}
		items = append(items, it)
		idx = append(idx, i)
	}
	if len(items) == 0 {
		return res, nil
	}
	var r struct {
		Constatacion struct {
			Resultados []Constatacion `json:"resultados"`
		} `json:"constatacion"`
	}
	if err := c.pedir(ctx, http.MethodPost, "/api/constatacion", map[string]any{"cuit": consultante, "items": items}, &r); err != nil {
		return nil, err
	}
	ahora := time.Now().UTC().Format(time.RFC3339)
	for k, x := range r.Constatacion.Resultados {
		if k < len(idx) {
			x.Fecha = ahora
			res[idx[k]] = x
		}
	}
	return res, nil
}

// Actividad es una actividad económica del padrón (código CLAE de seis dígitos).
type Actividad struct {
	Codigo      string `json:"codigo"`
	Descripcion string `json:"descripcion"`
	Orden       int    `json:"orden"` // 1 = principal
}

// DatosPadron es lo que Factorum usa del padrón A5: razón social, condición frente al IVA y la
// actividad principal (para el rubro del gasto).
type DatosPadron struct {
	Razon     string
	Condicion string
	Actividad *Actividad // nil si el padrón no informa actividades
}

// Padron consulta el padrón A5 (GET /api/padron/a5/:cuit?cuit=<consultante>).
func (c *Cliente) Padron(ctx context.Context, consultante, cuit string) (DatosPadron, error) {
	if consultante == "" {
		consultante = c.Consultante
	}
	var r struct {
		Datos map[string]any `json:"datos"`
	}
	if err := c.pedir(ctx, http.MethodGet, "/api/padron/a5/"+url.PathEscape(arca.SoloDigitos(cuit))+"?cuit="+url.QueryEscape(consultante), nil, &r); err != nil {
		return DatosPadron{}, err
	}
	return ExtraerPadron(r.Datos), nil
}

// ExtraerPadron busca los datos en la respuesta del A5, que cambia de forma según el tipo de persona
// (jurídica: razonSocial; humana: apellido + nombre). Las actividades vienen en
// datosRegimenGeneral.actividad y datosMonotributo.actividad (un objeto o una lista), cada una con
// idActividad, descripcionActividad y orden (1 = principal): se toma la de menor orden.
func ExtraerPadron(d map[string]any) DatosPadron {
	var p DatosPadron
	var buscar func(v any)
	buscar = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if s, ok := x["razonSocial"].(string); ok && p.Razon == "" {
				p.Razon = s
			}
			if a, ok := x["apellido"].(string); ok && p.Razon == "" {
				n, _ := x["nombre"].(string)
				p.Razon = strings.TrimSpace(a + " " + n)
			}
			if s, ok := x["condicionIva"].(string); ok && p.Condicion == "" {
				p.Condicion = s
			}
			if act, ok := actividadDe(x); ok && (p.Actividad == nil || act.Orden < p.Actividad.Orden) {
				p.Actividad = &act
			}
			for _, w := range x {
				buscar(w)
			}
		case []any:
			for _, w := range x {
				buscar(w)
			}
		}
	}
	buscar(d)
	return p
}

func actividadDe(x map[string]any) (Actividad, bool) {
	id := texto(x["idActividad"])
	if id == "" {
		return Actividad{}, false
	}
	a := Actividad{Codigo: id, Descripcion: texto(x["descripcionActividad"]), Orden: 99}
	if o, err := strconv.Atoi(texto(x["orden"])); err == nil && o > 0 {
		a.Orden = o
	}
	return a, true
}

// texto pasa a string un valor del JSON (ARCA manda los números a veces como texto y a veces no).
func texto(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	}
	return ""
}

// Apoc busca un CUIT en la base de apócrifos (GET /api/apoc/:cuit?cuit=<consultante>).
// Arcanum responde { ok, cuit, esApocrifo, codigo, descripcion, fechaCondicion, ... }; si ARCA no
// validó las credenciales (código 201) viene un "error" y no se puede decir que no sea apócrifo.
func (c *Cliente) Apoc(ctx context.Context, consultante, cuit string) (*Apoc, error) {
	if consultante == "" {
		consultante = c.Consultante
	}
	consultante = arca.SoloDigitos(consultante)
	if len(consultante) != 11 {
		return nil, errors.New("falta el CUIT consultante (con certificado en Arcanum): ARCANUM_CUIT_CONSULTANTE")
	}
	var r struct {
		EsApocrifo     *bool   `json:"esApocrifo"`
		FechaCondicion *string `json:"fechaCondicion"`
		Descripcion    string  `json:"descripcion"`
		Error          string  `json:"error"`
	}
	if err := c.pedir(ctx, http.MethodGet, "/api/apoc/"+url.PathEscape(arca.SoloDigitos(cuit))+"?cuit="+url.QueryEscape(consultante), nil, &r); err != nil {
		return nil, err
	}
	if r.Error != "" || r.EsApocrifo == nil {
		msg := r.Descripcion
		if msg == "" {
			msg = r.Error
		}
		if msg == "" {
			msg = "respuesta sin esApocrifo"
		}
		return nil, fmt.Errorf("la base de apócrifos no respondió bien: %s", msg)
	}
	return &Apoc{Consultado: true, EsApocrifo: r.EsApocrifo, FechaCondicion: r.FechaCondicion}, nil
}

// ConstatarTodo controla una lista de comprobantes con lo que corresponde a cada uno: las facturas
// van a WSCDC (con apócrifos) en un solo pedido; los tiques de controlador fiscal, que WSCDC no
// cubre, solo a la base de apócrifos (una consulta por CUIT). Devuelve una constatación por
// comprobante, en el mismo orden; la de un tique trae Apoc y Valido en nil. Un error de red o de
// Arcanum en las facturas corta todo (para reintentar); en un tique queda en su constatación.
func (c *Cliente) ConstatarTodo(ctx context.Context, consultante string, cbtes []*modelo.Comprobante) ([]Constatacion, error) {
	res := make([]Constatacion, len(cbtes))
	var facturas []*modelo.Comprobante
	var idxF []int
	apocs := map[string]Constatacion{}
	ahora := time.Now().UTC().Format(time.RFC3339)
	for i, cb := range cbtes {
		if !cb.EsTique && !arca.EsTique(cb.Tipo) {
			facturas = append(facturas, cb)
			idxF = append(idxF, i)
			continue
		}
		if !arca.CUITValido(cb.CUITEmisor) {
			res[i] = Constatacion{Error: "falta un CUIT del emisor válido para consultar la base de apócrifos"}
			continue
		}
		k, ok := apocs[cb.CUITEmisor]
		if !ok {
			a, err := c.Apoc(ctx, consultante, cb.CUITEmisor)
			if err != nil {
				var ec *errConexion
				if errors.As(err, &ec) {
					return nil, err // Arcanum no responde: que reintente la cola
				}
				k = Constatacion{Error: err.Error()}
			} else {
				k = Constatacion{Apoc: a, Fecha: ahora, Observaciones: []Observacion{}}
			}
			apocs[cb.CUITEmisor] = k
		}
		res[i] = k
	}
	if len(facturas) > 0 {
		kf, err := c.Constatar(ctx, consultante, facturas)
		if err != nil {
			return nil, err
		}
		for k, i := range idxF {
			res[i] = kf[k]
		}
	}
	return res, nil
}

// Veredicto resume la constatación y los hallazgos locales para una persona.
func Veredicto(rc *modelo.ResultadoCbte, k *Constatacion) (string, string) {
	errLocal := rc.Estado == modelo.EstadoError
	if k != nil && k.Apoc != nil && k.Apoc.EsApocrifo != nil && *k.Apoc.EsApocrifo {
		return "apocrifa", "El emisor figura en la base de apócrifos de ARCA: no computes este comprobante."
	}
	if c := &rc.Comprobante; c.EsTique || arca.EsTique(c.Tipo) {
		return veredictoTique(errLocal, k)
	}
	if k != nil && k.Valido != nil {
		if !*k.Valido {
			msg := "ARCA no reconoce el comprobante con esos datos."
			if len(k.Observaciones) > 0 {
				msg += " " + k.Observaciones[0].Message
			}
			return "invalida", msg
		}
		if errLocal {
			return "con_errores", "ARCA confirma el comprobante, pero el archivo tiene errores: revisalos."
		}
		return "valida", "ARCA confirma que el comprobante existe con esos datos."
	}
	if k != nil && k.Error != "" {
		return "no_verificable", "No se pudo constatar: " + k.Error
	}
	if errLocal {
		return "con_errores", "El comprobante tiene errores y no se pudo constatar con ARCA."
	}
	return "no_verificable", "Los controles locales dan bien, pero no hay conexión con ARCA para constatarlo."
}

// veredictoTique: WSCDC no cubre los tiques de controlador fiscal, así que lo que se puede decir es
// que el CUIT es válido y que el emisor no está en la base de apócrifos.
func veredictoTique(errLocal bool, k *Constatacion) (string, string) {
	if errLocal {
		return "con_errores", "El ticket tiene errores: revisalos (los tiques no se constatan en ARCA)."
	}
	if k != nil && k.Apoc != nil && k.Apoc.Consultado && k.Apoc.EsApocrifo != nil {
		return "tique_controlado", "Ticket controlado: el CUIT es válido y el emisor no figura como apócrifo (los tiques no se constatan en ARCA)."
	}
	if k != nil && k.Error != "" {
		return "no_verificable", "No se pudo consultar la base de apócrifos: " + k.Error
	}
	return "no_verificable", "Los controles locales dan bien, pero no hay conexión con Arcanum para consultar la base de apócrifos (los tiques no se constatan en ARCA)."
}
