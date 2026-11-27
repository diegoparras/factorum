package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/libroiva"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

// topeSinFecha es cuántos comprobantes sin fecha del cliente se muestran como problema.
const topeSinFecha = 500

// LibroIVA es un libro (compras, ventas o importaciones) de un cliente y un mes, listo para la
// API, MCP y el ZIP.
type LibroIVA struct {
	*libroiva.Resultado
	// Conciliacion dice si se pudo cruzar con Mis Comprobantes: «lista», «no_pedida» o el estado
	// de la conciliación que no se pudo hacer (sin_arcanum, sin_sincronizar...), con su mensaje.
	// Solo en compras.
	Conciliacion EstadoConcLibro `json:"conciliacion"`
	// Fuente: de dónde salen las ventas (Arcanum) y qué parte del período está bajada. Solo en ventas.
	Fuente       *FuenteVentas `json:"fuente,omitempty"`
	Importar     string        `json:"importar"` // qué elegir al importar en ARCA
	Presentacion Presentacion  `json:"presentacion"`
}

// EstadoConcLibro resume la conciliación que se usó para avisar.
type EstadoConcLibro struct {
	Estado  string `json:"estado"`
	Mensaje string `json:"mensaje,omitempty"`
}

// FuenteVentas es lo que dijo Arcanum de los emitidos del período.
type FuenteVentas struct {
	Estado         string              `json:"estado"` // lista o el estado del error (sin_arcanum, arcanum_viejo...)
	Mensaje        string              `json:"mensaje,omitempty"`
	Cobertura      *arcanum.Cobertura  `json:"cobertura,omitempty"`
	SinSincronizar []conciliar.Periodo `json:"sin_sincronizar"`
	CubreTodo      bool                `json:"cubre_todo"`
	Truncado       bool                `json:"truncado"`
	Emitidos       int                 `json:"emitidos"`
}

// Presentacion dice si el período ya se exportó (presentó) y qué cambió desde la última vez.
type Presentacion struct {
	Estado    string              `json:"estado"` // abierto o presentado
	Ultima    *store.Exportacion  `json:"ultima,omitempty"`
	Veces     int                 `json:"veces"`
	Cambio    bool                `json:"cambio"`
	Cambios   *Cambios            `json:"cambios,omitempty"`
	Historial []store.Exportacion `json:"historial"`
}

// Cambios entre la última exportación y lo que se exportaría ahora (con la misma opción).
type Cambios struct {
	Agregados   []CambioCbte `json:"agregados"`
	Quitados    []CambioCbte `json:"quitados"`
	Modificados []CambioCbte `json:"modificados"`
}

// CambioCbte es un comprobante que entró, salió o cambió.
type CambioCbte struct {
	Clave       string `json:"clave"`
	ID          int64  `json:"id,omitempty"`
	Descripcion string `json:"descripcion"`
}

// Estados de un período del libro.
const (
	PeriodoAbierto    = "abierto"
	PeriodoPresentado = "presentado"
)

// LibroIVA arma un libro de un cliente en un mes (AAAA-MM) para el Libro IVA Digital. Lo de la
// papelera no cuenta. En compras, con conConciliacion y Arcanum configurado, cruza con Mis
// Comprobantes para avisar lo que no figura en ARCA (nunca saca nada del archivo por eso). En
// ventas, los emitidos salen de Arcanum. El cliente ya viene con el alcance controlado por quien
// llama.
func (a *App) LibroIVA(ctx context.Context, cliente, periodo, libro string, soloAprobados, conConciliacion bool) (*LibroIVA, error) {
	cliente = arca.SoloDigitos(cliente)
	if libro == "" {
		libro = libroiva.LibroCompras
	}
	if !libroiva.LibroValido(libro) {
		return nil, fmt.Errorf("%w: libro %q (puede ser compras, ventas o importaciones)", store.ErrFiltro, libro)
	}
	desde, hasta, err := libroiva.ValidarPeriodo(periodo)
	if err != nil {
		return nil, err
	}
	periodo = desde[:7]
	res := &LibroIVA{Conciliacion: EstadoConcLibro{Estado: "no_pedida"}, Importar: libroiva.InstruccionImportar}
	o := libroiva.Opciones{Cliente: cliente, Periodo: periodo, SoloAprobados: soloAprobados}

	var generar func(o libroiva.Opciones) (*libroiva.Resultado, error)
	switch libro {
	case libroiva.LibroVentas:
		vs, f := a.ventasDeArcanum(ctx, cliente, desde, hasta)
		res.Fuente = f
		generar = func(o libroiva.Opciones) (*libroiva.Resultado, error) { return libroiva.GenerarVentas(vs, o) }
	default:
		es, err := a.entradasLibro(ctx, cliente, periodo, libro)
		if err != nil {
			return nil, err
		}
		if libro == libroiva.LibroImportaciones {
			generar = func(o libroiva.Opciones) (*libroiva.Resultado, error) { return libroiva.GenerarImportaciones(es, o) }
		} else {
			generar = func(o libroiva.Opciones) (*libroiva.Resultado, error) { return libroiva.Generar(es, o) }
			if conConciliacion {
				o.Conciliacion, res.Conciliacion = a.concParaLibro(ctx, cliente, desde, hasta)
			}
		}
	}
	r, err := generar(o)
	if err != nil {
		return nil, err
	}
	res.Resultado = r

	// ¿Ya se presentó? Lo que cambió se mide contra lo que se exportaría ahora con la misma opción
	// que la última vez (y sin la conciliación, que no cambia el archivo).
	exps, err := a.Store.Exportaciones(ctx, cliente, libro, periodo, 10)
	if err != nil {
		return nil, err
	}
	res.Presentacion = Presentacion{Estado: PeriodoAbierto, Historial: exps, Veces: len(exps)}
	if len(exps) > 0 {
		u := exps[0]
		res.Presentacion.Estado, res.Presentacion.Ultima = PeriodoPresentado, &u
		ahora := r
		if u.SoloAprobados != soloAprobados || o.Conciliacion != nil {
			o2 := o
			o2.SoloAprobados, o2.Conciliacion = u.SoloAprobados, nil
			if ahora, err = generar(o2); err != nil {
				return nil, err
			}
		}
		res.Presentacion.Cambios = compararExportacion(u.Comprobantes, ahora.Registros)
		c := res.Presentacion.Cambios
		res.Presentacion.Cambio = len(c.Agregados)+len(c.Quitados)+len(c.Modificados) > 0
	}
	return res, nil
}

// entradasLibro trae los comprobantes de compra (o los despachos) del cliente que van en el libro
// de ese período, más los que no tienen fecha (se informan como problema).
func (a *App) entradasLibro(ctx context.Context, cliente, periodo, libro string) ([]libroiva.Entrada, error) {
	var tipos, sin []int
	if libro == libroiva.LibroImportaciones {
		tipos = []int{modelo.TipoDespacho}
	} else {
		sin = []int{modelo.TipoDespacho}
	}
	cbs, err := a.Store.DelClientePeriodo(ctx, cliente, periodo, tipos, sin, topeFactorum)
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
			if libroiva.LibroDe(c.Datos.Tipo) != libro {
				continue
			}
			es = append(es, libroiva.Entrada{ID: c.ID, Estado: c.Estado, Constatacion: estadoConstatacion(c.Constatacion), Archivo: c.Archivo, C: c.Datos})
		}
	}
	return es, nil
}

// ventasDeArcanum pide los emitidos del período a Arcanum y los pasa al formato del libro.
func (a *App) ventasDeArcanum(ctx context.Context, cliente, desde, hasta string) ([]libroiva.Venta, *FuenteVentas) {
	f := &FuenteVentas{SinSincronizar: []conciliar.Periodo{}}
	arc := a.Arcanum()
	if arc == nil {
		f.Estado, f.Mensaje = ConcSinArcanum, "Factorum no está conectado con Arcanum. Las ventas salen de Mis Comprobantes (emitidos), que baja Arcanum: un admin lo configura en Configuración, Arcanum."
		return nil, f
	}
	r, err := arc.MisEmitidos(ctx, cliente, desde, hasta)
	if err != nil {
		f.Estado, f.Mensaje = estadoErrorEmitidos(err, cliente)
		if f.Estado == ConcArcanumError && a.Log != nil {
			a.Log.Warn("libro de ventas: Arcanum respondió con un error", "cliente", cliente, "error", err)
		}
		return nil, f
	}
	f.Cobertura, f.Truncado, f.Emitidos = &r.Cobertura, r.Truncado, len(r.Comprobantes)
	var bajados []conciliar.Periodo
	for _, x := range r.Cobertura.Rangos {
		bajados = append(bajados, conciliar.Periodo{Desde: x.Desde, Hasta: x.Hasta})
	}
	if s := conciliar.Faltantes(desde, hasta, bajados); s != nil {
		f.SinSincronizar = s
	}
	f.CubreTodo = len(f.SinSincronizar) == 0
	f.Estado = ConcLista
	switch {
	case !r.Cobertura.Sincronizado && len(r.Comprobantes) == 0:
		f.Estado = ConcSinSincronizar
		f.Mensaje = "Arcanum todavía no bajó los emitidos de " + arca.FormatearCUIT(cliente) + " de Mis Comprobantes. En Arcanum, cargá la clave fiscal del cliente y sincronizá los emitidos; después volvé a armar el libro."
	case !f.CubreTodo:
		f.Mensaje = "Arcanum no bajó todos los emitidos del período: lo que se emitió en los días sin sincronizar no está en el libro. Sincronizá los emitidos en Arcanum antes de presentarlo."
	}
	if r.Truncado {
		f.Mensaje = fmt.Sprintf("Arcanum tiene más emitidos de los que manda de una vez (%d): el libro está incompleto. %s", r.Total, f.Mensaje)
	}
	vs := make([]libroiva.Venta, 0, len(r.Comprobantes))
	for _, e := range r.Comprobantes {
		v := libroiva.Venta{Fecha: e.Fecha, Tipo: e.CbteTipo, PuntoVenta: e.PuntoVenta, Numero: e.NumeroDesde, NumeroHasta: e.NumeroHasta, CAE: e.CAE,
			DocTipo: e.DocTipo, DocNro: e.DocNro, Receptor: e.Receptor, Moneda: e.Moneda, Cotizacion: e.TipoCambio,
			NetoGravado: modelo.DesdeFloat(e.NetoGravado), NoGravado: modelo.DesdeFloat(e.NoGravado), Exento: modelo.DesdeFloat(e.Exento),
			IVA: modelo.DesdeFloat(e.IVA), Total: modelo.DesdeFloat(e.Total), FuenteAlicuotas: e.FuenteAlicuotas, DesgloseDistinto: e.DesgloseDistinto,
			FechaVtoPago: e.FechaVtoPago}
		if e.OtrosTributos != nil {
			x := modelo.DesdeFloat(*e.OtrosTributos)
			v.OtrosTributos = &x
		}
		for _, x := range e.Alicuotas {
			v.Alicuotas = append(v.Alicuotas, libroiva.AlicuotaVenta{Codigo: x.ID, Neto: modelo.DesdeFloat(x.NetoGravado), IVA: modelo.DesdeFloat(x.IVA)})
		}
		if e.Tributos != nil {
			v.Tributos = []libroiva.TributoVenta{}
			for _, t := range e.Tributos {
				v.Tributos = append(v.Tributos, libroiva.TributoVenta{ID: t.ID, Importe: modelo.DesdeFloat(t.Importe)})
			}
		}
		vs = append(vs, v)
	}
	return vs, f
}

// estadoErrorEmitidos traduce un error de Arcanum al pedir los emitidos (nunca el detalle crudo).
func estadoErrorEmitidos(err error, cliente string) (string, string) {
	if st, _, ok := arcanum.ErrHTTP(err); ok && st == 404 {
		return ConcArcanumViejo, "Esta versión de Arcanum no tiene los emitidos para el libro de ventas: hace falta Arcanum 2.4.0 o superior."
	}
	e, msg := estadoErrorArcanum(err, cliente)
	if e == ConcArcanumError && msg == "Arcanum no pudo devolver los recibidos ahora: probá en un rato." {
		msg = "Arcanum no pudo devolver los emitidos ahora: probá en un rato."
	}
	return e, msg
}

// compararExportacion dice qué entró, qué salió y qué cambió entre lo exportado y lo de ahora.
func compararExportacion(antes []store.CbteExportado, ahora []libroiva.Registro) *Cambios {
	c := &Cambios{Agregados: []CambioCbte{}, Quitados: []CambioCbte{}, Modificados: []CambioCbte{}}
	viejo := map[string]store.CbteExportado{}
	for _, x := range antes {
		viejo[x.Clave] = x
	}
	nuevo := map[string]bool{}
	for i := range ahora {
		r := &ahora[i]
		nuevo[r.Clave] = true
		x, ok := viejo[r.Clave]
		switch {
		case !ok:
			c.Agregados = append(c.Agregados, CambioCbte{Clave: r.Clave, ID: r.ID, Descripcion: r.Descripcion()})
		case x.Huella != r.Huella():
			c.Modificados = append(c.Modificados, CambioCbte{Clave: r.Clave, ID: r.ID, Descripcion: r.Descripcion()})
		}
	}
	for _, x := range antes {
		if !nuevo[x.Clave] {
			c.Quitados = append(c.Quitados, CambioCbte{Clave: x.Clave, ID: x.ID, Descripcion: x.Descripcion})
		}
	}
	return c
}

// RegistrarExportacion anota que se bajó el ZIP de un libro: el período queda presentado, con el
// SHA-256 de cada archivo y los comprobantes que fueron.
func (a *App) RegistrarExportacion(ctx context.Context, l *LibroIVA, usuario string) (*store.Exportacion, error) {
	e := &store.Exportacion{Cliente: l.Cliente, Periodo: l.Periodo, Libro: l.Libro, Usuario: usuario, SoloAprobados: l.SoloAprobados,
		Cantidad: len(l.Registros), Archivos: []store.ArchivoExportado{}, Comprobantes: []store.CbteExportado{}}
	for i, b := range [][]byte{l.Comprobantes, l.Alicuotas} {
		h := sha256.Sum256(b)
		e.Archivos = append(e.Archivos, store.ArchivoExportado{Nombre: l.Archivos[i], SHA256: hex.EncodeToString(h[:]), Bytes: len(b)})
	}
	for i := range l.Registros {
		r := &l.Registros[i]
		e.Comprobantes = append(e.Comprobantes, store.CbteExportado{Clave: r.Clave, ID: r.ID, Huella: r.Huella(), Descripcion: r.Descripcion()})
	}
	if _, err := a.Store.RegistrarExportacion(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
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
