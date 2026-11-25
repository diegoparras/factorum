package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/diegoparras/contadorfacturas/internal/modelo"
)

func filasPrueba() []Fila {
	return []Fila{
		{ID: 1, Archivo: "a.pdf", Estado: modelo.EstadoOK, C: modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1, Fecha: "2026-11-01", CUITEmisor: "30711111111",
			NetoGravado: 10000000, IVA21: 2100000, Total: 12100000, Moneda: "PES", Cotizacion: 1}},
		{ID: 2, Archivo: "nc.pdf", Estado: modelo.EstadoRevisar, Hallazgos: []modelo.Hallazgo{{Nivel: modelo.NivelAviso, Mensaje: "ojo"}},
			C: modelo.Comprobante{Tipo: 3, Letra: "A", PuntoVenta: 2, Numero: 2, NetoGravado: 1000000, IVA21: 210000, Total: 1210000, Moneda: "PES", Cotizacion: 1}},
		{ID: 3, Archivo: "usd.pdf", Estado: modelo.EstadoRevisar, C: modelo.Comprobante{Tipo: 1, Total: 121000, Moneda: "DOL", Cotizacion: 1000}},
	}
}

func TestResumen(t *testing.T) {
	r := Resumir(filasPrueba())
	if r.Cantidad != 3 || r.NetoGravado != 9000000 || r.IVA21 != 1890000 || r.Total != 10890000 || r.MonedaExtranjera != 1 {
		t.Fatalf("%+v", r)
	}
}

func TestExcelYCSV(t *testing.T) {
	var x bytes.Buffer
	if err := Excel(&x, filasPrueba()); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(x.Bytes(), []byte("PK")) {
		t.Fatal("no es un xlsx")
	}
	var c bytes.Buffer
	if err := CSV(&c, filasPrueba()); err != nil {
		t.Fatal(err)
	}
	s := c.String()
	if !strings.HasPrefix(s, "\xef\xbb\xbfFecha;") || !strings.Contains(s, ";-12100,00;") || !strings.Contains(s, "30-71111111-1") {
		t.Fatalf("%s", s)
	}
}
