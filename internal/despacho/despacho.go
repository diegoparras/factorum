// Package despacho lee, con cuidado, el texto de un despacho de importación (la declaración que
// imprime el Sistema Informático MALVINA de la Aduana: el PDF suele tener texto). Saca lo que
// puede asegurar —el número de despacho, la fecha de oficialización, la base y el IVA de la
// liquidación y las percepciones (IVA adicional, Ganancias, Ingresos Brutos)— y deja en blanco lo
// que no: si un dato aparece dos veces con valores distintos, o no se sabe de qué alícuota es un
// IVA, no lo pone. Nunca inventa. Todo lo que lee queda con origen «texto» y el comprobante, para
// revisar.
package despacho

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// CodigoLeido es el hallazgo que avisa que el comprobante se leyó como despacho.
const CodigoLeido = "despacho_leido"

var (
	reOficializacion = regexp.MustCompile(`OFICIALIZ[A-Z]*[^0-9\n]{0,40}?(\d{2})[/.-](\d{2})[/.-](\d{4})`)
	reImporte        = regexp.MustCompile(`(?:^|[^\d.,])(\d{1,3}(?:\.\d{3})+,\d{2}|\d+,\d{2})(?:$|[^\d,])`)
	reCodigoLinea    = regexp.MustCompile(`^\s*(?:\d{2,4}\s+)?`)
	reCAE            = regexp.MustCompile(`\bC\.?A\.?E\.?\b`)
)

func plano(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	r, _, err := transform.String(t, s)
	if err != nil {
		r = s
	}
	return strings.ToUpper(r)
}

// Parece dice si el texto es el de un despacho de importación: un número de destinación con la
// forma del SIM, la fecha de oficialización y algo de la Aduana o de la liquidación, y nada de una
// factura electrónica (CAE). Es estricto a propósito: una factura de un importador imprime el
// número de despacho de la mercadería, y no por eso es un despacho.
func Parece(texto string) bool {
	t := plano(texto)
	if len(arca.DespachosEnTexto(t)) == 0 || !strings.Contains(t, "OFICIALIZ") {
		return false
	}
	if !strings.Contains(t, "ADUANA") && !strings.Contains(t, "LIQUIDACION") && !strings.Contains(t, "DESTINACION") {
		return false
	}
	return !reCAE.MatchString(t)
}

type concepto struct {
	base, alicuota, importe modelo.Importe
	conAlicuota             bool
}

// importes de una línea, en el orden en que aparecen (formato argentino: 1.234,56).
func importes(l string) []modelo.Importe {
	var res []modelo.Importe
	for _, m := range reImporte.FindAllStringSubmatch(l, -1) {
		if v, err := modelo.ParsearImporte(m[1]); err == nil {
			res = append(res, v)
		}
	}
	return res
}

// leerConcepto saca base, alícuota e importe de una línea de la liquidación: con tres importes son
// base, alícuota e importe; con dos, base e importe; con uno, el importe.
func leerConcepto(l string) (concepto, bool) {
	vs := importes(l)
	switch len(vs) {
	case 1:
		return concepto{importe: vs[0]}, true
	case 2:
		return concepto{base: vs[0], importe: vs[1]}, true
	case 3:
		return concepto{base: vs[0], alicuota: vs[1], importe: vs[2], conAlicuota: true}, true
	}
	return concepto{}, false
}

// Leer arma el despacho con lo que se puede asegurar del texto. Los hallazgos dicen qué se leyó y
// qué no.
func Leer(texto string) (modelo.Comprobante, []modelo.Hallazgo) {
	t := plano(texto)
	c := modelo.Comprobante{Tipo: modelo.TipoDespacho, Moneda: "PES", Cotizacion: 1}
	c.Marcar("tipo", modelo.OrigenTexto)
	var leidos, faltan []string
	var hall []modelo.Hallazgo

	switch ds := arca.DespachosEnTexto(t); len(ds) {
	case 1:
		c.Despacho = ds[0]
		c.Marcar("despacho", modelo.OrigenTexto)
		leidos = append(leidos, "el número de despacho")
	default:
		faltan = append(faltan, fmt.Sprintf("el número de despacho (aparecen %d distintos)", len(ds)))
	}

	fechas := map[string]bool{}
	for _, m := range reOficializacion.FindAllStringSubmatch(t, -1) {
		fechas[m[3]+"-"+m[2]+"-"+m[1]] = true
	}
	if len(fechas) == 1 {
		for f := range fechas {
			c.Fecha = arca.NormalizarFecha(f)
		}
		c.Marcar("fecha", modelo.OrigenTexto)
		leidos = append(leidos, "la fecha de oficialización")
	} else {
		faltan = append(faltan, "la fecha de oficialización")
	}

	// La liquidación: una línea por concepto. Cada concepto vale si aparece en una sola línea.
	clases := map[string][]concepto{}
	for _, l := range strings.Split(t, "\n") {
		s := strings.TrimSpace(reCodigoLinea.ReplaceAllString(l, ""))
		var clase string
		switch {
		case strings.Contains(s, "IVA ADICIONAL") || strings.Contains(s, "PERCEPCION") && strings.Contains(s, "IVA"):
			clase = "percepcion_iva"
		case strings.Contains(s, "GANANCIAS"):
			clase = "percepcion_nacionales"
		case strings.Contains(s, "INGRESOS BRUTOS") || strings.Contains(s, "IIBB") || strings.Contains(s, "II.BB"):
			clase = "percepcion_iibb"
		case strings.HasPrefix(s, "IVA ") || strings.HasPrefix(s, "I.V.A"):
			clase = "iva"
		default:
			continue
		}
		if k, ok := leerConcepto(s); ok {
			clases[clase] = append(clases[clase], k)
		}
	}
	uno := func(clase string) (concepto, bool) {
		ks := clases[clase]
		if len(ks) != 1 {
			return concepto{}, false
		}
		return ks[0], true
	}
	nombres := map[string]string{"percepcion_iva": "el IVA adicional (percepción de IVA)", "percepcion_nacionales": "la percepción de Ganancias", "percepcion_iibb": "la percepción de Ingresos Brutos"}
	for _, clase := range []string{"percepcion_iva", "percepcion_nacionales", "percepcion_iibb"} {
		k, ok := uno(clase)
		if !ok {
			if len(clases[clase]) > 1 {
				faltan = append(faltan, nombres[clase]+" (aparece más de una vez)")
			}
			continue
		}
		switch clase {
		case "percepcion_iva":
			c.PercepcionIVA = k.importe
		case "percepcion_nacionales":
			c.PercepcionNacionales = k.importe
		case "percepcion_iibb":
			c.PercepcionIIBB = k.importe
		}
		c.OtrosTributos += k.importe
		c.Marcar(clase, modelo.OrigenTexto)
		leidos = append(leidos, nombres[clase])
	}
	if c.OtrosTributos > 0 {
		c.Marcar("otros_tributos", modelo.OrigenCalculo)
	}

	if k, ok := uno("iva"); ok && k.base > 0 && k.importe > 0 {
		// La alícuota: la que dice la línea, o la que da el IVA sobre la base (21 o 10,5 %, al centavo).
		var tasa modelo.Importe
		switch {
		case k.conAlicuota && (k.alicuota == 2100 || k.alicuota == 1050):
			tasa = k.alicuota
		case abs(k.base*21/100-k.importe) <= 1:
			tasa = 2100
		case abs(k.base*105/1000-k.importe) <= 1:
			tasa = 1050
		}
		if tasa == 0 {
			faltan = append(faltan, fmt.Sprintf("la alícuota del IVA (se leyó IVA %s sobre %s)", k.importe.AR(), k.base.AR()))
		} else {
			c.NetoGravado = k.base
			c.Marcar("neto_gravado", modelo.OrigenTexto)
			if tasa == 2100 {
				c.IVA21 = k.importe
				c.Marcar("iva_21", modelo.OrigenTexto)
			} else {
				c.IVA105 = k.importe
				c.Marcar("iva_10_5", modelo.OrigenTexto)
			}
			c.Total = c.NetoGravado + c.IVATotal() + c.OtrosTributos
			c.Marcar("total", modelo.OrigenCalculo)
			leidos = append(leidos, "la base y el IVA")
		}
	} else {
		faltan = append(faltan, "la base imponible y el IVA de la liquidación")
	}

	msg := "Se leyó como despacho de importación; revisá cada dato contra el PDF."
	if len(leidos) > 0 {
		msg += " Se leyó " + strings.Join(leidos, ", ") + "."
	}
	if len(faltan) > 0 {
		msg += " No se pudo asegurar " + strings.Join(faltan, ", ") + ": cargalo en Revisar."
	}
	msg += " Falta el proveedor del exterior (con su CUIT país)."
	hall = append(hall, modelo.Hallazgo{Codigo: CodigoLeido, Nivel: modelo.NivelAviso, Campo: "despacho", Mensaje: msg})
	return c, hall
}

func abs(v modelo.Importe) modelo.Importe {
	if v < 0 {
		return -v
	}
	return v
}
