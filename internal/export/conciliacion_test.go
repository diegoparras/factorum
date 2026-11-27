package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/modelo"
)

func resultadoPrueba() *conciliar.Resultado {
	f := func(id int64, tipo, pv int, num int64, fecha string, total int64) conciliar.Comprobante {
		return conciliar.Comprobante{ID: id, Tipo: tipo, PuntoVenta: pv, Numero: num, Fecha: fecha, CUITEmisor: "30500000001", RazonSocial: "=Proveedor SA",
			DocNroRec: "30711111118", Moneda: "PES", Total: modeloImporte(total), Estado: "ok", Archivo: "a.pdf"}
	}
	a := func(tipo, pv int, num int64, fecha string, total int64) conciliar.Recibido {
		return conciliar.Recibido{Fecha: fecha, Tipo: tipo, PuntoVenta: pv, NumeroDesde: num, NumeroHasta: num, CUITEmisor: "30500000001", Emisor: "PROVEEDOR SA",
			Moneda: "PES", Total: modeloImporte(total), CAE: "76000000000001"}
	}
	fs := []conciliar.Comprobante{f(1, 1, 3, 1, "2026-11-02", 121000), f(2, 1, 3, 2, "2026-11-03", 100000), f(3, 1, 3, 9, "2026-11-04", 5000), f(4, 3, 3, 4, "2026-11-05", 20000)}
	as := []conciliar.Recibido{a(1, 3, 1, "2026-11-02", 121000), a(1, 3, 2, "2026-11-03", 110000), a(1, 3, 3, "2026-11-04", 7777), a(3, 3, 4, "2026-11-05", 20000)}
	return conciliar.Conciliar(fs, as, conciliar.Opciones{Cliente: "30711111118", Desde: "2026-11-01", Hasta: "2026-11-30"})
}

func TestConciliacionExcel(t *testing.T) {
	r := resultadoPrueba()
	var b bytes.Buffer
	info := InfoConciliacion{Cliente: "30711111118", Desde: "2026-11-01", Hasta: "2026-11-30", Sincronizacion: "al 20/11/2026",
		SinSincronizar: []conciliar.Periodo{{Desde: "2026-11-21", Hasta: "2026-11-30"}}}
	if err := ConciliacionExcel(&b, r, info, ""); err != nil {
		t.Fatal(err)
	}
	x, err := excelize.OpenReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	if got := x.GetSheetList(); strings.Join(got, "|") != "Resumen|Coinciden|Con diferencias|Falta cargar|No figura en ARCA" {
		t.Fatalf("hojas: %v", got)
	}
	filas, _ := x.GetRows("Resumen")
	todo := ""
	for _, f := range filas {
		todo += strings.Join(f, "|") + "\n"
	}
	for _, s := range []string{"30-71111111-8", "01/11/2026 al 30/11/2026", "Sin sincronizar en Arcanum: 21/11/2026 al 30/11/2026", "Coinciden|2|1,010.00", "Falta cargar|1|77.77"} {
		if !strings.Contains(todo, s) {
			t.Errorf("el resumen no tiene %q:\n%s", s, todo)
		}
	}
	dif, _ := x.GetRows("Con diferencias")
	if len(dif) != 2 || !strings.Contains(strings.Join(dif[1], "|"), "Total: Factorum 1.000,00, ARCA 1.100,00 (-100,00)") {
		t.Fatalf("diferencias: %v", dif)
	}
	co, _ := x.GetRows("Coinciden")
	if len(co) != 3 || !strings.Contains(strings.Join(co[2], "|"), "-200.00") {
		t.Fatalf("la nota de crédito tiene que ir en negativo: %v", co)
	}

	// Solo «Falta cargar», para pedírselo al cliente.
	b.Reset()
	if err := ConciliacionExcel(&b, r, info, conciliar.FaltaCargar); err != nil {
		t.Fatal(err)
	}
	x, _ = excelize.OpenReader(&b)
	if got := x.GetSheetList(); strings.Join(got, "|") != "Resumen|Falta cargar" {
		t.Fatalf("hojas de falta cargar: %v", got)
	}
	filas, _ = x.GetRows("Resumen")
	todo = ""
	for _, f := range filas {
		todo += strings.Join(f, "|") + "\n"
	}
	if strings.Contains(todo, "Coinciden") || !strings.Contains(todo, "Falta cargar|1") {
		t.Fatalf("el resumen de falta cargar tiene solo ese grupo:\n%s", todo)
	}
}

func TestConciliacionCSV(t *testing.T) {
	r := resultadoPrueba()
	var b bytes.Buffer
	if err := ConciliacionCSV(&b, r, ""); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if !strings.HasPrefix(s, "\xef\xbb\xbfGrupo;Fecha;") || strings.Count(s, "\n") != 6 {
		t.Fatalf("csv:\n%s", s)
	}
	if !strings.Contains(s, "Con diferencias;03/11/2026;") || !strings.Contains(s, ";-100,00;") || !strings.Contains(s, "'=Proveedor SA") {
		t.Fatalf("csv:\n%s", s)
	}
	b.Reset()
	_ = ConciliacionCSV(&b, r, conciliar.FaltaCargar)
	if strings.Count(b.String(), "\n") != 2 || !strings.Contains(b.String(), "Falta cargar;04/11/2026") {
		t.Fatalf("falta cargar:\n%s", b.String())
	}
}

func modeloImporte(c int64) modelo.Importe { return modelo.Importe(c) }
