package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/diegoparras/factorum/internal/avisoia"
)

// logSeguro junta el log del servidor para buscar claves en él.
type logSeguro struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *logSeguro) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *logSeguro) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

const ticketLeido = `{"clase":"otro","letra":null,"codigo_tipo":83,"punto_venta":3,"numero":45678,"fecha":"2026-11-22","cuit_emisor":"30708888881","razon_social_emisor":"EL TREBOL S.R.L.","condicion_iva_emisor":null,"cuit_receptor":null,"razon_social_receptor":null,"neto_gravado":null,"no_gravado":null,"exento":null,"iva_27":null,"iva_21":null,"iva_10_5":null,"iva_5":null,"iva_2_5":null,"otros_tributos":null,"subtotal":null,"total":44062.5,"moneda":null,"cae":null,"vto_cae":null}`

// servidorChat es un proveedor compatible con OpenAI de mentira: lista modelos (con cosas que no
// leen imágenes) y contesta el ticket de prueba si la clave y el modelo están bien. Si la clave está
// mal, la repite en el error, como OpenAI.
type servidorChat struct {
	*httptest.Server
	chats atomic.Int32
	lista atomic.Int32
}

func nuevoServidorChat(t *testing.T, modelo, clave string) *servidorChat {
	t.Helper()
	s := &servidorChat{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+clave {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: ` + strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") + `"}}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/models"):
			s.lista.Add(1)
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-6.1-terra","created":30},{"id":"gpt-6-luna","created":20},{"id":"` + modelo + `","created":10},{"id":"whisper-1","created":5},{"id":"gpt-image-2","created":40},{"id":"text-embedding-3-large","created":1}]}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			s.chats.Add(1)
			var b struct {
				Model string `json:"model"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b.Model != modelo {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{"error":{"message":"The model ` + b.Model + ` does not exist","code":"model_not_found"}}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": ticketLeido}}}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func chatDePrueba(t *testing.T, modelo, clave string) *servidorChat {
	return nuevoServidorChat(t, modelo, clave)
}

// comoProveedor manda lo que va a cualquier host externo (api.openai.com...) a este servidor.
func (s *servidorChat) comoProveedor(t *testing.T) {
	iaFalsa.Store(s.Listener.Addr().String())
	t.Cleanup(func() { iaFalsa.Store("") })
}

func modelosDe(m map[string]any) []string {
	var r []string
	ms, _ := m["modelos"].([]any)
	for _, x := range ms {
		r = append(r, x.(map[string]any)["id"].(string))
	}
	return r
}

const claveIA = "sk-proj-clave-de-diego-1234567890"

func TestModelosIA(t *testing.T) {
	logs := &logSeguro{}
	e := armarOpc(t, opcEntorno{log: logs})
	sup := e.crearSuperadmin(t)
	prov := nuevoServidorChat(t, "gpt-6-sol", claveIA)
	prov.comoProveedor(t)

	// Solo un superadmin con sesión.
	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	if st, _ := adm.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai", "clave": claveIA}); st != 403 {
		t.Errorf("un admin pide la lista: %d", st)
	}
	b, _ := json.Marshal(map[string]any{"proveedor": "openai", "clave": claveIA})
	if st, _, _ := e.pedir(t, "POST", "/api/ia/modelos", claveOp, bytes.NewReader(b), "application/json"); st != 401 && st != 403 {
		t.Errorf("con una clave de API: %d", st)
	}

	// OpenAI: la lista filtrada, los recomendados marcados y la clave que no vuelve.
	st, m, crudo := sup.pedirCrudo("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai", "url": "", "clave": claveIA})
	if st != 200 || m["exito"] != true || strings.Join(modelosDe(m), ",") != "gpt-6.1-terra,gpt-6-luna,gpt-6-sol" {
		t.Fatalf("lista de OpenAI: %d %v", st, m)
	}
	if m["url"] != "https://api.openai.com/v1" || m["local"] != false || m["sello"] != "IA externa: OpenAI" || m["clave_guardada"] != false {
		t.Errorf("datos de la lista: %v", m)
	}
	primero := m["modelos"].([]any)[0].(map[string]any)
	if r, _ := primero["recomendado"].(map[string]any); r == nil || r["texto"] != "Probado por Factorum: 100 % · ~7 s por imagen" {
		t.Errorf("recomendado: %v", primero)
	}
	if len(m["recomendados"].([]any)) != 2 {
		t.Errorf("los recomendados de OpenAI: %v", m["recomendados"])
	}
	if strings.Contains(string(crudo), claveIA) {
		t.Error("la clave vuelve en la respuesta")
	}

	// Clave inválida: el mensaje claro, sin la clave (aunque el proveedor la repita).
	mala := "sk-proj-otra-clave-que-no-sirve-999"
	_, m, crudo = sup.pedirCrudo("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai", "clave": mala})
	if m["exito"] != false || m["motivo"] != "clave" || m["mensaje"] != "La clave no es válida para OpenAI" || strings.Contains(string(crudo), mala) {
		t.Errorf("clave inválida: %s", crudo)
	}
	// Sin clave (y sin una guardada): la pide.
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "anthropic"}); m["exito"] != false || m["motivo"] != "sin_clave" {
		t.Errorf("sin clave: %v", m)
	}
	// Una URL que no es del proveedor no se acepta.
	if st, _ := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai", "url": "https://ejemplo.com/v1", "clave": claveIA}); st != 400 {
		t.Errorf("URL de otro: %d", st)
	}
	if st, _ := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "inventado"}); st != 400 {
		t.Errorf("proveedor inventado: %d", st)
	}

	// Con la clave guardada: si la de la pantalla está vacía, se usa la guardada, solo para su host.
	cambios := map[string]any{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6-sol", "ia.api_key": claveIA}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}}); st != 200 {
		t.Fatalf("guardar OpenAI: %d %v", st, m)
	}
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai"}); m["exito"] != true || m["clave_guardada"] != true {
		t.Errorf("con la clave guardada: %v", m)
	}
	// Otra región de OpenAI es la misma cuenta: la clave guardada sirve.
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "openai", "url": "https://eu.api.openai.com/v1"}); m["exito"] != true || m["clave_guardada"] != true {
		t.Errorf("la clave guardada en otra región de OpenAI: %v", m)
	}
	// A OpenRouter no le llega la clave de OpenAI (el de mentira pide la clave: dice que falta).
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "openrouter"}); m["exito"] != false || m["motivo"] != "sin_clave" || m["clave_guardada"] != false {
		t.Errorf("OpenRouter con la clave de OpenAI: %v", m)
	}
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "otro", "url": "https://gateway.ejemplo.com/v1"}); m["exito"] != false || m["motivo"] != "sin_clave" || m["mensaje"] != "El servidor pide una clave: escribila para ver sus modelos" {
		t.Errorf("a otro host, sin clave: %v", m)
	}

	// Ollama: el local de la prueba, con y sin modelos que lean imágenes; y apagado.
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("a Ollama no se le manda la clave")
		}
		switch r.URL.Path {
		case "/api/tags":
			_, _ = w.Write([]byte(`{"models":[{"name":"llama3.2:3b","capabilities":["completion"]},{"name":"qwen3-vl:4b-instruct","capabilities":["completion","vision"],"size":3337225651,"details":{"parameter_size":"4.0B"}}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer ollama.Close()
	_, m = sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "ollama", "url": ollama.URL + "/v1", "clave": claveIA})
	if m["exito"] != true || strings.Join(modelosDe(m), ",") != "qwen3-vl:4b-instruct,llama3.2:3b" || m["local"] != true {
		t.Fatalf("Ollama: %v", m)
	}
	if x := m["modelos"].([]any)[1].(map[string]any); x["imagenes"] != false || x["nota"] != "no lee imágenes" {
		t.Errorf("el que no lee imágenes: %v", x)
	}
	ollama.Close()
	if _, m := sup.pedir("POST", "/api/ia/modelos", map[string]any{"proveedor": "ollama", "url": ollama.URL + "/v1"}); m["exito"] != false || m["motivo"] != "red" || !strings.Contains(m["mensaje"].(string), "Ollama no responde") {
		t.Errorf("Ollama apagado: %v", m)
	}

	// Nada de esto deja la clave en el log ni en la auditoría.
	if strings.Contains(logs.String(), claveIA) || strings.Contains(logs.String(), mala) {
		t.Error("la clave quedó en el log")
	}
	_, _, aud := sup.pedirCrudo("GET", "/api/auditoria?limite=200", nil)
	if strings.Contains(string(aud), claveIA) {
		t.Error("la clave quedó en la auditoría")
	}
}

func TestProbarIA(t *testing.T) {
	logs := &logSeguro{}
	e := armarOpc(t, opcEntorno{log: logs})
	sup := e.crearSuperadmin(t)
	prov := nuevoServidorChat(t, "gpt-6.1-terra", claveIA)
	prov.comoProveedor(t)

	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	if st, _ := adm.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": map[string]any{}}); st != 403 {
		t.Errorf("un admin prueba la IA: %d", st)
	}

	// Una externa sin aviso aceptado: primero el aviso, sin mandar nada.
	cambios := map[string]any{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6.1-terra", "ia.api_key": claveIA}
	st, m := sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": cambios})
	if st != 409 || m["codigo"] != "aviso_ia" || m["aviso_ia"].(map[string]any)["host"] != "api.openai.com" || prov.chats.Load() != 0 {
		t.Fatalf("probar sin el aviso: %d %v (chats %d)", st, m, prov.chats.Load())
	}
	if st, _ := sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.anthropic.com", "version": avisoia.Version}}); st != 409 {
		t.Errorf("aceptar el aviso de otro host: %d", st)
	}
	// Visto el aviso, prueba con lo que está en pantalla y dice qué leyó y cuánto tardó.
	st, m, crudo := sup.pedirCrudo("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}})
	if st != 200 || m["exito"] != true || !strings.Contains(m["mensaje"].(string), "leyó bien el ticket de prueba") || prov.chats.Load() != 1 {
		t.Fatalf("probar: %d %v", st, m)
	}
	leido := m["leido"].([]any)
	if leido[0].(map[string]any)["valor"] != "30-70888888-1" || leido[1].(map[string]any)["valor"] != "$ 44.062,50" || m["modelo"] != "gpt-6.1-terra" {
		t.Errorf("qué leyó: %v", leido)
	}
	if strings.Contains(string(crudo), claveIA) {
		t.Error("la clave vuelve en la respuesta")
	}
	// Probar no guarda nada, ni deja el aviso aceptado; queda en la auditoría.
	if c := e.app.Config(); c.LLMBaseURL != "" || c.IAAvisoHost != "" || c.IAProveedor != "" {
		t.Fatalf("probar guardó: %+v", c)
	}
	_, _, aud := sup.pedirCrudo("GET", "/api/auditoria?accion=ia.aviso_aceptado_prueba", nil)
	if !strings.Contains(string(aud), "api.openai.com") {
		t.Errorf("auditoría de la prueba: %s", aud)
	}

	// Clave inválida y modelo que no está.
	mal := map[string]any{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6.1-terra", "ia.api_key": "sk-proj-mala-pero-bien-larga-0000"}
	_, m, crudo = sup.pedirCrudo("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": mal, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}})
	if m["exito"] != false || m["mensaje"] != "La clave no es válida para OpenAI." || strings.Contains(string(crudo), "sk-proj-mala") {
		t.Errorf("clave inválida: %s", crudo)
	}
	mal["ia.api_key"], mal["ia.modelo"] = claveIA, "gpt-inventado"
	if _, m := sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": mal, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}}); m["exito"] != false || !strings.Contains(m["mensaje"].(string), "no tiene el modelo gpt-inventado") {
		t.Errorf("modelo que no está: %v", m)
	}
	// Sin clave, ni modelo, ni URL de OpenAI.
	for _, c := range []map[string]any{
		{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6.1-terra"},
		{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.api_key": claveIA},
		{"ia.proveedor": "openai", "ia.url": "https://ejemplo.com/v1", "ia.modelo": "x", "ia.api_key": claveIA},
	} {
		if _, m := sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": c}); m["exito"] != false {
			t.Errorf("%v: %v", c, m)
		}
	}

	// Guardada y aceptada, Probar no vuelve a pedir el aviso y usa la clave guardada.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": cambios, "aceptar_aviso_ia": map[string]any{"host": "api.openai.com", "version": avisoia.Version}}); st != 200 {
		t.Fatalf("guardar: %d %v", st, m)
	}
	if st, m := sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": map[string]any{}}); st != 200 || m["exito"] != true {
		t.Errorf("probar lo guardado: %d %v", st, m)
	}
	if strings.Contains(logs.String(), claveIA) {
		t.Error("la clave quedó en el log")
	}
}

func TestGuardarProveedorIA(t *testing.T) {
	e := armar(t)
	sup := e.crearSuperadmin(t)
	campo := func(clave string) map[string]any {
		_, m := sup.pedir("GET", "/api/configuracion", nil)
		for _, x := range m["campos"].([]any) {
			if c := x.(map[string]any); c["clave"] == clave {
				return c
			}
		}
		return nil
	}

	// Una configuración de antes (sin proveedor): se deduce de la URL.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.url": "http://ollama:11434/v1", "ia.modelo": "qwen3-vl:4b-instruct"}}); st != 200 {
		t.Fatalf("guardar como antes: %d %v", st, m)
	}
	_, m := sup.pedir("GET", "/api/configuracion", nil)
	ia := m["ia"].(map[string]any)
	if ia["proveedor"] != "ollama" || len(ia["proveedores"].([]any)) != 6 || len(ia["recomendados"].([]any)) != 4 || ia["prueba"] == "" {
		t.Fatalf("catálogo de la IA: %v", ia)
	}
	if c := campo("ia.proveedor"); c == nil || c["tipo"] != "opcion" || c["env"] != "FACTORUM_IA_PROVEEDOR" || len(c["opciones"].([]any)) != 6 {
		t.Errorf("campo ia.proveedor: %v", c)
	}
	if _, m := sup.pedir("GET", "/api/ia/aviso", nil); m["eleccion"] != "ollama" || len(m["recomendados"].([]any)) != 4 {
		t.Errorf("aviso con la elección y los recomendados: %v", m)
	}

	// OpenAI con su clave; después Anthropic sin clave nueva: la de OpenAI no se queda.
	ok := map[string]any{"host": "api.openai.com", "version": avisoia.Version}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "openai", "ia.url": "https://api.openai.com/v1", "ia.modelo": "gpt-6-luna", "ia.api_key": claveIA}, "aceptar_aviso_ia": ok}); st != 200 {
		t.Fatalf("guardar OpenAI: %d %v", st, m)
	}
	if c := e.app.Config(); c.IAProveedor != "openai" || c.LLMAPIKey != claveIA {
		t.Fatalf("OpenAI guardado: %+v", c)
	}
	// Otro modelo, mismo host: la clave sigue.
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.modelo": "gpt-6.1-terra"}}); st != 200 || e.app.Config().LLMAPIKey != claveIA {
		t.Fatalf("otro modelo: %d", st)
	}
	// Otra región de OpenAI: la misma cuenta, la clave sigue (y se acepta el aviso de ese host).
	eu := map[string]any{"host": "eu.api.openai.com", "version": avisoia.Version}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.url": "https://eu.api.openai.com/v1"}, "aceptar_aviso_ia": eu}); st != 200 || e.app.Config().LLMAPIKey != claveIA {
		t.Fatalf("región de OpenAI: %d %v", st, m)
	}
	// Un proveedor con otra URL: no se guarda.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "anthropic"}}); st != 400 || !strings.Contains(m["error"].(string), "no es de Anthropic") {
		t.Errorf("proveedor y URL que no van juntos: %d %v", st, m)
	}
	acepta := map[string]any{"host": "api.anthropic.com", "version": avisoia.Version}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "anthropic", "ia.url": "https://api.anthropic.com/v1", "ia.modelo": "claude-sonnet-5-5"}, "aceptar_aviso_ia": acepta}); st != 200 {
		t.Fatalf("guardar Anthropic: %d %v", st, m)
	}
	if c := e.app.Config(); c.LLMAPIKey != "" || c.IAProveedor != "anthropic" {
		t.Errorf("la clave de OpenAI no viaja a Anthropic: %q", c.LLMAPIKey)
	}
	if c := campo("ia.api_key"); c["configurado"] != false {
		t.Errorf("la clave quedó: %v", c)
	}
	// Ollama no lleva clave.
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "anthropic", "ia.api_key": "sk-ant-clave-larga-de-prueba"}}); st != 200 || e.app.Config().LLMAPIKey == "" {
		t.Fatalf("clave de Anthropic: %d", st)
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "ollama", "ia.url": "http://ollama:11434/v1", "ia.modelo": "qwen3-vl:4b-instruct"}}); st != 200 || e.app.Config().LLMAPIKey != "" {
		t.Fatalf("Ollama sin clave: %d %q", st, e.app.Config().LLMAPIKey)
	}
	// Un Ollama en una dirección pública es externo: pide el aviso, que lo nombra como Ollama.
	st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.url": "https://ollama.ejemplo.com/v1"}})
	if a, _ := m["aviso_ia"].(map[string]any); st != 409 || a["nombre"] != "Ollama en ollama.ejemplo.com" || avisoDe(a)["sello"] != "IA externa: Ollama en ollama.ejemplo.com" {
		t.Errorf("Ollama público: %d %v", st, m)
	}

	// El proveedor del entorno gana y bloquea el campo.
	e.fijadas.Store("FACTORUM_IA_PROVEEDOR", "ollama")
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.proveedor": "openai"}}); st != 409 {
		t.Errorf("proveedor fijado por el entorno: %d", st)
	}
	if c := campo("ia.proveedor"); c["bloqueado"] != true {
		t.Errorf("bloqueado: %v", c)
	}
}
