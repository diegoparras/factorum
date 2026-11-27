package avisoia

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/config"
)

func TestDetectar(t *testing.T) {
	casos := []struct {
		url       string
		proveedor Proveedor
		host      string
		region    string
	}{
		{"https://api.openai.com/v1", OpenAI, "api.openai.com", ""},
		{"https://API.OpenAI.com/v1/", OpenAI, "api.openai.com", ""},
		{"https://eu.api.openai.com/v1", OpenAI, "eu.api.openai.com", "eu"},
		{"https://us.api.openai.com/v1", OpenAI, "us.api.openai.com", "us"},
		{"https://gb.api.openai.com/v1", OpenAI, "gb.api.openai.com", "gb"},
		{"https://kr.api.openai.com/v1", OpenAI, "kr.api.openai.com", "kr"},
		{"https://xx.api.openai.com/v1", OpenAI, "xx.api.openai.com", ""}, // de OpenAI, pero sin región conocida
		{"https://api.anthropic.com/v1", Anthropic, "api.anthropic.com", ""},
		{"https://generativelanguage.googleapis.com/v1beta/openai", Google, "generativelanguage.googleapis.com", ""},
		{"http://ollama:11434/v1", Local, "ollama:11434", ""},
		{"http://localhost:1234/v1", Local, "localhost:1234", ""},
		{"http://127.0.0.1:11434/v1", Local, "127.0.0.1:11434", ""},
		{"http://[::1]:11434/v1", Local, "[::1]:11434", ""},
		{"http://host.docker.internal:11434/v1", Local, "host.docker.internal:11434", ""},
		{"http://gpu.internal/v1", Local, "gpu.internal", ""},
		{"http://192.168.1.20:11434/v1", Local, "192.168.1.20:11434", ""},
		{"http://10.0.0.5/v1", Local, "10.0.0.5", ""},
		{"http://172.20.0.3:8000/v1", Local, "172.20.0.3:8000", ""},
		{"http://lmstudio:1234/v1", Local, "lmstudio:1234", ""},
		// Antes se daban por locales por el texto de la URL: no lo son.
		{"https://ollama.com/v1", Desconocido, "ollama.com", ""},
		{"http://172.100.1.1/v1", Desconocido, "172.100.1.1", ""},
		{"https://api.openai.com.ejemplo.com/v1", Desconocido, "api.openai.com.ejemplo.com", ""},
		{"https://openrouter.ai/api/v1", Desconocido, "openrouter.ai", ""},
		{"http://8.8.8.8/v1", Desconocido, "8.8.8.8", ""},
	}
	for _, c := range casos {
		d := Detectar(c.url)
		if d.Proveedor != c.proveedor || d.Host != c.host || d.Region != c.region || d.Local != (c.proveedor == Local) {
			t.Errorf("%s: %+v", c.url, d)
		}
		if d.Externa() == d.Local {
			t.Errorf("%s: externa y local a la vez", c.url)
		}
	}
	if d := Detectar(""); d.Host != "" || d.Externa() || d.Local {
		t.Errorf("sin URL: %+v", d)
	}
	if d := Detectar("https://openrouter.ai/api/v1"); d.Nombre != "openrouter.ai" {
		t.Errorf("el desconocido se nombra por su host: %+v", d)
	}
}

func TestAceptado(t *testing.T) {
	c := &config.Config{LLMBaseURL: "https://api.openai.com/v1", LLMModelo: "gpt-6-luna"}
	if Aceptado(c) || EnUso(c) {
		t.Fatal("una IA externa sin aceptar no se usa")
	}
	c.IAAvisoHost, c.IAAvisoVersion = "api.openai.com", Version
	if !EnUso(c) {
		t.Fatal("aceptada, se usa")
	}
	c.IAAvisoVersion = Version - 1
	if EnUso(c) {
		t.Fatal("si cambia el texto del aviso hay que aceptarlo de nuevo")
	}
	c.IAAvisoVersion = Version
	c.LLMBaseURL = "https://eu.api.openai.com/v1"
	if EnUso(c) {
		t.Fatal("otro host, aunque sea del mismo proveedor, se acepta de nuevo")
	}
	c.LLMBaseURL = "http://ollama:11434/v1"
	if !EnUso(c) {
		t.Fatal("la local no pide aceptación")
	}
	c.LLMModelo = ""
	if EnUso(c) {
		t.Fatal("sin modelo no hay IA")
	}
}

// El aviso de cada proveedor: cuatro tarjetas, las fuentes oficiales con fecha y lo que no puede
// faltar. La local no tiene aviso: solo el sello.
func TestAvisoPorProveedor(t *testing.T) {
	todo := func(c Contenido) string { b, _ := json.Marshal(c); return string(b) }
	for _, c := range []struct {
		url     string
		fuentes int
		tiene   []string
		alerta  string
	}{
		{"https://api.openai.com/v1", 2, []string{"store=false", "30 días", "1 de marzo de 2023", "abuso sexual infantil", "Zero Data Retention", "no publica en esa página dónde"}, ""},
		{"https://eu.api.openai.com/v1", 2, []string{"Región: Europa (EEE y Suiza)", "procesa y guarda los datos en la región"}, ""},
		{"https://ca.api.openai.com/v1", 2, []string{"Región: Canadá", "solo guarda los datos en la región"}, ""},
		{"https://api.anthropic.com/v1", 2, []string{"30 días", "2 años", "7 años", "feedback"}, ""},
		{"https://generativelanguage.googleapis.com/v1beta/openai", 1, []string{"Plan gratuito", "tiempo limitado", "No publica cuántos días", "cualquier país", "23/3/2026"}, "mal"},
		{"https://openrouter.ai/api/v1", 0, []string{"no conoce las políticas de openrouter.ai", "en qué país los procesa"}, "aviso"},
	} {
		a := Aviso(Detectar(c.url))
		j := todo(a)
		if len(a.Tarjetas) != 4 || a.Tarjetas[0].ID != "donde" || a.Tarjetas[3].ID != "factorum" {
			t.Errorf("%s: tarjetas %+v", c.url, a.Tarjetas)
		}
		if len(a.Fuentes) != c.fuentes {
			t.Errorf("%s: fuentes %+v", c.url, a.Fuentes)
		}
		for _, f := range a.Fuentes {
			if !strings.HasPrefix(f.URL, "https://") || f.Fecha != Fecha {
				t.Errorf("%s: fuente %+v", c.url, f)
			}
		}
		for _, x := range c.tiene {
			if !strings.Contains(j, x) {
				t.Errorf("%s: falta %q", c.url, x)
			}
		}
		if (a.Alerta == nil) != (c.alerta == "") || (a.Alerta != nil && a.Alerta.Nivel != c.alerta) {
			t.Errorf("%s: alerta %+v", c.url, a.Alerta)
		}
		if a.StoreFalse != strings.Contains(c.url, "openai.com") || !strings.HasPrefix(a.Sello, "IA externa: ") {
			t.Errorf("%s: store %v, sello %q", c.url, a.StoreFalse, a.Sello)
		}
		for _, tj := range a.Tarjetas {
			if tj.Destacado == "" || len(tj.Puntos) == 0 || len(tj.Puntos) > 3 {
				t.Errorf("%s: tarjeta %+v", c.url, tj)
			}
		}
	}
	l := Aviso(Detectar("http://ollama:11434/v1"))
	if len(l.Tarjetas) != 0 || l.Sello != "Local: las imágenes no salen de tu servidor" {
		t.Errorf("local: %+v", l)
	}
}
