package factorum

import (
	"strings"
	"testing"
)

func TestNovedades(t *testing.T) {
	n := Novedades()
	if len(n) < 4 {
		t.Fatalf("esperaba al menos cuatro versiones, hay %d", len(n))
	}
	if n[0].Version != "0.11.2" || n[0].Fecha != "2026-11-28" || !strings.Contains(n[0].Cuerpo, "Contraseñas de aplicación") ||
		strings.Contains(n[0].Cuerpo, "## [0.11.1]") {
		t.Fatalf("la primera tiene que ser la 0.11.2 del 28/11: %+v", n[0])
	}
	if n[1].Version != "0.11.1" || !strings.Contains(n[1].Cuerpo, "### Arreglos") || strings.Contains(n[1].Cuerpo, "## [0.11.0]") {
		t.Fatalf("la segunda tiene que ser la 0.11.1: %+v", n[1])
	}
	if n[2].Version != "0.11.0" || !strings.Contains(n[2].Cuerpo, "### Buscar desde cualquier pantalla") || !strings.Contains(n[2].Cuerpo, "### Configuración, más clara") ||
		strings.Contains(n[2].Cuerpo, "## [0.10.0]") {
		t.Fatalf("la tercera tiene que ser la 0.11.0: %+v", n[2])
	}
	if n[3].Version != "0.10.0" || !strings.Contains(n[3].Cuerpo, "### Facturas que llegan tarde") {
		t.Fatalf("la cuarta tiene que ser la 0.10.0: %+v", n[3])
	}
	if n[len(n)-1].Version != "0.1.0" {
		t.Fatalf("la última tiene que ser la 0.1.0: %+v", n[len(n)-1])
	}
}
