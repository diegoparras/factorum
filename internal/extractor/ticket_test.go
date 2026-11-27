package extractor

import (
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/rubro"
)

// Textos como los que devuelve Tesseract con fotos de tickets (con su ruido).
const (
	ocrSuper = `LA ECONOMIA
SUPERMERCADOS LA ECONOMIA S.A
Av. Rivadavia 5200 - CABA
C.U.1.T. Nro.: 30-71222333-9
IVA RESPONSABLE INSCRIPTO
Ing. Brutos: 901-555222-1
Início de actividades: 01/03/2012
TIQUE FACTURA 8 Nro 0005-00012345
Fecha: 21/11/2026 Hora: 13:45:10
A CONSUMIDOR FIMAL
LECHE ENTERA 11 (10,50)

2 x 1.150,00 2.300,00
PAN LACTAL 5506 (10,50) 2.890,00
GASEOSA COLA 2,25L (21,00)
SUBTOTAL 16.045,00
NETO GRAVADO 13.997,74
IVA 21,00% 1.155,00
IVA 10,50% 892,26
TOTAL $ 16.045,00
RECIBIMOS EFECTIVO 20.000,00
VUELTO 3.955,00
GRACIAS POR SU COMPRA
CAJA 03 CAJERO 117 P.V. 0005
REG. CF: AB 12345678`

	ocrYPF = `YPF
EST. DE SERVICIO EL TREBOL
ESTACION DE SERVICIO
EL TREBOL S.R.L.
Ruta 8 Km 54 - Pilar
CUIT: 30-70888888-1
IVA A EA INSCRIPTO

TIQUE
P.V. Nro.: 0003
Nro, T.: 00045678
Fecha: 22/11/26 Mora: 08:12:45
CF: A CONSUMIDOR FIMAL

NAFTA SUPER (21,00)
35,250 L x 1,250,009 44.062,50

TOTAL $44.062, 50
IVA CONTEMIDO 7.647,13
TARJETA DEBITO 44.062, sd`

	// Ruido de más: O por 0 en los números, TOTAL con puntos, espacios dentro del importe, "$" leído
	// como S, la letra de la factura en otra línea y el importe separado de su etiqueta.
	ocrRuidoso = `FARMACIA DEL PUEBLO
de GONZALEZ ANA MARIA
Belgrano 120 - Lujan
C.U.I.T. Mro.: 27-2727272 7-4
RESPONSABLE MONOTRIBUTO
TIQUE FACTURA €
Nro OOO2-OOOOO789
Fecha: 23/11/2O26     19:03:22
IBUPROFENO 400 X10
1 x 3,450,00        3.450,00
T.O.T.A.L.......... S 5.55O,0O
RECIBI (MOS)           10.000,00
SU VUELTO              4.450,00`

	ocrTFA = `LA PARRILLA DE DON TITO S.R.L.
Av. Cabildo 2100 - CABA
CUIT: 30-71555444-1
IVA RESPONSABLE INSCRIPTO
TIQUE FACTURA A               P.V. Nro.: 0004
Nro. T.: 00001234
Fecha: 20/11/26                Hora: 21:30:05
GOMEZ MARIA LAURA
CUIT: 20-22222222-3
IVA RESPONSABLE INSCRIPTO
BIFE DE CHORIZO (21,00)
  2 x 15.000,00                    30.000,00
SUBTOTAL                           40.000,00
NETO GRAVADO                       40.000,00
IVA 21%
8.400,00
T O T A L                       $ 48.400,00
TARJETA CREDITO VISA               48.400,00`
)

func TestParsearTicket(t *testing.T) {
	casos := []struct {
		nombre  string
		txt     string
		quiero  modelo.Comprobante
		origTip modelo.Origen
	}{
		{"super", ocrSuper, modelo.Comprobante{Tipo: 82, Letra: "B", PuntoVenta: 5, Numero: 12345, Fecha: "2026-11-21", Hora: "13:45:10",
			CUITEmisor: "30712223339", RazonSocialEmisor: "SUPERMERCADOS LA ECONOMIA S.A", CondicionIVAEmisor: "IVA Responsable Inscripto",
			DocTipoReceptor: 99, NetoGravado: 1399774, IVA21: 115500, IVA105: 89226, Subtotal: 1604500, Total: 1604500, Rubro: rubro.Supermercado}, modelo.OrigenOCR},
		{"ypf", ocrYPF, modelo.Comprobante{Tipo: 83, PuntoVenta: 3, Numero: 45678, Fecha: "2026-11-22", Hora: "08:12:45",
			CUITEmisor: "30708888881", RazonSocialEmisor: "EL TREBOL S.R.L.", DocTipoReceptor: 99, IVAContenido: 764713, Total: 4406250, Rubro: rubro.Combustible}, modelo.OrigenOCR},
		{"ruidoso", ocrRuidoso, modelo.Comprobante{Tipo: 111, Letra: "C", PuntoVenta: 2, Numero: 789, Fecha: "2026-11-23", Hora: "19:03:22",
			CUITEmisor: "27272727274", RazonSocialEmisor: "GONZALEZ ANA MARIA", CondicionIVAEmisor: "Responsable Monotributo", Total: 555000, Rubro: rubro.Farmacia}, modelo.OrigenOCR},
		{"tfa", ocrTFA, modelo.Comprobante{Tipo: 81, Letra: "A", PuntoVenta: 4, Numero: 1234, Fecha: "2026-11-20", Hora: "21:30:05",
			CUITEmisor: "30715554441", RazonSocialEmisor: "LA PARRILLA DE DON TITO S.R.L.", CondicionIVAEmisor: "IVA Responsable Inscripto",
			DocTipoReceptor: 80, DocNroReceptor: "20222222223", RazonSocialReceptor: "GOMEZ MARIA LAURA",
			NetoGravado: 4000000, IVA21: 840000, Subtotal: 4000000, Total: 4840000, Rubro: rubro.Gastronomia}, modelo.OrigenTexto},
	}
	for _, k := range casos {
		t.Run(k.nombre, func(t *testing.T) {
			if !PareceTicket(k.txt) {
				t.Fatal("tenía que parecer un ticket")
			}
			c := ParsearTicket(k.txt, k.origTip)
			q := k.quiero
			chk := func(campo string, got, want any) {
				t.Helper()
				if got != want {
					t.Errorf("%s = %v, quería %v", campo, got, want)
				}
			}
			chk("es_tique", c.EsTique, true)
			chk("tipo", c.Tipo, q.Tipo)
			chk("letra", c.Letra, q.Letra)
			chk("punto_venta", c.PuntoVenta, q.PuntoVenta)
			chk("numero", c.Numero, q.Numero)
			chk("fecha", c.Fecha, q.Fecha)
			chk("hora", c.Hora, q.Hora)
			chk("cuit_emisor", c.CUITEmisor, q.CUITEmisor)
			chk("razon_social_emisor", c.RazonSocialEmisor, q.RazonSocialEmisor)
			chk("condicion_iva_emisor", c.CondicionIVAEmisor, q.CondicionIVAEmisor)
			chk("doc_tipo_receptor", c.DocTipoReceptor, q.DocTipoReceptor)
			chk("doc_nro_receptor", c.DocNroReceptor, q.DocNroReceptor)
			chk("razon_social_receptor", c.RazonSocialReceptor, q.RazonSocialReceptor)
			chk("neto_gravado", c.NetoGravado, q.NetoGravado)
			chk("iva_21", c.IVA21, q.IVA21)
			chk("iva_10_5", c.IVA105, q.IVA105)
			chk("iva_contenido", c.IVAContenido, q.IVAContenido)
			chk("subtotal", c.Subtotal, q.Subtotal)
			chk("total", c.Total, q.Total)
			chk("rubro", c.Rubro, q.Rubro)
			chk("cae", c.CAE, "")
			// Cada dato que se cargó dice de dónde salió; el pago y el vuelto no se toman como total.
			for _, campo := range []string{"fecha", "cuit_emisor", "total", "rubro"} {
				chk("origen "+campo, c.Origen(campo), k.origTip)
			}
			if c.Origen("cae") != "" || c.Origen("iva_27") != "" {
				t.Errorf("se marcaron campos que no están: %v", c.Origenes)
			}
		})
	}
}

// Nunca se inventa: un ticket sin datos legibles devuelve campos vacíos (salvo el tipo 83 por
// defecto, marcado como cálculo).
func TestParsearTicketNoInventa(t *testing.T) {
	c := ParsearTicket("T1QUE\nXXXX ilegible\nT0TAL\nVUELTO 100,00\nRECIBI 500,00", modelo.OrigenOCR)
	if c.Total != 0 || c.CUITEmisor != "" || c.Fecha != "" || c.Numero != 0 || c.Rubro != "" {
		t.Fatalf("inventó datos: %+v", c)
	}
	if c.Tipo != 83 || c.Origen("tipo") != modelo.OrigenOCR {
		t.Fatalf("tipo %d (%s)", c.Tipo, c.Origen("tipo"))
	}
	c = ParsearTicket("COMERCIO X\nCUIT: 30-71222333-9\nTOTAL $ 1.000,00\nVUELTO 0,00", modelo.OrigenOCR)
	if c.Tipo != 83 || c.Origen("tipo") != modelo.OrigenCalculo || c.Total != 100000 {
		t.Fatalf("%+v", c)
	}
}

func TestPareceTicket(t *testing.T) {
	no := []string{
		// Una factura electrónica de "Comprobantes en línea", aunque sea a consumidor final.
		`ORIGINAL
B
FACTURA
COD. 06
Punto de Venta: 00003 Comp. Nro: 00020077
Fecha de Emisión: 20/11/2026
CUIT: 30711111111
Condición frente al IVA: Consumidor Final
Importe Total: $ 60500,00
CAE N°: 76451111122223
Fecha de Vto. de CAE: 30/11/2026`,
		// Un supermercado que manda factura electrónica B por mail: trae CAE.
		`SUPERMERCADOS LA ECONOMIA S.A.
FACTURA B Nro 0005-00012345
Fecha: 21/11/2026 Hora: 13:45
TOTAL $ 16.045,00
CAE: 76451111122223 Vto: 01/12/2026`,
		"hola, esto no es nada",
		"",
	}
	for _, s := range no {
		if PareceTicket(s) {
			t.Errorf("no es un ticket: %.60q", s)
		}
	}
	si := []string{ocrSuper, ocrYPF, ocrRuidoso, ocrTFA,
		"KIOSCO\nP.V. Nro.: 0001 Nro. T.: 00000012\nTOTAL 500,00",
		"BAR\nFecha 01/02/26 Hora 10:11\nTOTAL 1.500,00\nSU VUELTO 500,00\nCAJA 1",
	}
	for _, s := range si {
		if !PareceTicket(s) {
			t.Errorf("es un ticket: %.60q", s)
		}
	}
}

func TestMontosOCR(t *testing.T) {
	casos := map[string]modelo.Importe{
		"$ 16.045,00":        1604500,
		"$44.062, 50":        4406250,
		"$44 .062,50":        4406250,
		" 1 155,00":          115500,
		"S 5.55O,0O":         555000,
		"......... 892,26":   89226,
		"1,234.50":           123450,
		"20.000,00 3.955,00": 395500, // el último de la línea
	}
	for s, quiero := range casos {
		ms := montosOCR(s)
		if len(ms) == 0 || ms[len(ms)-1] != quiero {
			t.Errorf("montosOCR(%q) = %v, quería %v", s, ms, quiero)
		}
	}
	if ms := montosOCR("44.062, sd"); len(ms) != 0 {
		t.Errorf("no tenía que leer nada: %v", ms)
	}
}
