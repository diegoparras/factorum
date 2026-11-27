package iaproveedor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/extractor"
)

const claveSecreta = "sk-prueba-1234567890-secreta"

func servidor(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(h)
	t.Cleanup(s.Close)
	return s
}

func ids(ms []Modelo) []string {
	var r []string
	for _, m := range ms {
		r = append(r, m.ID)
	}
	return r
}

func motivo(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Motivo
	}
	return ""
}

func TestProveedores(t *testing.T) {
	var vs []string
	for _, p := range Lista {
		vs = append(vs, p.ID)
		if p.Nombre == "" || p.Corto == "" || p.Ayuda == "" {
			t.Errorf("%s: faltan textos", p.ID)
		}
		if (p.Clave == "si") != (p.ClaveURL != "" && p.ClaveDonde != "") {
			t.Errorf("%s: los que piden clave dicen dónde se saca", p.ID)
		}
		if !p.Editable && !Compatible(p.ID, p.URL) {
			t.Errorf("%s: su URL no es compatible", p.ID)
		}
	}
	if !slices.Equal(vs, config.ProveedoresIA) {
		t.Errorf("los proveedores de config no son los de la lista: %v", vs)
	}
	if p, _ := Por(Ollama); p.Clave != "no" || !p.Editable || p.URL != "http://ollama:11434/v1" {
		t.Errorf("Ollama: %+v", p)
	}
	o, _ := Por(OpenAI)
	if len(o.Regiones) != 11 || o.Regiones[0].URL != "https://api.openai.com/v1" || o.Regiones[2].URL != "https://eu.api.openai.com/v1" || !strings.Contains(o.Regiones[7].Nombre, "solo guarda") {
		t.Errorf("regiones de OpenAI: %+v", o.Regiones)
	}
	for _, r := range o.Regiones {
		if !Compatible(OpenAI, r.URL) {
			t.Errorf("región %s no es de OpenAI", r.URL)
		}
	}
}

func TestDeducir(t *testing.T) {
	for url, prov := range map[string]string{
		"":                                    "",
		"http://ollama:11434/v1":              Ollama,
		"http://192.168.1.20:11434/v1":        Ollama,
		"http://ollama.internal/v1":           Ollama,
		"https://api.openai.com/v1":           OpenAI,
		"https://eu.api.openai.com/v1":        OpenAI,
		"https://api.anthropic.com/v1":        Anthropic,
		"https://openrouter.ai/api/v1":        OpenRouter,
		"http://lmstudio:1234/v1":             Otro,
		"https://gateway.ejemplo.com/v1":      Otro,
		"https://api.openai.com.ejemplo.com/": Otro,
		"https://generativelanguage.googleapis.com/v1beta/openai": Gemini,
	} {
		if d := Deducir(url); d != prov {
			t.Errorf("%q: %q, esperaba %q", url, d, prov)
		}
	}
	// Lo elegido gana si corresponde con la URL; si no, se deduce (configuraciones viejas o del entorno).
	casos := []struct{ elegido, url, efectivo string }{
		{"", "https://api.openai.com/v1", OpenAI},
		{Otro, "https://api.openai.com/v1", Otro},
		{Ollama, "https://ollama.ejemplo.com/v1", Ollama},
		{Anthropic, "https://api.openai.com/v1", OpenAI},
		{"cualquiera", "http://ollama:11434/v1", Ollama},
		{Gemini, "", Gemini},
	}
	for _, c := range casos {
		if e := Efectivo(c.elegido, c.url); e != c.efectivo {
			t.Errorf("Efectivo(%q, %q) = %q, esperaba %q", c.elegido, c.url, e, c.efectivo)
		}
	}
	if !MismoHost("https://api.openai.com/v1", "https://API.openai.com/v1/") || MismoHost("https://api.openai.com/v1", "https://eu.api.openai.com/v1") || MismoHost("", "") {
		t.Error("MismoHost")
	}
}

func TestRecomendados(t *testing.T) {
	esperados := map[string]string{
		"gpt-6.1-terra":        "Probado por Factorum: 100 % · ~7 s por imagen",
		"gpt-6-luna":           "Probado por Factorum: 95 % · ~4 s por imagen",
		"qwen3-vl:4b-instruct": "Probado por Factorum: 79 % · ~73 s por imagen en 4 núcleos sin GPU",
		"gemma4:e4b":           "Probado por Factorum: 61 % (en la mitad de la prueba) · ~3 min 45 s por imagen en 4 núcleos sin GPU",
	}
	if len(Recomendados) != len(esperados) {
		t.Fatalf("recomendados: %+v", Recomendados)
	}
	for _, r := range Recomendados {
		if r.Texto != esperados[r.Modelo] {
			t.Errorf("%s: %q", r.Modelo, r.Texto)
		}
	}
	if r := RecomendadoDe(OpenAI, "gpt-6.1-terra"); r == nil || r.Corto != "100 % · ~7 s" || r.Nota != "Leyó bien 56 de 56 campos." {
		t.Errorf("terra: %+v", r)
	}
	if r := RecomendadoDe(Ollama, "gemma4:e4b"); r == nil || !strings.Contains(r.Nota, "mitad") {
		t.Errorf("gemma: %+v", r)
	}
	if RecomendadoDe(Anthropic, "gpt-6.1-terra") != nil || RecomendadoDe(OpenAI, "gpt-6-sol") != nil {
		t.Error("solo los probados, con su proveedor")
	}
	if r := RecomendadoDe(OpenRouter, "openai/gpt-6-luna"); r == nil || !strings.Contains(r.Texto, "medido con OpenAI directo") {
		t.Errorf("OpenRouter: %+v", r)
	}
	if len(RecomendadosDe(Ollama)) != 2 || RecomendadosDe(Ollama)[0].Modelo != "qwen3-vl:4b-instruct" {
		t.Errorf("de Ollama, el mejor primero: %+v", RecomendadosDe(Ollama))
	}
}

func TestOllama(t *testing.T) {
	var conClave atomic.Bool
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			conClave.Store(true)
		}
		switch r.URL.Path {
		case "/api/tags":
			// Uno con capabilities (Ollama nuevo) y dos sin (hay que preguntar con /api/show).
			_, _ = w.Write([]byte(`{"models":[
				{"name":"llama3.2:3b","size":2000000000,"details":{"parameter_size":"3.2B"}},
				{"name":"qwen3-vl:4b-instruct","size":3337225651,"details":{"parameter_size":"4.0B"},"capabilities":["completion","vision","tools"]},
				{"name":"gemma4:e4b","size":6583656505,"details":{"parameter_size":"7.5B"}}]}`))
		case "/api/show":
			var b struct{ Model string }
			_ = json.NewDecoder(r.Body).Decode(&b)
			caps := `["completion"]`
			if b.Model == "gemma4:e4b" {
				caps = `["completion","vision","audio"]`
			}
			_, _ = w.Write([]byte(`{"capabilities":` + caps + `}`))
		default:
			w.WriteHeader(404)
		}
	})
	ms, err := Listar(context.Background(), nil, Ollama, srv.URL+"/v1/", claveSecreta)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(ms), []string{"gemma4:e4b", "qwen3-vl:4b-instruct", "llama3.2:3b"}) {
		t.Fatalf("primero los que leen imágenes: %v", ids(ms))
	}
	if !ms[0].Imagenes || !ms[1].Imagenes || ms[2].Imagenes || ms[2].Nota != "no lee imágenes" {
		t.Errorf("capacidades: %+v", ms)
	}
	if ms[1].Detalle != "4.0B · 3,3 GB" || ms[1].Recomendado == nil || ms[0].Recomendado == nil || ms[2].Recomendado != nil {
		t.Errorf("detalle y recomendados: %+v", ms)
	}
	if conClave.Load() {
		t.Error("a Ollama no se le manda clave")
	}

	// Vacío, apagado y algo que no es Ollama.
	vacio := servidor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"models":[]}`)) })
	if _, err := Listar(context.Background(), nil, Ollama, vacio.URL+"/v1", ""); motivo(err) != MotivoVacia || !strings.Contains(err.Error(), "ollama pull qwen3-vl:4b-instruct") {
		t.Errorf("vacío: %v", err)
	}
	apagado := httptest.NewServer(http.NotFoundHandler())
	apagado.Close()
	if _, err := Listar(context.Background(), nil, Ollama, apagado.URL+"/v1", ""); motivo(err) != MotivoRed || !strings.Contains(err.Error(), "Ollama no responde") || !strings.Contains(err.Error(), "¿está prendido?") {
		t.Errorf("apagado: %v", err)
	}
	otro := servidor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) })
	if _, err := Listar(context.Background(), nil, Ollama, otro.URL+"/v1", ""); motivo(err) != MotivoRespuesta || !strings.Contains(err.Error(), "no es Ollama") {
		t.Errorf("no es Ollama: %v", err)
	}
}

func TestExcluidosOpenAI(t *testing.T) {
	// Ids reales de GET /v1/models (27/11/2026).
	quedan := []string{"gpt-6.2-sol", "gpt-6.2-sol-healthcare", "gpt-6.2-astra", "gpt-6.1-terra", "gpt-6.1-sol", "gpt-6-luna", "gpt-6-sol", "gpt-6-astra",
		"gpt-5.6-luna", "chat-latest", "gpt-5.5", "gpt-5.4-mini", "gpt-5.4-nano", "gpt-5.2", "gpt-5.1", "gpt-5", "gpt-5-mini", "gpt-4.1", "gpt-4.1-mini", "o3", "gpt-4o", "gpt-4o-mini"}
	fuera := map[string]string{
		"gpt-live-1.1": "voz en vivo", "gpt-live-transcribe": "audio", "gpt-image-2.6-sunburst": "genera imágenes", "chatgpt-image-latest": "genera imágenes",
		"gpt-transcribe": "audio", "gpt-4o-mini-transcribe": "audio", "gpt-realtime-2.1-mini": "voz en tiempo real", "gpt-realtime-whisper": "audio",
		"gpt-5.5-pro": "solo con la API Responses", "gpt-5.5-pro-2026-04-23": "solo con la API Responses", "o3-pro": "solo con la API Responses",
		"gpt-5.3-codex": "solo con la API Responses", "gpt-audio-1.5": "audio", "gpt-4o-mini-tts": "voz", "tts-1-hd": "voz", "whisper-1": "audio",
		"gpt-5-search-api": "búsqueda", "omni-moderation-latest": "moderación", "text-embedding-3-large": "embeddings", "dall-e-3": "genera imágenes",
		"davinci-002": "modelo viejo de texto", "babbage-002": "modelo viejo de texto", "gpt-3.5-turbo": "no lee imágenes", "computer-use-preview": "maneja una computadora",
		"o4-mini-deep-research": "solo con la API Responses",
	}
	for _, id := range quedan {
		if m, ok := ExcluidoOpenAI(id); ok {
			t.Errorf("%s queda afuera (%s) y sirve", id, m)
		}
	}
	for id, m := range fuera {
		if got, ok := ExcluidoOpenAI(id); !ok || got != m {
			t.Errorf("%s: %q %v, esperaba %q", id, got, ok, m)
		}
	}
}

func TestOpenAI(t *testing.T) {
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+claveSecreta {
			w.WriteHeader(401)
			// OpenAI repite la clave en el error: no tiene que llegar a la pantalla.
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + `"}}`))
			return
		}
		if r.URL.Path != "/v1/models" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"gpt-6-luna","created":1789406102},{"id":"gpt-6.1-terra","created":1791908780},{"id":"gpt-image-2","created":1776399795},
			{"id":"gpt-5.4-mini","created":1773451123},{"id":"gpt-5.4-mini-2026-03-17","created":1773451076},{"id":"gpt-5.4-2026-03-05","created":1772654062},
			{"id":"whisper-1","created":1677532384},{"id":"text-embedding-3-small","created":1705948997},{"id":"gpt-5.5-pro","created":1776894349},
			{"id":"gpt-6-sol","created":1789405937},{"id":"o3","created":1744225308}]}`))
	})
	ms, err := Listar(context.Background(), nil, OpenAI, srv.URL+"/v1", claveSecreta)
	if err != nil {
		t.Fatal(err)
	}
	// Los más nuevos arriba; la versión con fecha queda si no está su alias.
	if want := []string{"gpt-6.1-terra", "gpt-6-luna", "gpt-6-sol", "gpt-5.4-mini", "gpt-5.4-2026-03-05", "o3"}; !slices.Equal(ids(ms), want) {
		t.Fatalf("lista: %v", ids(ms))
	}
	if ms[0].Recomendado == nil || ms[0].Recomendado.Porcentaje != 100 || ms[1].Recomendado == nil || ms[2].Recomendado != nil {
		t.Errorf("recomendados: %+v", ms[:3])
	}
	_, err = Listar(context.Background(), nil, OpenAI, srv.URL+"/v1", "sk-otra-clave-que-no-va-12345")
	if motivo(err) != MotivoClave || err.Error() != "La clave no es válida para OpenAI" {
		t.Errorf("401: %v", err)
	}
	if _, err := Listar(context.Background(), nil, OpenAI, srv.URL+"/v1", ""); motivo(err) != MotivoSinClave {
		t.Errorf("sin clave: %v", err)
	}
	// Un error que repite la clave: el mensaje no la trae.
	eco := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":{"message":"falló con ` + r.Header.Get("Authorization") + `"}}`))
	})
	_, err = Listar(context.Background(), nil, OpenAI, eco.URL+"/v1", claveSecreta)
	if err == nil || strings.Contains(err.Error(), claveSecreta) || !strings.Contains(err.Error(), "OpenAI respondió 500") {
		t.Errorf("500 con la clave en el cuerpo: %v", err)
	}
	// Sin red hacia el proveedor.
	cerrado := httptest.NewServer(http.NotFoundHandler())
	cerrado.Close()
	if _, err := Listar(context.Background(), nil, OpenAI, cerrado.URL+"/v1", claveSecreta); motivo(err) != MotivoRed || !strings.Contains(err.Error(), "No se pudo conectar con OpenAI") {
		t.Errorf("sin red: %v", err)
	}
	// Una redirección no se sigue (la clave no va a otro host).
	var llego bool
	destino := servidor(t, func(w http.ResponseWriter, r *http.Request) { llego = true })
	redir := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destino.URL+"/v1/models", http.StatusFound)
	})
	if _, err := Listar(context.Background(), Cliente(nil), OpenAI, redir.URL+"/v1", claveSecreta); err == nil || llego {
		t.Errorf("redirección: %v (llegó %v)", err, llego)
	}
	vacio := servidor(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"data":[{"id":"whisper-1"}]}`)) })
	if _, err := Listar(context.Background(), nil, OpenAI, vacio.URL+"/v1", claveSecreta); motivo(err) != MotivoVacia || !strings.Contains(err.Error(), "que lean imágenes") {
		t.Errorf("vacía: %v", err)
	}
}

func TestAnthropic(t *testing.T) {
	paginas := 0
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != claveSecreta || r.Header.Get("anthropic-version") != "2023-06-01" || r.Header.Get("Authorization") != "" {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
			return
		}
		paginas++
		if r.URL.Query().Get("after_id") == "" {
			_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"claude-sonnet-5-5","display_name":"Claude Sonnet 5.5","created_at":"2026-09-01T00:00:00Z"}],"has_more":true,"last_id":"claude-sonnet-5-5"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"type":"model","id":"claude-opus-5-5","display_name":"Claude Opus 5.5","created_at":"2026-10-01T00:00:00Z"},{"type":"model","id":"claude-haiku-4-5","display_name":"Claude Haiku 4.5","created_at":"2025-10-01T00:00:00Z"}],"has_more":false,"last_id":"claude-haiku-4-5"}`))
	})
	ms, err := Listar(context.Background(), nil, Anthropic, srv.URL+"/v1", claveSecreta)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(ms), []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-4-5"}) || ms[0].Nombre != "Claude Opus 5.5" || !ms[2].Imagenes || paginas != 2 {
		t.Fatalf("Anthropic: %+v (%d páginas)", ms, paginas)
	}
	if _, err := Listar(context.Background(), nil, Anthropic, srv.URL+"/v1", "otra-clave-larga"); motivo(err) != MotivoClave || err.Error() != "La clave no es válida para Anthropic" {
		t.Errorf("401: %v", err)
	}
}

func TestGemini(t *testing.T) {
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+claveSecreta {
			w.WriteHeader(400) // Gemini responde 400 a una clave mala
			_, _ = w.Write([]byte(`{"error":{"code":400,"message":"Please pass a valid API key","status":"INVALID_ARGUMENT"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"models/gemini-2.5-flash"},{"id":"models/gemini-3-pro"},{"id":"models/gemini-embedding-001"},
			{"id":"models/gemini-2.5-flash-preview-tts"},{"id":"models/gemini-2.5-flash-image"},{"id":"models/imagen-4.0-generate-001"},{"id":"models/gemma-3-27b-it"},
			{"id":"models/gemini-live-2.5-flash"}]}`))
	})
	ms, err := Listar(context.Background(), nil, Gemini, srv.URL+"/v1beta/openai", claveSecreta)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(ms), []string{"gemini-3-pro", "gemini-2.5-flash"}) {
		t.Fatalf("Gemini: %v", ids(ms))
	}
	if _, err := Listar(context.Background(), nil, Gemini, srv.URL+"/v1beta/openai", "AIza-mala-pero-larga"); motivo(err) != MotivoClave || err.Error() != "La clave no es válida para Google Gemini" {
		t.Errorf("clave mala: %v", err)
	}
}

func TestOpenRouter(t *testing.T) {
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("para listar no se manda la clave")
		}
		_, _ = w.Write([]byte(`{"data":[
			{"id":"openai/gpt-6-luna","name":"OpenAI: GPT-6 Luna","created":20,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
			{"id":"openai/gpt-6-luna:batch","name":"OpenAI: GPT-6 Luna (batch)","created":20,"architecture":{"input_modalities":["text","image"],"output_modalities":["text"]}},
			{"id":"deepseek/deepseek-v4","name":"DeepSeek: V4","created":30,"architecture":{"input_modalities":["text"],"output_modalities":["text"]}},
			{"id":"google/gemini-4-neon","name":"Google: Gemini 4 Neon","created":40,"architecture":{"input_modalities":["text","image","audio"],"output_modalities":["text"]}},
			{"id":"openai/gpt-image-2","name":"OpenAI: GPT Image 2","created":50,"architecture":{"input_modalities":["text","image"],"output_modalities":["image"]}},
			{"id":"openai/gpt-6.1-terra","name":"OpenAI: GPT-6.1 Terra","created":25,"architecture":{"input_modalities":["image","text"],"output_modalities":["text"]}}]}`))
	})
	ms, err := Listar(context.Background(), nil, OpenRouter, srv.URL+"/api/v1", claveSecreta)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ids(ms), []string{"google/gemini-4-neon", "openai/gpt-6.1-terra", "openai/gpt-6-luna"}) {
		t.Fatalf("OpenRouter: %v", ids(ms))
	}
	if ms[1].Grupo != "OpenAI" || ms[1].Nombre != "GPT-6.1 Terra" || ms[1].Recomendado == nil {
		t.Errorf("OpenRouter: %+v", ms[1])
	}
	// Sin clave también lista.
	if _, err := Listar(context.Background(), nil, OpenRouter, srv.URL+"/api/v1", ""); err != nil {
		t.Errorf("sin clave: %v", err)
	}
}

func TestOtro(t *testing.T) {
	srv := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" && r.Header.Get("Authorization") != "Bearer "+claveSecreta {
			w.WriteHeader(403)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5-vl-7b"},{"id":"text-embedding-nomic"}]}`))
	})
	for _, clave := range []string{"", claveSecreta} {
		ms, err := Listar(context.Background(), nil, Otro, srv.URL+"/v1", clave)
		if err != nil || !slices.Equal(ids(ms), []string{"qwen2.5-vl-7b", "text-embedding-nomic"}) {
			t.Errorf("otro (clave %q): %v %v", clave, ids(ms), err)
		}
	}
	if _, err := Listar(context.Background(), nil, Otro, srv.URL+"/v1", "mala-clave-larga"); motivo(err) != MotivoClave {
		t.Errorf("403: %v", err)
	}
	if _, err := Listar(context.Background(), nil, Otro, "", ""); err == nil || !strings.Contains(err.Error(), "URL") {
		t.Errorf("sin URL: %v", err)
	}
	if _, err := Listar(context.Background(), nil, "inventado", srv.URL, ""); err == nil {
		t.Error("proveedor inventado")
	}
}

// chatFalso responde al pedido de visión con el contenido que se le pasa.
func chatFalso(t *testing.T, contenido string, status int) *httptest.Server {
	return servidor(t, func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Messages []struct {
				Content any `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if len(b.Messages) != 2 || !strings.Contains(func() string { x, _ := json.Marshal(b.Messages[1].Content); return string(x) }(), "data:image/png;base64,") {
			t.Error("Probar manda la imagen del ticket")
		}
		if status != 200 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + `"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": contenido}}}})
	})
}

const leidoBien = `{"clase":"otro","letra":null,"codigo_tipo":83,"punto_venta":3,"numero":45678,"fecha":"2026-11-22","cuit_emisor":"30-70888888-1","razon_social_emisor":"EL TREBOL S.R.L.","condicion_iva_emisor":null,"cuit_receptor":null,"razon_social_receptor":null,"neto_gravado":null,"no_gravado":null,"exento":null,"iva_27":null,"iva_21":7647.13,"iva_10_5":null,"iva_5":null,"iva_2_5":null,"otros_tributos":null,"subtotal":null,"total":44062.5,"moneda":null,"cae":null,"vto_cae":null}`

func TestProbar(t *testing.T) {
	ahora := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	reloj := func() time.Time { ahora = ahora.Add(3400 * time.Millisecond); return ahora }
	srv := chatFalso(t, leidoBien, 200)
	v := &extractor.Vision{BaseURL: srv.URL + "/v1", Modelo: "gpt-6.1-terra", APIKey: claveSecreta}
	r := Probar(context.Background(), v, OpenAI, reloj)
	if !r.Exito || r.Segundos != 3.4 || !strings.Contains(r.Mensaje, "leyó bien el ticket de prueba") || !strings.Contains(r.Mensaje, "3,4 s") {
		t.Fatalf("bien: %+v", r)
	}
	if r.Leido[0].Valor != "30-70888888-1" || !*r.Leido[0].Bien || r.Leido[1].Valor != "$ 44.062,50" || r.Leido[2].Valor != "EL TREBOL S.R.L." || r.Leido[4].Valor != "00003-00045678" {
		t.Errorf("qué leyó: %+v", r.Leido)
	}

	mal := chatFalso(t, strings.Replace(leidoBien, "44062.5", "4406.25", 1), 200)
	v.BaseURL = mal.URL + "/v1"
	if r := Probar(context.Background(), v, OpenAI, nil); r.Exito || !strings.Contains(r.Mensaje, "leyó mal el total") || *r.Leido[1].Bien {
		t.Errorf("total mal: %+v", r)
	}

	sinClave := chatFalso(t, "", 401)
	v.BaseURL = sinClave.URL + "/v1"
	r = Probar(context.Background(), v, OpenAI, nil)
	if r.Exito || r.Mensaje != "La clave no es válida para OpenAI." {
		t.Errorf("401: %+v", r)
	}
	if b, _ := json.Marshal(r); strings.Contains(string(b), claveSecreta) {
		t.Error("la clave no vuelve")
	}
	cerrado := httptest.NewServer(http.NotFoundHandler())
	cerrado.Close()
	v.BaseURL = cerrado.URL + "/v1"
	if r := Probar(context.Background(), v, Otro, nil); r.Exito || !strings.Contains(r.Mensaje, "No se pudo conectar") {
		t.Errorf("sin red: %+v", r)
	}
}
