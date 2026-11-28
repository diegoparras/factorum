package factorum

import (
	"strings"
	"testing"
)

func TestNovedades(t *testing.T) {
	n := Novedades()
	if len(n) < 2 {
		t.Fatalf("esperaba al menos dos versiones, hay %d", len(n))
	}
	if n[0].Version != "0.11.0" || n[0].Fecha != "2026-11-28" {
		t.Fatalf("la primera tiene que ser la 0.11.0 del 28/11: %+v", n[0])
	}
	if !strings.Contains(n[0].Cuerpo, "### Buscar desde cualquier pantalla") || !strings.Contains(n[0].Cuerpo, "### Configuración, más clara") ||
		strings.Contains(n[0].Cuerpo, "## [0.10.0]") || strings.Contains(n[0].Cuerpo, "### Facturas que llegan tarde") {
		t.Fatalf("cuerpo mal cortado: %q", n[0].Cuerpo)
	}
	if n[1].Version != "0.10.0" || !strings.Contains(n[1].Cuerpo, "### Facturas que llegan tarde") {
		t.Fatalf("la segunda tiene que ser la 0.10.0: %+v", n[1])
	}
	if n[len(n)-1].Version != "0.1.0" {
		t.Fatalf("la última tiene que ser la 0.1.0: %+v", n[len(n)-1])
	}
}
