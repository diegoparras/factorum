package mudanza

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
)

// Args es el trabajo de River que corre el pase (o la liberación del disco).
type Args struct{}

func (Args) Kind() string { return "r2_mudanza" }

// InsertOpts: va a la cola de mantenimiento, para no frenar el procesamiento de facturas.
func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: Cola, MaxAttempts: 10}
}

// Cola de los trabajos de mantenimiento (el pase a R2 y las copias).
const Cola = "mantenimiento"

// Worker corre un tramo del pase; si se cumple, encola el siguiente.
type Worker struct {
	river.WorkerDefaults[Args]
	S *Servicio
}

func (w *Worker) Timeout(*river.Job[Args]) time.Duration {
	if w.S.Tramo <= 0 {
		return -1
	}
	return w.S.Tramo + 15*time.Minute
}

func (w *Worker) Work(ctx context.Context, _ *river.Job[Args]) error {
	err := w.S.Correr(ctx, nil)
	switch {
	case errors.Is(err, ErrSigue):
		return w.S.encolar(context.WithoutCancel(ctx))
	case errors.Is(err, ErrOcupado):
		return nil // lo está haciendo otro (otro trabajo o la consola)
	}
	return err
}
