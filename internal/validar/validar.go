// Package validar tiene los controles que hace un contador con la factura al lado: que las sumas
// cierren, que el CUIT sea válido, que la letra corresponda al tipo, que el CAE esté y no esté
// vencido a la fecha de emisión. Son funciones puras: mismo comprobante, mismo resultado.
package validar

import (
	"fmt"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Tolerancia de redondeo para comparar sumas: un centavo por componente, como mínimo dos.
func tolerancia(c *modelo.Comprobante) modelo.Importe {
	n := 0
	for _, v := range []modelo.Importe{c.NetoGravado, c.NoGravado, c.Exento, c.IVA27, c.IVA21, c.IVA105, c.IVA5, c.IVA25, c.OtrosTributos} {
		if v != 0 {
			n++
		}
	}
	if n < 2 {
		n = 2
	}
	return modelo.Importe(n)
}

func abs(i modelo.Importe) modelo.Importe {
	if i < 0 {
		return -i
	}
	return i
}

// Comprobante corre todos los controles. hoy se usa para detectar fechas futuras.
func Comprobante(c *modelo.Comprobante, hoy time.Time) []modelo.Hallazgo {
	var h []modelo.Hallazgo
	add := func(cod string, n modelo.Nivel, campo, f string, a ...any) {
		h = append(h, modelo.Hallazgo{Codigo: cod, Nivel: n, Campo: campo, Mensaje: fmt.Sprintf(f, a...)})
	}

	// --- Datos clave ---
	faltan := []struct{ campo, nombre string }{}
	if c.Tipo == 0 {
		faltan = append(faltan, struct{ campo, nombre string }{"tipo", "tipo de comprobante"})
	}
	if c.PuntoVenta == 0 {
		faltan = append(faltan, struct{ campo, nombre string }{"punto_venta", "punto de venta"})
	}
	if c.Numero == 0 {
		faltan = append(faltan, struct{ campo, nombre string }{"numero", "número"})
	}
	if c.Fecha == "" {
		faltan = append(faltan, struct{ campo, nombre string }{"fecha", "fecha"})
	}
	if c.CUITEmisor == "" {
		faltan = append(faltan, struct{ campo, nombre string }{"cuit_emisor", "CUIT del emisor"})
	}
	if c.Total == 0 {
		faltan = append(faltan, struct{ campo, nombre string }{"total", "importe total"})
	}
	for _, f := range faltan {
		add("falta_"+f.campo, modelo.NivelError, f.campo, "No se encontró el %s.", f.nombre)
	}

	if c.RazonSocialEmisor == "" && c.CUITEmisor != "" {
		add("falta_razon_social_emisor", modelo.NivelAviso, "razon_social_emisor", "No se encontró la razón social del emisor (se puede completar con el padrón de ARCA).")
	}

	// --- CUIT ---
	if c.CUITEmisor != "" && !arca.CUITValido(c.CUITEmisor) {
		add("cuit_emisor_invalido", modelo.NivelError, "cuit_emisor", "El CUIT del emisor %s no es válido (dígito verificador).", arca.FormatearCUIT(c.CUITEmisor))
	}
	if c.DocTipoReceptor == 80 && c.DocNroReceptor != "" && !arca.CUITValido(c.DocNroReceptor) {
		add("cuit_receptor_invalido", modelo.NivelError, "doc_nro_receptor", "El CUIT del receptor %s no es válido (dígito verificador).", arca.FormatearCUIT(c.DocNroReceptor))
	}

	// --- Tipo y letra ---
	tp, okTipo := arca.Tipo(c.Tipo)
	if c.Tipo != 0 && !okTipo {
		add("tipo_desconocido", modelo.NivelAviso, "tipo", "El código de comprobante %d no está en la tabla de ARCA que conoce Factorum.", c.Tipo)
	}
	if okTipo && c.Letra != "" && tp.Letra != "" && c.Letra != tp.Letra {
		add("letra_no_coincide", modelo.NivelError, "letra", "La letra impresa (%s) no corresponde al código %d (%s).", c.Letra, c.Tipo, tp.Nombre)
	}

	// --- Fechas ---
	var fecha time.Time
	if c.Fecha != "" {
		f, err := time.Parse("2006-01-02", c.Fecha)
		if err != nil {
			add("fecha_invalida", modelo.NivelError, "fecha", "La fecha %q no es válida.", c.Fecha)
		} else {
			fecha = f
			if f.After(hoy.AddDate(0, 0, 1)) {
				add("fecha_futura", modelo.NivelError, "fecha", "La fecha de emisión (%s) es posterior a hoy.", c.Fecha)
			}
			if f.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
				add("fecha_antigua", modelo.NivelAviso, "fecha", "La fecha de emisión (%s) es anterior al año 2000.", c.Fecha)
			}
		}
	}

	// --- CAE ---
	if c.CAE == "" {
		add("sin_cae", modelo.NivelAviso, "cae", "No se encontró el CAE: sin él no se puede constatar el comprobante.")
	} else if len(arca.SoloDigitos(c.CAE)) != 14 {
		add("cae_invalido", modelo.NivelError, "cae", "El CAE %q no tiene 14 dígitos.", c.CAE)
	}
	if c.VtoCAE != "" && !fecha.IsZero() {
		if v, err := time.Parse("2006-01-02", c.VtoCAE); err == nil && v.Before(fecha) {
			add("cae_vencido_a_la_emision", modelo.NivelError, "vto_cae", "El CAE vence (%s) antes de la fecha de emisión (%s).", c.VtoCAE, c.Fecha)
		}
	}

	// --- Sumas ---
	if c.Total != 0 {
		tol := tolerancia(c)
		switch {
		case c.Letra == "B" || c.Letra == "C":
			if c.Subtotal != 0 {
				if d := abs(c.Subtotal + c.OtrosTributos - c.Total); d > tol {
					add("suma_no_cierra", modelo.NivelError, "total", "Subtotal + otros tributos (%s) no da el total (%s).", (c.Subtotal + c.OtrosTributos).AR(), c.Total.AR())
				}
			}
			if c.Letra == "C" && c.IVATotal() != 0 {
				add("iva_en_factura_c", modelo.NivelAviso, "iva_21", "Una factura C no discrimina IVA, pero se leyó IVA por %s.", c.IVATotal().AR())
			}
		default:
			suma := c.SumaComponentes()
			if suma == 0 {
				add("sin_detalle_importes", modelo.NivelAviso, "neto_gravado", "No se encontró el detalle de neto e IVA: solo el total.")
			} else if d := abs(suma - c.Total); d > tol {
				add("suma_no_cierra", modelo.NivelError, "total", "Neto + IVA + otros (%s) no da el total (%s). Diferencia: %s.", suma.AR(), c.Total.AR(), (c.Total - suma).AR())
			}
			// Con una sola alícuota se puede controlar el IVA contra el neto.
			if c.NetoGravado != 0 {
				type alic struct {
					v    modelo.Importe
					tasa float64
					txt  string
				}
				var con []alic
				for _, a := range []alic{{c.IVA27, 0.27, "27%"}, {c.IVA21, 0.21, "21%"}, {c.IVA105, 0.105, "10,5%"}, {c.IVA5, 0.05, "5%"}, {c.IVA25, 0.025, "2,5%"}} {
					if a.v != 0 {
						con = append(con, a)
					}
				}
				if len(con) == 1 {
					esperado := modelo.DesdeFloat(c.NetoGravado.Float() * con[0].tasa)
					if d := abs(esperado - con[0].v); d > 2 {
						add("iva_no_corresponde", modelo.NivelAviso, "neto_gravado", "El IVA %s (%s) no es el %s del neto gravado (%s daría %s).", con[0].txt, con[0].v.AR(), con[0].txt, c.NetoGravado.AR(), esperado.AR())
					}
				}
			}
		}
	}
	for _, v := range []struct {
		campo string
		val   modelo.Importe
	}{{"neto_gravado", c.NetoGravado}, {"total", c.Total}, {"otros_tributos", c.OtrosTributos}} {
		if v.val < 0 {
			add("importe_negativo", modelo.NivelAviso, v.campo, "El importe %s es negativo.", v.campo)
		}
	}

	// --- Moneda ---
	if c.Moneda != "" && c.Moneda != "PES" {
		add("moneda_extranjera", modelo.NivelAviso, "moneda", "Comprobante en moneda extranjera (%s, cotización %.4f): los importes no están en pesos.", c.Moneda, c.Cotizacion)
	}
	return h
}

// Estado resume los hallazgos y el origen de los datos clave.
func Estado(c *modelo.Comprobante, h []modelo.Hallazgo) modelo.Estado {
	for _, x := range h {
		if x.Nivel == modelo.NivelError {
			return modelo.EstadoError
		}
	}
	for _, x := range h {
		if x.Nivel == modelo.NivelAviso {
			return modelo.EstadoRevisar
		}
	}
	// Datos que salieron de OCR o de IA siempre los mira una persona.
	for _, o := range c.Origenes {
		if o == modelo.OrigenOCR || o == modelo.OrigenVision {
			return modelo.EstadoRevisar
		}
	}
	return modelo.EstadoOK
}

// Confianza da un número entre 0 y 1 según de dónde salieron los campos clave.
func Confianza(c *modelo.Comprobante, h []modelo.Hallazgo) float64 {
	peso := map[modelo.Origen]float64{
		modelo.OrigenQR: 1, modelo.OrigenManual: 1, modelo.OrigenArcanum: 1, modelo.OrigenTexto: 0.95,
		modelo.OrigenCalculo: 0.9, modelo.OrigenOCR: 0.7, modelo.OrigenVision: 0.6,
	}
	claves := []string{"tipo", "punto_venta", "numero", "fecha", "cuit_emisor", "total", "cae"}
	suma := 0.0
	for _, k := range claves {
		suma += peso[c.Origen(k)]
	}
	conf := suma / float64(len(claves))
	for _, x := range h {
		switch x.Nivel {
		case modelo.NivelError:
			conf *= 0.6
		case modelo.NivelAviso:
			conf *= 0.9
		}
	}
	return float64(int(conf*1000)) / 1000
}
