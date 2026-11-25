package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/canal"
)

func TestDecodificar(t *testing.T) {
	pdf := []byte("%PDF-1.4 hola")
	png := []byte("\x89PNG\r\n\x1a\n resto")
	body := `{
		"chat": "5491100000000@s.whatsapp.net",
		"remitente": 5491100000000,
		"nombre_remitente": " Diego ",
		"texto": "te paso las facturas",
		"archivos": [
			{"nombre": "f.pdf", "mime": "application/pdf", "contenido_base64": "` + base64.StdEncoding.EncodeToString(pdf) + `"},
			{"nombre": "C:\\fotos\\ticket.png", "contenido_base64": "data:image/png;base64,` + base64.RawURLEncoding.EncodeToString(png) + `"},
			{"contenido_base64": "` + base64.StdEncoding.EncodeToString(pdf) + `"}
		]
	}`
	m, err := Decodificar([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if m.Canal != "webhook" || m.Chat != "5491100000000@s.whatsapp.net" || m.Remitente != "5491100000000" ||
		m.NombreRemitente != "Diego" || m.Texto != "te paso las facturas" {
		t.Errorf("mensaje mal: %+v", m)
	}
	if len(m.Archivos) != 3 {
		t.Fatalf("archivos = %d", len(m.Archivos))
	}
	if a := m.Archivos[0]; a.Nombre != "f.pdf" || a.Mime != "application/pdf" || string(a.Datos) != string(pdf) {
		t.Errorf("archivo 1 mal: %+v", a)
	}
	if a := m.Archivos[1]; a.Nombre != "ticket.png" || a.Mime != "image/png" || string(a.Datos) != string(png) {
		t.Errorf("archivo 2 mal (nombre sin ruta, mime por extensión, base64 URL sin relleno): %q %q", a.Nombre, a.Mime)
	}
	if a := m.Archivos[2]; a.Nombre != "archivo-3" || a.Mime != "application/pdf" {
		t.Errorf("archivo 3 mal (nombre por defecto, mime detectado): %q %q", a.Nombre, a.Mime)
	}
}

func TestDecodificarErrores(t *testing.T) {
	casos := map[string]string{
		"json roto":     `{"chat":`,
		"sin chat":      `{"texto":"hola"}`,
		"vacío":         `{"chat":"1"}`,
		"base64 roto":   `{"chat":"1","archivos":[{"nombre":"a.pdf","contenido_base64":"%%%"}]}`,
		"sin contenido": `{"chat":"1","archivos":[{"nombre":"a.pdf"}]}`,
		"chat objeto":   `{"chat":{"id":1},"texto":"x"}`,
	}
	for nombre, body := range casos {
		if _, err := Decodificar([]byte(body)); err == nil {
			t.Errorf("%s: tendría que fallar", nombre)
		}
	}
}

func TestDecodificarTope(t *testing.T) {
	mitad := base64.StdEncoding.EncodeToString(make([]byte, MaxDecodificado/2+10))
	body := `{"chat":"1","archivos":[{"nombre":"a","contenido_base64":"` + mitad + `"},{"nombre":"b","contenido_base64":"` + mitad + `"}]}`
	if _, err := Decodificar([]byte(body)); err == nil || !strings.Contains(err.Error(), "25 MB") {
		t.Errorf("pasarse de 25 MB en total tendría que fallar, dio %v", err)
	}
	justo := base64.StdEncoding.EncodeToString(make([]byte, MaxDecodificado))
	if _, err := Decodificar([]byte(`{"chat":"1","archivos":[{"nombre":"a","contenido_base64":"` + justo + `"}]}`)); err != nil {
		t.Errorf("25 MB justos tendrían que entrar: %v", err)
	}
	if int64(len(justo)) > MaxCuerpo {
		t.Errorf("MaxCuerpo (%d) no alcanza para el base64 de MaxDecodificado (%d)", MaxCuerpo, len(justo))
	}
}

func TestAutorizado(t *testing.T) {
	w := New(Config{Token: "secreto"})
	if !w.Autorizado("secreto") {
		t.Error("el token correcto tendría que pasar")
	}
	for _, malo := range []string{"", "secret", "secreto ", "SECRETO", "otro"} {
		if w.Autorizado(malo) {
			t.Errorf("%q no tendría que pasar", malo)
		}
	}
	if New(Config{}).Autorizado("") {
		t.Error("sin token configurado no se autoriza nada")
	}
}

func TestRecibir(t *testing.T) {
	w := New(Config{Token: "x"})
	if err := w.Recibir(context.Background(), canal.Mensaje{Chat: "1"}); !errors.Is(err, ErrNoIniciado) {
		t.Errorf("antes de Iniciar: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	llegados := make(chan canal.Mensaje, 1)
	fin := make(chan error, 1)
	go func() {
		fin <- w.Iniciar(ctx, func(hctx context.Context, m canal.Mensaje) {
			if hctx.Err() != nil {
				t.Error("el manejador tendría que recibir un contexto vivo")
			}
			llegados <- m
		})
	}()
	esperar(t, func() bool { return w.Estado().Conectado })

	// El contexto del pedido se cancela enseguida (como al responder el HTTP): el manejador sigue.
	rctx, rcancel := context.WithCancel(context.Background())
	if err := w.Recibir(rctx, canal.Mensaje{Chat: "1", Texto: "hola"}); err != nil {
		t.Fatal(err)
	}
	rcancel()
	select {
	case m := <-llegados:
		if m.Texto != "hola" || m.Canal != "webhook" {
			t.Errorf("mensaje = %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("el manejador no recibió el mensaje")
	}

	if err := w.Iniciar(ctx, func(context.Context, canal.Mensaje) {}); err == nil {
		t.Error("iniciar dos veces tendría que fallar")
	}

	cancel()
	select {
	case err := <-fin:
		if err != nil {
			t.Errorf("Iniciar devolvió %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Iniciar no terminó al cancelar")
	}
	if err := w.Recibir(context.Background(), canal.Mensaje{Chat: "1"}); !errors.Is(err, ErrNoIniciado) {
		t.Errorf("después de cancelar: %v", err)
	}
	if w.Estado().Conectado {
		t.Error("después de cancelar no está conectado")
	}
}

func TestEnviarFirmado(t *testing.T) {
	var (
		mu     sync.Mutex
		cuerpo []byte
		firma  string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		cuerpo, firma = b, r.Header.Get(CabeceraFirma)
		mu.Unlock()
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
		}
	}))
	defer srv.Close()

	w := New(Config{Token: "clave", SalidaURL: srv.URL})
	if err := w.Enviar(context.Background(), "chat-1", "listo: 3 comprobantes"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var got map[string]string
	if err := json.Unmarshal(cuerpo, &got); err != nil {
		t.Fatal(err)
	}
	if got["chat"] != "chat-1" || got["texto"] != "listo: 3 comprobantes" || len(got) != 2 {
		t.Errorf("cuerpo = %s", cuerpo)
	}
	mac := hmac.New(sha256.New, []byte("clave"))
	mac.Write(cuerpo)
	if want := "sha256=" + hex.EncodeToString(mac.Sum(nil)); firma != want {
		t.Errorf("firma = %q, quería %q", firma, want)
	}
	if Firmar("clave", cuerpo) != firma {
		t.Error("Firmar no coincide con la cabecera")
	}
}

func TestEnviarReintenta(t *testing.T) {
	var intentos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if intentos.Add(1) < 3 {
			rw.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	w := New(Config{Token: "k", SalidaURL: srv.URL})
	w.esperaInicial = time.Millisecond
	if err := w.Enviar(context.Background(), "1", "x"); err != nil {
		t.Fatalf("al tercer intento tendría que andar: %v", err)
	}
	if n := intentos.Load(); n != 3 {
		t.Errorf("intentos = %d", n)
	}
}

func TestEnviarSeRinde(t *testing.T) {
	var intentos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		intentos.Add(1)
		rw.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	w := New(Config{Token: "k", SalidaURL: srv.URL})
	w.esperaInicial = time.Millisecond
	if err := w.Enviar(context.Background(), "1", "x"); err == nil {
		t.Fatal("tendría que fallar")
	}
	if n := intentos.Load(); n != 3 {
		t.Errorf("intentos = %d, quería 1 + 2 reintentos", n)
	}
	if d := w.Estado().Detalle; !strings.Contains(d, "falló") {
		t.Errorf("el estado tendría que mostrar el fallo: %q", d)
	}
}

func TestEnviarNoReintenta4xx(t *testing.T) {
	var intentos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		intentos.Add(1)
		rw.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	w := New(Config{Token: "k", SalidaURL: srv.URL})
	w.esperaInicial = time.Millisecond
	if err := w.Enviar(context.Background(), "1", "x"); err == nil {
		t.Fatal("tendría que fallar")
	}
	if n := intentos.Load(); n != 1 {
		t.Errorf("un 401 no se reintenta: intentos = %d", n)
	}
}

func TestEnviarTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("tarda más de 10 segundos")
	}
	var intentos atomic.Int32
	liberar := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		intentos.Add(1)
		_, _ = io.ReadAll(r.Body)
		select {
		case <-r.Context().Done():
		case <-liberar:
		}
	}))
	defer srv.Close()
	defer close(liberar)
	w := New(Config{Token: "k", SalidaURL: srv.URL})
	w.esperaInicial = time.Millisecond
	// Un contexto de 11 s: el primer intento corta a los 10 s (timeout propio) y el segundo por ctx.
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Second)
	defer cancel()
	inicio := time.Now()
	if err := w.Enviar(ctx, "1", "x"); err == nil {
		t.Fatal("tendría que fallar")
	}
	if d := time.Since(inicio); d < 10*time.Second || d > 13*time.Second {
		t.Errorf("tardó %v", d)
	}
	if n := intentos.Load(); n != 2 {
		t.Errorf("intentos = %d, quería 2", n)
	}
}

func TestEnviarSinURL(t *testing.T) {
	w := New(Config{Token: "k"})
	if err := w.Enviar(context.Background(), "1", "x"); err == nil {
		t.Error("sin SalidaURL tendría que fallar")
	}
	if d := w.Estado().Detalle; !strings.Contains(d, "sin URL de salida") {
		t.Errorf("detalle = %q", d)
	}
}

func TestEnviarErrorSinURL(t *testing.T) {
	// La URL de salida puede llevar credenciales: no tiene que aparecer en el error.
	w := New(Config{Token: "k", SalidaURL: "http://127.0.0.1:1/hook?clave=SECRETA"})
	w.esperaInicial = time.Millisecond
	err := w.Enviar(context.Background(), "1", "x")
	if err == nil {
		t.Fatal("tendría que fallar")
	}
	if strings.Contains(err.Error(), "SECRETA") {
		t.Errorf("el error filtra la URL: %v", err)
	}
}

func esperar(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("se agotó la espera")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
