package ajustes

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/diegoparras/factorum/internal/config"
)

func TestDecodificar(t *testing.T) {
	campoDe := func(k string) Campo {
		c, ok := CampoPorClave(k)
		if !ok {
			t.Fatalf("falta %s", k)
		}
		return c
	}
	casos := []struct {
		clave, valor string
		bien         bool
	}{
		{"general.url_publica", `"https://factorum.ejemplo.com/"`, true},
		{"general.url_publica", `"factorum.ejemplo.com"`, false},
		{"general.url_publica", `""`, true},
		{"arcanum.cuit_consultante", `"20-22222222-3"`, true},
		{"arcanum.cuit_consultante", `"20222222224"`, false},
		{"telegram.permitidos", `"1, 2\n3"`, true},
		{"telegram.permitidos", `["1","1"]`, true},
		{"whatsapp.activo", `"si"`, false},
		{"clientes.canales", `"5491155550000 = 20-22222222-3\n@ana=30711111111"`, true},
		{"clientes.canales", `"5491155550000 = 1"`, false},
		{"clientes.canales", `{"x":"20222222223"}`, true},
	}
	for _, c := range casos {
		v, err := decodificar(campoDe(c.clave), json.RawMessage(c.valor))
		if (err == nil) != c.bien {
			t.Errorf("%s %s: %v %v", c.clave, c.valor, v, err)
		}
		if err != nil && !errors.Is(err, ErrInvalido) {
			t.Errorf("el error tiene que ser ErrInvalido: %v", err)
		}
	}
	v, _ := decodificar(campoDe("general.url_publica"), json.RawMessage(`"https://f.com/"`))
	if v != "https://f.com" {
		t.Errorf("sin la barra final: %v", v)
	}
	v, _ = decodificar(campoDe("telegram.permitidos"), json.RawMessage(`"1, 2\n3, 2"`))
	if l := v.([]string); len(l) != 3 {
		t.Errorf("lista: %v", l)
	}
	v, _ = decodificar(campoDe("clientes.canales"), json.RawMessage(`"a = 20-22222222-3"`))
	if m := v.(map[string]string); m["a"] != "20222222223" {
		t.Errorf("clientes: %v", m)
	}
}

func TestCamposCompletos(t *testing.T) {
	c := &config.Config{CanalClienteCUIT: map[string]string{}}
	vistos := map[string]bool{}
	for _, cp := range Campos {
		if vistos[cp.Clave] || cp.Env == "" || cp.get == nil || cp.set == nil {
			t.Errorf("campo mal armado: %s", cp.Clave)
		}
		vistos[cp.Clave] = true
		found := false
		for _, s := range Secciones {
			found = found || s == cp.Seccion
		}
		if !found {
			t.Errorf("%s: sección desconocida %s", cp.Clave, cp.Seccion)
		}
		_ = cp.get(c)
	}
	if ultimos("corto") != "" || ultimos("123456789012abcd") != "abcd" {
		t.Error("últimos 4")
	}
}
