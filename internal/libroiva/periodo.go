package libroiva

import (
	"strconv"
	"time"
)

var meses = []string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"}

// NombreMes escribe un período AAAA-MM para una persona: «octubre de 2026».
func NombreMes(p string) string {
	t, err := time.Parse("2006-01", p)
	if err != nil {
		return p
	}
	return meses[t.Month()-1] + " de " + strconv.Itoa(t.Year())
}

// MesSiguiente devuelve el período que sigue a p (AAAA-MM). Vacío si p no se entiende.
func MesSiguiente(p string) string {
	t, err := time.Parse("2006-01", p)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 1, 0).Format("2006-01")
}

// PrimerPeriodoAbierto es el primer período posterior a mes que no está presentado. Los
// comprobantes que llegan después de presentar su mes van ahí (no se puede registrar un comprobante
// en un período ya presentado sin rectificarlo). Si mes no está presentado, es el mismo mes.
func PrimerPeriodoAbierto(mes string, presentados map[string]bool) string {
	if !presentados[mes] {
		return mes
	}
	p := mes
	for range 1200 { // cien años: nunca se llega
		p = MesSiguiente(p)
		if p == "" || !presentados[p] {
			return p
		}
	}
	return p
}

// LibroDe dice en qué libro va un comprobante de compra guardado en Factorum: el de importaciones
// (los despachos, tipo 066) o el de compras.
func LibroDe(tipo int) string {
	if tipo == 66 {
		return LibroImportaciones
	}
	return LibroCompras
}
