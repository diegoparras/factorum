package arca

import "testing"

func TestTiques(t *testing.T) {
	for _, c := range []int{81, 82, 83, 109, 110, 111, 112, 113, 114, 115, 116, 117, 118, 119, 120} {
		if !EsTique(c) {
			t.Errorf("%d tiene que ser tique", c)
		}
	}
	for _, c := range []int{1, 6, 11, 51, 201, 0, 999} {
		if EsTique(c) {
			t.Errorf("%d no es tique", c)
		}
	}
	if tp, _ := Tipo(82); tp.Nombre != "Tique Factura B" || tp.Letra != "B" {
		t.Fatalf("%+v", tp)
	}
	casos := []struct {
		clase Clase
		letra string
		cod   int
	}{
		{ClaseFactura, "", 83}, {ClaseFactura, "A", 81}, {ClaseFactura, "B", 82}, {ClaseFactura, "C", 111}, {ClaseFactura, "M", 118},
		{ClaseNotaCredito, "", 110}, {ClaseNotaCredito, "A", 112}, {ClaseNotaCredito, "C", 114}, {ClaseNotaCredito, "M", 119},
		{ClaseNotaDebito, "B", 116}, {ClaseNotaDebito, "M", 120}, {ClaseNotaDebito, "", 0}, {ClaseFactura, "E", 0},
	}
	for _, c := range casos {
		if got := TiqueDesde(c.clase, c.letra); got != c.cod {
			t.Errorf("TiqueDesde(%s, %q) = %d, quería %d", c.clase, c.letra, got, c.cod)
		}
	}
	// TipoDesde nunca confunde una factura con un tique (el mapa no tiene orden).
	for i := 0; i < 50; i++ {
		if TipoDesde(ClaseFactura, "C", false) != 11 || TipoDesde(ClaseFactura, "B", false) != 6 || TipoDesde(ClaseNotaCredito, "A", false) != 3 {
			t.Fatal("TipoDesde devolvió un tique")
		}
	}
	if len(CodigosTique()) != 15 {
		t.Fatal(CodigosTique())
	}
}
