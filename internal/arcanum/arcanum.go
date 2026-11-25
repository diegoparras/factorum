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
		return fmt.Errorf("no se pudo contactar a Arcanum: %w", err)
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

// Padron devuelve la razón social y la condición frente al IVA (padrón A5).
func (c *Cliente) Padron(ctx context.Context, consultante, cuit string) (razon, condicion string, err error) {
	if consultante == "" {
		consultante = c.Consultante
	}
	var r struct {
		Datos map[string]any `json:"datos"`
	}
	if err := c.pedir(ctx, http.MethodGet, "/api/padron/a5/"+url.PathEscape(arca.SoloDigitos(cuit))+"?cuit="+url.QueryEscape(consultante), nil, &r); err != nil {
		return "", "", err
	}
	razon, condicion = extraerPadron(r.Datos)
	return razon, condicion, nil
}

// extraerPadron busca la razón social en la respuesta del A5, que cambia de forma según el tipo de
// persona (jurídica: razonSocial; humana: apellido + nombre).
func extraerPadron(d map[string]any) (string, string) {
	var razon, cond string
	var buscar func(v any)
	buscar = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if s, ok := x["razonSocial"].(string); ok && razon == "" {
				razon = s
			}
			if a, ok := x["apellido"].(string); ok && razon == "" {
				n, _ := x["nombre"].(string)
				razon = strings.TrimSpace(a + " " + n)
			}
			if s, ok := x["condicionIva"].(string); ok && cond == "" {
				cond = s
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
	return razon, cond
}

// Veredicto resume la constatación y los hallazgos locales para una persona.
func Veredicto(rc *modelo.ResultadoCbte, k *Constatacion) (string, string) {
	errLocal := rc.Estado == modelo.EstadoError
	if k != nil && k.Apoc != nil && k.Apoc.EsApocrifo != nil && *k.Apoc.EsApocrifo {
		return "apocrifa", "El emisor figura en la base de apócrifos de ARCA: no computes este comprobante."
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
