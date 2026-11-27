package conciliar

import (
	"reflect"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

const (
	cliente = "30711111118"
	prov    = "30500000001"
	prov2   = "20263630743"
)

func f(id int64, tipo, pv int, num int64, fecha string, total modelo.Importe) Comprobante {
	return Comprobante{ID: id, Tipo: tipo, PuntoVenta: pv, Numero: num, Fecha: fecha, CUITEmisor: prov, RazonSocial: "Proveedor SA",
		DocTipoRec: 80, DocNroRec: cliente, Moneda: "PES", Total: total, Estado: "ok"}
}

func a(tipo, pv int, num int64, fecha string, total modelo.Importe) Recibido {
	return Recibido{Fecha: fecha, Tipo: tipo, PuntoVenta: pv, NumeroDesde: num, NumeroHasta: num, CUITEmisor: prov, Emisor: "PROVEEDOR SA",
		Moneda: "PES", TipoCambio: 1, Total: total}
}

var noviembre = Opciones{Cliente: cliente, Desde: "2026-11-01", Hasta: "2026-11-30"}

func uno(t *testing.T, r *Resultado, grupo string) Item {
	t.Helper()
	if n := len(r.Grupos[grupo]); n != 1 {
		t.Fatalf("esperaba 1 en %s, hay %d: %+v", grupo, n, r.Grupos)
	}
	return r.Grupos[grupo][0]
}

func cuantos(r *Resultado) map[string]int {
	m := map[string]int{}
	for _, g := range Grupos {
		m[g] = len(r.Grupos[g])
	}
	return m
}

func TestClaveNormalizada(t *testing.T) {
	if got := Clave("30-50000000-1", 1, 3, 42); got != "30500000001-1-3-42" {
		t.Fatalf("clave: %q", got)
	}
	for _, c := range []struct {
		cuit string
		tipo int
		num  int64
	}{{"", 1, 1}, {prov, 0, 1}, {prov, 1, 0}} {
		if Clave(c.cuit, c.tipo, 1, c.num) != "" {
			t.Fatalf("sin datos clave tiene que dar vacío: %+v", c)
		}
	}
	if Clave(prov, 1, 0, 5) == "" {
		t.Fatal("punto de venta 0 es válido")
	}
}

func TestNormalizarMoneda(t *testing.T) {
	for in, want := range map[string]string{"": "PES", "$": "PES", "ars": "PES", "PES": "PES", "USD": "DOL", "dol": "DOL", "U$S": "DOL", "EUR": "060", "060": "060", "012": "012"} {
		if got := NormalizarMoneda(in); got != want {
			t.Errorf("%q: %q, esperaba %q", in, got, want)
		}
	}
}

func TestCoinciden(t *testing.T) {
	fc := f(1, 1, 3, 100, "2026-11-05", 121000)
	fc.Neto, fc.IVA = 100000, 21000
	ar := a(1, 3, 100, "2026-11-05", 121001) // un centavo de redondeo
	ar.Neto, ar.IVA = 100000, 21001
	r := Conciliar([]Comprobante{fc}, []Recibido{ar}, noviembre)
	it := uno(t, r, Coinciden)
	if it.Factorum == nil || it.Arca == nil || it.Factorum.ID != 1 || it.Emisor != "PROVEEDOR SA" {
		t.Fatalf("item: %+v", it)
	}
	if r.Totales[Coinciden].Cantidad != 1 || r.Totales[Coinciden].Pesos != 121001 {
		t.Fatalf("totales: %+v", r.Totales[Coinciden])
	}
}

func TestDiferenciasDeImportesFechaYMoneda(t *testing.T) {
	fc := f(1, 1, 3, 100, "2026-11-05", 121000)
	fc.Neto, fc.IVA = 100000, 21000
	ar := a(1, 3, 100, "2026-11-07", 131000)
	ar.Neto, ar.IVA = 110000, 21000
	r := Conciliar([]Comprobante{fc}, []Recibido{ar}, noviembre)
	it := uno(t, r, Diferencias)
	want := []Diferencia{
		{Campo: "total", Factorum: "1210.00", Arca: "1310.00", Importe: -10000},
		{Campo: "neto_gravado", Factorum: "1000.00", Arca: "1100.00", Importe: -10000},
		{Campo: "fecha", Factorum: "2026-11-05", Arca: "2026-11-07", Dias: -2},
	}
	if !reflect.DeepEqual(it.Diferencias, want) {
		t.Fatalf("diferencias:\n%+v\nesperaba\n%+v", it.Diferencias, want)
	}
	if it.CasiIgual {
		t.Fatal("misma clave no es casi igual")
	}
	tot := r.Totales[Diferencias]
	if tot.PesosArca != 131000 || tot.PesosFactorum != 121000 {
		t.Fatalf("totales de diferencias: %+v", tot)
	}

	// Moneda distinta: se dice la moneda y no se comparan importes.
	fd := f(2, 1, 3, 101, "2026-11-05", 100000)
	ad := a(1, 3, 101, "2026-11-05", 1000)
	ad.Moneda, ad.TipoCambio = "DOL", 1000
	r = Conciliar([]Comprobante{fd}, []Recibido{ad}, noviembre)
	it = uno(t, r, Diferencias)
	if len(it.Diferencias) != 1 || it.Diferencias[0].Campo != "moneda" || it.Diferencias[0].Arca != "DOL" {
		t.Fatalf("moneda: %+v", it.Diferencias)
	}
}

func TestNetoEIVASoloSiDiscriminaYArcaLosTrae(t *testing.T) {
	// Factura B: el IVA va incluido; Factorum no lo separa y ARCA sí. No es una diferencia.
	fb := f(1, 6, 2, 7, "2026-11-10", 50000)
	ab := a(6, 2, 7, "2026-11-10", 50000)
	ab.Neto, ab.IVA = 41322, 8678
	// Factura A sin neto en ARCA: no se compara el neto.
	fa := f(2, 1, 2, 8, "2026-11-10", 121000)
	fa.Neto, fa.IVA = 100000, 21000
	aa := a(1, 2, 8, "2026-11-10", 121000)
	r := Conciliar([]Comprobante{fb, fa}, []Recibido{ab, aa}, noviembre)
	if c := cuantos(r); c[Coinciden] != 2 {
		t.Fatalf("esperaba 2 coincidencias: %+v", r.Grupos)
	}
}

func TestNotaDeCreditoRestaYCoincideAunqueUnLadoVengaEnNegativo(t *testing.T) {
	fc := f(1, 1, 3, 100, "2026-11-05", 100000)
	nc := f(2, 3, 3, 9, "2026-11-06", 20000)
	anc := a(3, 3, 9, "2026-11-06", -20000)
	r := Conciliar([]Comprobante{fc, nc}, []Recibido{a(1, 3, 100, "2026-11-05", 100000), anc}, noviembre)
	if c := cuantos(r); c[Coinciden] != 2 {
		t.Fatalf("grupos: %+v", r.Grupos)
	}
	if got := r.Totales[Coinciden].Pesos; got != 80000 {
		t.Fatalf("la nota de crédito tiene que restar: %d", got)
	}
}

func TestFaltaCargarYMonedaExtranjeraAparte(t *testing.T) {
	ad := a(1, 5, 300, "2026-11-12", 150000)
	ad.Moneda = "DOL"
	ap := true
	ad.EmisorApocrifo = &ap
	r := Conciliar(nil, []Recibido{a(1, 5, 200, "2026-11-11", 242000), ad}, noviembre)
	if c := cuantos(r); c[FaltaCargar] != 2 {
		t.Fatalf("grupos: %+v", r.Grupos)
	}
	tot := r.Totales[FaltaCargar]
	if tot.Pesos != 242000 || tot.CantidadExtranjera != 1 || tot.Extranjera["DOL"] != 150000 {
		t.Fatalf("totales: %+v", tot)
	}
	if it := r.Grupos[FaltaCargar][1]; it.Arca.EmisorApocrifo == nil || !*it.Arca.EmisorApocrifo || it.Factorum != nil {
		t.Fatalf("falta cargar: %+v", it)
	}
}

func TestNoFiguraConSusCausas(t *testing.T) {
	tique := f(1, 82, 4, 1, "2026-11-02", 5000)
	tique.EsTique = true
	dolares := f(2, 1, 1, 2, "2026-11-02", 1000)
	dolares.Moneda = "DOL"
	exterior := f(3, 1, 1, 3, "2026-11-02", 1000)
	exterior.CUITEmisor = "55000002206"
	cf := f(4, 6, 1, 4, "2026-11-02", 1000)
	cf.DocTipoRec, cf.DocNroRec = 99, "0"
	otro := f(5, 1, 1, 5, "2026-11-02", 1000)
	otro.DocNroRec = prov2
	sinSync := f(6, 1, 1, 6, "2026-11-25", 1000)
	nc := f(7, 3, 1, 7, "2026-11-03", 1000)
	nada := f(8, 1, 1, 8, "2026-11-04", 1000)
	apocrifo := f(9, 1, 1, 9, "2026-11-04", 1000)
	apocrifo.Constatacion = "apocrifo"
	invalido := f(10, 1, 1, 10, "2026-11-04", 1000)
	invalido.Constatacion = "invalido"
	valido := f(11, 1, 1, 11, "2026-11-04", 1000)
	valido.Constatacion = "valido"
	incompleto := f(12, 1, 1, 0, "2026-11-04", 1000)
	o := noviembre
	o.SinSincronizar = []Periodo{{"2026-11-21", "2026-11-30"}}
	r := Conciliar([]Comprobante{tique, dolares, exterior, cf, otro, sinSync, nc, nada, apocrifo, invalido, valido, incompleto}, nil, o)
	porID := map[int64]Item{}
	for _, it := range r.Grupos[NoFigura] {
		porID[it.Factorum.ID] = it
	}
	if len(porID) != 12 {
		t.Fatalf("esperaba 12 en no figura: %d", len(porID))
	}
	want := map[int64]string{1: CausaTique, 2: CausaExterior, 3: CausaExterior, 4: CausaOtroReceptor, 5: CausaOtroReceptor, 6: CausaNoSincronizad,
		7: CausaNota, 8: CausaSinExplicar, 9: CausaSinExplicar, 10: CausaSinExplicar, 11: CausaSinExplicar, 12: CausaIncompleto}
	for id, c := range want {
		if porID[id].Causa != c {
			t.Errorf("comprobante %d: causa %q, esperaba %q", id, porID[id].Causa, c)
		}
	}
	for _, id := range []int64{1, 2, 3, 4, 5, 6, 7} {
		if !porID[id].Legitima || porID[id].Alarma {
			t.Errorf("comprobante %d tendría que ser legítimo y sin alarma: %+v", id, porID[id])
		}
	}
	if !porID[8].Constatar || porID[8].Legitima || porID[8].Alarma {
		t.Errorf("sin explicación: hay que sugerir constatarlo: %+v", porID[8])
	}
	if !porID[9].Alarma || !porID[10].Alarma || porID[9].Constatar || porID[11].Alarma || porID[11].Constatar {
		t.Errorf("alarmas: apócrifo %+v, inválido %+v, válido %+v", porID[9], porID[10], porID[11])
	}
	tot := r.Totales[NoFigura]
	if tot.Legitimas != 7 || tot.Alarmas != 2 || tot.PorCausa[CausaSinExplicar] != 4 || r.Incompletos != 1 {
		t.Fatalf("totales: %+v, incompletos %d", tot, r.Incompletos)
	}
	if tot.CantidadExtranjera != 1 || tot.Extranjera["DOL"] != 1000 {
		t.Fatalf("la moneda extranjera va aparte: %+v", tot)
	}
}

func TestCasiIgualEsSugerenciaEnDiferencias(t *testing.T) {
	// Mismo emisor y número; Factorum leyó el punto de venta 8 en lugar del 3.
	r := Conciliar([]Comprobante{f(1, 1, 8, 1234, "2026-11-05", 121000)}, []Recibido{a(1, 3, 1234, "2026-11-05", 121000)}, noviembre)
	it := uno(t, r, Diferencias)
	if !it.CasiIgual || len(it.Diferencias) != 1 || it.Diferencias[0].Campo != "punto_venta" || it.Diferencias[0].Arca != "3" {
		t.Fatalf("casi igual: %+v", it)
	}
	if c := cuantos(r); c[Coinciden] != 0 || c[FaltaCargar] != 0 || c[NoFigura] != 0 {
		t.Fatalf("un casi igual nunca coincide ni queda suelto: %+v", c)
	}
	if r.Totales[Diferencias].CasiIguales != 1 {
		t.Fatalf("totales: %+v", r.Totales[Diferencias])
	}

	// Otro tipo de la misma clase (A leída como B) también; con otra clase (factura contra nota) no.
	r = Conciliar([]Comprobante{f(1, 6, 3, 55, "2026-11-05", 1000), f(2, 3, 3, 56, "2026-11-05", 1000)},
		[]Recibido{a(1, 3, 55, "2026-11-05", 1000), a(1, 3, 56, "2026-11-05", 1000)}, noviembre)
	if c := cuantos(r); c[Diferencias] != 1 || c[FaltaCargar] != 1 || c[NoFigura] != 1 {
		t.Fatalf("grupos: %+v", r.Grupos)
	}
	if it := r.Grupos[Diferencias][0]; it.Diferencias[0].Campo != "tipo" {
		t.Fatalf("tipo: %+v", it)
	}

	// Mismo número pero ni el total ni la fecha: son dos comprobantes distintos.
	r = Conciliar([]Comprobante{f(1, 1, 8, 77, "2026-11-05", 1000)}, []Recibido{a(1, 3, 77, "2026-11-20", 99999)}, noviembre)
	if c := cuantos(r); c[Diferencias] != 0 || c[FaltaCargar] != 1 || c[NoFigura] != 1 {
		t.Fatalf("no tendría que sugerir: %+v", r.Grupos)
	}
}

func TestDuplicadosDeCadaLado(t *testing.T) {
	// Cargado dos veces en Factorum: se empareja el que cierra y el otro se muestra como duplicado.
	malo := f(1, 1, 3, 100, "2026-11-05", 999)
	bueno := f(2, 1, 3, 100, "2026-11-05", 121000)
	bueno.Estado = "aprobado"
	ar := a(1, 3, 100, "2026-11-05", 121000)
	r := Conciliar([]Comprobante{malo, bueno}, []Recibido{ar, ar}, noviembre)
	it := uno(t, r, Coinciden)
	if it.Factorum.ID != 2 || !reflect.DeepEqual(it.DuplicadosFactorum, []int64{1}) || it.DuplicadosArca != 1 {
		t.Fatalf("duplicados: %+v", it)
	}
	if r.Duplicados.Factorum != 1 || r.Duplicados.Arca != 1 || len(r.Grupos[NoFigura]) != 0 {
		t.Fatalf("resumen de duplicados: %+v, grupos %+v", r.Duplicados, cuantos(r))
	}
	// Duplicado en Factorum de algo que no está en ARCA: un solo item, con el otro id.
	r = Conciliar([]Comprobante{f(5, 1, 3, 9, "2026-11-05", 10), f(6, 1, 3, 9, "2026-11-05", 10)}, nil, noviembre)
	if it := uno(t, r, NoFigura); !reflect.DeepEqual(it.DuplicadosFactorum, []int64{6}) {
		t.Fatalf("no figura duplicado: %+v", it)
	}
}

func TestMargenDeFechas(t *testing.T) {
	// Factorum leyó mal el mes: está en octubre pero ARCA lo tiene en noviembre. Se empareja igual.
	r := Conciliar([]Comprobante{f(1, 1, 3, 100, "2026-10-05", 1000)}, []Recibido{a(1, 3, 100, "2026-11-05", 1000)}, noviembre)
	it := uno(t, r, Diferencias)
	if it.Diferencias[0].Campo != "fecha" || it.Diferencias[0].Dias != -31 {
		t.Fatalf("fecha: %+v", it.Diferencias)
	}
	// Lo que está afuera del período en los dos lados no aparece.
	r = Conciliar([]Comprobante{f(1, 1, 3, 100, "2026-10-05", 1000), f(2, 1, 3, 101, "2026-12-02", 1)},
		[]Recibido{a(1, 3, 100, "2026-10-05", 1000), a(1, 3, 102, "2026-12-03", 5)}, noviembre)
	if c := cuantos(r); c[Coinciden]+c[Diferencias]+c[FaltaCargar]+c[NoFigura] != 0 {
		t.Fatalf("fuera del período: %+v", r.Grupos)
	}
}

func TestRangoDeNumerosEnArca(t *testing.T) {
	ar := a(6, 2, 10, "2026-11-08", 500000)
	ar.NumeroHasta = 20
	r := Conciliar([]Comprobante{f(1, 6, 2, 15, "2026-11-08", 1000)}, []Recibido{ar}, noviembre)
	it := uno(t, r, Coinciden)
	if it.Nota == "" || it.Total != 1000 {
		t.Fatalf("rango: %+v", it)
	}
	r = Conciliar(nil, []Recibido{ar}, noviembre)
	if it := uno(t, r, FaltaCargar); it.Nota == "" {
		t.Fatalf("rango sin cargar: %+v", it)
	}
}

func TestOrdenYGruposVacios(t *testing.T) {
	r := Conciliar(nil, nil, noviembre)
	for _, g := range Grupos {
		if r.Grupos[g] == nil || r.Totales[g] == nil || r.Totales[g].Cantidad != 0 {
			t.Fatalf("grupo %s sin inicializar", g)
		}
	}
	r = Conciliar(nil, []Recibido{a(1, 1, 3, "2026-11-20", 1), a(1, 1, 2, "2026-11-02", 1), a(1, 1, 1, "2026-11-20", 1)}, noviembre)
	var nums []int64
	for _, it := range r.Grupos[FaltaCargar] {
		nums = append(nums, it.Numero)
	}
	if !reflect.DeepEqual(nums, []int64{2, 1, 3}) {
		t.Fatalf("orden: %v", nums)
	}
}

func TestFaltantes(t *testing.T) {
	cases := []struct {
		bajados []Periodo
		want    []Periodo
	}{
		{nil, []Periodo{{"2026-11-01", "2026-11-30"}}},
		{[]Periodo{{"2026-01-01", "2026-12-31"}}, nil},
		{[]Periodo{{"2026-01-01", "2026-11-20"}}, []Periodo{{"2026-11-21", "2026-11-30"}}},
		{[]Periodo{{"2026-11-10", "2026-11-12"}, {"2026-11-05", "2026-11-08"}}, []Periodo{{"2026-11-01", "2026-11-04"}, {"2026-11-09", "2026-11-09"}, {"2026-11-13", "2026-11-30"}}},
		{[]Periodo{{"2026-12-01", "2026-12-31"}}, []Periodo{{"2026-11-01", "2026-11-30"}}},
	}
	for i, c := range cases {
		if got := Faltantes("2026-11-01", "2026-11-30", c.bajados); !reflect.DeepEqual(got, c.want) {
			t.Errorf("caso %d: %+v, esperaba %+v", i, got, c.want)
		}
	}
}
