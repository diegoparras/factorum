package iaproveedor

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/modelo"
)

// TicketPrueba es el ticket que manda «Probar»: un tique de una estación de servicio, inventado
// (testdata/tickets/02_ypf_tique.png en escala de grises), con un CUIT y un total conocidos.
//
//go:embed ticket_prueba.png
var TicketPrueba []byte

// Lo que tiene que leer.
const (
	PruebaCUIT  = "30708888881"
	PruebaTotal = modelo.Importe(4406250) // $ 44.062,50
)

// Resultado de «Probar».
type Resultado struct {
	Exito    bool    `json:"exito"`
	Mensaje  string  `json:"mensaje"`
	Segundos float64 `json:"segundos,omitempty"`
	Modelo   string  `json:"modelo,omitempty"`
	// Leido: lo que leyó el modelo del ticket de prueba (vacío si no respondió).
	Leido []CampoLeido `json:"leido,omitempty"`
}

// CampoLeido es una fila de «qué leyó».
type CampoLeido struct {
	Campo    string `json:"campo"`
	Valor    string `json:"valor"`
	Esperado string `json:"esperado,omitempty"`
	Bien     *bool  `json:"bien,omitempty"` // nil: no se controla
}

// Probar le manda el ticket de prueba al modelo y dice qué leyó y cuánto tardó. prov es el
// proveedor elegido (para los mensajes). Nunca devuelve la clave.
func Probar(ctx context.Context, v *extractor.Vision, prov string, ahora func() time.Time) Resultado {
	if ahora == nil {
		ahora = time.Now
	}
	nombre := Nombre(prov)
	t0 := ahora()
	c, err := v.Extraer(ctx, TicketPrueba, "image/png", "")
	seg := math.Round(ahora().Sub(t0).Seconds()*10) / 10
	if err != nil {
		return Resultado{Mensaje: Ocultar(mensajeError(err, nombre, v.Modelo, seg), v.APIKey), Modelo: v.Modelo}
	}
	bien := func(b bool) *bool { return &b }
	cuitOK := c.CUITEmisor == PruebaCUIT
	totalOK := c.Total == PruebaTotal
	leido := []CampoLeido{
		{Campo: "CUIT del emisor", Valor: cuitTexto(c.CUITEmisor), Esperado: arca.FormatearCUIT(PruebaCUIT), Bien: bien(cuitOK)},
		{Campo: "Total", Valor: totalTexto(c), Esperado: "$ " + PruebaTotal.AR(), Bien: bien(totalOK)},
		{Campo: "Razón social", Valor: vacioSi(c.RazonSocialEmisor)},
		{Campo: "Fecha", Valor: vacioSi(c.Fecha)},
		{Campo: "Punto de venta y número", Valor: numeroTexto(c)},
	}
	r := Resultado{Segundos: seg, Modelo: v.Modelo, Leido: leido}
	tarda := segundosTexto(seg)
	switch {
	case cuitOK && totalOK:
		r.Exito = true
		r.Mensaje = fmt.Sprintf("Anda: %s leyó bien el ticket de prueba (CUIT y total) en %s.", v.Modelo, tarda)
	case cuitOK || totalOK:
		mal := "el total"
		if !cuitOK {
			mal = "el CUIT"
		}
		r.Mensaje = fmt.Sprintf("Responde en %s, pero leyó mal %s del ticket de prueba. Puede andar con comprobantes claros: probá con otro modelo o revisá lo que lea.", tarda, mal)
	default:
		r.Mensaje = fmt.Sprintf("Responde en %s, pero no leyó ni el CUIT ni el total del ticket de prueba: ¿el modelo lee imágenes?", tarda)
	}
	return r
}

func mensajeError(err error, nombre, mod string, seg float64) string {
	var em *extractor.ErrorModelo
	if errors.As(err, &em) {
		cuerpo := strings.ToLower(em.Cuerpo)
		switch {
		case em.Status == 401 || em.Status == 403:
			return "La clave no es válida para " + nombre + "."
		case em.Status == 404 || strings.Contains(cuerpo, "model_not_found") || strings.Contains(cuerpo, "does not exist") || strings.Contains(cuerpo, "not found"):
			return fmt.Sprintf("%s no tiene el modelo %s (o tu clave no tiene acceso). Elegilo de la lista.", capital(nombre), mod)
		case em.Status == 429:
			return fmt.Sprintf("%s dice que te pasaste del límite o que no hay saldo (429). Revisá la cuenta.", capital(nombre))
		case em.Status == 400 && (strings.Contains(cuerpo, "image") || strings.Contains(cuerpo, "vision")):
			return fmt.Sprintf("%s rechazó la imagen: el modelo %s no lee imágenes. Elegí otro.", capital(nombre), mod)
		}
		return fmt.Sprintf("%s respondió %d: %s", capital(nombre), em.Status, resumen(em.Cuerpo))
	}
	m := err.Error()
	if strings.Contains(m, "no respondió en") {
		return fmt.Sprintf("El modelo %s no respondió en %s. Si es Ollama sin GPU, puede tardar más: subí LLM_TIMEOUT_S o probá un modelo más chico.", mod, segundosTexto(seg))
	}
	if strings.Contains(m, "no se pudo hablar") {
		return fmt.Sprintf("No se pudo conectar con %s: revisá la URL y que el servidor de Factorum llegue hasta ahí.", nombre)
	}
	return capital(m)
}

// resumen saca el mensaje de un error JSON de un proveedor.
func resumen(cuerpo string) string {
	if m := strings.TrimPrefix(mensajeDe([]byte(cuerpo), ""), ": "); m != "" {
		return m
	}
	if r := []rune(cuerpo); len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return cuerpo
}

func capital(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return strings.ToUpper(string(r[0])) + string(r[1:])
}

func segundosTexto(s float64) string {
	if s >= 120 {
		return duracion(int(math.Round(s)))
	}
	return strings.Replace(fmt.Sprintf("%.1f s", s), ".", ",", 1)
}

func vacioSi(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no lo leyó)"
	}
	return s
}

func cuitTexto(s string) string {
	if s == "" {
		return "(no lo leyó)"
	}
	if len(s) == 11 {
		return arca.FormatearCUIT(s)
	}
	return s
}

func totalTexto(c modelo.Comprobante) string {
	if c.Origen("total") == "" {
		return "(no lo leyó)"
	}
	return "$ " + c.Total.AR()
}

func numeroTexto(c modelo.Comprobante) string {
	if c.PuntoVenta == 0 && c.Numero == 0 {
		return "(no lo leyó)"
	}
	return fmt.Sprintf("%05d-%08d", c.PuntoVenta, c.Numero)
}
