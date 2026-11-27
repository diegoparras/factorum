package libroiva

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

var actualizar = flag.Bool("actualizar", false, "reescribe los golden files de testdata")

const cliente = "20222222223"

func m(v float64) modelo.Importe { return modelo.DesdeFloat(v) }

// juego cubre lo que pide el Libro IVA Digital: A con varias alícuotas (y neto calculado), B, C,
// nota de crédito, tique factura A, tique factura B, tique, moneda extranjera, exento, no gravado,
// gravado con exento, otros tributos, M con 5 % y 2,5 %, y un nombre con acentos y Ñ más largo
// que el campo.
func juego() []Entrada {
	cb := func(id int64, est modelo.Estado, c modelo.Comprobante) Entrada {
		if c.DocNroReceptor == "" && (c.Letra == "A" || c.Letra == "M") {
			c.DocTipoReceptor, c.DocNroReceptor = 80, cliente
		}
		if c.Moneda == "" {
			c.Moneda, c.Cotizacion = "PES", 1
		}
		return Entrada{ID: id, Estado: est, Archivo: "f" + string(rune('a'+id)) + ".pdf", C: c}
	}
	return []Entrada{
		cb(1, modelo.EstadoOK, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1544, Fecha: "2026-11-05", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO SOCIEDAD DE RESPONSABILIDAD LIMITADA", NetoGravado: m(2023.57), IVA21: m(259.26), IVA105: m(82.85), Total: m(2365.68)}),
		cb(2, modelo.EstadoOK, modelo.Comprobante{Tipo: 6, Letra: "B", PuntoVenta: 3, Numero: 20077, Fecha: "2026-11-20", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", Subtotal: m(60500), Total: m(60500)}),
		cb(3, modelo.EstadoOK, modelo.Comprobante{Tipo: 11, Letra: "C", PuntoVenta: 5, Numero: 91, Fecha: "2026-11-15", CUITEmisor: "20123456786",
			RazonSocialEmisor: "PEREZ JUAN CARLOS", DocTipoReceptor: 80, DocNroReceptor: cliente, Subtotal: m(112100), Total: m(112100)}),
		cb(4, modelo.EstadoOK, modelo.Comprobante{Tipo: 3, Letra: "A", PuntoVenta: 2, Numero: 88, Fecha: "2026-11-21", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", NetoGravado: m(10000), IVA21: m(2100), Total: m(12100)}),
		cb(5, modelo.EstadoAprobado, modelo.Comprobante{Tipo: 81, Letra: "A", EsTique: true, PuntoVenta: 4, Numero: 1234, Fecha: "2026-11-20", CUITEmisor: "30715554441",
			RazonSocialEmisor: "LA PARRILLA DE DON JOSE S.A.", NetoGravado: m(40000), IVA21: m(8400), Total: m(48400)}),
		cb(6, modelo.EstadoAprobado, modelo.Comprobante{Tipo: 82, Letra: "B", EsTique: true, PuntoVenta: 5, Numero: 12345, Fecha: "2026-11-21", CUITEmisor: "30712223339",
			RazonSocialEmisor: "SUPERMERCADOS LA ESQUINA S.A.", NetoGravado: m(13997.74), IVA21: m(1155), IVA105: m(892.26), Subtotal: m(16045), Total: m(16045)}),
		cb(7, modelo.EstadoAprobado, modelo.Comprobante{Tipo: 83, EsTique: true, PuntoVenta: 3, Numero: 45678, Fecha: "2026-11-22", CUITEmisor: "30708888881",
			RazonSocialEmisor: "EL TREBOL S.R.L.", Total: m(44062.5)}),
		cb(8, modelo.EstadoAprobado, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1545, Fecha: "2026-11-22", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", Moneda: "DOL", Cotizacion: 1180.5, NetoGravado: m(1000), IVA21: m(210), Total: m(1210)}),
		cb(9, modelo.EstadoOK, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 7, Numero: 300, Fecha: "2026-11-10", CUITEmisor: "30711111111",
			RazonSocialEmisor: "LIBRERÍA ÑANDÚ S.A.", Exento: m(5000), Total: m(5000)}),
		cb(10, modelo.EstadoOK, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 8, Numero: 400, Fecha: "2026-11-11", CUITEmisor: "30711111111",
			RazonSocialEmisor: "Cooperadora “La Unión”", NoGravado: m(3000), Total: m(3000)}),
		cb(11, modelo.EstadoOK, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1546, Fecha: "2026-11-12", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", NetoGravado: m(50000), IVA27: m(13500), OtrosTributos: m(1500), Total: m(65000)}),
		cb(12, modelo.EstadoOK, modelo.Comprobante{Tipo: 51, Letra: "M", PuntoVenta: 9, Numero: 77, Fecha: "2026-11-13", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", NetoGravado: m(10000), IVA5: m(300), IVA25: m(100), Total: m(10400)}),
		cb(13, modelo.EstadoOK, modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1547, Fecha: "2026-11-14", CUITEmisor: "30711111111",
			RazonSocialEmisor: "DISTRIBUIDORA EJEMPLO S.R.L.", NetoGravado: m(1000), IVA21: m(210), Exento: m(500), Total: m(1710)}),
	}
}

func generar(t *testing.T, es []Entrada, o Opciones) *Resultado {
	t.Helper()
	if o.Cliente == "" {
		o.Cliente = cliente
	}
	if o.Periodo == "" {
		o.Periodo = "2026-11"
	}
	r, err := Generar(es, o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func lineas(t *testing.T, b []byte, largo int) []string {
	t.Helper()
	if len(b) == 0 {
		return nil
	}
	if !bytes.HasSuffix(b, []byte("\r\n")) {
		t.Fatalf("el archivo tiene que terminar en CRLF")
	}
	ls := strings.Split(strings.TrimSuffix(string(b), "\r\n"), "\r\n")
	for i, l := range ls {
		if len(l) != largo {
			t.Fatalf("línea %d: %d posiciones, el diseño pide %d: %q", i+1, len(l), largo, l)
		}
		if strings.ContainsAny(l, "\r\n") {
			t.Fatalf("línea %d con un salto suelto", i+1)
		}
		for j := 0; j < len(l); j++ {
			if c := l[j]; (c < 0x20 || c > 0x7e) && c != 0xD1 && c != 0xF1 {
				t.Fatalf("línea %d, posición %d: byte %#x fuera de ASCII y de la Ñ", i+1, j+1, c)
			}
		}
	}
	return ls
}

// campo devuelve el valor del campo n de un registro.
func campo(l string, campos []Campo, n int) string {
	c := campos[n-1]
	return l[c.Desde-1 : c.Hasta]
}

func golden(t *testing.T, nombre string, got []byte) {
	t.Helper()
	ruta := filepath.Join("testdata", nombre)
	if *actualizar {
		if err := os.WriteFile(ruta, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatalf("%v (corré go test ./internal/libroiva -actualizar)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s distinto del golden file.\nquedó:\n%s\nesperaba:\n%s", nombre, LeerLatin1(got), LeerLatin1(want))
	}
}

func TestDisenoDeRegistro(t *testing.T) {
	for nombre, d := range map[string]struct {
		campos []Campo
		largo  int
	}{"comprobantes": {CamposCbte, LargoCbte}, "alícuotas": {CamposAlicuota, LargoAlicuota}} {
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
	// Las posiciones del Anexo I que más se usan, escritas a mano para que un cambio se note.
	for n, want := range map[int][2]int{1: {1, 8}, 7: {55, 74}, 8: {75, 104}, 9: {105, 119}, 17: {225, 227}, 18: {228, 237}, 19: {238, 238}, 20: {239, 239}, 21: {240, 254}, 22: {255, 269}, 25: {311, 325}} {
		if c := CamposCbte[n-1]; c.Desde != want[0] || c.Hasta != want[1] {
			t.Errorf("comprobantes, campo %d: %d-%d, el diseño dice %d-%d", n, c.Desde, c.Hasta, want[0], want[1])
		}
	}
	for n, want := range map[int][2]int{3: {9, 28}, 5: {31, 50}, 6: {51, 65}, 7: {66, 69}, 8: {70, 84}} {
		if c := CamposAlicuota[n-1]; c.Desde != want[0] || c.Hasta != want[1] {
			t.Errorf("alícuotas, campo %d: %d-%d, el diseño dice %d-%d", n, c.Desde, c.Hasta, want[0], want[1])
		}
	}
}

func TestGoldenFiles(t *testing.T) {
	r := generar(t, juego(), Opciones{})
	if len(r.Registros) != 13 || r.Excluidos != 0 {
		t.Fatalf("tienen que ir los 13: van %d, afuera %d: %+v", len(r.Registros), r.Excluidos, r.Grupos)
	}
	for _, g := range r.Grupos {
		if g.Nivel == NivelBloquea || g.Nivel == NivelFuerte {
			t.Fatalf("el juego no tiene que tener problemas que bloqueen: %+v", g)
		}
	}
	golden(t, "compras_cbte.golden", r.Comprobantes)
	golden(t, "compras_alicuotas.golden", r.Alicuotas)
	cbs := lineas(t, r.Comprobantes, LargoCbte)
	alis := lineas(t, r.Alicuotas, LargoAlicuota)
	if len(cbs) != 13 {
		t.Fatalf("13 registros de comprobantes: %d", len(cbs))
	}

	// Cada comprobante tiene en el archivo de alícuotas tantos registros como dice su campo 19, en
	// el mismo orden, con el mismo tipo, punto de venta, número y vendedor.
	k := 0
	for _, l := range cbs {
		n := int(campo(l, CamposCbte, 19)[0] - '0')
		for range n {
			a := alis[k]
			k++
			if campo(a, CamposAlicuota, 1) != campo(l, CamposCbte, 2) || campo(a, CamposAlicuota, 2) != campo(l, CamposCbte, 3) ||
				campo(a, CamposAlicuota, 3) != campo(l, CamposCbte, 4) || campo(a, CamposAlicuota, 4) != campo(l, CamposCbte, 6) ||
				campo(a, CamposAlicuota, 5) != campo(l, CamposCbte, 7) {
				t.Fatalf("la alícuota %q no es del comprobante %q", a, l)
			}
		}
	}
	if k != len(alis) {
		t.Fatalf("sobran registros de alícuotas: %d de %d", len(alis)-k, len(alis))
	}

	por := map[string]string{} // tipo-pv-número -> línea
	for _, l := range cbs {
		por[campo(l, CamposCbte, 2)+"-"+strings.TrimLeft(campo(l, CamposCbte, 3), "0")+"-"+strings.TrimLeft(campo(l, CamposCbte, 4), "0")] = l
	}
	chk := func(clave string, n int, want string) {
		t.Helper()
		l, ok := por[clave]
		if !ok {
			t.Fatalf("no está %s", clave)
		}
		if got := campo(l, CamposCbte, n); got != want {
			t.Errorf("%s, campo %d (%s): %q, esperaba %q", clave, n, CamposCbte[n-1].Nombre, got, want)
		}
	}
	// A con dos alícuotas: crédito fiscal = IVA total, dos registros, PES con tipo de cambio 1.
	chk("001-2-1544", 1, "20261105")
	chk("001-2-1544", 5, "0000000000000000")
	chk("001-2-1544", 6, "80")
	chk("001-2-1544", 7, "00000000030711111111")
	chk("001-2-1544", 8, "DISTRIBUIDORA EJEMPLO SOCIEDAD")
	chk("001-2-1544", 9, "000000000236568")
	chk("001-2-1544", 17, "PES")
	chk("001-2-1544", 18, "0001000000")
	chk("001-2-1544", 19, "2")
	chk("001-2-1544", 20, " ")
	chk("001-2-1544", 21, "000000000034211")
	chk("001-2-1544", 23, "00000000000")
	chk("001-2-1544", 24, strings.Repeat(" ", 30))
	// B, C y tiques B o sin letra: sin alícuotas, crédito fiscal 0, solo el total.
	for _, c := range []string{"006-3-20077", "011-5-91", "082-5-12345", "083-3-45678"} {
		chk(c, 19, "0")
		chk(c, 20, " ")
		chk(c, 21, "000000000000000")
		chk(c, 10, "000000000000000")
	}
	chk("082-5-12345", 9, "000000001604500")
	// Nota de crédito: en positivo (el tipo dice que resta).
	chk("003-2-88", 9, "000000001210000")
	chk("003-2-88", 21, "000000000210000")
	// Tique factura A: discrimina, con su alícuota y su crédito fiscal.
	chk("081-4-1234", 19, "1")
	chk("081-4-1234", 21, "000000000840000")
	// Dólares: importes en la moneda original, código DOL y la cotización con 6 decimales.
	chk("001-2-1545", 17, "DOL")
	chk("001-2-1545", 18, "1180500000")
	chk("001-2-1545", 9, "000000000121000")
	// Exento: una alícuota 0 % con neto 0, código de operación E y el importe en el campo 11.
	chk("001-7-300", 11, "000000000500000")
	chk("001-7-300", 19, "1")
	chk("001-7-300", 20, "E")
	chk("001-7-300", 21, "000000000000000")
	chk("001-7-300", 8, "LIBRERIA \xd1ANDU S.A."+strings.Repeat(" ", 11))
	// No gravado: campo 10 y código N.
	chk("001-8-400", 10, "000000000300000")
	chk("001-8-400", 20, "N")
	chk("001-8-400", 8, `Cooperadora "La Union"`+strings.Repeat(" ", 8))
	// Otros tributos: en el campo 22, nunca como percepción.
	chk("001-2-1546", 22, "000000000150000")
	for n := 12; n <= 16; n++ {
		chk("001-2-1546", n, "000000000000000")
	}
	// Gravado con exento: código de operación en blanco (hay alícuota distinta de 0).
	chk("001-2-1547", 20, " ")
	chk("001-2-1547", 11, "000000000050000")

	// Las alícuotas, por código.
	ali := map[string][]string{}
	for _, a := range alis {
		k := campo(a, CamposAlicuota, 1) + "-" + strings.TrimLeft(campo(a, CamposAlicuota, 2), "0") + "-" + strings.TrimLeft(campo(a, CamposAlicuota, 3), "0")
		ali[k] = append(ali[k], campo(a, CamposAlicuota, 7)+"|"+campo(a, CamposAlicuota, 6)+"|"+campo(a, CamposAlicuota, 8))
	}
	for k, want := range map[string]string{
		// 259,26 / 0,21 = 1234,57 y 82,85 / 0,105 = 789,05 suman 2023,62: los 5 centavos de más salen de la mayor.
		"001-2-1544":  "0004|000000000078905|000000000008285,0005|000000000123452|000000000025926",
		"001-7-300":   "0003|000000000000000|000000000000000",
		"001-8-400":   "0003|000000000000000|000000000000000",
		"001-2-1546":  "0006|000000005000000|000000001350000",
		"051-9-77":    "0008|000000000600000|000000000030000,0009|000000000400000|000000000010000",
		"081-4-1234":  "0005|000000004000000|000000000840000",
		"003-2-88":    "0005|000000001000000|000000000210000",
		"001-2-1547":  "0005|000000000100000|000000000021000",
		"001-2-1545":  "0005|000000000100000|000000000021000",
		"006-3-20077": "",
	} {
		if got := strings.Join(ali[k], ","); got != want {
			t.Errorf("alícuotas de %s: %q, esperaba %q", k, got, want)
		}
	}
}

func TestResumen(t *testing.T) {
	r := generar(t, juego(), Opciones{})
	s := r.Resumen
	if s.Comprobantes != 13 || s.NotasCredito != 1 || s.SinCreditoFiscal != 4 || s.LineasAlicuotas != 11 {
		t.Fatalf("cantidades: %+v", s)
	}
	// En pesos, la nota de crédito resta y los dólares van aparte.
	iva := m(259.26+82.85) - m(2100) + m(8400) + m(13500) + m(300+100) + m(210)
	if s.IVA != iva || s.CreditoFiscal != iva {
		t.Errorf("IVA %s y crédito fiscal %s, esperaba %s", s.IVA.AR(), s.CreditoFiscal.AR(), iva.AR())
	}
	total := m(2365.68 + 60500 + 112100 - 12100 + 48400 + 16045 + 44062.5 + 5000 + 3000 + 65000 + 10400 + 1710)
	if s.Total != total || s.Exento != m(5500) || s.NoGravado != m(3000) || s.OtrosTributos != m(1500) {
		t.Errorf("totales: %+v (total esperado %s)", s, total.AR())
	}
	if len(s.Extranjera) != 1 || s.Extranjera[0].Moneda != "DOL" || s.Extranjera[0].Total != m(1210) || s.Extranjera[0].CreditoFiscal != m(210) {
		t.Errorf("moneda extranjera: %+v", s.Extranjera)
	}
	got := map[string]TotalAlicuota{}
	for _, a := range s.PorAlicuota {
		got[a.Codigo] = a
	}
	if a := got["0005"]; a.IVA != m(259.26+8400-2100+210) || a.Neto != m(1234.52+40000-10000+1000) || a.Lineas != 4 {
		t.Errorf("21 %%: %+v", a)
	}
	if a := got["0003"]; a.Lineas != 2 || a.Neto != 0 {
		t.Errorf("0 %%: %+v", a)
	}
	if s.PorAlicuota[0].Codigo != "0003" {
		t.Errorf("las alícuotas van en el orden de su código: %+v", s.PorAlicuota)
	}
	var neto modelo.Importe
	for _, a := range s.PorAlicuota {
		neto += a.Neto
	}
	if neto != s.NetoGravado {
		t.Errorf("el neto por alícuota (%s) tiene que sumar el neto gravado (%s)", neto.AR(), s.NetoGravado.AR())
	}
}

func grupo(r *Resultado, codigo string) *Grupo {
	for i := range r.Grupos {
		if r.Grupos[i].Codigo == codigo {
			return &r.Grupos[i]
		}
	}
	return nil
}

func ids(g *Grupo) []int64 {
	if g == nil {
		return nil
	}
	var r []int64
	for _, it := range g.Items {
		r = append(r, it.ID)
	}
	return r
}

func TestProblemasQueBloquean(t *testing.T) {
	base := modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Fecha: "2026-11-05", CUITEmisor: "30711111111", RazonSocialEmisor: "X SA",
		NetoGravado: m(1000), IVA21: m(210), Total: m(1210), Moneda: "PES", Cotizacion: 1, DocNroReceptor: cliente}
	casos := map[string]func(c *modelo.Comprobante){
		"sin_cuit_emisor":       func(c *modelo.Comprobante) { c.CUITEmisor = "30711111112" },
		"sin_fecha":             func(c *modelo.Comprobante) { c.Fecha = "" },
		"fuera_del_periodo":     func(c *modelo.Comprobante) { c.Fecha = "2026-10-31" },
		"tipo_no_admitido":      func(c *modelo.Comprobante) { c.Tipo, c.Letra = 19, "E" },
		"sin_numero":            func(c *modelo.Comprobante) { c.PuntoVenta = 0 },
		"sin_total":             func(c *modelo.Comprobante) { c.Total, c.NetoGravado, c.IVA21 = 0, 0, 0 },
		"importe_negativo":      func(c *modelo.Comprobante) { c.OtrosTributos = -m(5); c.Total -= m(5) },
		"moneda_desconocida":    func(c *modelo.Comprobante) { c.Moneda = "XYZ" },
		"moneda_sin_cotizacion": func(c *modelo.Comprobante) { c.Moneda, c.Cotizacion = "DOL", 0 },
		"neto_sin_iva":          func(c *modelo.Comprobante) { c.IVA21 = 0; c.Total = m(1000) },
		"iva_sin_neto":          func(c *modelo.Comprobante) { c.NetoGravado = 0; c.Total = m(210) },
		"importes_no_cierran":   func(c *modelo.Comprobante) { c.Total = m(1121) },
		"alicuotas_no_cierran":  func(c *modelo.Comprobante) { c.NetoGravado = m(2000); c.IVA105 = m(66.45); c.Total = m(2276.45) },
		"no_entra": func(c *modelo.Comprobante) {
			c.NetoGravado, c.IVA21 = MaxImporte, MaxImporte/100*21
			c.Total = c.NetoGravado + c.IVA21 // no entra en 13 enteros y 2 decimales
		},
	}
	var es []Entrada
	var n int64
	cod := map[int64]string{}
	for k, f := range casos {
		n++
		c := base
		c.Numero = 100 + n
		f(&c)
		es = append(es, Entrada{ID: n, Estado: modelo.EstadoAprobado, C: c})
		cod[n] = k
	}
	r := generar(t, es, Opciones{})
	if len(r.Registros) != 0 || r.Excluidos != len(es) || r.CantidadTodos != 0 || len(r.Comprobantes) != 0 || len(r.Alicuotas) != 0 {
		t.Fatalf("ninguno tiene que ir: van %d, afuera %d", len(r.Registros), r.Excluidos)
	}
	for id, k := range cod {
		g := grupo(r, k)
		found := false
		for _, x := range ids(g) {
			found = found || x == id
		}
		if !found {
			t.Errorf("%s: el %d no aparece (%+v)", k, id, r.Grupos)
			continue
		}
		if g.Nivel != NivelBloquea || !g.Items[0].Excluido {
			t.Errorf("%s: tiene que bloquear: %+v", k, g)
		}
	}
	if g := grupo(r, "fuera_del_periodo"); g == nil || !strings.Contains(g.Items[0].Detalle, "31/10/2026") {
		t.Errorf("fuera del período dice la fecha: %+v", g)
	}
}

func TestDuplicadosVaUnoSolo(t *testing.T) {
	c := juego()[0].C
	es := []Entrada{
		{ID: 10, Estado: modelo.EstadoRevisar, C: c},
		{ID: 11, Estado: modelo.EstadoAprobado, C: c},
		{ID: 12, Estado: modelo.EstadoOK, C: c},
	}
	r := generar(t, es, Opciones{})
	if len(r.Registros) != 1 || r.Registros[0].ID != 11 {
		t.Fatalf("va la aprobada: %+v", r.Registros)
	}
	if got := ids(grupo(r, "duplicado")); len(got) != 2 || got[0] != 10 || got[1] != 12 {
		t.Fatalf("duplicados: %v", got)
	}
	if grupo(r, "sin_aprobar") != nil {
		t.Errorf("la copia que sobra no se informa como sin aprobar: %+v", grupo(r, "sin_aprobar"))
	}
}

func TestSoloAprobadosYAdvertencias(t *testing.T) {
	es := juego()
	es[0].Estado = modelo.EstadoRevisar // A, para revisar
	es[1].Estado = modelo.EstadoError   // B, con errores
	es[2].Constatacion = "apocrifo"     // C, apócrifo
	es[3].Constatacion = "invalido"     // NC, ARCA no la reconoce
	es[4].C.DocNroReceptor = "30500000001"
	es[5].C.DocNroReceptor = "30500000001" // tique factura B a otro CUIT: advertencia, no fuerte
	k := &Conciliacion{NoFigura: map[int64]string{9: "Sin explicación"}, Alarma: map[int64]bool{10: true}, Diferencias: map[int64]string{11: "Total: ARCA 60.000,00"}}

	todos := generar(t, es, Opciones{Conciliacion: k})
	listos := generar(t, es, Opciones{Conciliacion: k, SoloAprobados: true})
	if len(todos.Registros) != 13 || todos.CantidadTodos != 13 || todos.CantidadListos != 7 {
		t.Fatalf("todos: van %d, todos %d, listos %d", len(todos.Registros), todos.CantidadTodos, todos.CantidadListos)
	}
	if len(listos.Registros) != 7 || listos.Excluidos != 6 || listos.CantidadListos != 7 {
		t.Fatalf("solo los listos: van %d, afuera %d", len(listos.Registros), listos.Excluidos)
	}
	va := map[int64]bool{}
	for _, x := range listos.Registros {
		va[x.ID] = true
	}
	for _, id := range []int64{1, 2, 3, 4, 5, 10} {
		if va[id] {
			t.Errorf("con solo los listos no va el %d", id)
		}
	}
	if !va[6] || !va[9] || !va[11] {
		t.Errorf("con solo los listos van el tique a otro CUIT (advertencia), el que no figura en ARCA y el que tiene diferencias: %v", va)
	}
	for codigo, want := range map[string][]int64{
		"sin_aprobar": {1, 2}, "apocrifo": {3, 10}, "invalido": {4}, "otro_receptor_a": {5}, "otro_receptor": {6},
		"no_figura_arca": {9}, "diferencia_arca": {11},
	} {
		got := ids(grupo(todos, codigo))
		if len(got) != len(want) {
			t.Errorf("%s: %v, esperaba %v", codigo, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				// los ítems van por fecha: comparamos como conjunto
				gs := map[int64]bool{}
				for _, x := range got {
					gs[x] = true
				}
				for _, w := range want {
					if !gs[w] {
						t.Errorf("%s: %v, esperaba %v", codigo, got, want)
					}
				}
				break
			}
		}
	}
	if g := grupo(todos, "apocrifo"); g.Nivel != NivelFuerte || g.Items[0].Excluido {
		t.Errorf("con todos, el apócrifo va con advertencia fuerte: %+v", g)
	}
	if g := grupo(listos, "apocrifo"); !g.Items[0].Excluido {
		t.Errorf("con solo los listos, el apócrifo queda afuera: %+v", g)
	}
	// Los niveles van en orden: lo que bloquea, lo fuerte, las advertencias y los datos.
	ult := -1
	for _, g := range todos.Grupos {
		if ordenNivel[g.Nivel] < ult {
			t.Fatalf("grupos fuera de orden: %+v", todos.Grupos)
		}
		ult = ordenNivel[g.Nivel]
	}
	if g := grupo(todos, "otros_tributos"); g == nil || ids(g)[0] != 11 {
		t.Errorf("otros tributos sin detalle: %+v", g)
	}
	if g := grupo(todos, "moneda_extranjera"); g == nil || ids(g)[0] != 8 || !strings.Contains(g.Ayuda, "Moneda Original") {
		t.Errorf("moneda extranjera: %+v", g)
	}
	if g := grupo(todos, "neto_calculado"); g == nil || len(g.Items) != 2 {
		t.Errorf("neto calculado en las dos con varias alícuotas: %+v", g)
	}
}

func TestOpcionesInvalidas(t *testing.T) {
	for _, o := range []Opciones{{Cliente: "123", Periodo: "2026-11"}, {Cliente: cliente, Periodo: "2026-13"}, {Cliente: cliente, Periodo: "11/2026"}, {Cliente: cliente}} {
		if _, err := Generar(nil, o); err == nil {
			t.Errorf("%+v: tiene que dar error", o)
		}
	}
	d, h, err := ValidarPeriodo("2024-02")
	if err != nil || d != "2024-02-01" || h != "2024-02-29" {
		t.Fatalf("febrero bisiesto: %s %s %v", d, h, err)
	}
	r := generar(t, nil, Opciones{})
	if len(r.Comprobantes) != 0 || len(r.Registros) != 0 || r.Resumen.Total != 0 || r.Grupos == nil {
		t.Fatalf("sin comprobantes: %+v", r)
	}
}

func TestRedondeoYIVAQueNoDa(t *testing.T) {
	c := juego()[0].C
	c.Total += 1 // un centavo
	d := juego()[3].C
	d.IVA21 = m(2000) // 20 % en vez de 21
	d.Total = m(12000)
	r := generar(t, []Entrada{{ID: 1, Estado: modelo.EstadoOK, C: c}, {ID: 2, Estado: modelo.EstadoOK, C: d}}, Opciones{})
	if len(r.Registros) != 2 {
		t.Fatalf("los dos van: %+v", r.Grupos)
	}
	if g := grupo(r, "redondeo"); g == nil || ids(g)[0] != 1 || g.Nivel != NivelAdvertencia {
		t.Errorf("redondeo: %+v", g)
	}
	if g := grupo(r, "iva_no_cierra"); g == nil || ids(g)[0] != 2 {
		t.Errorf("IVA que no da: %+v", g)
	}
}

func TestZip(t *testing.T) {
	r := generar(t, juego(), Opciones{})
	var buf bytes.Buffer
	if err := r.Zip(&buf, "GOMEZ MARIA LAURA", time.Date(2026, 11, 27, 10, 30, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var nombres []string
	contenido := map[string][]byte{}
	for _, f := range z.File {
		nombres = append(nombres, f.Name)
		rc, _ := f.Open()
		var b bytes.Buffer
		_, _ = b.ReadFrom(rc)
		rc.Close()
		contenido[f.Name] = b.Bytes()
	}
	if strings.Join(nombres, ",") != NombreCbte+","+NombreAlicuotas+","+NombreResumen {
		t.Fatalf("archivos: %v", nombres)
	}
	if !bytes.Equal(contenido[NombreCbte], r.Comprobantes) || !bytes.Equal(contenido[NombreAlicuotas], r.Alicuotas) {
		t.Fatal("los .txt del ZIP tienen que ser los generados")
	}
	csv := string(contenido[NombreResumen])
	for _, want := range []string{"\xef\xbb\xbfLibro IVA Digital: compras", "20-22222222-3 GOMEZ MARIA LAURA", "Período;2026-11", "Crédito fiscal computable",
		"21 %;0005;4;", "Moneda Original", "-12100,00", "LIBRERÍA ÑANDÚ S.A.", "DOL;1;210,00;210,00;1210,00"} {
		if !strings.Contains(csv, want) {
			t.Errorf("el resumen no tiene %q:\n%s", want, csv)
		}
	}
}

// Una copia con un error de lectura (la foto del mismo ticket) no tapa a la que está bien: va la
// buena y la otra figura solo como duplicada.
func TestDuplicadoConUnaCopiaMalLeida(t *testing.T) {
	bien := juego()[4].C
	mal := bien
	mal.IVA21 = 0 // la foto leyó mal el IVA
	r := generar(t, []Entrada{{ID: 1, Estado: modelo.EstadoError, C: mal}, {ID: 2, Estado: modelo.EstadoAprobado, C: bien}}, Opciones{})
	if len(r.Registros) != 1 || r.Registros[0].ID != 2 {
		t.Fatalf("va la que está bien: %+v", r.Registros)
	}
	if len(r.Grupos) != 1 || r.Grupos[0].Codigo != "duplicado" || r.Grupos[0].Items[0].ID != 1 {
		t.Fatalf("la mal leída figura solo como duplicada: %+v", r.Grupos)
	}
	// Las dos mal: la primera muestra sus problemas.
	r = generar(t, []Entrada{{ID: 1, Estado: modelo.EstadoError, C: mal}, {ID: 2, Estado: modelo.EstadoError, C: mal}}, Opciones{})
	if len(r.Registros) != 0 || !strings.Contains(fmtGrupos(r), "neto_sin_iva:1") || !strings.Contains(fmtGrupos(r), "duplicado:2") {
		t.Fatalf("las dos mal: %s", fmtGrupos(r))
	}
}

func fmtGrupos(r *Resultado) string {
	var p []string
	for _, g := range r.Grupos {
		for _, it := range g.Items {
			p = append(p, fmt.Sprintf("%s:%d", g.Codigo, it.ID))
		}
	}
	return strings.Join(p, " ")
}

func TestLaClaveDelDuplicadoUsaElCUITLimpio(t *testing.T) {
	c := juego()[0].C
	d := c
	d.CUITEmisor = arca.FormatearCUIT(c.CUITEmisor)
	r := generar(t, []Entrada{{ID: 1, Estado: modelo.EstadoOK, C: c}, {ID: 2, Estado: modelo.EstadoOK, C: d}}, Opciones{})
	if len(r.Registros) != 1 {
		t.Fatalf("con o sin guiones, es el mismo comprobante: %d", len(r.Registros))
	}
}
