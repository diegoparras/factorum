package despacho

import (
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

// Un despacho como lo imprime el SIM (con el texto de la declaración y la liquidación).
const declaracion = `DIRECCION GENERAL DE ADUANAS
DECLARACION DETALLADA - IMPORTACION A CONSUMO
Destinación: 26 001 IC04 123456 K        Aduana: 001 BUENOS AIRES
Fecha de oficialización: 04/11/2026
Importador: 20-22222222-3 GOMEZ MARIA LAURA
LIQUIDACION
Concepto                         Base imponible     Alícuota      Importe
010 DERECHOS DE IMPORTACION         900.000,00        16,00     144.000,00
011 TASA DE ESTADISTICA             900.000,00         3,00      27.000,00
020 IVA                           1.000.000,00        21,00     210.000,00
021 IVA ADICIONAL                 1.000.000,00        20,00     200.000,00
030 IMPUESTO A LAS GANANCIAS      1.000.000,00         6,00      60.000,00
040 INGRESOS BRUTOS               1.000.000,00         2,50      25.000,00
`

func TestLeeUnDespacho(t *testing.T) {
	if !Parece(declaracion) {
		t.Fatal("es un despacho")
	}
	c, h := Leer(declaracion)
	if c.Tipo != modelo.TipoDespacho || c.Despacho != "26001IC04123456K" || c.Fecha != "2026-11-04" {
		t.Fatalf("identidad: %+v", c)
	}
	if c.NetoGravado != 100000000 || c.IVA21 != 21000000 || c.PercepcionIVA != 20000000 || c.PercepcionNacionales != 6000000 || c.PercepcionIIBB != 2500000 {
		t.Fatalf("liquidación: %+v", c)
	}
	if c.OtrosTributos != 28500000 || c.Total != 100000000+21000000+28500000 || c.CUITEmisor != "" {
		t.Fatalf("totales: %+v", c)
	}
	if c.Origen("despacho") != modelo.OrigenTexto || c.Origen("total") != modelo.OrigenCalculo {
		t.Fatalf("orígenes: %v", c.Origenes)
	}
	if len(h) != 1 || h[0].Codigo != CodigoLeido || h[0].Nivel != modelo.NivelAviso || !strings.Contains(h[0].Mensaje, "proveedor del exterior") {
		t.Fatalf("hallazgo: %+v", h)
	}
}

func TestNoInventa(t *testing.T) {
	// Dos números de despacho, dos fechas, dos líneas de IVA y un IVA sin alícuota clara: nada de eso.
	texto := strings.NewReplacer("Fecha de oficialización: 04/11/2026", "Fecha de oficialización: 04/11/2026\nOficializado el 05/11/2026 (rectificada 26001IC04999999Z)",
		"021 IVA ADICIONAL", "022 IVA ADICIONAL                 1.000.000,00        20,00     200.000,00\n021 IVA ADICIONAL",
		"020 IVA                           1.000.000,00        21,00     210.000,00", "020 IVA                           1.000.000,00                  150.000,00").Replace(declaracion)
	c, h := Leer(texto)
	if c.Despacho != "" || c.Fecha != "" || c.PercepcionIVA != 0 || c.IVA21 != 0 || c.NetoGravado != 0 || c.Total != 0 {
		t.Fatalf("no tenía que poner nada dudoso: %+v", c)
	}
	if c.PercepcionNacionales != 6000000 {
		t.Fatalf("lo seguro sí: %+v", c)
	}
	for _, want := range []string{"número de despacho (aparecen 2 distintos)", "fecha de oficialización", "IVA adicional (percepción de IVA) (aparece más de una vez)", "alícuota del IVA"} {
		if !strings.Contains(h[0].Mensaje, want) {
			t.Errorf("el aviso no dice %q: %s", want, h[0].Mensaje)
		}
	}
	// El IVA sin columna de alícuota, pero que da justo el 10,5 % de la base.
	c, _ = Leer(strings.Replace(declaracion, "020 IVA                           1.000.000,00        21,00     210.000,00", "020 IVA    1.000.000,00    105.000,00", 1))
	if c.IVA105 != 10500000 || c.IVA21 != 0 {
		t.Fatalf("10,5 %%: %+v", c)
	}
}

func TestUnaFacturaNoEsUnDespacho(t *testing.T) {
	factura := `FACTURA A  Nro 0002-00001544
Mercadería importada, despacho 26001IC04123456K, oficializado el 04/11/2026 en la Aduana de Buenos Aires
CAE N°: 76451234567890`
	if Parece(factura) {
		t.Fatal("una factura con CAE que menciona el despacho no es un despacho")
	}
	if Parece("Fecha de oficialización 04/11/2026, Aduana 001") {
		t.Fatal("sin número de despacho no es un despacho")
	}
}
