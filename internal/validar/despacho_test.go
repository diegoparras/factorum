package validar

import (
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

func TestDespachoDeImportacion(t *testing.T) {
	d := modelo.Comprobante{Tipo: modelo.TipoDespacho, Fecha: "2026-11-04", Despacho: "26001IC04123456K", CUITEmisor: "55000002206",
		RazonSocialEmisor: "PROVEEDOR DEL EXTERIOR", NetoGravado: 100000, IVA21: 21000, OtrosTributos: 26000, PercepcionIVA: 20000,
		PercepcionNacionales: 6000, Total: 147000, Moneda: "PES"}
	h := Comprobante(&d, hoy)
	if c := codigos(h); len(c) != 0 {
		t.Fatalf("un despacho completo no tiene problemas (sin punto de venta, número ni CAE): %v", h)
	}
	if Estado(&d, h) != modelo.EstadoOK {
		t.Fatal("estado")
	}
	for nombre, caso := range map[string]struct {
		cambiar func(*modelo.Comprobante)
		codigo  string
		nivel   modelo.Nivel
	}{
		"sin despacho":        {func(c *modelo.Comprobante) { c.Despacho = "" }, "falta_despacho", modelo.NivelError},
		"despacho corto":      {func(c *modelo.Comprobante) { c.Despacho = "26001IC04123" }, "despacho_invalido", modelo.NivelError},
		"forma rara":          {func(c *modelo.Comprobante) { c.Despacho = "ABCDEFGHIJKLMNOP" }, "despacho_forma_rara", modelo.NivelAviso},
		"sin proveedor":       {func(c *modelo.Comprobante) { c.CUITEmisor = "" }, "falta_cuit_emisor", modelo.NivelAviso},
		"percepciones de más": {func(c *modelo.Comprobante) { c.PercepcionIIBB = 5000 }, "detalle_tributos_no_cierra", modelo.NivelError},
		"base sin IVA":        {func(c *modelo.Comprobante) { c.IVA21 = 0; c.Total = 126000 }, "despacho_sin_iva", modelo.NivelAviso},
		"total que no cierra": {func(c *modelo.Comprobante) { c.Total = 150000 }, "suma_no_cierra", modelo.NivelError},
		"CUIT argentino malo": {func(c *modelo.Comprobante) { c.CUITEmisor = "30711111112" }, "cuit_emisor_invalido", modelo.NivelError},
	} {
		x := d
		caso.cambiar(&x)
		var hall *modelo.Hallazgo
		for _, y := range Comprobante(&x, hoy) {
			if y.Codigo == caso.codigo {
				hall = &y
			}
		}
		if hall == nil || hall.Nivel != caso.nivel {
			t.Errorf("%s: quería %s (%s): %v", nombre, caso.codigo, caso.nivel, Comprobante(&x, hoy))
		}
	}
}
