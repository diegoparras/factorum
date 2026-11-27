package validar

import (
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

func tiqueBase() modelo.Comprobante {
	return modelo.Comprobante{
		Tipo: 83, EsTique: true, PuntoVenta: 3, Numero: 45678, Fecha: "2026-11-22", Hora: "08:12:45",
		CUITEmisor: "30708888881", RazonSocialEmisor: "EL TREBOL S.R.L.", DocTipoReceptor: 99, Total: 4406250, Moneda: "PES",
	}
}

func TestTiqueConsumidorFinal(t *testing.T) {
	c := tiqueBase()
	h := Comprobante(&c, hoy)
	cod := codigos(h)
	if cod["sin_cae"] || cod["cae_invalido"] || cod["cae_vencido_a_la_emision"] {
		t.Fatalf("un tique no lleva CAE: %+v", h)
	}
	if !cod["tique_sin_constatacion"] || !cod["tique_sin_detalle_iva"] {
		t.Fatalf("faltan los info del tique: %+v", h)
	}
	for _, x := range h {
		if x.Nivel != modelo.NivelInfo {
			t.Fatalf("un tique a consumidor final completo no tiene avisos: %+v", h)
		}
	}
	if Estado(&c, h) != modelo.EstadoOK {
		t.Fatal("estado")
	}
	// Sin CAE la confianza no baja: no se cuenta.
	c.Marcar("tipo", modelo.OrigenTexto)
	for _, k := range []string{"punto_venta", "numero", "fecha", "cuit_emisor", "total"} {
		c.Marcar(k, modelo.OrigenTexto)
	}
	if Confianza(&c, h) < 0.94 {
		t.Fatalf("confianza %v", Confianza(&c, h))
	}
}

func TestTiqueControles(t *testing.T) {
	casos := []struct {
		nombre  string
		cambiar func(c *modelo.Comprobante)
		codigo  string
		nivel   modelo.Nivel
	}{
		{"detalle que cierra", func(c *modelo.Comprobante) {
			c.Tipo, c.Letra = 82, "B"
			c.NetoGravado, c.IVA21, c.IVA105, c.Total = 1399774, 115500, 89226, 1604500
		}, "", ""},
		{"detalle que no cierra", func(c *modelo.Comprobante) {
			c.Tipo, c.Letra = 81, "A"
			c.NetoGravado, c.IVA21, c.Total = 4000000, 840000, 4900000
		}, "suma_no_cierra", modelo.NivelError},
		{"IVA que no es el 21% del neto", func(c *modelo.Comprobante) {
			c.Tipo, c.Letra = 81, "A"
			c.NetoGravado, c.IVA21, c.Total = 4000000, 800000, 4800000
		}, "iva_no_corresponde", modelo.NivelAviso},
		{"tique factura A sin detalle", func(c *modelo.Comprobante) { c.Tipo, c.Letra = 81, "A" }, "sin_detalle_importes", modelo.NivelAviso},
		{"tique C sin detalle", func(c *modelo.Comprobante) { c.Tipo, c.Letra = 111, "C" }, "tique_sin_detalle_iva", modelo.NivelInfo},
		{"letra que no corresponde", func(c *modelo.Comprobante) { c.Tipo, c.Letra = 82, "A" }, "letra_no_coincide", modelo.NivelError},
		{"sin número (foto borrosa)", func(c *modelo.Comprobante) { c.Numero = 0 }, "falta_numero", modelo.NivelAviso},
		{"sin total", func(c *modelo.Comprobante) { c.Total = 0 }, "falta_total", modelo.NivelError},
		{"CUIT inválido", func(c *modelo.Comprobante) { c.CUITEmisor = "30708888882" }, "cuit_emisor_invalido", modelo.NivelError},
	}
	for _, k := range casos {
		t.Run(k.nombre, func(t *testing.T) {
			c := tiqueBase()
			k.cambiar(&c)
			h := Comprobante(&c, hoy)
			if k.codigo == "" {
				for _, x := range h {
					if x.Nivel != modelo.NivelInfo {
						t.Fatalf("no tenía que haber problemas: %+v", h)
					}
				}
				return
			}
			for _, x := range h {
				if x.Codigo == k.codigo {
					if x.Nivel != k.nivel {
						t.Fatalf("%s con nivel %s, quería %s", x.Codigo, x.Nivel, k.nivel)
					}
					return
				}
			}
			t.Fatalf("faltó %s: %+v", k.codigo, h)
		})
	}
	// En un tique 83 la letra no se controla (no tiene).
	c := tiqueBase()
	c.Letra = "B"
	if codigos(Comprobante(&c, hoy))["letra_no_coincide"] {
		t.Fatal("el tique 83 no tiene letra para controlar")
	}
}
