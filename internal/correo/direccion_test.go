package correo

import (
	"strings"
	"testing"
)

func TestProblemaDireccion(t *testing.T) {
	for _, c := range []struct{ en, quiere string }{
		{"Factorum <facturas@estudio.com.ar>", ""},
		{"facturas@estudio.com.ar", ""},
		{"  ", ""},
		{"Factorum (diegoparras@gmail.com)", "Escribila así: Factorum <diegoparras@gmail.com>"},
		{"Estudio Parras ( a@b.com )", "Escribila así: Estudio Parras <a@b.com>"},
		{"Factorum", "no es una dirección válida"},
	} {
		got := ProblemaDireccion(c.en)
		if (c.quiere == "") != (got == "") || !strings.Contains(got, c.quiere) {
			t.Errorf("%q: %q, quería %q", c.en, got, c.quiere)
		}
	}
	// El error del envío explica lo mismo.
	_, err := SMTP{Host: "smtp.ejemplo.com", Remitente: "Factorum (a@b.com)"}.Conectar(t.Context())
	if err == nil || !strings.Contains(err.Error(), "Factorum <a@b.com>") {
		t.Fatalf("Conectar: %v", err)
	}
}

func TestClaveAplicacion(t *testing.T) {
	for _, c := range []struct{ en, sale string }{
		{"abcd efgh ijkl mnop", "abcdefghijklmnop"},
		{"  abcd  efgh ijkl mnop \n", "abcdefghijklmnop"},
		{"abcdefghijklmnop", "abcdefghijklmnop"},
		{"mi clave con espacios", "mi clave con espacios"}, // no es de aplicación: queda igual
		{"re_ABC123 xyz", "re_ABC123 xyz"},
		{"abcd efgh ijkl", "abcd efgh ijkl"},
	} {
		if got := ClaveAplicacion(c.en); got != c.sale {
			t.Errorf("%q: %q, quería %q", c.en, got, c.sale)
		}
	}
}
