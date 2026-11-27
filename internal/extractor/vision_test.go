package extractor

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Los modelos que solo aceptan la temperatura por defecto (los de razonamiento de OpenAI) rechazan
// temperature 0 con un 400: Factorum repite el pedido sin ella y no la vuelve a mandar.
func TestVisionSinTemperatura(t *testing.T) {
	var pedidos, conTemperatura atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pedidos.Add(1)
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		if _, ok := cuerpo["temperature"]; ok {
			conTemperatura.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported."}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{
			"content": `{"clase":"factura","letra":"B","codigo_tipo":null,"punto_venta":3,"numero":45678,"fecha":"2026-11-22","cuit_emisor":"30708888881","razon_social_emisor":"EL TREBOL S.R.L.","condicion_iva_emisor":null,"cuit_receptor":null,"razon_social_receptor":null,"neto_gravado":null,"no_gravado":null,"exento":null,"iva_27":null,"iva_21":null,"iva_10_5":null,"iva_5":null,"iva_2_5":null,"otros_tributos":null,"subtotal":null,"total":44062.5,"moneda":null,"cae":null,"vto_cae":null}`}}}})
	}))
	defer srv.Close()
	v := &Vision{BaseURL: srv.URL + "/v1", Modelo: "gpt-6-luna"}
	for i := range 2 {
		c, err := v.Extraer(context.Background(), []byte("png"), "image/png", "")
		if err != nil {
			t.Fatalf("pedido %d: %v", i, err)
		}
		if c.Total != 4406250 || c.PuntoVenta != 3 || c.Numero != 45678 {
			t.Fatalf("pedido %d: %+v", i, c)
		}
	}
	// El primero va con temperatura y se repite sin ella; el segundo ya va sin ella.
	if pedidos.Load() != 3 || conTemperatura.Load() != 1 {
		t.Fatalf("pedidos %d, con temperatura %d", pedidos.Load(), conTemperatura.Load())
	}
}

// Otro 400 (por ejemplo, un modelo que no existe) no se reintenta.
func TestVisionOtro400NoReintenta(t *testing.T) {
	var pedidos atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pedidos.Add(1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist"}}`))
	}))
	defer srv.Close()
	v := &Vision{BaseURL: srv.URL, Modelo: "no-existe"}
	if _, err := v.Extraer(context.Background(), []byte("png"), "image/png", ""); err == nil {
		t.Fatal("tenía que fallar")
	}
	if pedidos.Load() != 1 {
		t.Fatalf("pedidos %d", pedidos.Load())
	}
}

// A OpenAI (y solo a OpenAI) se le pide que no guarde la respuesta: "store": false. Un servidor falso
// atiende como si fuera cada host, para mirar el cuerpo sin salir a internet.
func TestVisionStoreFalseSoloOpenAI(t *testing.T) {
	var ultimo atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		cuerpo["_host"] = r.Host
		ultimo.Store(cuerpo)
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"total":1}`}}}})
	}))
	defer srv.Close()
	// Todo pedido, sea al host que sea, termina en el servidor falso.
	cli := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}}}
	for _, c := range []struct {
		base  string
		store bool
	}{
		{"http://api.openai.com/v1", true},
		{"http://eu.api.openai.com/v1", true},
		{"http://api.anthropic.com/v1", false},
		{"http://generativelanguage.googleapis.com/v1beta/openai", false},
		{"http://ollama:11434/v1", false},
		{"http://ia.ejemplo.com/v1", false},
	} {
		v := &Vision{BaseURL: c.base, Modelo: "m", HTTP: cli}
		if _, err := v.Extraer(context.Background(), nil, "", "texto del comprobante"); err != nil {
			t.Fatalf("%s: %v", c.base, err)
		}
		cuerpo := ultimo.Load().(map[string]any)
		st, hay := cuerpo["store"]
		if c.store != hay || (hay && st != false) {
			t.Errorf("%s (host %v): store = %v (está: %v)", c.base, cuerpo["_host"], st, hay)
		}
	}
}
