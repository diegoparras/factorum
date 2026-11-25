package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/diegoparras/factorum/internal/canal"
)

const tokenPrueba = "123:ABC"

// apiFalsa simula la Bot API: getMe, getUpdates (entrega una tanda y después espera),
// getFile, la descarga de archivos y sendMessage.
type apiFalsa struct {
	t *testing.T

	mu         sync.Mutex
	tandas     [][]update
	offsets    []int64
	archivos   map[string]archivo // file_id -> archivo
	contenidos map[string][]byte  // file_path -> datos
	enviados   []map[string]any
	pollFalla  int // cuántos getUpdates fallan antes de andar
}

func (a *apiFalsa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefijo := "/bot" + tokenPrueba + "/"
	prefijoArchivo := "/file/bot" + tokenPrueba + "/"
	switch {
	case strings.HasPrefix(r.URL.Path, prefijoArchivo):
		ruta := strings.TrimPrefix(r.URL.Path, prefijoArchivo)
		a.mu.Lock()
		datos, ok := a.contenidos[ruta]
		a.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(datos)
		return
	case !strings.HasPrefix(r.URL.Path, prefijo):
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"ok":false,"error_code":401,"description":"Unauthorized"}`)
		return
	}
	var params map[string]any
	_ = json.NewDecoder(r.Body).Decode(&params)
	switch metodo := strings.TrimPrefix(r.URL.Path, prefijo); metodo {
	case "getMe":
		responder(w, usuario{ID: 1, IsBot: true, FirstName: "Factorum", Username: "factorum_bot"})
	case "getUpdates":
		a.mu.Lock()
		if a.pollFalla > 0 {
			a.pollFalla--
			a.mu.Unlock()
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":502,"description":"Bad Gateway"}`)
			return
		}
		off, _ := params["offset"].(float64)
		a.offsets = append(a.offsets, int64(off))
		if len(a.tandas) > 0 {
			tanda := a.tandas[0]
			a.tandas = a.tandas[1:]
			a.mu.Unlock()
			responder(w, tanda)
			return
		}
		a.mu.Unlock()
		// Sin novedades: como el long polling real, retiene hasta que el cliente corta.
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
		responder(w, []update{})
	case "getFile":
		id, _ := params["file_id"].(string)
		a.mu.Lock()
		f, ok := a.archivos[id]
		a.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"ok":false,"error_code":400,"description":"Bad Request: invalid file_id"}`)
			return
		}
		responder(w, f)
	case "sendMessage":
		a.mu.Lock()
		a.enviados = append(a.enviados, params)
		a.mu.Unlock()
		responder(w, map[string]any{"message_id": 99})
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"ok":false,"error_code":404,"description":"Not Found: %s"}`, metodo)
	}
}

func responder(w http.ResponseWriter, v any) {
	res, _ := json.Marshal(v)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": json.RawMessage(res)})
}

func nuevo(t *testing.T, api *apiFalsa, max int64) (*Telegram, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	tg := New(Config{Token: tokenPrueba, BaseURL: srv.URL, MaxBytes: max})
	tg.esperaInicial = 10 * time.Millisecond
	return tg, srv
}

func TestRecibeDocumentoFotoYTexto(t *testing.T) {
	pdf := []byte("%PDF-1.4 factura de prueba")
	jpg := []byte("\xff\xd8\xff\xe0 foto grande")
	api := &apiFalsa{
		t: t,
		tandas: [][]update{{
			{UpdateID: 10, Message: &message{
				MessageID: 1, From: &usuario{ID: 555, FirstName: "Diego", LastName: "Parras"}, Chat: chat{ID: 777, Type: "private"},
				Caption:  "la de noviembre",
				Document: &documento{FileID: "doc1", FileName: "factura.pdf", MimeType: "application/pdf", FileSize: int64(len(pdf))},
			}},
			{UpdateID: 11, Message: &message{
				MessageID: 2, From: &usuario{ID: 556, Username: "contadora"}, Chat: chat{ID: -100123, Type: "group"},
				Photo: []foto{
					{FileID: "chica", Width: 90, Height: 90, FileSize: 100},
					{FileID: "grande", Width: 1280, Height: 960, FileSize: int64(len(jpg))},
					{FileID: "media", Width: 320, Height: 240, FileSize: 1000},
				},
			}},
			{UpdateID: 12, Message: &message{MessageID: 3, From: &usuario{ID: 557, FirstName: "Ana"}, Chat: chat{ID: 778}, Text: "hola"}},
		}},
		archivos: map[string]archivo{
			"doc1":   {FileID: "doc1", FileSize: int64(len(pdf)), FilePath: "documents/file_1.pdf"},
			"grande": {FileID: "grande", FileSize: int64(len(jpg)), FilePath: "photos/file_2.jpg"},
			"chica":  {FileID: "chica", FileSize: 100, FilePath: "photos/chica.jpg"},
		},
		contenidos: map[string][]byte{
			"documents/file_1.pdf": pdf,
			"photos/file_2.jpg":    jpg,
			"photos/chica.jpg":     []byte("chica"),
		},
	}
	tg, _ := nuevo(t, api, 0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llegados := make(chan canal.Mensaje, 10)
	fin := make(chan error, 1)
	go func() { fin <- tg.Iniciar(ctx, func(_ context.Context, m canal.Mensaje) { llegados <- m }) }()

	var ms []canal.Mensaje
	for len(ms) < 3 {
		select {
		case m := <-llegados:
			ms = append(ms, m)
		case <-time.After(5 * time.Second):
			t.Fatalf("llegaron %d mensajes de 3", len(ms))
		}
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].Remitente < ms[j].Remitente })

	doc := ms[0]
	if doc.Canal != "telegram" || doc.Chat != "777" || doc.Remitente != "555" || doc.NombreRemitente != "Diego Parras" {
		t.Errorf("documento: datos del mensaje mal: %+v", doc)
	}
	if doc.Texto != "la de noviembre" {
		t.Errorf("documento: el texto debería ser el caption, es %q", doc.Texto)
	}
	if len(doc.Archivos) != 1 || doc.Archivos[0].Nombre != "factura.pdf" || doc.Archivos[0].Mime != "application/pdf" || string(doc.Archivos[0].Datos) != string(pdf) {
		t.Errorf("documento: adjunto mal: %+v", doc.Archivos)
	}

	ft := ms[1]
	if ft.Chat != "-100123" || ft.NombreRemitente != "contadora" {
		t.Errorf("foto: datos del mensaje mal: %+v", ft)
	}
	if len(ft.Archivos) != 1 || ft.Archivos[0].Nombre != "foto.jpg" || ft.Archivos[0].Mime != "image/jpeg" || string(ft.Archivos[0].Datos) != string(jpg) {
		t.Errorf("foto: tendría que bajar la más grande: %+v", ft.Archivos)
	}

	txt := ms[2]
	if txt.Texto != "hola" || len(txt.Archivos) != 0 || txt.NombreRemitente != "Ana" {
		t.Errorf("texto: mal: %+v", txt)
	}

	// El siguiente getUpdates tiene que pedir desde el último update_id + 1.
	deadline := time.Now().Add(3 * time.Second)
	for {
		api.mu.Lock()
		offs := append([]int64(nil), api.offsets...)
		api.mu.Unlock()
		if len(offs) >= 2 {
			if offs[0] != 0 || offs[1] != 13 {
				t.Errorf("offsets = %v, quería [0 13 ...]", offs)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no hubo un segundo getUpdates (offsets %v)", offs)
		}
		time.Sleep(10 * time.Millisecond)
	}

	e := tg.Estado()
	if !e.Habilitado || !e.Conectado || e.Detalle != "@factorum_bot" {
		t.Errorf("estado = %+v", e)
	}

	cancel()
	select {
	case err := <-fin:
		if err != nil {
			t.Errorf("Iniciar devolvió %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Iniciar no terminó al cancelar el contexto")
	}
	if tg.Estado().Conectado {
		t.Error("después de cancelar no debería figurar conectado")
	}
}

func TestArchivoMuyGrande(t *testing.T) {
	api := &apiFalsa{
		t: t,
		tandas: [][]update{{
			{UpdateID: 1, Message: &message{
				From: &usuario{ID: 1, FirstName: "X"}, Chat: chat{ID: 2}, Caption: "va la factura",
				Document: &documento{FileID: "big", FileName: "enorme.pdf", FileSize: 5000},
			}},
			// Sin file_size declarado: se corta al descargar.
			{UpdateID: 2, Message: &message{
				From: &usuario{ID: 1, FirstName: "X"}, Chat: chat{ID: 2},
				Document: &documento{FileID: "sintam", FileName: "otra.pdf"},
			}},
		}},
		archivos:   map[string]archivo{"sintam": {FileID: "sintam", FilePath: "d/otra.pdf"}},
		contenidos: map[string][]byte{"d/otra.pdf": []byte(strings.Repeat("x", 2000))},
	}
	tg, _ := nuevo(t, api, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llegados := make(chan canal.Mensaje, 2)
	go func() { _ = tg.Iniciar(ctx, func(_ context.Context, m canal.Mensaje) { llegados <- m }) }()

	for range 2 {
		select {
		case m := <-llegados:
			if len(m.Archivos) != 0 {
				t.Errorf("no tendría que adjuntar un archivo de más de MaxBytes: %+v", m.Archivos)
			}
			if !strings.Contains(m.Texto, "pesa más de") {
				t.Errorf("el texto tendría que avisar que el archivo es muy grande: %q", m.Texto)
			}
			if strings.Contains(m.Texto, "enorme.pdf") && !strings.HasPrefix(m.Texto, "va la factura\n") {
				t.Errorf("el caption original se tiene que conservar: %q", m.Texto)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("no llegó el mensaje")
		}
	}
}

func TestReintentaSiFallaElPoll(t *testing.T) {
	api := &apiFalsa{
		t:         t,
		pollFalla: 2,
		tandas:    [][]update{{{UpdateID: 5, Message: &message{From: &usuario{ID: 1}, Chat: chat{ID: 1}, Text: "dale"}}}},
	}
	tg, _ := nuevo(t, api, 0)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	llegados := make(chan canal.Mensaje, 1)
	go func() { _ = tg.Iniciar(ctx, func(_ context.Context, m canal.Mensaje) { llegados <- m }) }()
	select {
	case m := <-llegados:
		if m.Texto != "dale" {
			t.Errorf("texto = %q", m.Texto)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no se recuperó de los errores de getUpdates")
	}
}

func TestEnviarPartido(t *testing.T) {
	api := &apiFalsa{t: t}
	tg, _ := nuevo(t, api, 0)

	linea := strings.Repeat("a", 99) + "\n" // 100 caracteres
	texto := strings.Repeat(linea, 100)     // 10.000 caracteres
	if err := tg.Enviar(context.Background(), "777", texto); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.enviados) != 3 {
		t.Fatalf("mandó %d mensajes, quería 3", len(api.enviados))
	}
	var junto strings.Builder
	for _, p := range api.enviados {
		if p["chat_id"] != "777" {
			t.Errorf("chat_id = %v", p["chat_id"])
		}
		if _, hay := p["parse_mode"]; hay {
			t.Error("tiene que ir como texto plano, sin parse_mode")
		}
		s, _ := p["text"].(string)
		if utf8.RuneCountInString(s) > MaxTexto {
			t.Errorf("un trozo tiene %d caracteres", utf8.RuneCountInString(s))
		}
		if !strings.HasSuffix(s, "\n") && junto.Len()+len(s) < len(texto) {
			t.Errorf("tendría que cortar en un salto de línea: termina en %q", s[len(s)-5:])
		}
		junto.WriteString(s)
	}
	if junto.String() != texto {
		t.Error("los trozos juntos no dan el texto original")
	}
}

func TestPartir(t *testing.T) {
	// Sin espacios ni saltos: corte duro, sin romper runas.
	s := strings.Repeat("ñ", 10)
	partes := partir(s, 3)
	if len(partes) != 4 || strings.Join(partes, "") != s {
		t.Errorf("partir = %q", partes)
	}
	for _, p := range partes {
		if !utf8.ValidString(p) {
			t.Errorf("trozo inválido %q", p)
		}
	}
	// Las runas fuera del BMP cuentan 2 unidades UTF-16 (como cuenta Telegram).
	astral := strings.Repeat("\U0001D11E", 5) // clave de sol, 2 unidades cada una
	partes = partir(astral, 4)
	if len(partes) != 3 || utf8.RuneCountInString(partes[0]) != 2 {
		t.Errorf("partir con runas astrales = %q", partes)
	}
	if got := partir("corto", 4096); len(got) != 1 || got[0] != "corto" {
		t.Errorf("partir corto = %q", got)
	}
}

func TestEnviarValida(t *testing.T) {
	api := &apiFalsa{t: t}
	tg, _ := nuevo(t, api, 0)
	if err := tg.Enviar(context.Background(), "", "hola"); err == nil {
		t.Error("sin chat tendría que fallar")
	}
	if err := tg.Enviar(context.Background(), "1", "  "); err == nil {
		t.Error("sin texto tendría que fallar")
	}
}

func TestErroresSinToken(t *testing.T) {
	// Un servidor que corta la conexión: el error de net/http trae la URL, que lleva el token.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hj, _ := w.(http.Hijacker)
		c, _, _ := hj.Hijack()
		_ = c.Close()
	}))
	defer srv.Close()
	tg := New(Config{Token: "999:SECRETO", BaseURL: srv.URL})
	err := tg.Enviar(context.Background(), "1", "hola")
	if err == nil {
		t.Fatal("tendría que fallar")
	}
	if strings.Contains(err.Error(), "SECRETO") {
		t.Errorf("el error filtra el token: %v", err)
	}
}

func TestTokenInvalido(t *testing.T) {
	api := &apiFalsa{t: t}
	srv := httptest.NewServer(api)
	defer srv.Close()
	tg := New(Config{Token: "otro:token", BaseURL: srv.URL})
	tg.esperaInicial = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	fin := make(chan struct{})
	go func() { _ = tg.Iniciar(ctx, func(context.Context, canal.Mensaje) {}); close(fin) }()
	deadline := time.Now().Add(3 * time.Second)
	for tg.Estado().Detalle != "token inválido" {
		if time.Now().After(deadline) {
			t.Fatalf("estado = %+v", tg.Estado())
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-fin:
	case <-time.After(3 * time.Second):
		t.Fatal("Iniciar no terminó al cancelar durante los reintentos")
	}
}

func TestSinToken(t *testing.T) {
	tg := New(Config{})
	if err := tg.Iniciar(context.Background(), func(context.Context, canal.Mensaje) {}); err == nil {
		t.Error("sin token Iniciar tendría que fallar")
	}
	if e := tg.Estado(); e.Habilitado {
		t.Errorf("sin token no está habilitado: %+v", e)
	}
}
