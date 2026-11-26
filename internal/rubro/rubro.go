// Package rubro clasifica un gasto (supermercado, combustible, farmacia...) en tres capas, de menor
// a mayor precedencia:
//
//  1. Palabras clave del nombre del comercio y del texto del comprobante (DesdeTexto).
//  2. La actividad principal del emisor en el padrón de ARCA, vía Arcanum (DesdeActividad).
//  3. La corrección de una persona, que Factorum recuerda por CUIT del emisor.
//
// Las dos primeras son funciones puras; la tercera vive en el store.
package rubro

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	Supermercado = "supermercado"
	Combustible  = "combustible"
	Farmacia     = "farmacia"
	Gastronomia  = "gastronomia"
	Transporte   = "transporte"
	Servicios    = "servicios"
	Otros        = "otros"
)

// Todos los rubros válidos, en el orden en que se desempatan las palabras clave.
var Todos = []string{Combustible, Farmacia, Gastronomia, Supermercado, Transporte, Servicios, Otros}

// Valido dice si r es uno de los rubros que conoce Factorum.
func Valido(r string) bool {
	for _, x := range Todos {
		if x == r {
			return true
		}
	}
	return false
}

// Palabras clave por rubro. Con "*" al final, es un prefijo ("FARMACI*" toma FARMACIA y
// FARMACIAS); si no, la palabra (o frase) tiene que aparecer entera.
var claves = []struct {
	rubro    string
	palabras []string
}{
	{Combustible, []string{"YPF", "SHELL", "AXION", "PUMA", "GULF", "ESTACION DE SERVICIO*", "ESTACIONES DE SERVICIO*", "ESTACION SERVICIO*",
		"NAFTA*", "GASOIL", "GAS OIL", "DIESEL", "EURODIESEL", "INFINIA", "V POWER", "GNC", "COMBUSTIBLE*", "LUBRICENTRO*"}},
	{Farmacia, []string{"FARMACI*", "FARMACITY", "DROGUERI*", "MEDICAMENTO*"}},
	{Gastronomia, []string{"RESTAURANT*", "RESTO", "RESTOBAR", "PARRILLA*", "BAR", "BODEGON", "CAFE", "CAFETERIA*", "CONFITERIA*", "PIZZERIA*",
		"HELADERIA*", "CERVECERIA*", "SANDWICHERIA*", "EMPANADA*", "CUBIERTO*", "MCDONALD*", "MC DONALD*", "BURGER*", "COMEDOR*", "ROTISERIA*", "MOZO"}},
	{Supermercado, []string{"SUPERMERCADO*", "HIPERMERCADO*", "MINIMERCADO*", "AUTOSERVICIO*", "COTO", "CARREFOUR", "JUMBO", "DISCO", "VEA",
		"LA ANONIMA", "CHANGOMAS", "WALMART", "MAYORISTA*", "MAKRO", "VITAL", "YAGUAR", "DIARCO", "MAXICONSUMO", "ALMACEN*", "VERDULERIA*",
		"CARNICERIA*", "FIAMBRERIA*", "DIETETICA*"}},
	{Transporte, []string{"PEAJE*", "AUTOPISTA*", "ESTACIONAMIENTO*", "PARKING", "COCHERA*", "TAXI*", "REMIS*", "TRANSPORTE*", "PASAJE*",
		"OMNIBUS", "AEROLINEA*", "SUBTE", "TERMINAL DE OMNIBUS"}},
	{Servicios, []string{"LAVADERO*", "TINTORERIA*", "PELUQUERIA*", "CERRAJERIA*", "FOTOCOPIA*", "TALLER*", "GOMERIA*", "SERVICIO TECNICO",
		"REPARACION*", "VETERINARIA*", "MANO DE OBRA", "LAVADO"}},
}

// Normalizar pasa a mayúsculas, saca acentos y deja solo letras y dígitos separados por un espacio,
// con un espacio al principio y al final (así " BAR " no aparece dentro de "BARRA").
func Normalizar(s string) string {
	var b strings.Builder
	b.WriteByte(' ')
	esp := true
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		r = unicode.ToUpper(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			esp = false
		} else if !esp {
			b.WriteByte(' ')
			esp = true
		}
	}
	if !esp {
		b.WriteByte(' ')
	}
	return b.String()
}

func aparece(texto, palabra string) bool {
	if strings.HasSuffix(palabra, "*") {
		return strings.Contains(texto, " "+strings.TrimSuffix(palabra, "*"))
	}
	return strings.Contains(texto, " "+palabra+" ")
}

// DesdeTexto es la capa 1: busca palabras clave en el encabezado (nombre del comercio y razón
// social, que pesan 3) y en el resto del texto (pesa 1). Gana el rubro con más peso; en un empate,
// el que va primero en Todos. Devuelve "" si no encontró nada: un rubro no se inventa.
func DesdeTexto(encabezado, cuerpo string) string {
	enc, cue := Normalizar(encabezado), Normalizar(cuerpo)
	mejor, peso := "", 0
	for _, c := range claves {
		p := 0
		for _, w := range c.palabras {
			if aparece(enc, w) {
				p += 3
			}
			if aparece(cue, w) {
				p++
			}
		}
		if p > peso {
			mejor, peso = c.rubro, p
		}
	}
	return mejor
}

// DesdeActividad es la capa 2: la actividad principal del padrón de ARCA (código CLAE de seis
// dígitos, F.883, y su descripción). Primero el código; si no alcanza, la descripción. Devuelve ""
// si no hay con qué decidir.
func DesdeActividad(codigo, descripcion string) string {
	cod := ""
	for _, r := range codigo {
		if r >= '0' && r <= '9' {
			cod += string(r)
		}
	}
	if len(cod) >= 2 {
		pre := func(ps ...string) bool {
			for _, p := range ps {
				if strings.HasPrefix(cod, p) {
					return true
				}
			}
			return false
		}
		switch {
		case pre("4730", "4661"): // venta de combustibles (al por menor y por mayor)
			return Combustible
		case pre("47731"): // productos farmacéuticos y de herboristería
			return Farmacia
		case pre("4711", "4721", "4722"): // supermercados, almacenes, alimentos y bebidas
			return Supermercado
		case pre("56"): // servicios de comidas y bebidas
			return Gastronomia
		case pre("49", "50", "51", "52"): // transporte y sus servicios (peajes, estacionamientos)
			return Transporte
		}
	}
	if r := desdeDescripcion(descripcion); r != "" {
		return r
	}
	if len(cod) >= 2 {
		div := cod[:2]
		switch {
		case div == "33", div >= "35" && div <= "39", div == "53", div == "55", div >= "58" && div <= "99",
			strings.HasPrefix(cod, "452"), strings.HasPrefix(cod, "4540"):
			return Servicios
		}
		return Otros
	}
	return ""
}

func desdeDescripcion(d string) string {
	n := Normalizar(d)
	if strings.TrimSpace(n) == "" {
		return ""
	}
	switch {
	case aparece(n, "COMBUSTIBLE*"), aparece(n, "ESTACION DE SERVICIO*"), aparece(n, "ESTACIONES DE SERVICIO*"):
		return Combustible
	case aparece(n, "FARMAC*"):
		return Farmacia
	case aparece(n, "SUPERMERCADO*"), aparece(n, "HIPERMERCADO*"), aparece(n, "AUTOSERVICIO*"), aparece(n, "MINIMERCADO*"):
		return Supermercado
	case aparece(n, "RESTAURANT*"), aparece(n, "BAR"), aparece(n, "BARES"), aparece(n, "CONFITERIA*"), aparece(n, "COMIDAS"):
		return Gastronomia
	case aparece(n, "TRANSPORTE*"), aparece(n, "PEAJE*"), aparece(n, "ESTACIONAMIENTO*"):
		return Transporte
	case aparece(n, "SERVICIO*"):
		return Servicios
	}
	return ""
}
