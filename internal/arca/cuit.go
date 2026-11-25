// Package arca tiene lo que es de ARCA y no cambia: CUIT, códigos de comprobante y el QR de la RG 4892.
package arca

import "strings"

// SoloDigitos limpia guiones, puntos y espacios.
func SoloDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// CUITValido controla largo, prefijo y dígito verificador (módulo 11).
func CUITValido(s string) bool {
	c := SoloDigitos(s)
	if len(c) != 11 {
		return false
	}
	switch c[:2] {
	case "20", "23", "24", "25", "26", "27", "30", "33", "34":
	default:
		return false
	}
	pesos := [10]int{5, 4, 3, 2, 7, 6, 5, 4, 3, 2}
	suma := 0
	for i, p := range pesos {
		suma += int(c[i]-'0') * p
	}
	dv := 11 - suma%11
	switch dv {
	case 11:
		dv = 0
	case 10:
		// Con resto 1 ARCA asigna otro prefijo (23/33): un CUIT con DV 10 no existe.
		return false
	}
	return int(c[10]-'0') == dv
}

// FormatearCUIT devuelve 30-71111111-1. Si no tiene 11 dígitos, lo devuelve como vino.
func FormatearCUIT(s string) string {
	c := SoloDigitos(s)
	if len(c) != 11 {
		return s
	}
	return c[:2] + "-" + c[2:10] + "-" + c[10:]
}
