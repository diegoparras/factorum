package arca

import (
	"regexp"
	"strings"
)

// El Libro IVA Digital pide el número de despacho de importación en 16 posiciones alfanuméricas
// (Anexo I, «Despacho de importación», tipo de dato 3) y no dice más. El número de una destinación
// del Sistema Informático MALVINA (SIM) tiene esa forma: año (2 dígitos), aduana (3 dígitos), tipo
// de destinación (4: dos letras y dos dígitos, por ejemplo IC04), número (6 dígitos) y un carácter
// de control (letra o dígito). Esa estructura no está en el diseño de ARCA: Factorum exige los 16
// caracteres y solo avisa si no tiene esa forma. Ver docs/libro-iva-digital.md.

var (
	reDespacho     = regexp.MustCompile(`^[0-9A-Z]{16}$`)
	reDespachoSIM  = regexp.MustCompile(`^(\d{2})(\d{3})([A-Z]{2}\d{2})(\d{6})([0-9A-Z])$`)
	sepDespacho    = strings.NewReplacer(" ", "", "-", "", ".", "", "/", "", "\t", "")
	reDespachoText = regexp.MustCompile(`\b(\d{2})[ -]?(\d{3})[ -]?([A-Z]{2}\d{2})[ -]?(\d{6})[ -]?([0-9A-Z])\b`)
)

// NormalizarDespacho saca blancos, guiones, puntos y barras y pasa a mayúsculas.
func NormalizarDespacho(s string) string {
	return strings.ToUpper(sepDespacho.Replace(strings.TrimSpace(s)))
}

// DespachoValido dice si el número (ya normalizado) tiene los 16 caracteres alfanuméricos que
// pide el diseño de registro.
func DespachoValido(s string) bool { return reDespacho.MatchString(s) }

// PartesDespacho separa un número con la estructura usual del SIM: año, aduana, tipo de destinación,
// número y carácter de control. ok=false si no tiene esa forma.
func PartesDespacho(s string) (anio, aduana, destinacion, numero, control string, ok bool) {
	m := reDespachoSIM.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", "", "", false
	}
	return m[1], m[2], m[3], m[4], m[5], true
}

// DespachosEnTexto devuelve los números con la estructura del SIM que aparecen en un texto, sin
// repetir y normalizados, en el orden en que aparecen.
func DespachosEnTexto(t string) []string {
	var res []string
	vistos := map[string]bool{}
	for _, m := range reDespachoText.FindAllStringSubmatch(strings.ToUpper(t), -1) {
		n := m[1] + m[2] + m[3] + m[4] + m[5]
		if !vistos[n] {
			vistos[n] = true
			res = append(res, n)
		}
	}
	return res
}
