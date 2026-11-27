package pipeline

import (
	"context"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

// Un despacho de importación en PDF con texto: se lee como despacho (tipo 066), con lo que el
// lector puede asegurar, y queda para revisar (falta el proveedor del exterior).
func TestDespachoEnPDF(t *testing.T) {
	p := nuevo(t)
	r := p.Procesar(context.Background(), "10_despacho.pdf", leer(t, "pdf/10_despacho.pdf"), Opciones{})
	if r.Error != "" || len(r.Comprobantes) != 1 {
		t.Fatalf("resultado: %+v", r)
	}
	rc := r.Comprobantes[0]
	c := rc.Comprobante
	if c.Tipo != modelo.TipoDespacho || c.Despacho != "26001IC04123456K" || c.Fecha != "2026-11-04" || c.Letra != "" {
		t.Fatalf("despacho: %+v", c)
	}
	if c.NetoGravado != 100000000 || c.IVA21 != 21000000 || c.PercepcionIVA != 20000000 || c.PercepcionNacionales != 6000000 || c.PercepcionIIBB != 2500000 || c.Total != 149500000 {
		t.Fatalf("importes: %+v", c)
	}
	if rc.Estado != modelo.EstadoRevisar {
		t.Fatalf("para revisar: %s %+v", rc.Estado, rc.Hallazgos)
	}
	cod := map[string]bool{}
	for _, h := range rc.Hallazgos {
		cod[h.Codigo] = true
	}
	if !cod["despacho_leido"] || !cod["falta_cuit_emisor"] || cod["falta_punto_venta"] || cod["sin_cae"] {
		t.Fatalf("hallazgos: %+v", rc.Hallazgos)
	}
}
