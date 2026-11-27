package libroiva

import (
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

const clienteVentas = "30722222225"

func ip(v float64) *modelo.Importe { x := m(v); return &x }

// juegoVentas cubre lo que pide el libro de ventas: A con dos alícuotas (detalle de Mis
// Comprobantes), B a consumidor final sin identificar, B con DNI, nota de crédito A, tiques
// agrupados (venta global diaria), dólares, exento, exportación, una sola alícuota deducida, detalle
// de la emisión propia de Arcanum con percepciones, otros tributos deducidos y un consumidor final
// por más de $ 10.000.000.
func juegoVentas() []Venta {
	a := func(c int, n, i float64) AlicuotaVenta { return AlicuotaVenta{Codigo: c, Neto: m(n), IVA: m(i)} }
	cero := ip(0)
	return []Venta{
		{Fecha: "2026-11-03", Tipo: 1, PuntoVenta: 2, Numero: 120, NumeroHasta: 120, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SOCIEDAD ANONIMA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(1500), IVA: m(262.5), OtrosTributos: cero, Total: m(1762.5),
			Alicuotas: []AlicuotaVenta{a(5, 1000, 210), a(4, 500, 52.5)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-04", Tipo: 6, PuntoVenta: 2, Numero: 48, NumeroHasta: 48, DocTipo: 99, DocNro: "0", Receptor: "",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(1000), IVA: m(210), OtrosTributos: cero, Total: m(1210),
			Alicuotas: []AlicuotaVenta{a(5, 1000, 210)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-05", Tipo: 6, PuntoVenta: 2, Numero: 49, NumeroHasta: 49, DocTipo: 96, DocNro: "28111222", Receptor: "PEREZ JOSÉ MARÍA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(2000), IVA: m(420), OtrosTributos: cero, Total: m(2420),
			Alicuotas: []AlicuotaVenta{a(5, 2000, 420)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-06", Tipo: 3, PuntoVenta: 2, Numero: 7, NumeroHasta: 7, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(100), IVA: m(21), OtrosTributos: cero, Total: m(121),
			Alicuotas: []AlicuotaVenta{a(5, 100, 21)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-07", Tipo: 83, PuntoVenta: 5, Numero: 1001, NumeroHasta: 1050, DocTipo: 99, DocNro: "0", Receptor: "",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(50000), IVA: m(10500), OtrosTributos: cero, Total: m(60500),
			Alicuotas: []AlicuotaVenta{a(5, 50000, 10500)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-10", Tipo: 1, PuntoVenta: 2, Numero: 121, NumeroHasta: 121, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SA",
			Moneda: "DOL", Cotizacion: 1180.5, NetoGravado: m(1000), IVA: m(210), OtrosTributos: cero, Total: m(1210),
			Alicuotas: []AlicuotaVenta{a(5, 1000, 210)}, FuenteAlicuotas: "mis_comprobantes"},
		{Fecha: "2026-11-11", Tipo: 1, PuntoVenta: 2, Numero: 122, NumeroHasta: 122, DocTipo: 80, DocNro: "30711111111", Receptor: "COOPERADORA ESCOLAR",
			Moneda: "PES", Cotizacion: 1, Exento: m(5000), OtrosTributos: cero, Total: m(5000)},
		{Fecha: "2026-11-12", Tipo: 19, PuntoVenta: 4, Numero: 15, NumeroHasta: 15, DocTipo: 80, DocNro: "55000002206", Receptor: "IMPORTADORA DEL URUGUAY",
			Moneda: "DOL", Cotizacion: 1150, OtrosTributos: cero, Total: m(8000)},
		{Fecha: "2026-11-13", Tipo: 1, PuntoVenta: 2, Numero: 123, NumeroHasta: 123, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(3000), IVA: m(315), OtrosTributos: cero, Total: m(3315)}, // sin detalle: 10,5 %
		{Fecha: "2026-11-14", Tipo: 1, PuntoVenta: 2, Numero: 124, NumeroHasta: 124, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(10000), IVA: m(2100), OtrosTributos: ip(450), Total: m(12550),
			Alicuotas: []AlicuotaVenta{a(5, 10000, 2100)}, FuenteAlicuotas: "arcanum",
			Tributos: []TributoVenta{{ID: 7, Importe: m(300)}, {ID: 6, Importe: m(150)}}, FechaVtoPago: "20261130"},
		{Fecha: "2026-11-15", Tipo: 1, PuntoVenta: 2, Numero: 125, NumeroHasta: 125, DocTipo: 80, DocNro: "30711111111", Receptor: "CLIENTE MAYORISTA SA",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(1000), IVA: m(210), Total: m(1240),
			Alicuotas: []AlicuotaVenta{a(5, 1000, 210)}, FuenteAlicuotas: "mis_comprobantes"}, // otros tributos sin dato: 30
		{Fecha: "2026-11-16", Tipo: 6, PuntoVenta: 2, Numero: 50, NumeroHasta: 50, DocTipo: 99, DocNro: "0", Receptor: "CONSUMIDOR FINAL",
			Moneda: "PES", Cotizacion: 1, NetoGravado: m(10000000), IVA: m(2100000), OtrosTributos: cero, Total: m(12100000),
			Alicuotas: []AlicuotaVenta{a(5, 10000000, 2100000)}, FuenteAlicuotas: "mis_comprobantes"},
	}
}

func generarVentas(t *testing.T, vs []Venta, o Opciones) *Resultado {
	t.Helper()
	if o.Cliente == "" {
		o.Cliente = clienteVentas
	}
	if o.Periodo == "" {
		o.Periodo = "2026-11"
	}
	r, err := GenerarVentas(vs, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDisenoVentas(t *testing.T) {
	for nombre, d := range map[string]struct {
		campos []Campo
		largo  int
	}{"ventas": {CamposVentasCbte, LargoVentasCbte}, "ventas alícuotas": {CamposVentasAlicuota, LargoVentasAlicuota},
		"importación alícuotas": {CamposImportacionAlicuota, LargoImportacionAlicuota}} {
		pos := 1
		for i, c := range d.campos {
			if c.Nro != i+1 || c.Desde != pos || c.Hasta < c.Desde {
				t.Fatalf("%s: campo %d mal ubicado: %+v (esperaba desde %d)", nombre, i+1, c, pos)
			}
			pos = c.Hasta + 1
		}
		if pos-1 != d.largo {
			t.Fatalf("%s: los campos suman %d posiciones, el registro tiene %d", nombre, pos-1, d.largo)
		}
	}
	// Posiciones del Anexo I escritas a mano.
	for n, want := range map[int][2]int{5: {37, 56}, 6: {57, 58}, 7: {59, 78}, 8: {79, 108}, 9: {109, 123}, 11: {139, 153}, 12: {154, 168},
		17: {229, 231}, 18: {232, 241}, 19: {242, 242}, 20: {243, 243}, 21: {244, 258}, 22: {259, 266}} {
		if c := CamposVentasCbte[n-1]; c.Desde != want[0] || c.Hasta != want[1] {
			t.Errorf("ventas, campo %d: %d-%d, el diseño dice %d-%d", n, c.Desde, c.Hasta, want[0], want[1])
		}
	}
	for n, want := range map[int][2]int{3: {9, 28}, 4: {29, 43}, 5: {44, 47}, 6: {48, 62}} {
		if c := CamposVentasAlicuota[n-1]; c.Desde != want[0] || c.Hasta != want[1] {
			t.Errorf("ventas alícuotas, campo %d: %d-%d, el diseño dice %d-%d", n, c.Desde, c.Hasta, want[0], want[1])
		}
	}
	for n, want := range map[int][2]int{1: {1, 16}, 2: {17, 31}, 3: {32, 35}, 4: {36, 50}} {
		if c := CamposImportacionAlicuota[n-1]; c.Desde != want[0] || c.Hasta != want[1] {
			t.Errorf("importación alícuotas, campo %d: %d-%d, el diseño dice %d-%d", n, c.Desde, c.Hasta, want[0], want[1])
		}
	}
}

func TestGoldenVentas(t *testing.T) {
	r := generarVentas(t, juegoVentas(), Opciones{})
	if len(r.Registros) != 12 || r.Excluidos != 0 {
		t.Fatalf("tienen que ir los 12: van %d, afuera %d: %+v", len(r.Registros), r.Excluidos, r.Grupos)
	}
	for _, g := range r.Grupos {
		if g.Nivel == NivelBloquea || g.Nivel == NivelFuerte {
			t.Fatalf("el juego no tiene que tener problemas que bloqueen: %+v", g)
		}
	}
	golden(t, "ventas_cbte.golden", r.Comprobantes)
	golden(t, "ventas_alicuotas.golden", r.Alicuotas)
	cbs := lineas(t, r.Comprobantes, LargoVentasCbte)
	alis := lineas(t, r.Alicuotas, LargoVentasAlicuota)
	k := 0
	for _, l := range cbs {
		n := int(campo(l, CamposVentasCbte, 19)[0] - '0')
		if n == 0 {
			t.Fatalf("en ventas todos llevan al menos una alícuota: %q", l)
		}
		for range n {
			a := alis[k]
			k++
			if campo(a, CamposVentasAlicuota, 1) != campo(l, CamposVentasCbte, 2) || campo(a, CamposVentasAlicuota, 2) != campo(l, CamposVentasCbte, 3) ||
				campo(a, CamposVentasAlicuota, 3) != campo(l, CamposVentasCbte, 4) {
				t.Fatalf("la alícuota %q no es del comprobante %q", a, l)
			}
		}
	}
	if k != len(alis) {
		t.Fatalf("sobran registros de alícuotas: %d de %d", len(alis)-k, len(alis))
	}
	por := map[string]string{}
	for _, l := range cbs {
		por[campo(l, CamposVentasCbte, 2)+"-"+strings.TrimLeft(campo(l, CamposVentasCbte, 3), "0")+"-"+strings.TrimLeft(campo(l, CamposVentasCbte, 4), "0")] = l
	}
	chk := func(clave string, n int, want string) {
		t.Helper()
		l, ok := por[clave]
		if !ok {
			t.Fatalf("no está %s", clave)
		}
		if got := campo(l, CamposVentasCbte, n); got != want {
			t.Errorf("%s, campo %d (%s): %q, esperaba %q", clave, n, CamposVentasCbte[n-1].Nombre, got, want)
		}
	}
	chk("001-2-120", 5, "00000000000000000120")
	chk("001-2-120", 6, "80")
	chk("001-2-120", 7, "00000000030711111111")
	chk("001-2-120", 8, "CLIENTE MAYORISTA SOCIEDAD ANO")
	chk("001-2-120", 19, "2")
	chk("001-2-120", 20, " ")
	chk("001-2-120", 22, "00000000")
	// Consumidor final sin identificar: 99, número 0 y la leyenda.
	chk("006-2-48", 6, "99")
	chk("006-2-48", 7, strings.Repeat("0", 20))
	chk("006-2-48", 8, "CONSUMIDOR FINAL"+strings.Repeat(" ", 14))
	chk("006-2-48", 19, "1")
	// DNI.
	chk("006-2-49", 6, "96")
	chk("006-2-49", 7, "00000000000028111222")
	chk("006-2-49", 8, "PEREZ JOSE MARIA"+strings.Repeat(" ", 14))
	// Tiques agrupados: número hasta y venta global diaria.
	chk("083-5-1001", 5, "00000000000000001050")
	chk("083-5-1001", 8, "VENTA GLOBAL DIARIA"+strings.Repeat(" ", 11))
	// Nota de crédito en positivo.
	chk("003-2-7", 9, "000000000012100")
	// Dólares.
	chk("001-2-121", 17, "DOL")
	chk("001-2-121", 18, "1180500000")
	// Exento: alícuota 0 % y código E; exportación: X.
	chk("001-2-122", 12, "000000000500000")
	chk("001-2-122", 20, "E")
	chk("019-4-15", 20, "X")
	chk("019-4-15", 19, "1")
	// Emisión propia: percepciones separadas (IIBB 300 en el 14, IVA 150 en el 13) y vencimiento.
	chk("001-2-124", 13, "000000000015000")
	chk("001-2-124", 14, "000000000030000")
	chk("001-2-124", 21, "000000000000000")
	chk("001-2-124", 22, "20261130")
	// Otros tributos deducidos del total.
	chk("001-2-125", 21, "000000000003000")

	ali := map[string][]string{}
	for _, a := range alis {
		k := campo(a, CamposVentasAlicuota, 1) + "-" + strings.TrimLeft(campo(a, CamposVentasAlicuota, 2), "0") + "-" + strings.TrimLeft(campo(a, CamposVentasAlicuota, 3), "0")
		ali[k] = append(ali[k], campo(a, CamposVentasAlicuota, 5)+"|"+campo(a, CamposVentasAlicuota, 4)+"|"+campo(a, CamposVentasAlicuota, 6))
	}
	for k, want := range map[string]string{
		"001-2-120": "0004|000000000050000|000000000005250,0005|000000000100000|000000000021000",
		"006-2-48":  "0005|000000000100000|000000000021000",
		"001-2-122": "0003|000000000000000|000000000000000",
		"019-4-15":  "0003|000000000000000|000000000000000",
		"001-2-123": "0004|000000000300000|000000000031500",
	} {
		if got := strings.Join(ali[k], ","); got != want {
			t.Errorf("alícuotas de %s: %q, esperaba %q", k, got, want)
		}
	}
	for codigo, n := range map[string]int{"alicuota_deducida": 1, "desglose_arcanum": 1, "otros_tributos_deducidos": 1, "consumidor_final_identificar": 1, "exportacion": 1, "moneda_extranjera": 2} {
		if g := grupo(r, codigo); g == nil || len(g.Items) != n {
			t.Errorf("%s: %+v", codigo, g)
		}
	}
	if s := r.Resumen; s.CreditoFiscal != 0 || s.Percepciones != m(450) || s.NotasCredito != 1 {
		t.Errorf("resumen de ventas: %+v", s)
	}
}

func TestVentasQueNoVan(t *testing.T) {
	vs := []Venta{
		{Fecha: "2026-11-03", Tipo: 11, PuntoVenta: 1, Numero: 5, DocTipo: 99, Moneda: "PES", Cotizacion: 1, Total: m(1000)},                                                          // C
		{Fecha: "2026-11-03", Tipo: 1, PuntoVenta: 1, Numero: 6, DocTipo: 80, DocNro: "30711111111", Receptor: "X", Moneda: "PES", NetoGravado: m(1000), IVA: m(150), Total: m(1150)}, // ni 21 ni 10,5
		{Fecha: "2026-10-31", Tipo: 1, PuntoVenta: 1, Numero: 7, DocTipo: 80, DocNro: "30711111111", Receptor: "X", Moneda: "PES", NetoGravado: m(1000), IVA: m(210), Total: m(1210)}, // otro mes
		{Fecha: "2026-11-04", Tipo: 1, PuntoVenta: 1, Numero: 8, DocTipo: 80, DocNro: "30711111111", Receptor: "X", Moneda: "PES", NetoGravado: m(1000), IVA: m(210), Total: m(1500),
			OtrosTributos: ip(0), Alicuotas: []AlicuotaVenta{{Codigo: 5, Neto: m(1000), IVA: m(210)}}}, // no cierra
		{Fecha: "2026-11-05", Tipo: 1, PuntoVenta: 1, Numero: 9, DocTipo: 80, DocNro: "30711111112", Receptor: "X", Moneda: "PES", NetoGravado: m(1000), IVA: m(210), Total: m(1210),
			OtrosTributos: ip(0), Alicuotas: []AlicuotaVenta{{Codigo: 5, Neto: m(1000), IVA: m(210)}}, DesgloseDistinto: true}, // CUIT inválido: advertencia
		{Fecha: "2026-11-05", Tipo: 1, PuntoVenta: 1, Numero: 9, DocTipo: 80, DocNro: "30711111111", Receptor: "X", Moneda: "PES", NetoGravado: m(1000), IVA: m(210), Total: m(1210),
			OtrosTributos: ip(0), Alicuotas: []AlicuotaVenta{{Codigo: 5, Neto: m(1000), IVA: m(210)}}}, // duplicado
	}
	r := generarVentas(t, vs, Opciones{})
	for codigo, want := range map[string]int{"tipo_no_admitido": 1, "sin_desglose": 1, "fuera_del_periodo": 1, "importes_no_cierran": 1, "doc_comprador": 1, "desglose_distinto": 1, "duplicado": 1} {
		if g := grupo(r, codigo); g == nil || len(g.Items) != want {
			t.Errorf("%s: %+v", codigo, g)
		}
	}
	if len(r.Registros) != 1 || r.Registros[0].Numero != 9 {
		t.Fatalf("va solo uno de los dos 9: %+v", r.Registros)
	}
}
