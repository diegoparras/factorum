package main

import (
	"context"
	"log/slog"

	"github.com/diegoparras/contadorfacturas/internal/api"
	"github.com/diegoparras/contadorfacturas/internal/app"
	"github.com/diegoparras/contadorfacturas/internal/store"
)

// iniciarCanales conecta Telegram, WhatsApp y el webhook según la configuración.
func iniciarCanales(ctx context.Context, a *app.App, st *store.Store, log *slog.Logger) api.Webhook {
	return nil
}
