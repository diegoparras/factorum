package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/xuri/excelize/v2"
)

func filasConTickets() []Fila {
	f := filasPrueba()
	return append(f,
		Fila{ID: 4, Archivo: "super.jpg", Estado: modelo.EstadoRevisar, C: modelo.Comprobante{Tipo: 82, Letra: "B", EsTique: true, PuntoVenta: 5, Numero: 12345,
			Fecha: "2026-11-21", Hora: "13:45:10", CUITEmisor: "30712223339", Rubro: "supermercado", NetoGravado: 1399774, IVA21: 115500, IVA105: 89226,
			Total: 1604500, Moneda: "PES", Cotizacion: 1}},
		Fila{ID: 5, Archivo: "ypf.jpg", Estado: modelo.EstadoRevisar, C: modelo.Comprobante{Tipo: 83, EsTique: true, PuntoVenta: 3, Numero: 45678,
			Fecha: "2026-11-22", Hora: "08:12:45", CUITEmisor: "30708888881", Rubro: "combustible", Total: 4406250, Moneda: "PES", Cotizacion: 1}},
		Fila{ID: 6, Archivo: "ypf2.jpg", Estado: modelo.EstadoRevisar, C: modelo.Comprobante{Tipo: 83, EsTique: true, CUITEmisor: "30708888881",
			Rubro: "combustible", Total: 1000000, Moneda: "PES", Cotizacion: 1}},
		// Una nota de crédito de tique resta.
		Fila{ID: 7, Archivo: "nc.jpg", Estado: modelo.EstadoRevisar, C: modelo.Comprobante{Tipo: 110, EsTique: true, CUITEmisor: "30708888881",
			Rubro: "combustible", Total: 6250, Moneda: "PES", Cotizacion: 1}},
	)
}

func TestResumenTickets(t *testing.T) {
	r := Resumir(filasConTickets())
	if r.Cantidad != 7 || r.Tickets.Cantidad != 4 || r.Tickets.Total != 1604500+4406250+1000000-6250 {
		t.Fatalf("%+v", r)
	}
	if r.PorRubro["combustible"] != 5400000 || r.PorRubro["supermercado"] != 1604500 {
		t.Fatalf("por rubro: %v", r.PorRubro)
	}
	// Las facturas sin rubro van aparte; las de moneda extranjera no se suman.
	if r.PorRubro[SinRubro] != 12100000-1210000 {
		t.Fatalf("sin rubro: %v", r.PorRubro)
	}
	if r.PorTipo["Tique"] != 2 || r.PorTipo["Tique Factura B"] != 1 {
		t.Fatalf("por tipo: %v", r.PorTipo)
	}
}

func TestExportTickets(t *testing.T) {
	var c bytes.Buffer
	if err := CSV(&c, filasConTickets()); err != nil {
		t.Fatal(err)
	}
	s := c.String()
	enc := strings.SplitN(s, "\n", 2)[0]
	if !strings.Contains(enc, ";Rubro;") || !strings.Contains(enc, "Fecha;Hora;") {
		t.Fatalf("encabezado: %s", enc)
	}
	for _, want := range []string{"2026-11-21;13:45:10;82;Tique Factura B;B;5;12345;30-71222333-9", ";supermercado;", ";Tique;", ";combustible;", ";-62,50;"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q en el CSV:\n%s", want, s)
		}
	}
	var x bytes.Buffer
	if err := Excel(&x, filasConTickets()); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(&x)
	if err != nil {
		t.Fatal(err)
	}
	filas, _ := f.GetRows("Resumen")
	var todo []string
	for _, r := range filas {
		todo = append(todo, strings.Join(r, "|"))
	}
	txt := strings.Join(todo, "\n")
	for _, want := range []string{"Tickets de compra|4", "Total por rubro", "combustible", "supermercado", "sin rubro"} {
		if !strings.Contains(txt, want) {
			t.Errorf("falta %q en el resumen del Excel:\n%s", want, txt)
		}
	}
}
