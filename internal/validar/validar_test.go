package validar

import (
	"testing"
	"time"

	"github.com/diegoparras/contadorfacturas/internal/modelo"
)

var hoy = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func base() modelo.Comprobante {
	return modelo.Comprobante{
		Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: 1543, Fecha: "2026-11-03",
		CUITEmisor: "30711111111", RazonSocialEmisor: "X", DocTipoReceptor: 80, DocNroReceptor: "20222222223",
		NetoGravado: 10000000, IVA21: 2100000, Total: 12100000, CAE: "76451234567890", VtoCAE: "2026-11-13", Moneda: "PES",
	}
}

func codigos(h []modelo.Hallazgo) map[string]bool {
	m := map[string]bool{}
	for _, x := range h {
		m[x.Codigo] = true
	}
	return m
}

func TestComprobanteOK(t *testing.T) {
	c := base()
	h := Comprobante(&c, hoy)
	if len(h) != 0 {
		t.Fatalf("no debería haber hallazgos: %+v", h)
	}
	if Estado(&c, h) != modelo.EstadoOK {
		t.Fatal("estado")
	}
}

func TestControles(t *testing.T) {
	casos := []struct {
		nombre string
		mod    func(*modelo.Comprobante)
		cod    string
		estado modelo.Estado
	}{
		{"suma", func(c *modelo.Comprobante) { c.Total = 11210000 }, "suma_no_cierra", modelo.EstadoError},
		{"redondeo tolerado", func(c *modelo.Comprobante) { c.Total = 12100001 }, "", modelo.EstadoOK},
		{"iva no corresponde", func(c *modelo.Comprobante) { c.IVA21 = 2000000; c.Total = 12000000 }, "iva_no_corresponde", modelo.EstadoRevisar},
		{"cuit", func(c *modelo.Comprobante) { c.CUITEmisor = "30711111117" }, "cuit_emisor_invalido", modelo.EstadoError},
		{"letra", func(c *modelo.Comprobante) { c.Letra = "B" }, "letra_no_coincide", modelo.EstadoError},
		{"fecha futura", func(c *modelo.Comprobante) { c.Fecha = "2027-01-01" }, "fecha_futura", modelo.EstadoError},
		{"cae vencido", func(c *modelo.Comprobante) { c.VtoCAE = "2026-11-01" }, "cae_vencido_a_la_emision", modelo.EstadoError},
		{"sin cae", func(c *modelo.Comprobante) { c.CAE = "" }, "sin_cae", modelo.EstadoRevisar},
		{"cae corto", func(c *modelo.Comprobante) { c.CAE = "123" }, "cae_invalido", modelo.EstadoError},
		{"moneda", func(c *modelo.Comprobante) { c.Moneda = "DOL"; c.Cotizacion = 1000 }, "moneda_extranjera", modelo.EstadoRevisar},
		{"falta numero", func(c *modelo.Comprobante) { c.Numero = 0 }, "falta_numero", modelo.EstadoError},
		{"C con IVA", func(c *modelo.Comprobante) { c.Tipo, c.Letra, c.Subtotal = 11, "C", 12100000 }, "iva_en_factura_c", modelo.EstadoRevisar},
		{"C subtotal no cierra", func(c *modelo.Comprobante) {
			c.Tipo, c.Letra, c.Subtotal, c.NetoGravado, c.IVA21 = 11, "C", 100, 0, 0
		}, "suma_no_cierra", modelo.EstadoError},
	}
	for _, cs := range casos {
		c := base()
		cs.mod(&c)
		h := Comprobante(&c, hoy)
		if cs.cod != "" && !codigos(h)[cs.cod] {
			t.Errorf("%s: faltó %s en %+v", cs.nombre, cs.cod, h)
		}
		if e := Estado(&c, h); e != cs.estado {
			t.Errorf("%s: estado %s, quería %s (%+v)", cs.nombre, e, cs.estado, h)
		}
	}
}

func TestDatosDeIARequierenRevision(t *testing.T) {
	c := base()
	c.Marcar("total", modelo.OrigenVision)
	h := Comprobante(&c, hoy)
	if Estado(&c, h) != modelo.EstadoRevisar {
		t.Fatal("lo que lee la IA lo revisa una persona")
	}
	c2 := base()
	for _, k := range []string{"tipo", "punto_venta", "numero", "fecha", "cuit_emisor", "total", "cae"} {
		c2.Marcar(k, modelo.OrigenQR)
	}
	if Confianza(&c2, nil) != 1 || Confianza(&c, nil) >= 1 {
		t.Fatal("confianza")
	}
}
