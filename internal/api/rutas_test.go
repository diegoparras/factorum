package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/diegoparras/factorum/internal/config"
)

// El ruteo completo (UI + MCP + API) se arma sin conflictos y la UI sale con CSP.
func TestRuteoCompleto(t *testing.T) {
	s := &Server{Cfg: &config.Config{APIKey: "k"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Web: fstest.MapFS{"index.html": {Data: []byte("<html>Factorum</html>")}},
		MCP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) })}
	h := s.Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Factorum") || !strings.Contains(rec.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Fatalf("UI: %d %q", rec.Code, rec.Header())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/mcp", nil))
	if rec.Code != 299 {
		t.Fatalf("MCP: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/", nil))
	if rec.Code != 405 {
		t.Fatalf("POST /: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/lotes", nil))
	if rec.Code != 401 {
		t.Fatalf("sin clave: %d", rec.Code)
	}
}
