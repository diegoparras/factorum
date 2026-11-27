package main

import (
	"context"
	"database/sql"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/diegoparras/factorum/internal/api"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/canal/telegram"
	"github.com/diegoparras/factorum/internal/canal/webhook"
	"github.com/diegoparras/factorum/internal/canal/whatsapp"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/store"
)

// entrada adapta el canal webhook a lo que necesita la API.
type entrada struct{ *webhook.Webhook }

func (entrada) Decodificar(b []byte) (canal.Mensaje, error) { return webhook.Decodificar(b) }

// gestorCanales arranca, para y rearranca Telegram, WhatsApp y el webhook según la configuración
// vigente. Se llama al arrancar y cada vez que se guarda la configuración.
type gestorCanales struct {
	ctx context.Context
	a   *app.App
	db  *sql.DB
	log *slog.Logger

	mu      sync.Mutex
	activos map[string]*canalActivo
	webhook atomic.Pointer[webhook.Webhook]
}

type canalActivo struct {
	firma  string
	cancel context.CancelFunc
	listo  chan struct{} // se cierra cuando Iniciar volvió
}

func nuevoGestor(ctx context.Context, a *app.App, st *store.Store, log *slog.Logger) *gestorCanales {
	return &gestorCanales{ctx: ctx, a: a, db: stdlib.OpenDBFromPool(st.Pool), log: log, activos: map[string]*canalActivo{}}
}

// Webhook es el webhook vigente para la API (nil = apagado).
func (g *gestorCanales) Webhook() api.Webhook {
	if w := g.webhook.Load(); w != nil {
		return entrada{w}
	}
	return nil
}

// Aplicar deja los canales como dice la configuración. Lo que no cambió sigue corriendo.
func (g *gestorCanales) Aplicar(c *config.Config) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Telegram: el token. Los permitidos se leen en cada mensaje.
	firma := ""
	if c.TelegramToken != "" {
		firma = c.TelegramToken + "|" + strconv.FormatInt(c.MaxArchivo, 10)
		if len(c.TelegramPermitidos) == 0 {
			g.log.Warn("Telegram sin usuarios permitidos: el bot va a contestarle a cada uno su id, pero no va a procesar nada")
		}
	}
	g.cambiar("telegram", firma, func() canal.Canal {
		return telegram.New(telegram.Config{Token: c.TelegramToken, MaxBytes: c.MaxArchivo})
	})

	// WhatsApp: prendido o apagado. La sesión vive en la base; los permitidos se leen en cada mensaje.
	firma = ""
	if c.WhatsApp {
		firma = "si"
		if len(c.WhatsAppPermitidos) == 0 {
			g.log.Warn("WhatsApp sin números permitidos: no va a atender a nadie")
		}
	}
	g.cambiar("whatsapp", firma, func() canal.Canal {
		return whatsapp.New(whatsapp.Config{DB: g.db, Dialecto: "postgres", MaxBytes: c.MaxArchivo,
			Permitir: func(rem string) bool { return g.a.Permitido(canal.Mensaje{Canal: "whatsapp", Remitente: rem}) }})
	})

	// Webhook: token y URL de salida.
	firma = ""
	if c.WebhookToken != "" {
		firma = c.WebhookToken + "|" + c.WebhookSalidaURL
	}
	var wh *webhook.Webhook
	g.cambiar("webhook", firma, func() canal.Canal {
		wh = webhook.New(webhook.Config{Token: c.WebhookToken, SalidaURL: c.WebhookSalidaURL})
		return wh
	})
	switch {
	case firma == "":
		g.webhook.Store(nil)
	case wh != nil:
		g.webhook.Store(wh)
	}
}

// cambiar para el canal si su firma cambió y, si hay firma, arranca uno nuevo.
func (g *gestorCanales) cambiar(nombre, firma string, nuevo func() canal.Canal) {
	act := g.activos[nombre]
	if act != nil && act.firma == firma {
		return
	}
	if act != nil {
		act.cancel()
		select { // WhatsApp no puede tener dos clientes con la misma sesión
		case <-act.listo:
		case <-time.After(15 * time.Second):
			g.log.Warn("el canal tardó en detenerse", "canal", nombre)
		}
		delete(g.activos, nombre)
		g.a.QuitarCanal(nombre)
		g.log.Info("canal detenido", "canal", nombre)
	}
	if firma == "" {
		return
	}
	ch := nuevo()
	ctx, cancel := context.WithCancel(g.ctx)
	act = &canalActivo{firma: firma, cancel: cancel, listo: make(chan struct{})}
	g.activos[nombre] = act
	g.a.RegistrarCanal(ch)
	manejar := func(ctx context.Context, m canal.Mensaje) { g.a.ManejarMensaje(ctx, m) }
	go func() {
		defer close(act.listo)
		if err := ch.Iniciar(ctx, manejar); err != nil && ctx.Err() == nil {
			g.log.Error("el canal se detuvo", "canal", ch.Nombre(), "error", err)
		}
	}()
	g.log.Info("canal iniciado", "canal", nombre)
}
