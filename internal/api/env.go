package api

import (
	"context"
	"net/http"
	"os"
	"time"
)

func getenv(k string) string { return os.Getenv(k) }

func contextoCorto(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}
