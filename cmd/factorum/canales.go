package main

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/diegoparras/contadorfacturas/internal/api"
	"github.com/diegoparras/contadorfacturas/internal/app"
	"github.com/diegoparras/contadorfacturas/internal/canal"
	"github.com/diegoparras/contadorfacturas/internal/canal/telegram"
	"github.com/diegoparras/contadorfacturas/internal/canal/webhook"
	"github.com/diegoparras/contadorfacturas/internal/canal/whatsapp"
	"github.com/diegoparras/contadorfacturas/internal/store"
)

// entrada adapta el canal webhook a lo que necesita la API.
type entrada struct{ *webhook.Webhook }

func (entrada) Decodificar(b []byte) (canal.Mensaje, error) { return webhook.Decodificar(b) }

// iniciarCanales conecta Telegram, WhatsApp y el webhook según la configuración.
func iniciarCanales(ctx context.Context, a *app.App, st *store.Store, log *slog.Logger) api.Webhook {
	c := a.Cfg
	manejar := func(ctx context.Context, m canal.Mensaje) { a.ManejarMensaje(ctx, m) }
	arrancar := func(ch canal.Canal) {
		a.RegistrarCanal(ch)
		go func() {
			if err := ch.Iniciar(ctx, manejar); err != nil && ctx.Err() == nil {
				log.Error("el canal se detuvo", "canal", ch.Nombre(), "error", err)
			}
		}()
	}

	if c.TelegramToken != "" {
		if len(c.TelegramPermitidos) == 0 {
			log.Warn("Telegram sin FACTORUM_TELEGRAM_PERMITIDOS: el bot va a contestarle a cada uno su id, pero no va a procesar nada")
		}
		arrancar(telegram.New(telegram.Config{Token: c.TelegramToken, MaxBytes: c.MaxArchivo}))
	}

	if c.WhatsApp {
		if len(c.WhatsAppPermitidos) == 0 {
			log.Warn("WhatsApp sin FACTORUM_WHATSAPP_PERMITIDOS: no va a atender a nadie")
		}
		db := stdlib.OpenDBFromPool(st.Pool)
		arrancar(whatsapp.New(whatsapp.Config{
			DB: db, Dialecto: "postgres", MaxBytes: c.MaxArchivo,
			Permitir: func(rem string) bool { return a.Permitido(canal.Mensaje{Canal: "whatsapp", Remitente: rem}) },
		}))
	}

	if c.WebhookToken != "" {
		wh := webhook.New(webhook.Config{Token: c.WebhookToken, SalidaURL: c.WebhookSalidaURL})
		arrancar(wh)
		return entrada{wh}
	}
	return nil
}
