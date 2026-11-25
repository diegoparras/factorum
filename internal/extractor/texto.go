package extractor

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/diegoparras/contadorfacturas/internal/arca"
	"github.com/diegoparras/contadorfacturas/internal/modelo"
)

// ParsearTexto saca los campos de la capa de texto de un comprobante (o del texto que devolvió el
// OCR). Primero busca las etiquetas de "Comprobantes en línea" de ARCA, que es el formato más
// común; después prueba etiquetas genéricas que usan otros sistemas. Solo completa lo que
// encuentra: lo que no está queda vacío, nunca se inventa.
func ParsearTexto(txt string, origen modelo.Origen) modelo.Comprobante {
	var c modelo.Comprobante
	t := normalizar(txt)
	lineas := strings.Split(t, "\n")

	set := func(campo string) { c.Marcar(campo, origen) }

	// --- Tipo y letra ---
	if m := reCod.FindStringSubmatch(t); m != nil {
		if n, _ := strconv.Atoi(m[1]); n > 0 {
			if tp, ok := arca.Tipo(n); ok {
				c.Tipo, c.Letra = n, tp.Letra
				set("tipo")
				set("letra")
			}
		}
	}
	if c.Letra == "" {
		if m := reLetraSola.FindStringSubmatch(t); m != nil {
			c.Letra = m[1] + m[2]
			set("letra")
		}
	}
	if c.Tipo == 0 && c.Letra != "" {
		if clase := claseDesdeTexto(t); clase != "" {
			fce := reFCE.MatchString(t)
			if n := arca.TipoDesde(clase, c.Letra, fce); n > 0 {
				c.Tipo = n
				c.Marcar("tipo", modelo.OrigenCalculo)
			}
		}
	}

	// --- Punto de venta y número ---
	if m := rePVNro.FindStringSubmatch(t); m != nil {
		c.PuntoVenta, _ = strconv.Atoi(m[1])
		c.Numero, _ = strconv.ParseInt(m[2], 10, 64)
		set("punto_venta")
		set("numero")
	} else if m := rePVNroGuion.FindStringSubmatch(t); m != nil {
		c.PuntoVenta, _ = strconv.Atoi(m[1])
		c.Numero, _ = strconv.ParseInt(m[2], 10, 64)
		set("punto_venta")
		set("numero")
	}

	// --- Fecha de emisión ---
	if m := reFechaEmisionRCEL.FindStringSubmatch(t); m != nil {
		c.Fecha = fechaISO(m[1])
		set("fecha")
	} else if m := reFechaEmision.FindStringSubmatch(t); m != nil {
		c.Fecha = fechaISO(m[1])
		set("fecha")
	}

	// --- CUITs: el primero es el emisor, salvo que esté rotulado como receptor ---
	cuits := cuitsEnOrden(lineas)
	for _, cu := range cuits {
		if cu.receptor {
			if c.DocNroReceptor == "" {
				c.DocNroReceptor, c.DocTipoReceptor = cu.nro, 80
				set("doc_nro_receptor")
				set("doc_tipo_receptor")
			}
			continue
		}
		if c.CUITEmisor == "" {
			c.CUITEmisor = cu.nro
			set("cuit_emisor")
		} else if c.DocNroReceptor == "" && cu.nro != c.CUITEmisor {
			c.DocNroReceptor, c.DocTipoReceptor = cu.nro, 80
			set("doc_nro_receptor")
			set("doc_tipo_receptor")
		}
	}
	if c.DocNroReceptor == "" {
		if m := reDNI.FindStringSubmatch(t); m != nil {
			c.DocNroReceptor, c.DocTipoReceptor = m[1], 96
			set("doc_nro_receptor")
			set("doc_tipo_receptor")
		}
	}

	// --- Razones sociales y condición de IVA ---
	if v := primerValor(lineas, reRazonEmisor); v != "" {
		c.RazonSocialEmisor = v
		set("razon_social_emisor")
	}
	if v := primerValor(lineas, reRazonReceptor); v != "" {
		c.RazonSocialReceptor = v
		set("razon_social_receptor")
	}
	if v := primerValor(lineas, reCondIVA); v != "" {
		c.CondicionIVAEmisor = v
		set("condicion_iva_emisor")
	}

	// --- Importes ---
	importes := []struct {
		campo string
		dst   *modelo.Importe
		res   []*regexp.Regexp
	}{
		{"neto_gravado", &c.NetoGravado, reNeto},
		{"no_gravado", &c.NoGravado, reNoGravado},
		{"exento", &c.Exento, reExento},
		{"iva_27", &c.IVA27, reIVA("27")},
		{"iva_21", &c.IVA21, reIVA("21")},
		{"iva_10_5", &c.IVA105, reIVA(`10[,.]5`)},
		{"iva_5", &c.IVA5, reIVA("5")},
		{"iva_2_5", &c.IVA25, reIVA(`2[,.]5`)},
		{"otros_tributos", &c.OtrosTributos, reOtros},
		{"total", &c.Total, reTotal},
	}
	for _, im := range importes {
		if v, ok := importeEtiquetado(lineas, im.res); ok {
			*im.dst = v
			set(im.campo)
		}
	}
	// B y C no discriminan IVA: el "Subtotal" va en su propio campo.
	if c.Letra == "B" || c.Letra == "C" {
		if v, ok := importeEtiquetado(lineas, reSubtotal); ok {
			c.Subtotal = v
			set("subtotal")
		}
		if v, ok := importeEtiquetado(lineas, reIVAContenido); ok {
			c.IVAContenido = v
			set("iva_contenido")
		}
	}

	// --- CAE ---
	if m := reCAE.FindStringSubmatch(t); m != nil {
		c.CAE = m[2]
		c.TipoCodAut = "E"
		if strings.EqualFold(m[1], "CAEA") {
			c.TipoCodAut = "A"
		}
		set("cae")
	}
	if m := reVtoCAE.FindStringSubmatch(t); m != nil {
		c.VtoCAE = fechaISO(m[1])
		set("vto_cae")
	}

	// --- Moneda ---
	if m := reMoneda.FindStringSubmatch(t); m != nil {
		c.Moneda = monedaCodigo(m[1])
		set("moneda")
		if m[2] != "" {
			if v, err := modelo.ParsearImporte(m[2]); err == nil {
				c.Cotizacion = v.Float()
				set("cotizacion")
			}
		}
	}
	return c
}

func normalizar(s string) string {
	r := strings.NewReplacer(" ", " ", "\r", "", "º", "°", "–", "-", "—", "-")
	return r.Replace(s)
}

var (
	reCod              = regexp.MustCompile(`(?i)\bC[OÓ]D(?:IGO)?\.?\s*(?:N[°o]\.?\s*)?(\d{1,3})\b`)
	reLetraSola        = regexp.MustCompile(`(?m)(?:^\s*([ABCEM])\s*$|\s{3,}([ABCEM])\s{3,})`)
	reFCE              = regexp.MustCompile(`(?i)(cr[eé]dito\s+electr[oó]nica|mipyme|FCE)`)
	rePVNro            = regexp.MustCompile(`(?i)Punto\s+de\s+Venta:?\s*(\d{1,5})\s+Comp(?:robante)?\.?\s*N(?:ro|°|úmero)?\.?:?\s*(\d{1,8})`)
	rePVNroGuion       = regexp.MustCompile(`(?i)(?:N[°o]\.?|Nro\.?|N[uú]mero:?|Comprobante:?)\s*(\d{4,5})\s*-\s*(\d{8})\b`)
	reFechaEmisionRCEL = regexp.MustCompile(`(?i)Fecha\s+de\s+Emisi[oó]n:?\s*(\d{2}[/-]\d{2}[/-]\d{4})`)
	reFechaEmision     = regexp.MustCompile(`(?i)Fecha(?:\s+de)?(?:\s+Emisi[oó]n)?:?\s*(\d{2}[/-]\d{2}[/-]\d{4})`)
	reCUIT             = regexp.MustCompile(`(?i)C\.?U\.?I\.?T\.?\s*(?:N[°o]\.?)?:?\s*(\d{2}-?\d{8}-?\d)\b`)
	reDNI              = regexp.MustCompile(`(?i)\bD\.?N\.?I\.?:?\s*(\d{7,8})\b`)
	reCAE              = regexp.MustCompile(`(?i)\b(CAEA?)\s*(?:N[°o]\.?)?:?\s*(\d{14})\b`)
	reVtoCAE           = regexp.MustCompile(`(?i)(?:Fecha\s+de\s+)?V(?:to|encimiento)\.?\s+(?:de(?:l)?\s+)?CAEA?:?\s*(\d{2}[/-]\d{2}[/-]\d{4})`)
	reMoneda           = regexp.MustCompile(`(?i)Moneda:?\s*([A-Z]{3}|D[oó]lar(?:es)?(?:\s+Estadounidense)?|Euros?)\b(?:.*?Cotizaci[oó]n:?\s*\$?\s*([\d.,]+))?`)

	reRazonEmisor   = regexp.MustCompile(`(?i)^\s*Raz[oó]n\s+Social:\s*(.+?)(?:\s{2,}|$)`)
	reRazonReceptor = regexp.MustCompile(`(?i)(?:Apellido\s+y\s+Nombre\s*/\s*Raz[oó]n\s+Social|Raz[oó]n\s+Social\s+del\s+(?:receptor|cliente)|Señor(?:es)?|Sr\.?\s*\(?es\)?|Cliente):\s*(.+?)(?:\s{2,}|$)`)
	reCondIVA       = regexp.MustCompile(`(?i)Condici[oó]n\s+frente\s+al\s+IVA:\s*(.+?)(?:\s{2,}|$)`)

	reNeto         = []*regexp.Regexp{regexp.MustCompile(`(?i)Importe\s+Neto\s+Gravado`), regexp.MustCompile(`(?i)\bNeto\s+Gravado`), regexp.MustCompile(`(?i)Subtotal\s+Neto`)}
	reNoGravado    = []*regexp.Regexp{regexp.MustCompile(`(?i)(?:Importe\s+)?(?:Neto\s+)?No\s+Gravado`)}
	reExento       = []*regexp.Regexp{regexp.MustCompile(`(?i)Importe\s+Exento`), regexp.MustCompile(`(?i)^\s*Exento\b`)}
	reOtros        = []*regexp.Regexp{regexp.MustCompile(`(?i)Importe\s+Otros\s+Tributos`), regexp.MustCompile(`(?i)Otros\s+Tributos`), regexp.MustCompile(`(?i)Percepciones`)}
	reTotal        = []*regexp.Regexp{regexp.MustCompile(`(?i)Importe\s+Total`), regexp.MustCompile(`(?i)^\s*Total(?:\s+a\s+pagar|\s+general)?\s*:?\s*\$`), regexp.MustCompile(`(?i)\bTOTAL\b`)}
	reSubtotal     = []*regexp.Regexp{regexp.MustCompile(`(?i)^\s*Subtotal`), regexp.MustCompile(`(?i)\s{2,}Subtotal\s*:`)}
	reIVAContenido = []*regexp.Regexp{regexp.MustCompile(`(?i)IVA\s+Contenido`)}
	reMonto        = regexp.MustCompile(`-?\d{1,3}(?:\.\d{3})+,\d{2}|-?\d{1,3}(?:,\d{3})+\.\d{2}|-?\d+[.,]\d{2}\b|-?\d+\b`)
)

func reIVA(alic string) []*regexp.Regexp {
	return []*regexp.Regexp{regexp.MustCompile(`(?i)\bI\.?V\.?A\.?\s*(?:\(\s*)?` + alic + `(?:[,.]0+)?\s*%`)}
}

// importeEtiquetado busca la etiqueta y toma el primer monto después de ella en la misma línea.
func importeEtiquetado(lineas []string, res []*regexp.Regexp) (modelo.Importe, bool) {
	for _, re := range res {
		for _, l := range lineas {
			loc := re.FindStringIndex(l)
			if loc == nil {
				continue
			}
			resto := l[loc[1]:]
			// Saltar "%" de la etiqueta de IVA y separadores.
			resto = strings.TrimLeft(resto, " %:$.\t")
			if m := reMonto.FindString(resto); m != "" {
				if v, err := modelo.ParsearImporte(m); err == nil {
					return v, true
				}
			}
		}
	}
	return 0, false
}

func primerValor(lineas []string, re *regexp.Regexp) string {
	for _, l := range lineas {
		if m := re.FindStringSubmatch(l); m != nil {
			v := strings.TrimSpace(m[1])
			if v != "" && !strings.HasSuffix(v, ":") {
				return v
			}
		}
	}
	return ""
}

type cuitHallado struct {
	nro      string
	receptor bool
}

var reRotuloReceptor = regexp.MustCompile(`(?i)(receptor|cliente|señor(?:es)?|sr\.?\s*/?\s*es|comprador)`)

func cuitsEnOrden(lineas []string) []cuitHallado {
	var res []cuitHallado
	vistos := map[string]bool{}
	for _, l := range lineas {
		for _, m := range reCUIT.FindAllStringSubmatchIndex(l, -1) {
			nro := arca.SoloDigitos(l[m[2]:m[3]])
			if vistos[nro] {
				continue
			}
			vistos[nro] = true
			antes := l[:m[0]]
			res = append(res, cuitHallado{nro: nro, receptor: reRotuloReceptor.MatchString(antes)})
		}
	}
	return res
}

func claseDesdeTexto(t string) arca.Clase {
	u := strings.ToUpper(t)
	switch {
	case strings.Contains(u, "NOTA DE CREDITO") || strings.Contains(u, "NOTA DE CRÉDITO"):
		return arca.ClaseNotaCredito
	case strings.Contains(u, "NOTA DE DEBITO") || strings.Contains(u, "NOTA DE DÉBITO"):
		return arca.ClaseNotaDebito
	case strings.Contains(u, "FACTURA"):
		return arca.ClaseFactura
	case strings.Contains(u, "RECIBO"):
		return arca.ClaseRecibo
	}
	return ""
}

func fechaISO(s string) string {
	s = strings.ReplaceAll(s, "-", "/")
	p := strings.Split(s, "/")
	if len(p) != 3 {
		return s
	}
	return p[2] + "-" + p[1] + "-" + p[0]
}

func monedaCodigo(s string) string {
	u := strings.ToUpper(s)
	switch {
	case strings.HasPrefix(u, "DOL"), u == "USD":
		return "DOL"
	case strings.HasPrefix(u, "EUR"):
		return "060"
	case u == "ARS", u == "PES":
		return "PES"
	}
	return u
}
