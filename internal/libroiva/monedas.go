package libroiva

import (
	"strconv"
	"strings"
)

// monedasNumericas son los códigos numéricos de la tabla «Código de Moneda» de las Tablas del
// Sistema del Libro IVA Digital (arca.gob.ar/libro-iva-digital/documentos/Libro-IVA-Digital-Tablas-del-Sistema.pdf).
// Además están PES (pesos) y DOL (dólar estadounidense).
var monedasNumericas = map[int]string{
	2: "Dólar libre EEUU", 7: "Florines holandeses", 9: "Franco suizo", 10: "Pesos mejicanos", 11: "Pesos uruguayos",
	12: "Real", 14: "Coronas danesas", 15: "Coronas noruegas", 16: "Coronas suecas", 18: "Dólar canadiense", 19: "Yens",
	21: "Libra esterlina", 23: "Bolívar venezolano", 24: "Corona checa", 25: "Dinar yugoslavo", 26: "Dólar australiano",
	27: "Dracma griego", 28: "Florín Antillas Holandesas", 29: "Guaraní", 30: "Shekel Israel", 31: "Peso boliviano",
	32: "Peso colombiano", 33: "Peso chileno", 34: "Rand sudafricano", 35: "Nuevo sol peruano", 36: "Sucre ecuatoriano",
	39: "Nuevo lev búlgaro", 40: "Lei rumano", 41: "Derechos especiales de giro", 42: "Peso dominicano",
	43: "Balboas panameñas", 44: "Córdoba nicaragüense", 45: "Dirham marroquí", 46: "Libra egipcia", 47: "Riyal saudita",
	49: "Gramos de oro fino", 51: "Dólar de Hong Kong", 52: "Dólar de Singapur", 53: "Dólar de Jamaica", 54: "Dólar de Taiwán",
	55: "Quetzal guatemalteco", 56: "Forint Hungría", 57: "Baht Tailandia", 59: "Dinar kuwaití", 60: "Euro",
	61: "Zloty polaco", 62: "Rupia hindú", 63: "Lempira hondureña", 64: "Yuan Rep. Pop. China",
}

// isoAMoneda lleva los códigos ISO 4217 que a veces se leen en un comprobante al código de ARCA.
var isoAMoneda = map[string]string{
	"ARS": "PES", "USD": "DOL", "EUR": "060", "BRL": "012", "UYU": "011", "CLP": "033", "GBP": "021",
	"JPY": "019", "CHF": "009", "CNY": "064", "CAD": "018", "MXN": "010", "PYG": "029", "BOB": "031",
	"COP": "032", "PEN": "035", "AUD": "026",
}

// CodigoMoneda devuelve el código de moneda para el campo 17 (3 posiciones) y si se reconoce.
// Vacío es PES. Los códigos numéricos van con ceros a la izquierda ("060"), como los devuelve el
// web service de facturación electrónica: la tabla del Libro IVA Digital los escribe sin ceros
// («60») y el diseño no dice cómo completarlos (ver docs/libro-iva-digital.md, «No confirmado»).
func CodigoMoneda(m string) (string, bool) {
	s := strings.ToUpper(strings.TrimSpace(m))
	switch s {
	case "", "PES", "$", "PESOS":
		return "PES", true
	case "DOL", "U$S", "US$", "U$D":
		return "DOL", true
	}
	if x, ok := isoAMoneda[s]; ok {
		return x, true
	}
	if n, err := strconv.Atoi(s); err == nil {
		if _, ok := monedasNumericas[n]; ok {
			r := strconv.Itoa(n)
			return strings.Repeat("0", 3-len(r)) + r, true
		}
	}
	return s, false
}
