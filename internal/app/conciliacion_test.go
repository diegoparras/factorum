package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/arcanum"
)

func TestValidarPeriodo(t *testing.T) {
	for _, c := range []struct {
		d, h string
		ok   bool
	}{{"2026-11-01", "2026-11-30", true}, {"2026-01-01", "2026-12-31", true}, {"2026-11-30", "2026-11-01", false},
		{"2025-01-01", "2026-01-02", false}, {"01/11/2026", "2026-11-30", false}, {"", "", false}} {
		if err := ValidarPeriodo(c.d, c.h); (err == nil) != c.ok {
			t.Errorf("%s a %s: %v", c.d, c.h, err)
		}
	}
}

// Cada error de Arcanum se traduce a un estado con un mensaje para la persona (nunca el detalle crudo).
func TestEstadoErrorArcanum(t *testing.T) {
	estado := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(estado)
		_, _ = w.Write([]byte(`{"ok":false,"error":"Error interno (ref. abcd)."}`))
	}))
	c := arcanum.Nuevo(srv.URL, "ark_x", "", time.Second)
	for st, want := range map[int]string{401: ConcArcanumError, 403: ConcSinPermiso, 404: ConcArcanumViejo, 500: ConcArcanumError, 502: ConcArcanumError} {
		estado = st
		_, err := c.MisRecibidos(context.Background(), "20222222223", "2026-11-01", "2026-11-30")
		got, msg := estadoErrorArcanum(err, "20222222223")
		if got != want || msg == "" || strings.Contains(msg, "abcd") {
			t.Errorf("%d: %s %q", st, got, msg)
		}
	}
	srv.Close()
	_, err := c.MisRecibidos(context.Background(), "20222222223", "2026-11-01", "2026-11-30")
	if got, _ := estadoErrorArcanum(err, "20222222223"); got != ConcArcanumCaido {
		t.Errorf("caído: %s", got)
	}
}

func TestCacheRecibidos(t *testing.T) {
	var c cacheRecibidos
	cli, otro := &arcanum.Cliente{}, &arcanum.Cliente{}
	t0 := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	c.guardar("k", entradaRecibidos{cliente: cli, vence: t0.Add(TTLRecibidos), datos: &arcanum.Recibidos{Total: 3}}, t0)
	if e, ok := c.leer("k", cli, t0.Add(time.Minute)); !ok || e.datos.Total != 3 {
		t.Fatal("tendría que estar guardado")
	}
	if _, ok := c.leer("k", otro, t0.Add(time.Minute)); ok {
		t.Fatal("con otro cliente de Arcanum (se cambió la configuración) no vale")
	}
	if _, ok := c.leer("k", cli, t0.Add(TTLRecibidos+time.Second)); ok {
		t.Fatal("vencido no vale")
	}
}
