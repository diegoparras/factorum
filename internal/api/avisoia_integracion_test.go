package api_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/diegoparras/factorum/internal/avisoia"
	"github.com/diegoparras/factorum/internal/canal"
)

// modeloFalso es un servidor compatible con OpenAI que cuenta los pedidos de chat.
func modeloFalso(t *testing.T) *atomic.Int32 {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-6-luna"}]}`))
			return
		}
		n.Add(1)
		contenido := `{"clase":"factura","letra":"B","codigo_tipo":6,"punto_venta":3,"numero":99999,"fecha":"2026-11-20","cuit_emisor":"30711111111","razon_social_emisor":"DISTRIBUIDORA EJEMPLO S.R.L.","condicion_iva_emisor":null,"cuit_receptor":null,"razon_social_receptor":null,"neto_gravado":null,"no_gravado":null,"exento":null,"iva_27":null,"iva_21":null,"iva_10_5":null,"iva_5":null,"iva_2_5":null,"otros_tributos":null,"subtotal":null,"total":1,"moneda":null,"cae":null,"vto_cae":null}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": contenido}}}})
	}))
	t.Cleanup(srv.Close)
	iaFalsa.Store(srv.Listener.Addr().String())
	t.Cleanup(func() { iaFalsa.Store("") })
	return &n
}

func foto(t *testing.T) []byte {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "foto_fb.png"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// verificarFoto manda la foto a /api/verificar con «Usar IA si hace falta».
func (n *navegador) verificarFoto(ia bool) (int, map[string]any) {
	return n.pedir("POST", "/api/verificar", map[string]any{"nombre": "foto.png", "contenido_base64": base64.StdEncoding.EncodeToString(foto(n.t)), "ia": fmt.Sprint(ia)})
}

// cargarFoto sube un lote con la foto desde la pantalla.
func (n *navegador) cargarFoto(ia bool) (int, map[string]any) {
	n.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("ia", fmt.Sprint(ia))
	_ = mw.WriteField("constatar", "false")
	fw, _ := mw.CreateFormFile("archivos", "foto.png")
	_, _ = fw.Write(foto(n.t))
	mw.Close()
	req, _ := http.NewRequest("POST", n.e.srv.URL+"/api/lotes", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Factorum", "1")
	req.Header.Set("Origin", n.origen)
	res, err := n.c.Do(req)
	if err != nil {
		n.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return res.StatusCode, m
}

func avisoDe(m map[string]any) map[string]any {
	a, _ := m["aviso"].(map[string]any)
	return a
}

func TestAvisoIAExterna(t *testing.T) {
	e := armar(t)
	llamadas := modeloFalso(t)
	sup := e.crearSuperadmin(t)
	ctx := context.Background()

	// Sin IA no hay nada que avisar.
	_, m := sup.pedir("GET", "/api/ia/aviso", nil)
	if m["configurada"] != false || m["en_uso"] != false {
		t.Fatalf("sin IA: %v", m)
	}

	// Local: se guarda sin aviso y el sello lo dice.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.url": "http://ollama:11434/v1", "ia.modelo": "qwen2.5vl:7b"}}); st != 200 {
		t.Fatalf("guardar la IA local: %d %v", st, m)
	}
	_, m = sup.pedir("GET", "/api/ia/aviso", nil)
	if m["local"] != true || m["en_uso"] != true || m["visto"] != true || avisoDe(m)["sello"] != "Local: las imágenes no salen de tu servidor" || len(avisoDe(m)["tarjetas"].([]any)) != 0 {
		t.Fatalf("local: %v", m)
	}
	if e.app.Pipeline.Vision() == nil {
		t.Fatal("la local se usa")
	}

	// Externa: antes de guardar hay que aceptar el aviso. Sin aceptarlo no se guarda nada.
	cambios := map[string]any{"ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6-luna", "ia.api_key": "sk-de-prueba"}
	st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios})
	if st != 409 || m["codigo"] != "aviso_ia" {
		t.Fatalf("externa sin aceptar: %d %v", st, m)
	}
	aviso := m["aviso_ia"].(map[string]any)
	if aviso["proveedor"] != "openai" || aviso["host"] != "api.openai.com" || aviso["aceptado"] != false || len(avisoDe(aviso)["tarjetas"].([]any)) != 4 {
		t.Fatalf("el aviso que vuelve para mostrar: %v", aviso)
	}
	if e.app.Config().LLMBaseURL != "http://ollama:11434/v1" {
		t.Fatal("sin aceptar no se guarda")
	}
	// Aceptar otro host (o una versión vieja del aviso) no sirve.
	for _, mal := range []map[string]any{{"host": "api.anthropic.com", "version": avisoia.Version}, {"host": "api.openai.com", "version": avisoia.Version - 1}} {
		if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios, "aceptar_aviso_ia": mal}); st != 409 {
			t.Errorf("aceptar %v: %d", mal, st)
		}
	}
	// Un admin no lo acepta.
	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	if st, _ := adm.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{}, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}}); st != 403 {
		t.Errorf("un admin acepta el aviso: %d", st)
	}
	st, m = sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}})
	if st != 200 {
		t.Fatalf("aceptar y guardar: %d %v", st, m)
	}
	c := e.app.Config()
	if c.LLMBaseURL != "https://api.openai.com/v1" || c.IAAvisoHost != "api.openai.com" || c.IAAvisoVersion != avisoia.Version || c.IAAvisoPor != "diego@ejemplo.com" || c.IAAvisoCuando == "" {
		t.Fatalf("la aceptación guardada: %+v", c)
	}
	if ia := m["ia_aviso"].(map[string]any); ia["aceptado"] != true || ia["aceptacion"].(map[string]any)["por"] != "diego@ejemplo.com" {
		t.Errorf("Configuración muestra quién lo aceptó: %v", ia)
	}
	_, _, raw := sup.pedirCrudo("GET", "/api/auditoria?accion=ia.aviso_aceptado", nil)
	if !strings.Contains(string(raw), "api.openai.com") {
		t.Errorf("auditoría de la aceptación: %s", raw)
	}
	if e.app.Pipeline.Vision() == nil {
		t.Fatal("aceptada, se usa")
	}
	// Cambiar el modelo del mismo host no pide aceptar de nuevo.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.modelo": "gpt-6-sol"}}); st != 200 {
		t.Fatalf("otro modelo, mismo host: %d %v", st, m)
	}

	// Cada persona: la primera vez que va a usar la IA externa tiene que ver el aviso.
	usu, _ := e.altaUsuario(t, sup, "ana@ejemplo.com", "usuario", nil)
	_, m = usu.pedir("GET", "/api/ia/aviso", nil)
	if m["visto"] != false || m["persona"] != true || m["en_uso"] != true || m["aceptacion"] != nil {
		t.Fatalf("aviso para la persona: %v", m)
	}
	antes := llamadas.Load()
	if st, m := usu.verificarFoto(true); st != 409 || m["codigo"] != "aviso_ia" {
		t.Fatalf("verificar con IA sin ver el aviso: %d %v", st, m)
	}
	if st, m := usu.cargarFoto(true); st != 409 || m["codigo"] != "aviso_ia" {
		t.Fatalf("cargar con IA sin ver el aviso: %d %v", st, m)
	}
	if llamadas.Load() != antes {
		t.Fatal("sin ver el aviso no se llama al modelo")
	}
	// Sin IA anda igual.
	if st, m := usu.verificarFoto(false); st != 200 {
		t.Fatalf("verificar sin IA: %d %v", st, m)
	}
	if st, _ := usu.pedir("POST", "/api/ia/aviso/visto", map[string]any{"host": "api.anthropic.com", "version": avisoia.Version}); st != 409 {
		t.Errorf("visto de otro host: %d", st)
	}
	if st, m := usu.pedir("POST", "/api/ia/aviso/visto", map[string]any{"host": "api.openai.com", "version": avisoia.Version}); st != 200 || m["visto"] != true {
		t.Fatalf("visto: %d %v", st, m)
	}
	if st, m := usu.verificarFoto(true); st != 200 || llamadas.Load() != antes+1 {
		t.Fatalf("verificar con IA, visto el aviso: %d %v (llamadas %d)", st, m, llamadas.Load()-antes)
	}
	if visto, _ := e.app.Store.AvisoIAVisto(ctx, 1, "api.openai.com", avisoia.Version); visto {
		t.Error("lo que vio una persona no cuenta para otra")
	}
	_, m = sup.pedir("GET", "/api/ia/aviso", nil)
	if m["visto"] != false {
		t.Error("el superadmin aceptó para la instalación, pero como persona todavía no lo vio")
	}
	// Una clave de API no es una persona: le alcanza con la aceptación.
	antes = llamadas.Load()
	b, _ := json.Marshal(map[string]any{"nombre": "foto.png", "contenido_base64": base64.StdEncoding.EncodeToString(foto(t)), "ia": "true"})
	if st, _, _ := e.pedir(t, "POST", "/api/verificar", claveLect, bytes.NewReader(b), "application/json"); st != 200 || llamadas.Load() != antes+1 {
		t.Fatalf("clave de API con IA aceptada: %d", st)
	}

	// Cambia el host (otro proveedor): hay que aceptar de nuevo, y cada persona lo vuelve a ver.
	cambios = map[string]any{"ia.url": "https://api.anthropic.com/v1", "ia.modelo": "claude-de-prueba"}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios}); st != 409 || m["aviso_ia"].(map[string]any)["proveedor"] != "anthropic" {
		t.Fatalf("otro host sin aceptar: %d %v", st, m)
	}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.anthropic.com", "version": avisoia.Version}}); st != 200 {
		t.Fatalf("aceptar Anthropic: %d %v", st, m)
	}
	if _, m := usu.pedir("GET", "/api/ia/aviso", nil); m["visto"] != false || m["proveedor"] != "anthropic" {
		t.Fatalf("la persona tiene que ver el aviso del proveedor nuevo: %v", m)
	}
	if st, _ := usu.cargarFoto(true); st != 409 {
		t.Fatalf("cargar con el proveedor nuevo sin ver el aviso: %d", st)
	}
}

// Una IA externa que viene por variables de entorno, sin aceptación, no se usa por ningún lado: ni
// la pantalla, ni los canales, ni MCP. Configuración avisa para aceptarla.
func TestAvisoIAFailClosed(t *testing.T) {
	e := armar(t)
	llamadas := modeloFalso(t)
	sup := e.crearSuperadmin(t)
	ctx := context.Background()
	// Como si viniera del entorno: el valor en la configuración de arranque y el campo fijado.
	e.cfg.LLMBaseURL, e.cfg.LLMModelo = "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-de-prueba"
	e.cfg.IAPorDefecto = true // los canales y MCP la usan si hace falta
	e.fijadas.Store("LLM_API_BASE_URL", e.cfg.LLMBaseURL)
	e.fijadas.Store("LLM_MODEL", e.cfg.LLMModelo)
	if err := e.aj.Cargar(ctx); err != nil {
		t.Fatal(err)
	}
	// Cargar no avisa a los suscriptores: se arma como al arrancar.
	e.app.SetConfig(e.aj.Actual())
	e.app.Pipeline.SetVision(armarVisionPrueba(e.aj.Actual()))
	if e.app.Pipeline.Vision() != nil {
		t.Fatal("externa sin aceptar: no se arma")
	}
	_, m := sup.pedir("GET", "/api/configuracion", nil)
	ia := m["ia_aviso"].(map[string]any)
	if ia["externa"] != true || ia["aceptado"] != false || ia["en_uso"] != false || ia["proveedor"] != "google" || ia["puede_aceptar"] != true {
		t.Fatalf("Configuración tiene que mostrar el aviso para aceptar: %v", ia)
	}
	if a := avisoDe(ia); a["alerta"].(map[string]any)["nivel"] != "mal" {
		t.Errorf("Google avisa fuerte por el plan gratuito: %v", a["alerta"])
	}
	_, m = sup.pedir("GET", "/healthz", nil)
	if m["ia"].(map[string]any)["habilitada"] != false {
		t.Errorf("salud: %v", m["ia"])
	}

	// Pantalla (sesión y clave), canales y MCP: nadie llama al modelo.
	if st, m := sup.verificarFoto(true); st != 200 {
		t.Fatalf("verificar: %d %v", st, m)
	}
	b, _ := json.Marshal(map[string]any{"nombre": "foto.png", "contenido_base64": base64.StdEncoding.EncodeToString(foto(t)), "ia": "true"})
	if st, _, _ := e.pedir(t, "POST", "/api/verificar", claveOp, bytes.NewReader(b), "application/json"); st != 200 {
		t.Fatalf("verificar con clave: %d", st)
	}
	resp := e.app.ManejarMensaje(ctx, canal.Mensaje{Canal: "webhook", Chat: "c1", Remitente: "r1", Texto: "verificar", Archivos: []canal.Archivo{{Nombre: "foto.png", Datos: foto(t)}}})
	if resp == "" {
		t.Fatal("el canal tiene que contestar")
	}
	mcp := func(cuerpo string) string {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+claveOp)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		out, _ := io.ReadAll(res.Body)
		return string(out)
	}
	mcp(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	out := mcp(fmt.Sprintf(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"verificar_factura","arguments":{"nombre":"foto.png","contenido_base64":%q}}}`, base64.StdEncoding.EncodeToString(foto(t))))
	if !strings.Contains(out, "veredicto") {
		t.Fatalf("MCP verificar: %s", out)
	}
	if n := llamadas.Load(); n != 0 {
		t.Fatalf("una IA externa sin aceptar no se usa: %d llamadas", n)
	}

	// Aceptada desde Configuración (sin cambios: la URL la fija el entorno), se usa en todos lados.
	st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{}, "aceptar_aviso_ia": map[string]any{"host": "generativelanguage.googleapis.com", "version": avisoia.Version}})
	if st != 200 || m["ia_aviso"].(map[string]any)["en_uso"] != true {
		t.Fatalf("aceptar la del entorno: %d %v", st, m)
	}
	if e.app.Pipeline.Vision() == nil {
		t.Fatal("aceptada, se arma")
	}
	e.app.ManejarMensaje(ctx, canal.Mensaje{Canal: "webhook", Chat: "c1", Remitente: "r1", Texto: "verificar", Archivos: []canal.Archivo{{Nombre: "foto.png", Datos: foto(t)}}})
	if llamadas.Load() != 1 {
		t.Fatalf("canal con la IA aceptada: %d llamadas", llamadas.Load())
	}
	mcp(fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"verificar_factura","arguments":{"nombre":"foto.png","contenido_base64":%q}}}`, base64.StdEncoding.EncodeToString(foto(t))))
	if llamadas.Load() != 2 {
		t.Fatalf("MCP con la IA aceptada: %d llamadas", llamadas.Load())
	}
}
