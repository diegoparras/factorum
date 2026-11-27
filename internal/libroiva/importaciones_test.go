package libroiva

import (
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

func despacho(id int64, numero, fecha string, c modelo.Comprobante) Entrada {
	c.Tipo, c.Despacho, c.Fecha = modelo.TipoDespacho, numero, fecha
	if c.Moneda == "" {
		c.Moneda, c.Cotizacion = "PES", 1
	}
	return Entrada{ID: id, Estado: modelo.EstadoAprobado, Archivo: "despacho.pdf", C: c}
}

// juegoImportaciones: un despacho al 21 % con IVA adicional (percepción de IVA), percepción de
// Ganancias y de Ingresos Brutos; uno al 10,5 % de un proveedor con CUIT país; uno sin IVA (código
// de operación X) y uno que entró de un mes anterior.
func juegoImportaciones() []Entrada {
	return []Entrada{
		despacho(1, "26001IC04123456K", "2026-11-04", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "SHENZHEN ELECTRONICS CO LTD",
			NetoGravado: m(1000000), IVA21: m(210000), OtrosTributos: m(260000), PercepcionIVA: m(200000), PercepcionNacionales: m(60000),
			Total: m(1470000)}),
		despacho(2, "26073IC05000777Z", "2026-11-12", modelo.Comprobante{CUITEmisor: "55000002206", RazonSocialEmisor: "INDÚSTRIA PAULISTA LTDA",
			NetoGravado: m(500000), IVA105: m(52500), OtrosTributos: m(25000), PercepcionIIBB: m(25000), Total: m(577500)}),
		despacho(3, "26001IC04000999A", "2026-11-20", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "LIBROS DEL MUNDO",
			Exento: m(80000), Total: m(80000)}),
		despacho(4, "26001IC04000500B", "2026-10-28", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "SHENZHEN ELECTRONICS CO LTD",
			NetoGravado: m(100000), IVA21: m(21000), Total: m(121000), PeriodoIVA: "2026-11"}),
	}
}

func TestGoldenImportaciones(t *testing.T) {
	if !arca.CUITPais("55000002206") {
		t.Fatal("55000002206 tiene la forma de una CUIT país")
	}
	r, err := GenerarImportaciones(juegoImportaciones(), Opciones{Cliente: cliente, Periodo: "2026-11"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Registros) != 4 || r.Excluidos != 0 {
		t.Fatalf("van los 4: %d, afuera %d: %+v", len(r.Registros), r.Excluidos, r.Grupos)
	}
	golden(t, "importaciones_cbte.golden", r.Comprobantes)
	golden(t, "importaciones_alicuotas.golden", r.Alicuotas)
	cbs := lineas(t, r.Comprobantes, LargoCbte)
	alis := lineas(t, r.Alicuotas, LargoImportacionAlicuota)
	if len(alis) != 4 {
		t.Fatalf("una alícuota por despacho: %d", len(alis))
	}
	k := 0
	for _, l := range cbs {
		if campo(l, CamposCbte, 2) != "066" || campo(l, CamposCbte, 3) != "00000" || campo(l, CamposCbte, 4) != strings.Repeat("0", 20) {
			t.Fatalf("despacho con tipo 066, punto de venta y número en cero: %q", l)
		}
		n := int(campo(l, CamposCbte, 19)[0] - '0')
		for range n {
			if campo(alis[k], CamposImportacionAlicuota, 1) != campo(l, CamposCbte, 5) {
				t.Fatalf("la alícuota %q no es del despacho %q", alis[k], l)
			}
			k++
		}
	}
	por := map[string]string{}
	for _, l := range cbs {
		por[campo(l, CamposCbte, 5)] = l
	}
	chk := func(d string, n int, want string) {
		t.Helper()
		if got := campo(por[d], CamposCbte, n); got != want {
			t.Errorf("%s, campo %d (%s): %q, esperaba %q", d, n, CamposCbte[n-1].Nombre, got, want)
		}
	}
	chk("26001IC04123456K", 1, "20261104")
	chk("26001IC04123456K", 6, "80")
	chk("26001IC04123456K", 12, "000000020000000") // IVA adicional: percepción de IVA
	chk("26001IC04123456K", 13, "000000006000000") // Ganancias
	chk("26001IC04123456K", 21, "000000021000000")
	chk("26001IC04123456K", 22, "000000000000000")
	chk("26073IC05000777Z", 7, "00000000055000002206")
	chk("26073IC05000777Z", 8, "INDUSTRIA PAULISTA LTDA"+strings.Repeat(" ", 7))
	chk("26073IC05000777Z", 14, "000000002500000")
	chk("26001IC04000999A", 20, "X")
	chk("26001IC04000999A", 11, "000000008000000")
	chk("26001IC04000500B", 1, "20261028")
	if g := grupo(r, "de_mes_anterior"); g == nil || len(g.Items) != 1 || g.Items[0].ID != 4 {
		t.Errorf("entró de un mes anterior: %+v", g)
	}
	if r.Resumen.DeMesesAnteriores != 1 || r.Resumen.Percepciones != m(285000) || r.Resumen.CreditoFiscal != m(283500) {
		t.Errorf("resumen: %+v", r.Resumen)
	}
}

func TestImportacionesQueNoVan(t *testing.T) {
	es := []Entrada{
		despacho(1, "", "2026-11-04", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		despacho(2, "26001IC0412345", "2026-11-04", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		despacho(3, "ABCDEFGHIJKLMNOP", "2026-11-04", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		despacho(4, "26001IC04123456K", "2026-11-05", modelo.Comprobante{RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		despacho(5, "26001IC04123457K", "2026-11-05", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21),
			OtrosTributos: m(10), PercepcionIVA: m(20), Total: m(131)}),
		despacho(6, "26001IC04123458K", "2026-11-05", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		despacho(7, "26001IC04123458K", "2026-11-05", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "A", NetoGravado: m(100), IVA21: m(21), Total: m(121)}),
		{ID: 8, Estado: modelo.EstadoOK, C: modelo.Comprobante{Tipo: 1, Fecha: "2026-11-05", CUITEmisor: "30700000008", Total: m(121), Despacho: "26001IC04123459K"}},
	}
	r, err := GenerarImportaciones(es, Opciones{Cliente: cliente, Periodo: "2026-11"})
	if err != nil {
		t.Fatal(err)
	}
	for codigo, want := range map[string][]int64{"sin_despacho": {1}, "despacho_invalido": {2}, "despacho_forma": {3}, "sin_cuit_emisor": {4}, "detalle_tributos": {5},
		"duplicado": {7}, "tipo_no_admitido": {8}} {
		got := ids(grupo(r, codigo))
		if len(got) != len(want) || got[0] != want[0] {
			t.Errorf("%s: %v, esperaba %v", codigo, got, want)
		}
	}
	if len(r.Registros) != 2 {
		t.Fatalf("van el de forma rara y el 6: %+v", r.Registros)
	}
}

func TestComprasYDespachos(t *testing.T) {
	es := juego()
	es = append(es, despacho(99, "26001IC04123456K", "2026-11-04", modelo.Comprobante{CUITEmisor: "30700000008", RazonSocialEmisor: "X",
		NetoGravado: m(100), IVA21: m(21), Total: m(121)}))
	r := generar(t, es, Opciones{})
	if g := grupo(r, "tipo_no_admitido"); g == nil || g.Items[0].ID != 99 || !strings.Contains(g.Items[0].Detalle, "Importaciones") {
		t.Fatalf("un despacho no va en compras: %+v", g)
	}
	// Percepciones separadas en compras: van en su campo y en «Otros tributos» queda el resto.
	c := juego()[10] // A con otros tributos 1500
	c.C.PercepcionIVA, c.C.PercepcionIIBB = m(1000), m(300)
	r = generar(t, []Entrada{c}, Opciones{})
	l := lineas(t, r.Comprobantes, LargoCbte)[0]
	if campo(l, CamposCbte, 12) != "000000000100000" || campo(l, CamposCbte, 14) != "000000000030000" || campo(l, CamposCbte, 22) != "000000000020000" {
		t.Errorf("percepciones: %q %q %q", campo(l, CamposCbte, 12), campo(l, CamposCbte, 14), campo(l, CamposCbte, 22))
	}
	if g := grupo(r, "otros_tributos"); g == nil || !strings.Contains(g.Items[0].Detalle, "200,00") {
		t.Errorf("quedan 200 sin detalle: %+v", g)
	}
}

func TestPeriodos(t *testing.T) {
	if NombreMes("2026-10") != "octubre de 2026" || MesSiguiente("2026-12") != "2027-01" {
		t.Fatal("nombres de los meses")
	}
	p := map[string]bool{"2026-10": true, "2026-11": true}
	if got := PrimerPeriodoAbierto("2026-10", p); got != "2026-12" {
		t.Errorf("octubre y noviembre presentados: va en diciembre, no %s", got)
	}
	if got := PrimerPeriodoAbierto("2026-09", p); got != "2026-09" {
		t.Errorf("septiembre está abierto: %s", got)
	}
	if LibroDe(66) != LibroImportaciones || LibroDe(1) != LibroCompras {
		t.Error("libro de cada tipo")
	}
	// Una compra de octubre que se pasó a noviembre: va en noviembre con su fecha, y no en octubre.
	e := juego()[0]
	e.C.Fecha, e.C.PeriodoIVA = "2026-10-30", "2026-11"
	nov := generar(t, []Entrada{e}, Opciones{})
	if len(nov.Registros) != 1 || !nov.Registros[0].DeMesAnterior || nov.Resumen.DeMesesAnteriores != 1 {
		t.Fatalf("noviembre: %+v", nov.Registros)
	}
	if campo(lineas(t, nov.Comprobantes, LargoCbte)[0], CamposCbte, 1) != "20261030" {
		t.Error("va con su fecha")
	}
	oct := generar(t, []Entrada{e}, Opciones{Periodo: "2026-10"})
	if g := grupo(oct, "fuera_del_periodo"); g == nil || !strings.Contains(g.Items[0].Detalle, "noviembre de 2026") {
		t.Fatalf("octubre: %+v", oct.Grupos)
	}
	// Un período anterior a la fecha no vale.
	e.C.Fecha, e.C.PeriodoIVA = "2026-11-05", "2026-10"
	if g := grupo(generar(t, []Entrada{e}, Opciones{Periodo: "2026-10"}), "fuera_del_periodo"); g == nil {
		t.Fatal("un período anterior a la fecha no vale")
	}
	h1, h2 := nov.Registros[0].Huella(), generar(t, []Entrada{juego()[0]}, Opciones{}).Registros[0].Huella()
	if h1 == h2 || len(h1) != 24 {
		t.Errorf("la huella cambia con el registro: %s %s", h1, h2)
	}
}
