package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
	"github.com/diegoparras/factorum/internal/validar"
)

// codigoReconstatar: se cambió el cliente del lote y la constatación con el CUIT viejo ya no vale.
const codigoReconstatar = "reconstatar"

// PurgarArgs: el borrado definitivo de lo vencido en la papelera (cada hora, en la cola de
// mantenimiento).
type PurgarArgs struct{}

func (PurgarArgs) Kind() string { return "purgar_papelera" }
func (PurgarArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: ColaMantenimiento}
}

type purgarWorker struct {
	river.WorkerDefaults[PurgarArgs]
	a *App
}

func (w *purgarWorker) Timeout(*river.Job[PurgarArgs]) time.Duration { return 30 * time.Minute }
func (w *purgarWorker) Work(ctx context.Context, _ *river.Job[PurgarArgs]) error {
	_, err := w.a.PurgarPapelera(ctx, w.a.ahora())
	return err
}

// LoteBorrado es lo que devuelve BorrarLote.
type LoteBorrado struct {
	store.Borrado
	// Cancelados: trabajos de la cola que se cancelaron (lo que faltaba procesar o constatar).
	Cancelados int `json:"trabajos_cancelados"`
}

// BorrarLote manda un lote entero a la papelera. Si todavía se está procesando, en la misma
// transacción se cancelan sus trabajos de la cola (los que esperan y el que está corriendo): lo que
// quedó sin procesar se vuelve a encolar si se restaura. Si un documento termina de leerse justo
// después, su resultado no se guarda (GuardarResultado ve que se fue a la papelera).
func (a *App) BorrarLote(ctx context.Context, id int64, quien string, alcance []string) (*LoteBorrado, error) {
	res := &LoteBorrado{}
	err := pgx.BeginFunc(ctx, a.Store.Pool, func(tx pgx.Tx) error {
		b, err := a.Store.BorrarLote(ctx, tx, id, quien, alcance)
		if err != nil {
			return err
		}
		res.Borrado = *b
		rows, err := tx.Query(ctx, `SELECT id FROM river_job WHERE state IN ('available','pending','retryable','running','scheduled')
			AND ((kind = $1 AND (args->>'documento_id')::bigint = ANY($2)) OR (kind = $3 AND (args->>'lote_id')::bigint = $4))
			ORDER BY id`, ProcesarArgs{}.Kind(), b.Pendientes, ConstatarArgs{}.Kind(), id)
		if err != nil {
			return err
		}
		trabajos, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		for _, j := range trabajos {
			if _, err := a.River.JobCancelTx(ctx, tx, j); err != nil {
				return fmt.Errorf("cancelando el trabajo %d de la cola: %w", j, err)
			}
		}
		res.Cancelados = len(trabajos)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// BorrarComprobantes manda comprobantes sueltos a la papelera (y sus documentos, si se quedan sin
// ninguno vivo).
func (a *App) BorrarComprobantes(ctx context.Context, ids []int64, quien string, alcance []string) (*store.Borrado, error) {
	return a.Store.BorrarComprobantes(ctx, ids, quien, alcance)
}

// Restaurar saca algo de la papelera y vuelve a encolar lo que había quedado sin procesar (en la
// misma transacción). Si el lote se constata y le falta algo, encola la constatación.
func (a *App) Restaurar(ctx context.Context, ref store.RefPapelera, alcance []string) (*store.Restaurado, error) {
	var r *store.Restaurado
	err := pgx.BeginFunc(ctx, a.Store.Pool, func(tx pgx.Tx) error {
		var err error
		if r, err = a.Store.RestaurarTx(ctx, tx, ref, alcance); err != nil {
			return err
		}
		if len(r.Pendientes) == 0 {
			return nil
		}
		var jobs []river.InsertManyParams
		for _, d := range r.Pendientes {
			jobs = append(jobs, river.InsertManyParams{Args: ProcesarArgs{DocumentoID: d}})
		}
		_, err = a.River.InsertManyTx(ctx, tx, jobs)
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(r.Pendientes) == 0 && r.Lotes > 0 {
		l, err := a.Store.Lote(ctx, r.LoteID)
		if err == nil && l.Terminado == nil {
			a.verificarFinDeLote(ctx, l.ID)
		} else if err == nil && l.SinConstatar && a.Arcanum() != nil {
			// Se borró mientras se constataba (el trabajo se canceló): se sigue.
			if _, err := a.River.Insert(ctx, ConstatarArgs{LoteID: l.ID}, nil); err != nil {
				a.Log.Error("no se pudo encolar la constatación", "lote", l.ID, "error", err)
			}
		}
	}
	return r, nil
}

// diasPapelera son los días vigentes antes del borrado definitivo.
func (a *App) diasPapelera() int { return a.Config().DiasDePapelera() }

// Papelera lista lo que está en la papelera (con los días que le quedan a cada cosa).
func (a *App) Papelera(ctx context.Context, alcance []string, limite, offset int) ([]store.ItemPapelera, int, int, error) {
	dias := a.diasPapelera()
	items, total, err := a.Store.Papelera(ctx, alcance, a.ahora(), dias, limite, offset)
	return items, total, dias, err
}

// PurgarPapelera borra para siempre lo que lleva en la papelera más de los días configurados (con
// ahora: el reloj se puede cambiar en las pruebas) y los archivos que ya no usa nadie.
func (a *App) PurgarPapelera(ctx context.Context, ahora time.Time) (store.Purga, error) {
	dias := a.diasPapelera()
	p, err := a.Store.Purgar(ctx, store.FiltroPurga{Antes: ahora.Add(-time.Duration(dias) * 24 * time.Hour)})
	if err != nil {
		return p, err
	}
	n, err := a.LimpiarAlmacen(ctx)
	p.Archivos = n
	if !p.Vacia() {
		a.Log.Info("papelera: borrado definitivo de lo vencido", "lotes", p.Lotes, "documentos", p.Documentos, "comprobantes", p.Comprobantes, "archivos", p.Archivos)
		if e := a.Store.Auditar(context.WithoutCancel(ctx), store.Evento{Actor: "Factorum", ActorTipo: "sistema", Accion: "papelera.purgar",
			Objeto: fmt.Sprintf("más de %d días", dias), Detalle: map[string]any{"lotes": p.Lotes, "documentos": p.Documentos,
				"comprobantes": p.Comprobantes, "archivos": p.Archivos, "dias": dias}}); e != nil {
			a.Log.Error("no se pudo anotar en la auditoría", "accion", "papelera.purgar", "error", e)
		}
	}
	return p, err
}

// BorrarDeLaPapelera borra para siempre, ya, lo que diga el filtro (Vaciar o Borrar ya) y los
// archivos que ya no usa nadie.
func (a *App) BorrarDeLaPapelera(ctx context.Context, f store.FiltroPurga) (store.Purga, error) {
	f.Antes = time.Time{}
	p, err := a.Store.Purgar(ctx, f)
	if err != nil {
		return p, err
	}
	n, err := a.LimpiarAlmacen(ctx)
	p.Archivos = n
	return p, err
}

// LimpiarAlmacen saca del almacén (el disco y R2) los archivos anotados en almacen_por_borrar que
// ya no usa ningún documento, vivo o en la papelera. Los objetos son direccionados por contenido:
// el mismo PDF subido en dos lotes es un solo archivo, y se borra recién cuando no queda ninguno.
// Mientras corre el pase a R2 o la liberación del disco, espera (quedan anotados para la próxima).
func (a *App) LimpiarAlmacen(ctx context.Context) (int, error) {
	if ocupado, err := a.Store.PaseR2Trabajando(ctx); err != nil || ocupado {
		if ocupado {
			a.Log.Info("papelera: los archivos se borran cuando termine el pase a R2")
		}
		return 0, err
	}
	borrados := 0
	for {
		shas, err := a.Store.PorBorrarDelAlmacen(ctx, 200)
		if err != nil {
			return borrados, err
		}
		for _, sha := range shas {
			ok, err := a.Store.BorrarArchivoSiNadieLoUsa(ctx, sha, func(ctx context.Context) error {
				for _, k := range store.ClavesArchivo(sha) {
					if err := a.Almacen.Borrar(ctx, k); err != nil {
						return fmt.Errorf("borrando %s del almacén: %w", k, err)
					}
				}
				return nil
			})
			if err != nil {
				return borrados, err // queda anotado: se reintenta en la próxima pasada
			}
			if ok {
				borrados++
			}
		}
		if len(shas) < 200 {
			return borrados, nil
		}
	}
}

// ---------- Cambiar el cliente de un lote ----------

// ClienteCambiado es lo que devuelve CambiarCliente.
type ClienteCambiado struct {
	store.CambioCliente
	// Reconstatar: comprobantes que se habían constatado con el CUIT consultante del cliente viejo y
	// se vuelven a constatar con el nuevo. Encolado: si ya quedó en la cola (hace falta Arcanum).
	Reconstatar int  `json:"reconstatar"`
	Encolado    bool `json:"constatacion_encolada"`
}

// consultante es el CUIT con el que se constata lo de un cliente: el del cliente si es válido; si
// no, el de Arcanum.
func (a *App) consultante(cliente string) string {
	if arca.CUITValido(cliente) {
		return cliente
	}
	if arc := a.Arcanum(); arc != nil {
		return arc.Consultante
	}
	return ""
}

// CambiarCliente pasa un lote (y sus comprobantes) a otro cliente. Lo que ya se había constatado con
// el CUIT consultante del cliente viejo pierde esa constatación (queda con el aviso «se vuelve a
// constatar») y se encola de nuevo: el lote entero si se constata al terminar (si todavía se está
// procesando, se constata al terminar, con el CUIT nuevo), o solo esos comprobantes si se habían
// constatado de a uno. Sin Arcanum, quedan sin constatar con el aviso.
func (a *App) CambiarCliente(ctx context.Context, id int64, nuevo string, alcance []string) (*ClienteCambiado, error) {
	nuevo = arca.SoloDigitos(nuevo)
	if !arca.CUITValido(nuevo) {
		return nil, fmt.Errorf("%w: el CUIT %s no es válido (revisá el dígito verificador)", store.ErrFiltro, arca.FormatearCUIT(nuevo))
	}
	c, err := a.Store.CambiarClienteLote(ctx, id, nuevo, alcance)
	if err != nil {
		return nil, err
	}
	res := &ClienteCambiado{CambioCliente: *c}
	if c.Antes == c.Despues || a.consultante(c.Antes) == a.consultante(c.Despues) {
		return res, nil
	}
	cbs, err := a.Store.ConstatadosDeLote(ctx, id)
	if err != nil {
		return res, err
	}
	arc := a.Arcanum()
	msg := "Se cambió el cliente del lote: se vuelve a constatar con el CUIT nuevo."
	if arc == nil {
		msg = "Se cambió el cliente del lote: falta volver a constatarlo con el CUIT nuevo (hace falta Arcanum)."
	}
	var ids []int64
	for i := range cbs {
		cb := &cbs[i]
		var h []modelo.Hallazgo
		for _, x := range cb.Hallazgos {
			if !codigosConstatacion[x.Codigo] {
				h = append(h, x)
			}
		}
		cb.Hallazgos = append(h, modelo.Hallazgo{Codigo: codigoReconstatar, Nivel: modelo.NivelInfo, Mensaje: msg})
		cb.Constatacion = nil
		if cb.Estado != modelo.EstadoAprobado {
			cb.Estado = validar.Estado(&cb.Datos, cb.Hallazgos)
		}
		cb.Confianza = validar.Confianza(&cb.Datos, cb.Hallazgos)
		if err := a.Store.ActualizarCbte(ctx, cb, nil, ""); err != nil && !errors.Is(err, store.ErrNoEncontrado) {
			return res, err
		}
		ids = append(ids, cb.ID)
	}
	res.Reconstatar = len(ids)
	if arc == nil || len(ids) == 0 {
		return res, nil
	}
	l := c.Lote
	switch {
	case l.Constatar && l.Terminado == nil:
		res.Encolado = true // lo constata el cierre del lote, con el cliente nuevo
	case l.Constatar:
		_, err = a.River.Insert(ctx, ConstatarArgs{LoteID: id}, nil)
		res.Encolado = err == nil
	default:
		_, err = a.River.Insert(ctx, ConstatarArgs{LoteID: id, Comprobantes: ids}, nil)
		res.Encolado = err == nil
	}
	return res, err
}
