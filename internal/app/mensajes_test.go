package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

func TestPermisosYComandosPorCanal(t *testing.T) {
	a := &App{Cfg: &config.Config{TelegramPermitidos: []string{"111"}, WhatsAppPermitidos: []string{"+54 9 11 5555-0000"}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if !a.Permitido(canal.Mensaje{Canal: "telegram", Remitente: "111"}) || a.Permitido(canal.Mensaje{Canal: "telegram", Remitente: "222"}) {
		t.Fatal("telegram")
	}
	if !a.Permitido(canal.Mensaje{Canal: "whatsapp", Remitente: "5491155550000"}) || a.Permitido(canal.Mensaje{Canal: "whatsapp", Remitente: "5491100000000"}) {
		t.Fatal("whatsapp")
	}
	ctx := context.Background()
	// En WhatsApp, una charla normal no se contesta; un comando explícito sí.
	if r := a.responder(ctx, canal.Mensaje{Canal: "whatsapp", Remitente: "5491155550000", Texto: "hola, ¿cómo andás?"}); r != "" {
		t.Fatalf("no tenía que contestar: %q", r)
	}
	if r := a.responder(ctx, canal.Mensaje{Canal: "whatsapp", Remitente: "5491155550000", Texto: "Factorum"}); !strings.Contains(r, "Factorum lee facturas") {
		t.Fatalf("ayuda: %q", r)
	}
	// En Telegram, un desconocido recibe su id para que lo habiliten.
	if r := a.responder(ctx, canal.Mensaje{Canal: "telegram", Remitente: "999", Texto: "hola"}); !strings.Contains(r, "999") {
		t.Fatalf("desconocido: %q", r)
	}
}

func TestTextoResumen(t *testing.T) {
	l := &store.Lote{ConError: 1}
	cbs := []store.Cbte{
		{Estado: modelo.EstadoOK, Datos: modelo.Comprobante{Tipo: 1, PuntoVenta: 2, Numero: 1543, RazonSocialEmisor: "X SA", Total: 12100000}},
		{Estado: modelo.EstadoError, Hallazgos: []modelo.Hallazgo{{Nivel: modelo.NivelError, Mensaje: "No cierra."}},
			Datos: modelo.Comprobante{Tipo: 6, PuntoVenta: 3, Numero: 7, CUITEmisor: "30711111111", Total: 100}},
	}
	s := TextoResumen(l, cbs)
	for _, want := range []string{"2 comprobante(s). 1 bien, 0 para revisar, 1 con errores", "1 archivo(s) no se pudieron leer",
		"Factura A 00002-00001543, X SA, $ 121.000,00: bien", "30-71111111-1", "(No cierra.)"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q en:\n%s", want, s)
		}
	}
	if fechaAR("2026-11-03") != "03/11/2026" {
		t.Fatal("fecha")
	}
}
