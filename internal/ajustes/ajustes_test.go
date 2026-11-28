package ajustes

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/store"
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
		{"clientes.canales", `"Juan@Cliente.com = 20-22222222-3\n@cliente.com = 30711111111"`, true},
		{"imap.puerto", `993`, true},
		{"imap.puerto", `"143"`, true},
		{"imap.puerto", `70000`, false},
		{"imap.intervalo", `5`, false},
		{"imap.tls", `"STARTTLS"`, true},
		{"imap.tls", `"ssl"`, false},
		{"correo.forma", `"gmail"`, true},
		{"correo.forma", `"pop3"`, false},
		{"correo.permitidos", `"Juan@Cliente.com\n@proveedor.com.ar"`, true},
		{"correo.permitidos", `"juan"`, false},
		{"avisos.lote_a", `"a@b.com, c@d.com"`, true},
		{"avisos.lote_a", `"@dominio.com"`, false},
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
	v, _ = decodificar(campoDe("correo.permitidos"), json.RawMessage(`"Juan@Cliente.com, juan@cliente.com"`))
	if l := v.([]string); len(l) != 1 || l[0] != "juan@cliente.com" {
		t.Errorf("permitidos: %v", l)
	}
	v, _ = decodificar(campoDe("imap.puerto"), json.RawMessage(`"143"`))
	if v != 143 {
		t.Errorf("puerto: %v", v)
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
		if vistos[cp.Clave] || (cp.Env == "" && !cp.Interno) || cp.get == nil || cp.set == nil {
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
	// Lo interno (el permiso de Google) no se muestra.
	srv := &Servicio{base: c, Getenv: func(string) string { return "" }, guardado: map[string]store.FilaConfig{}}
	srv.actual.Store(c)
	for _, v := range srv.Vista() {
		if v.Interno {
			t.Errorf("%s es interno y aparece en la vista", v.Clave)
		}
	}
	if ultimos("corto") != "" || ultimos("123456789012abcd") != "abcd" {
		t.Error("últimos 4")
	}
}

// El remitente y "Responder a" se validan al guardar, con la forma correcta cuando se adivina.
func TestDecodificarDireccion(t *testing.T) {
	campoDe := func(k string) Campo {
		c, ok := CampoPorClave(k)
		if !ok {
			t.Fatalf("falta %s", k)
		}
		return c
	}
	for _, c := range []struct {
		clave, valor, quiere string
	}{
		{"smtp.remitente", `"Factorum <diego@gmail.com>"`, ""},
		{"smtp.remitente", `"diego@gmail.com"`, ""},
		{"smtp.remitente", `""`, ""},
		{"smtp.remitente", `"Factorum (diego@gmail.com)"`, "Escribila así: Factorum <diego@gmail.com>"},
		{"smtp.remitente", `"Factorum diego arroba gmail"`, "no es una dirección válida"},
		{"smtp.responder_a", `"(estudio@estudio.com.ar)"`, "Escribila así: estudio@estudio.com.ar"},
	} {
		_, err := decodificar(campoDe(c.clave), json.RawMessage(c.valor))
		if c.quiere == "" {
			if err != nil {
				t.Errorf("%s %s: %v", c.clave, c.valor, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.quiere) {
			t.Errorf("%s %s: error %v, quería que diga %q", c.clave, c.valor, err, c.quiere)
		}
		if CampoDelError(err) != c.clave {
			t.Errorf("%s: el error no marca el campo (%q)", c.clave, CampoDelError(err))
		}
	}
}
