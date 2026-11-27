package app

import (
	"context"
	"fmt"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/libroiva"
	"github.com/diegoparras/factorum/internal/store"
)

// topeSinFecha es cuántos comprobantes sin fecha del cliente se muestran como problema.
const topeSinFecha = 500

// LibroIVA es el libro de compras de un cliente y un mes, listo para la API, MCP y el ZIP.
type LibroIVA struct {
	*libroiva.Resultado
	// Conciliacion dice si se pudo cruzar con Mis Comprobantes: «lista», «no_pedida» o el estado
	// de la conciliación que no se pudo hacer (sin_arcanum, sin_sincronizar...), con su mensaje.
	Conciliacion EstadoConcLibro `json:"conciliacion"`
	Importar     string          `json:"importar"` // qué elegir al importar en ARCA
}

// EstadoConcLibro resume la conciliación que se usó para avisar.
type EstadoConcLibro struct {
	Estado  string `json:"estado"`
	Mensaje string `json:"mensaje,omitempty"`
}

// LibroIVA arma las compras de un cliente en un mes (AAAA-MM) para el Libro IVA Digital. Lo de la
// papelera no cuenta. Con conConciliacion y Arcanum configurado, cruza con Mis Comprobantes para
// avisar lo que no figura en ARCA (nunca saca nada del archivo por eso). El cliente ya viene con
// el alcance controlado por quien llama.
func (a *App) LibroIVA(ctx context.Context, cliente, periodo string, soloAprobados, conConciliacion bool) (*LibroIVA, error) {
	cliente = arca.SoloDigitos(cliente)
	desde, hasta, err := libroiva.ValidarPeriodo(periodo)
	if err != nil {
		return nil, err
	}
	cbs, _, err := a.Store.DelCliente(ctx, cliente, desde, hasta, topeFactorum)
	if err != nil {
		return nil, err
	}
	if len(cbs) >= topeFactorum {
		return nil, fmt.Errorf("%w: el cliente tiene más de %d comprobantes en el mes", store.ErrFiltro, topeFactorum)
	}
	sinFecha, err := a.Store.SinFechaDelCliente(ctx, cliente, topeSinFecha)
	if err != nil {
		return nil, err
	}
	es := make([]libroiva.Entrada, 0, len(cbs)+len(sinFecha))
	for _, lista := range [][]store.Cbte{cbs, sinFecha} {
		for i := range lista {
			c := &lista[i]
			es = append(es, libroiva.Entrada{ID: c.ID, Estado: c.Estado, Constatacion: estadoConstatacion(c.Constatacion), Archivo: c.Archivo, C: c.Datos})
		}
	}

	res := &LibroIVA{Conciliacion: EstadoConcLibro{Estado: "no_pedida"}, Importar: libroiva.InstruccionImportar}
	o := libroiva.Opciones{Cliente: cliente, Periodo: periodo, SoloAprobados: soloAprobados}
	if conConciliacion {
		o.Conciliacion, res.Conciliacion = a.concParaLibro(ctx, cliente, desde, hasta)
	}
	r, err := libroiva.Generar(es, o)
	if err != nil {
		return nil, err
	}
	res.Resultado = r
	return res, nil
}

// concParaLibro concilia el mes y deja lo que sirve para avisar en el libro.
func (a *App) concParaLibro(ctx context.Context, cliente, desde, hasta string) (*libroiva.Conciliacion, EstadoConcLibro) {
	if a.Arcanum() == nil {
		return nil, EstadoConcLibro{Estado: ConcSinArcanum, Mensaje: "Factorum no está conectado con Arcanum: no se pudo ver qué no figura en ARCA."}
	}
	c, err := a.Conciliar(ctx, cliente, desde, hasta, false)
	if err != nil {
		return nil, EstadoConcLibro{Estado: ConcArcanumError, Mensaje: "No se pudo conciliar con ARCA ahora."}
	}
	if c.Estado != ConcLista || c.Resultado == nil {
		return nil, EstadoConcLibro{Estado: c.Estado, Mensaje: c.Mensaje}
	}
	k := &libroiva.Conciliacion{NoFigura: map[int64]string{}, Alarma: map[int64]bool{}, Diferencias: map[int64]string{}}
	for _, g := range conciliar.Grupos {
		for i := range c.Resultado.Grupos[g] {
			it := &c.Resultado.Grupos[g][i]
			if it.Factorum == nil {
				continue
			}
			id := it.Factorum.ID
			if it.Arca != nil && it.Arca.EmisorApocrifo != nil && *it.Arca.EmisorApocrifo {
				k.Alarma[id] = true
			}
			switch {
			case g == conciliar.NoFigura && !it.Legitima:
				nota := it.Nota
				if nota == "" {
					nota = "No está en los recibidos de ARCA."
				}
				k.NoFigura[id] = nota
			case g == conciliar.Diferencias:
				k.Diferencias[id] = export.DetalleDiferencias(it)
			}
		}
	}
	return k, EstadoConcLibro{Estado: ConcLista, Mensaje: c.Mensaje}
}
