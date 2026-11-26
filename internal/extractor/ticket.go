package extractor

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/rubro"
)

// Tickets de compra: tique y tique factura de controlador fiscal (supermercados, estaciones de
// servicio, farmacias, bares). No traen CAE ni QR de ARCA, el papel es angosto y casi siempre llegan
// como foto de celular, así que el texto viene del OCR con ruido: O por 0, espacios dentro de los
// números, "$" mal leído, "TOTAL" con puntos. El parser tolera eso, pero nunca completa un dato que
// no está escrito.

// PareceTicket dice si un texto es de un ticket de controlador fiscal y no de una factura
// electrónica: suma señales del papel térmico (TIQUE, P.V. Nro., Nro. T., hora, vuelto, consumidor
// final, caja) y descarta lo que trae CAE.
func PareceTicket(txt string) bool {
	u := strings.ToUpper(normalizar(txt))
	if reCAE.MatchString(u) || strings.Contains(u, "COMPROBANTE AUTORIZADO") || strings.Contains(u, "C.A.E.") {
		return false
	}
	p := 0
	if reTiquePalabra.MatchString(u) {
		p += 3
	}
	for _, s := range []struct {
		re   *regexp.Regexp
		peso int
	}{
		{reSenalPVNro, 2},
		{reSenalNroT, 2},
		{reSenalControlador, 2},
		{reSenalTicket, 1},
		{reSenalPago, 1},
		{reConsumidorFinal, 1},
		{reSenalHora, 1},
		{reSenalCaja, 1},
	} {
		if s.re.MatchString(u) {
			p += s.peso
		}
	}
	// El modelo de "Comprobantes en línea" de ARCA no es un ticket aunque diga "Consumidor Final".
	if strings.Contains(u, "PUNTO DE VENTA:") && strings.Contains(u, "COMP. NRO") {
		p -= 3
	}
	return p >= 3
}

var (
	reTiquePalabra     = regexp.MustCompile(`\bT\s?[I1L|]\s?[QO0]\s?U\s?[EÉ3]\b`)
	reSenalPVNro       = regexp.MustCompile(`\bP\s?\.\s?V\s?\.?\s*[NM]\s?(?:RO|°|O|º)`)
	reSenalNroT        = regexp.MustCompile(`\b[NM]\s?(?:RO|°|O)\s?[.,]?\s?T\s?[.,]?\s?:`)
	reSenalControlador = regexp.MustCompile(`CONTROLADOR\s+FISCAL|\bREG\.?\s*C\.?\s?F\b|\bC\.?\s?F\.?\s*:`)
	reSenalTicket      = regexp.MustCompile(`\bTICKET\b|NO\s+V[AÁ]LIDO\s+COMO\s+FACTURA`)
	reSenalPago        = regexp.MustCompile(`\bVUELTO\b|\bRECIB[I1L]\s*\(?\s*MOS|\bSU\s+PAGO\b|\bCAMBIO\b`)
	reConsumidorFinal  = regexp.MustCompile(`CONSUMIDOR\s+F[I1L][NM]A[LI1]|\bC\.\s?F\.(?:\s|$)`)
	reSenalHora        = regexp.MustCompile(`\b[HM]ORA\s*:?\s*\d{1,2}\s*[:.]\s*\d{2}`)
	reSenalCaja        = regexp.MustCompile(`\bCAJ(?:A|ERO)\b`)
)

// Etiquetas de un ticket. Todas sin distinguir mayúsculas y tolerantes a lo que suele confundir el
// OCR (0 por O, 1 o I por L, espacios y puntos de más).
var (
	reTkTiqueClase  = regexp.MustCompile(`(?i)T\s?[I1L|]\s?[QO0]\s?U\s?[EÉ3]\s*[-.]?\s*(FACTURA|NOTA\s+DE\s+CR[EÉ]DITO|NOTA\s+DE\s+D[EÉ]BITO)(?:\s*["“']?\s*([ABCM8€])(?:[^\pL\d]|$))?`)
	reTkFacturaSola = regexp.MustCompile(`(?im)^\W*(FACTURA|NOTA\s+DE\s+CR[EÉ]DITO|NOTA\s+DE\s+D[EÉ]BITO)\s*["“']?\s*([ABCM8])\b`)

	reTkPVNro = regexp.MustCompile(`(?i)\b([0-9OoIl]{4,5})\s?-\s?([0-9OoIl]{8})\b`)
	reTkPV    = regexp.MustCompile(`(?i)\bP\s?\.?\s?(?:V|DV|TO\.?\s*VTA)\s?\.?\s*(?:[NM]\s?(?:RO|°|O|º)\s*[.,]?)?\s*:?\s*([0-9OoIl]{1,5})\b`)
	reTkNro   = regexp.MustCompile(`(?i)\b[NM]\s?(?:RO|°|O|º)\s?[.,]?\s?T(?:IQUE|ICKET)?\s?[.,]?\s*:?\s*([0-9OoIl]{1,8})\b`)

	reTkFecha     = regexp.MustCompile(`(?i)FECHA\s*:?\s*([0-9OoIl]{1,2})\s?[/.-]\s?([0-9OoIl]{1,2})\s?[/.-]\s?([0-9OoIl]{4}|[0-9OoIl]{2})\b`)
	reTkFechaSola = regexp.MustCompile(`\b(\d{1,2})/(\d{1,2})/(\d{4}|\d{2})\b`)
	reTkNoEsFecha = regexp.MustCompile(`(?i)INICIO|ACTIVIDAD|VENC|VTO|DESDE|HASTA|NACIM`)
	reTkHora      = regexp.MustCompile(`(?i)\b[HM]ORA\s*:?\s*([0-2]?[0-9OoIl])\s?[:.]\s?([0-5][0-9Oo])(?:\s?[:.]\s?([0-5][0-9Oo]))?`)
	reTkHoraSola  = regexp.MustCompile(`\b([01]\d|2[0-3]):([0-5]\d)(?::([0-5]\d))?\b`)

	reTkCUIT     = regexp.MustCompile(`(?i)\bC\s?\.?\s?U\s?\.?\s?[I1LT|]\s?\.?\s?[TY]\s?\.?\s*(?:[NM]\s?(?:RO|°|O|º)\s*[.,]?)?\s*:?\s*([0-9OoIl]{2}\s?-?\s?(?:[0-9OoIl]\s?){7}[0-9OoIl]\s?[-.]?\s?[0-9OoIl])\b`)
	reTkCond     = regexp.MustCompile(`(?i)\b(RESPONSABLE\s+INSCRIPTO|RESPONSABLE\s+MONOTRIBUTO|MONOTRIBUTISTA|MONOTRIBUTO|SUJETO\s+EXENTO|IVA\s+EXENTO|EXENTO)\b`)
	reTkLegal    = regexp.MustCompile(`(?i)\b(S\.?\s?R\.?\s?L|S\.?\s?A\.?\s?S|S\.?\s?A\.?\s?I\.?\s?C|S\.?\s?C\.?\s?A|S\.?\s?A|S\.?\s?H|SOCIEDAD\s+AN[OÓ]NIMA|COOP(?:ERATIVA)?)\.?\s*$`)
	reTkNoNombre = regexp.MustCompile(`(?i)C\s?\.?\s?U\s?\.?\s?[I1L]\s?\.?\s?T|\bIVA\b|RESPONSABLE|MONOTRIBUT|EXENTO|ING\.?\s*BRUTOS|I\.?\s?I\.?\s?B\.?\s?B|INICIO|\bTE?L\.?\s*:|TEL[EÉ]FONO|WWW\.|@|\bAV\.|\bAVDA|\bCALLE\b|\bRUTA\b|\bKM\b|\bN[°º]\s*\d|\d{3,}|^\W*-{3,}`)

	reTkTotal     = regexp.MustCompile(`(?i)^\W*T\s?\.?\s?[O0]\s?\.?\s?T\s?\.?\s?A\s?\.?\s?[LI1]\b`)
	reTkTotalExcl = regexp.MustCompile(`(?i)SUB|PAGAD|RECIB|VUELTO|ITEM|ART[IÍ]C|CANT|\bIVA\b|UNIDADES|BULTOS`)
	reTkSubtotal  = regexp.MustCompile(`(?i)^\W*SUB\s?-?\s?T\s?[O0]\s?T\s?A\s?[LI1]`)
	reTkNeto      = regexp.MustCompile(`(?i)\b(?:IMPORTE\s+)?NET[O0](?:\s+GRAVAD[O0])?\b`)
	reTkNoGravado = regexp.MustCompile(`(?i)\bN[O0]\s+GRAVAD[O0]`)
	reTkExento    = regexp.MustCompile(`(?i)^\W*(?:IMPORTE\s+|TOTAL\s+)?EXENT[O0]\b`)
	reTkIVACont   = regexp.MustCompile(`(?i)\bIVA\s+CONTE[NM][I1L]D[O0]`)
	reTkOtros     = regexp.MustCompile(`(?i)IMP(?:UESTOS?)?\.?\s+INTERNOS?|PERCEPCI[OÓ]N|OTROS\s+TRIBUTOS|\bI\.?\s?T\.?\s?C\.?\b`)
	reTkOtrosExcl = regexp.MustCompile(`(?i)INDIRECT|CONTE[NM]IDO`)
	reTkIVAExcl   = regexp.MustCompile(`(?i)RESPONSABLE|INSCRIPT|CONTE[NM]IDO|EXENT|MONOTRIB|CONSUMIDOR`)
	reTkPago      = regexp.MustCompile(`(?i)RECIB|VUELTO|SU\s+PAGO|EFECTIVO|TARJETA|CAMBIO|D[EÉ]BITO|CR[EÉ]DITO\s+(?:VISA|MASTER|AMEX)|PROPINA|DESCUENTO|BONIF|AHORRO`)

	reTkMonto = regexp.MustCompile(`\d{1,3}(?:\.\d{3})+,\d{2}\b|\d{1,3}(?:,\d{3})+\.\d{2}\b|\d+,\d{2}\b|\d+\.\d{2}\b`)
)

// tkIVA arma la etiqueta "IVA 21%" tolerante: "I.V.A. 21,00 %", "IVA 21%", "IVA (21.00)".
func tkIVA(alic string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\bI\s?\.?\s?V\s?\.?\s?A\s?\.?\s*:?\s*\(?\s*` + alic + `(?:[,.]0{1,2})?\s*\)?\s*(?:%|\b)`)
}

var (
	reTkIVA21  = tkIVA(`21`)
	reTkIVA105 = tkIVA(`10[,.]50?`)
	reTkIVA27  = tkIVA(`27`)
	reTkIVA5   = tkIVA(`5`)
	reTkIVA25  = tkIVA(`2[,.]50?`)
)

// digitosOCR cambia por dígitos las letras que el OCR confunde (O y o por 0; I, l y | por 1). Se usa
// solo en lo que tiene que ser un número.
func digitosOCR(s string) string {
	return strings.NewReplacer("O", "0", "o", "0", "I", "1", "l", "1", "|", "1").Replace(s)
}

var (
	reTkEspacioSep    = regexp.MustCompile(`([.,])\s+(\d)`)
	reTkEspacioAntes  = regexp.MustCompile(`(\d)\s+([.,]\d)`)
	reTkEspacioMiles  = regexp.MustCompile(`(\d)\s+(\d{3}[.,])`)
	reTkLetraEnNumero = regexp.MustCompile(`([\d.,])([OoIl|])|([OoIl|])([\d.,])`)
)

// montosOCR devuelve los importes (con dos decimales) de un pedazo de línea, arreglando el ruido
// típico del OCR: letras dentro de los números y espacios después de un separador o entre miles.
func montosOCR(s string) []modelo.Importe {
	for i := 0; i < 3; i++ {
		s = reTkLetraEnNumero.ReplaceAllStringFunc(s, digitosOCR)
	}
	s = reTkEspacioSep.ReplaceAllString(s, "$1$2")
	s = reTkEspacioAntes.ReplaceAllString(s, "$1$2")
	s = reTkEspacioMiles.ReplaceAllString(s, "$1$2")
	var res []modelo.Importe
	for _, m := range reTkMonto.FindAllString(s, -1) {
		if v, err := modelo.ParsearImporte(m); err == nil {
			res = append(res, v)
		}
	}
	return res
}

// soloMonto dice si una línea no tiene más que un importe (el OCR a veces separa el importe
// alineado a la derecha de su etiqueta).
var reTkSoloMonto = regexp.MustCompile(`^[\s$S§:.]*[\d.,\sOo]+$`)

// montoTicket busca la primera línea con la etiqueta y toma el último importe que hay después de
// ella (en un ticket el importe va a la derecha). Si la línea no tiene importe y la siguiente es
// solo un importe, toma ese.
func montoTicket(lineas []string, re, excluir *regexp.Regexp) (modelo.Importe, bool) {
	for i, l := range lineas {
		loc := re.FindStringIndex(l)
		if loc == nil || (excluir != nil && excluir.MatchString(l)) {
			continue
		}
		if ms := montosOCR(l[loc[1]:]); len(ms) > 0 {
			return ms[len(ms)-1], true
		}
		if i+1 < len(lineas) && reTkSoloMonto.MatchString(lineas[i+1]) {
			if ms := montosOCR(lineas[i+1]); len(ms) == 1 {
				return ms[0], true
			}
		}
	}
	return 0, false
}

// ParsearTicket saca los campos de un ticket de controlador fiscal. Marca cada campo con el origen
// (texto u ocr); el tipo, si no está impreso, queda en 83 (Tique) como cálculo.
func ParsearTicket(txt string, origen modelo.Origen) modelo.Comprobante {
	c := modelo.Comprobante{EsTique: true}
	t := normalizar(txt)
	var lineas []string
	for _, l := range strings.Split(t, "\n") {
		if strings.TrimSpace(l) != "" {
			lineas = append(lineas, l)
		}
	}
	t = strings.Join(lineas, "\n")
	set := func(campo string) { c.Marcar(campo, origen) }

	// --- Tipo: TIQUE FACTURA B, TIQUE NOTA DE CREDITO A, TIQUE ---
	clase, letra, leido := arca.ClaseFactura, "", false
	if m := reTkTiqueClase.FindStringSubmatch(t); m != nil {
		clase, letra, leido = claseTicket(m[1]), letraOCR(m[2]), true
	} else if m := reTkFacturaSola.FindStringSubmatch(t); m != nil {
		clase, letra, leido = claseTicket(m[1]), letraOCR(m[2]), true
	} else if reTiquePalabra.MatchString(strings.ToUpper(t)) {
		leido = true
	}
	if n := arca.TiqueDesde(clase, letra); n > 0 {
		c.Tipo = n
		if leido {
			set("tipo")
		} else {
			c.Marcar("tipo", modelo.OrigenCalculo)
		}
		if tp, _ := arca.Tipo(n); tp.Letra != "" {
			c.Letra = tp.Letra
			set("letra")
		}
	}

	// --- Punto de venta y número ---
	if m := reTkPVNro.FindStringSubmatch(t); m != nil {
		c.PuntoVenta, _ = strconv.Atoi(digitosOCR(m[1]))
		c.Numero, _ = strconv.ParseInt(digitosOCR(m[2]), 10, 64)
	}
	if c.PuntoVenta == 0 {
		if m := reTkPV.FindStringSubmatch(t); m != nil {
			c.PuntoVenta, _ = strconv.Atoi(digitosOCR(m[1]))
		}
	}
	if c.Numero == 0 {
		if m := reTkNro.FindStringSubmatch(t); m != nil {
			c.Numero, _ = strconv.ParseInt(digitosOCR(m[1]), 10, 64)
		}
	}
	if c.PuntoVenta > 0 {
		set("punto_venta")
	}
	if c.Numero > 0 {
		set("numero")
	}

	// --- Fecha y hora ---
	lineaFecha := -1
	for i, l := range lineas {
		if m := reTkFecha.FindStringSubmatch(l); m != nil {
			if f := fechaTicket(digitosOCR(m[1]), digitosOCR(m[2]), digitosOCR(m[3])); f != "" {
				c.Fecha, lineaFecha = f, i
				break
			}
		}
	}
	if c.Fecha == "" {
		for i, l := range lineas {
			if reTkNoEsFecha.MatchString(l) {
				continue
			}
			if m := reTkFechaSola.FindStringSubmatch(l); m != nil {
				if f := fechaTicket(m[1], m[2], m[3]); f != "" {
					c.Fecha, lineaFecha = f, i
					break
				}
			}
		}
	}
	if c.Fecha != "" {
		set("fecha")
	}
	if m := reTkHora.FindStringSubmatch(t); m != nil {
		c.Hora = horaTicket(digitosOCR(m[1]), digitosOCR(m[2]), digitosOCR(m[3]))
	} else if lineaFecha >= 0 {
		if m := reTkHoraSola.FindStringSubmatch(lineas[lineaFecha]); m != nil {
			c.Hora = horaTicket(m[1], m[2], m[3])
		}
	}
	if c.Hora != "" {
		set("hora")
	}

	// --- CUIT del emisor (el primero) y del receptor (otro, en un tique factura A) ---
	lineaCUIT, lineaReceptor := -1, len(lineas)
	for i, l := range lineas {
		for _, m := range reTkCUIT.FindAllStringSubmatch(l, -1) {
			nro := arca.SoloDigitos(digitosOCR(m[1]))
			if len(nro) != 11 {
				continue
			}
			switch {
			case c.CUITEmisor == "":
				c.CUITEmisor, lineaCUIT = nro, i
				set("cuit_emisor")
			case nro != c.CUITEmisor && c.DocNroReceptor == "":
				c.DocNroReceptor, c.DocTipoReceptor, lineaReceptor = nro, 80, i
				set("doc_nro_receptor")
				set("doc_tipo_receptor")
			}
		}
	}
	if c.DocNroReceptor == "" {
		if m := reDNI.FindStringSubmatch(t); m != nil {
			c.DocNroReceptor, c.DocTipoReceptor = m[1], 96
			set("doc_nro_receptor")
			set("doc_tipo_receptor")
		} else if reConsumidorFinal.MatchString(strings.ToUpper(t)) {
			c.DocTipoReceptor = 99
			set("doc_tipo_receptor")
		}
	}

	// --- Condición frente al IVA del emisor: la primera, antes de los datos del receptor ---
	for i, l := range lineas {
		if i >= lineaReceptor {
			break
		}
		if m := reTkCond.FindStringSubmatch(l); m != nil {
			c.CondicionIVAEmisor = condicionTicket(m[1])
			set("condicion_iva_emisor")
			break
		}
	}

	// --- Razón social: en el encabezado, antes del CUIT ---
	fin := lineaCUIT
	if fin < 0 {
		fin = min(len(lineas), 5)
	}
	encabezado := lineas[:max(fin, 0)]
	if r := razonTicket(encabezado); r != "" {
		c.RazonSocialEmisor = r
		set("razon_social_emisor")
	}
	// En un tique factura A el nombre del receptor suele ir justo antes o después de su CUIT.
	if c.DocTipoReceptor == 80 && lineaReceptor < len(lineas) {
		for _, k := range []int{lineaReceptor - 1, lineaReceptor + 1} {
			if k > lineaCUIT && k >= 0 && k < len(lineas) && !reTkNoNombre.MatchString(lineas[k]) && esNombre(lineas[k]) {
				c.RazonSocialReceptor = strings.TrimSpace(lineas[k])
				set("razon_social_receptor")
				break
			}
		}
	}

	// --- Importes ---
	montos := []struct {
		campo   string
		dst     *modelo.Importe
		re      *regexp.Regexp
		excluir *regexp.Regexp
	}{
		{"total", &c.Total, reTkTotal, reTkTotalExcl},
		{"subtotal", &c.Subtotal, reTkSubtotal, nil},
		{"no_gravado", &c.NoGravado, reTkNoGravado, nil},
		{"neto_gravado", &c.NetoGravado, reTkNeto, reTkNoGravado},
		{"exento", &c.Exento, reTkExento, nil},
		{"iva_21", &c.IVA21, reTkIVA21, reTkIVAExcl},
		{"iva_10_5", &c.IVA105, reTkIVA105, reTkIVAExcl},
		{"iva_27", &c.IVA27, reTkIVA27, reTkIVAExcl},
		{"iva_5", &c.IVA5, reTkIVA5, reTkIVAExcl},
		{"iva_2_5", &c.IVA25, reTkIVA25, reTkIVAExcl},
		{"iva_contenido", &c.IVAContenido, reTkIVACont, nil},
		{"otros_tributos", &c.OtrosTributos, reTkOtros, reTkOtrosExcl},
	}
	for _, m := range montos {
		if v, ok := montoTicket(lineas, m.re, m.excluir); ok {
			*m.dst = v
			set(m.campo)
		}
	}
	// Sin la palabra TOTAL legible: si hay una línea con TOTAL mal leída no se adivina nada.

	// --- Rubro (capa 1: palabras clave) ---
	enc := strings.Join(encabezado, "\n") + "\n" + c.RazonSocialEmisor
	cuerpo := strings.Join(lineas[min(max(fin, 0), len(lineas)):], "\n")
	if r := rubro.DesdeTexto(enc, cuerpo); r != "" {
		c.Rubro = r
		set("rubro")
	}
	return c
}

func claseTicket(s string) arca.Clase {
	u := strings.ToUpper(s)
	switch {
	case strings.Contains(u, "CR"):
		return arca.ClaseNotaCredito
	case strings.Contains(u, "BITO"):
		return arca.ClaseNotaDebito
	}
	return arca.ClaseFactura
}

// letraOCR: el OCR lee a veces la B como 8 y la C como €.
func letraOCR(s string) string {
	switch s {
	case "8":
		return "B"
	case "€":
		return "C"
	}
	return strings.ToUpper(s)
}

func fechaTicket(d, m, a string) string {
	di, err1 := strconv.Atoi(d)
	mi, err2 := strconv.Atoi(m)
	ai, err3 := strconv.Atoi(a)
	if err1 != nil || err2 != nil || err3 != nil || di < 1 || di > 31 || mi < 1 || mi > 12 {
		return ""
	}
	if len(a) == 2 {
		ai += 2000 // los controladores fiscales imprimen el año con dos dígitos: 21/11/26
	} else if len(a) != 4 {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", ai, mi, di)
}

func horaTicket(h, m, s string) string {
	hi, err1 := strconv.Atoi(h)
	mi, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hi > 23 || mi > 59 {
		return ""
	}
	if si, err := strconv.Atoi(s); err == nil && s != "" && si <= 59 {
		return fmt.Sprintf("%02d:%02d:%02d", hi, mi, si)
	}
	return fmt.Sprintf("%02d:%02d", hi, mi)
}

func condicionTicket(s string) string {
	u := strings.ToUpper(s)
	switch {
	case strings.Contains(u, "INSCRIPTO"):
		return "IVA Responsable Inscripto"
	case strings.Contains(u, "MONOTRIBUT"):
		return "Responsable Monotributo"
	case strings.Contains(u, "EXENTO"):
		return "IVA Sujeto Exento"
	}
	return s
}

// esNombre: una línea que tiene mayormente letras.
func esNombre(l string) bool {
	letras, total := 0, 0
	for _, r := range l {
		if r == ' ' {
			continue
		}
		total++
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || strings.ContainsRune("ÁÉÍÓÚÑáéíóúñ", r) {
			letras++
		}
	}
	return total >= 3 && letras*10 >= total*7
}

// razonTicket elige la razón social entre las líneas del encabezado: primero una con forma
// societaria al final (S.A., S.R.L.), después una que empiece con "de" (el titular de un nombre de
// fantasía: "FARMACIA DEL PUEBLO / de GONZALEZ ANA MARIA") y si no, la primera que parezca un nombre.
func razonTicket(enc []string) string {
	var cands []string
	for _, l := range enc {
		l = strings.TrimSpace(l)
		if !reTkNoNombre.MatchString(l) && esNombre(l) {
			cands = append(cands, l)
		}
	}
	for _, l := range cands {
		if reTkLegal.MatchString(l) {
			return l
		}
	}
	for _, l := range cands {
		if len(l) > 3 && strings.EqualFold(l[:3], "de ") {
			return strings.TrimSpace(l[3:])
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

// CamposHallados cuenta los datos que salieron del documento (no los calculados), con premio si el
// CUIT es válido y si las sumas cierran. Sirve para elegir la mejor de dos lecturas del OCR.
func CamposHallados(c *modelo.Comprobante) int {
	n := 0
	for _, o := range c.Origenes {
		if o != modelo.OrigenCalculo {
			n++
		}
	}
	if arca.CUITValido(c.CUITEmisor) {
		n += 2
	}
	if c.Total != 0 {
		n++
		if s := c.SumaComponentes(); s != 0 && s-c.Total <= 2 && c.Total-s <= 2 {
			n += 2
		}
	}
	return n
}
