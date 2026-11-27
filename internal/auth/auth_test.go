package auth

import (
	"strings"
	"testing"
	"time"
)

func TestClaves(t *testing.T) {
	h, err := HashClave("una clave larga")
	if err != nil || !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("hash: %q %v", h, err)
	}
	if !VerificarClave(h, "una clave larga") || VerificarClave(h, "otra clave larga") || VerificarClave("basura", "x") {
		t.Fatal("verificar")
	}
	h2, _ := HashClave("una clave larga")
	if h == h2 {
		t.Fatal("la sal tiene que cambiar el hash")
	}
	if ValidarClave("corta") == nil || ValidarClave("diez letras") != nil {
		t.Fatal("largo mínimo")
	}
}

func TestRoles(t *testing.T) {
	if r, ok := RolDeApp("arcanum", "operador"); !ok || r != Usuario {
		t.Fatal("operador de Arcanum es usuario")
	}
	if _, ok := RolDeApp("arcanum", "usuario"); ok {
		t.Fatal("usuario no es un rol de Arcanum")
	}
	if NombreEnApp("arcanum", Usuario) != "operador" || NombreEnApp("arcanum", Admin) != "admin" {
		t.Fatal("nombre en Arcanum")
	}
	if !(Lectura < Usuario && Usuario < Admin && Admin < Superadmin) {
		t.Fatal("orden")
	}
	var r Rol
	if err := r.UnmarshalJSON([]byte(`"admin"`)); err != nil || r != Admin {
		t.Fatal("json")
	}
}

func TestAlcance(t *testing.T) {
	todos := &Identidad{}
	uno := &Identidad{CUITs: []string{"20222222223"}}
	if !todos.Permite("") || !uno.Permite("20-22222222-3") || uno.Permite("") || uno.Permite("30711111111") {
		t.Fatal("permite")
	}
	if Intersecar(nil, nil) != nil || len(Intersecar([]string{"a"}, nil)) != 1 || len(Intersecar([]string{"a"}, []string{"b"})) != 0 || Intersecar([]string{"a"}, []string{"b"}) == nil {
		t.Fatal("intersecar")
	}
	if !Contenido([]string{"a"}, nil) || Contenido(nil, []string{"a"}) || !Contenido([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("contenido")
	}
	if l, err := NormalizarCUITs([]string{"20-22222222-3", "20222222223", ""}); err != nil || len(l) != 1 {
		t.Fatal("normalizar", l, err)
	}
	if _, err := NormalizarCUITs([]string{"20222222224"}); err == nil {
		t.Fatal("dígito verificador")
	}
	if l, _ := NormalizarCUITs(nil); l != nil {
		t.Fatal("vacío es todos")
	}
}

func TestLimitador(t *testing.T) {
	ahora := time.Unix(1_800_000_000, 0)
	l := NuevoLimitador()
	l.Ahora = func() time.Time { return ahora }
	for i := 0; i < 4; i++ {
		l.Fallo("ip", "mail")
	}
	if l.Espera("ip") != 0 {
		t.Fatal("los primeros intentos son libres")
	}
	l.Fallo("ip", "mail")
	if d := l.Espera("ip"); d != 5*time.Second {
		t.Fatalf("primera espera: %v", d)
	}
	l.Fallo("ip")
	if d := l.Espera("ip", "mail"); d != 10*time.Second {
		t.Fatalf("la espera crece: %v", d)
	}
	for i := 0; i < 30; i++ {
		l.Fallo("ip")
	}
	if d := l.Espera("ip"); d != 15*time.Minute {
		t.Fatalf("tope: %v", d)
	}
	l.Exito("ip")
	if l.Espera("ip") != 0 {
		t.Fatal("éxito limpia")
	}
}

func TestTokens(t *testing.T) {
	c, p := NuevaClaveAPI()
	if !strings.HasPrefix(c, "fct_") || len(p) != 12 || !strings.HasPrefix(c, p) {
		t.Fatal(c, p)
	}
	if len(NuevoToken()) != 43 || HashToken("x") == HashToken("y") {
		t.Fatal("token")
	}
	if len(ClaveTemporal()) < MinClave || len(CodigoArranque()) != 14 {
		t.Fatal("temporales")
	}
	if !Iguales("a", "a") || Iguales("a", "b") || Iguales("", "") {
		t.Fatal("iguales")
	}
}
