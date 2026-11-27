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
	if n[0].Version != "0.5.0" || n[0].Fecha == "" {
		t.Fatalf("la primera tiene que ser la 0.5.0 con fecha: %+v", n[0])
	}
	if !strings.Contains(n[0].Cuerpo, "### ") || strings.Contains(n[0].Cuerpo, "## [0.4.0]") {
		t.Fatalf("cuerpo mal cortado: %q", n[0].Cuerpo)
	}
	if n[len(n)-1].Version != "0.1.0" {
		t.Fatalf("la última tiene que ser la 0.1.0: %+v", n[len(n)-1])
	}
}
