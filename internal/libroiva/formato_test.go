package libroiva

import (
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/modelo"
)

func TestImporte(t *testing.T) {
	for v, want := range map[modelo.Importe]string{
		0:                "000000000000000",
		1:                "000000000000001",
		123456:           "000000000123456",
		MaxImporte:       "999999999999999",
		-1:               "-00000000000001", // el signo en la primera posición
		-123456:          "-00000000123456",
		-MaxImporte / 10: "-99999999999999",
		12_100_000_00:    "000001210000000",
		-123_456_789_012: "-00123456789012",
	} {
		got, err := importe(v)
		if err != nil || got != want || len(got) != 15 {
			t.Errorf("importe(%d) = %q, %v; esperaba %q", v, got, err, want)
		}
	}
	for _, v := range []modelo.Importe{MaxImporte + 1, -MaxImporte/10 - 1} {
		if _, err := importe(v); err == nil {
			t.Errorf("importe(%d) no entra en 15 posiciones", v)
		}
	}
}

func TestNumero(t *testing.T) {
	if s, err := numero(1544, 20); err != nil || s != "00000000000000001544" {
		t.Fatalf("%q %v", s, err)
	}
	if _, err := numero(123456, 5); err == nil {
		t.Fatal("no entra")
	}
	if _, err := numero(-1, 5); err == nil {
		t.Fatal("negativo")
	}
}

func TestTipoCambio(t *testing.T) {
	for c, want := range map[float64]string{1: "0001000000", 1180.5: "1180500000", 0.000123: "0000000123", 9999.999999: "9999999999", 1.2345678: "0001234568"} {
		if got, err := tipoCambio(c); err != nil || got != want {
			t.Errorf("tipoCambio(%v) = %q, %v; esperaba %q", c, got, err, want)
		}
	}
	for _, c := range []float64{0, -1, 10000, 0.0000001} {
		if _, err := tipoCambio(c); err == nil {
			t.Errorf("tipoCambio(%v) tiene que fallar", c)
		}
	}
}

func TestAlfaYLatin1(t *testing.T) {
	for in, want := range map[string]string{
		"PEREZ JUAN":                           "PEREZ JUAN" + strings.Repeat(" ", 20),
		"Ñandú Comunicación":                   "\xd1andu Comunicacion" + strings.Repeat(" ", 12),
		"  dos   espacios\t ":                  "dos espacios" + strings.Repeat(" ", 18),
		"“Comillas” – raya":                    `"Comillas" - raya` + strings.Repeat(" ", 13),
		"Emoji 😀 y 中文":                         "Emoji y" + strings.Repeat(" ", 23),
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789": "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZ012 456789": "ABCDEFGHIJKLMNOPQRSTUVWXYZ012 ",
		"":                                     strings.Repeat(" ", 30),
	} {
		got := alfa(in, 30)
		if got != want || len(got) != 30 {
			t.Errorf("alfa(%q) = %q (%d), esperaba %q", in, got, len(got), want)
		}
	}
	if LeerLatin1([]byte(Latin1("Ñuñoa"))) != "Ñuñoa" {
		t.Fatal("la Ñ y la ñ quedan en Latin-1")
	}
}

func TestCodigoMoneda(t *testing.T) {
	for in, want := range map[string]string{"": "PES", "PES": "PES", "ars": "PES", "DOL": "DOL", "USD": "DOL", "US$": "DOL", "060": "060", "60": "060", "EUR": "060", "2": "002", "BRL": "012", "012": "012"} {
		if got, ok := CodigoMoneda(in); !ok || got != want {
			t.Errorf("CodigoMoneda(%q) = %q, %v; esperaba %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"XYZ", "1", "65", "EURO"} {
		if _, ok := CodigoMoneda(in); ok {
			t.Errorf("CodigoMoneda(%q) no tiene que reconocerse", in)
		}
	}
}
