package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Estados de una conciliación. Solo con «lista» hay grupos; los demás traen un mensaje para la persona.
const (
	ConcLista          = "lista"
	ConcSinArcanum     = "sin_arcanum"     // Factorum no está conectado con Arcanum
	ConcSinSincronizar = "sin_sincronizar" // Arcanum nunca bajó Mis Comprobantes de ese CUIT
	ConcSinPermiso     = "sin_permiso"     // la clave de Arcanum no tiene ese CUIT
	ConcArcanumViejo   = "arcanum_viejo"   // Arcanum no tiene la ruta (antes de 2.3.0)
	ConcArcanumCaido   = "arcanum_caido"   // no responde
	ConcArcanumError   = "arcanum_error"   // respondió con otro error
)

// MargenConciliacion son los días que se piden de más a cada lado del período: un comprobante con la
// fecha mal leída se empareja igual con el de ARCA.
const MargenConciliacion = 31

// MaxDiasConciliacion es el período más largo que se concilia de una vez.
const MaxDiasConciliacion = 366

// TTLRecibidos es cuánto se guarda lo que trajo Arcanum por CUIT y rango.
const TTLRecibidos = 5 * time.Minute

const topeFactorum = 50000

// Conciliacion es el resultado completo para la API, MCP y las exportaciones.
type Conciliacion struct {
	Estado  string `json:"estado"`
	Mensaje string `json:"mensaje,omitempty"`
	Cliente string `json:"cliente"`
	Desde   string `json:"desde"`
	Hasta   string `json:"hasta"`

	// Cobertura de la sincronización de Arcanum para ese CUIT. SinSincronizar son los tramos del
	// período pedido que todavía no bajó; CubreTodo, que no falta ninguno.
	Cobertura      *arcanum.Cobertura  `json:"cobertura,omitempty"`
	SinSincronizar []conciliar.Periodo `json:"sin_sincronizar"`
	CubreTodo      bool                `json:"cubre_todo"`

	EnArca     int                  `json:"en_arca"`  // recibidos que trajo Arcanum (con el margen de fechas)
	Truncado   bool                 `json:"truncado"` // Arcanum tenía más de los que manda en una respuesta
	EnFactorum int                  `json:"en_factorum"`
	SinFecha   int                  `json:"sin_fecha"` // del cliente, sin fecha: no se pueden ubicar en un período
	Consulta   time.Time            `json:"consultado"`
	DeCache    bool                 `json:"de_cache"`
	Resultado  *conciliar.Resultado `json:"resultado,omitempty"`
}

type entradaRecibidos struct {
	cliente *arcanum.Cliente
	vence   time.Time
	cuando  time.Time
	datos   *arcanum.Recibidos
}

type cacheRecibidos struct {
	mu sync.Mutex
	m  map[string]entradaRecibidos
}

func (c *cacheRecibidos) leer(k string, cli *arcanum.Cliente, ahora time.Time) (entradaRecibidos, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || e.cliente != cli || ahora.After(e.vence) {
		return entradaRecibidos{}, false
	}
	return e, true
}

func (c *cacheRecibidos) guardar(k string, e entradaRecibidos, ahora time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]entradaRecibidos{}
	}
	for x, v := range c.m { // se limpia lo vencido; con muchas entradas, se empieza de cero
		if ahora.After(v.vence) {
			delete(c.m, x)
		}
	}
	if len(c.m) >= 200 {
		c.m = map[string]entradaRecibidos{}
	}
	c.m[k] = e
}

// ValidarPeriodo controla desde y hasta (AAAA-MM-DD, desde <= hasta, a lo sumo MaxDiasConciliacion).
func ValidarPeriodo(desde, hasta string) error {
	d, e1 := time.Parse("2006-01-02", desde)
	h, e2 := time.Parse("2006-01-02", hasta)
	if e1 != nil || e2 != nil {
		return fmt.Errorf("elegí el período: desde y hasta con el formato AAAA-MM-DD")
	}
	if d.After(h) {
		return fmt.Errorf("el «desde» es posterior al «hasta»")
	}
	if h.Sub(d).Hours()/24 >= MaxDiasConciliacion {
		return fmt.Errorf("el período no puede pasar de un año: conciliá de a tramos")
	}
	return nil
}

func dia(f string, n int) string {
	t, _ := time.Parse("2006-01-02", f)
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// Conciliar cruza lo cargado para un cliente con lo que ARCA dice que recibió en el período. El
// cliente y el período ya vienen validados (y el alcance controlado) por quien llama. refrescar
// vuelve a pedirle a Arcanum aunque haya algo guardado.
func (a *App) Conciliar(ctx context.Context, cliente, desde, hasta string, refrescar bool) (*Conciliacion, error) {
	cliente = arca.SoloDigitos(cliente)
	if err := ValidarPeriodo(desde, hasta); err != nil {
		return nil, err
	}
	res := &Conciliacion{Cliente: cliente, Desde: desde, Hasta: hasta, SinSincronizar: []conciliar.Periodo{}}
	d0, h0 := dia(desde, -MargenConciliacion), dia(hasta, MargenConciliacion)

	cbs, sinFecha, err := a.Store.DelCliente(ctx, cliente, d0, h0, topeFactorum)
	if err != nil {
		return nil, err
	}
	res.SinFecha = sinFecha
	for _, c := range cbs {
		if c.Datos.Fecha >= desde && c.Datos.Fecha <= hasta {
			res.EnFactorum++
		}
	}

	arc := a.Arcanum()
	if arc == nil {
		res.Estado = ConcSinArcanum
		res.Mensaje = "Factorum no está conectado con Arcanum. La conciliación necesita Arcanum con Mis Comprobantes: un admin lo configura en Configuración, Arcanum."
		return res, nil
	}
	clave := cliente + "|" + d0 + "|" + h0
	ahora := a.ahora()
	e, ok := a.recibidos.leer(clave, arc, ahora)
	if ok && !refrescar {
		res.DeCache = true
	} else {
		r, err := arc.MisRecibidos(ctx, cliente, d0, h0)
		if err != nil {
			res.Estado, res.Mensaje = estadoErrorArcanum(err, cliente)
			if res.Estado == ConcArcanumError && a.Log != nil {
				a.Log.Warn("conciliación: Arcanum respondió con un error", "cliente", cliente, "error", err)
			}
			return res, nil
		}
		e = entradaRecibidos{cliente: arc, vence: ahora.Add(TTLRecibidos), cuando: ahora, datos: r}
		a.recibidos.guardar(clave, e, ahora)
	}
	rec := e.datos
	res.Consulta = e.cuando
	res.Cobertura = &rec.Cobertura
	res.EnArca, res.Truncado = len(rec.Comprobantes), rec.Truncado
	var bajados []conciliar.Periodo
	for _, x := range rec.Cobertura.Rangos {
		bajados = append(bajados, conciliar.Periodo{Desde: x.Desde, Hasta: x.Hasta})
	}
	res.SinSincronizar = conciliar.Faltantes(desde, hasta, bajados)
	if res.SinSincronizar == nil {
		res.SinSincronizar = []conciliar.Periodo{}
	}
	res.CubreTodo = len(res.SinSincronizar) == 0
	if !rec.Cobertura.Sincronizado && len(rec.Comprobantes) == 0 {
		res.Estado = ConcSinSincronizar
		res.Mensaje = "Arcanum todavía no bajó Mis Comprobantes de " + arca.FormatearCUIT(cliente) + ". En Arcanum, cargá la clave fiscal del cliente en Mis Comprobantes y sincronizá los recibidos; después volvé a conciliar."
		return res, nil
	}

	fs := make([]conciliar.Comprobante, 0, len(cbs))
	for i := range cbs {
		c := &cbs[i]
		d := &c.Datos
		fs = append(fs, conciliar.Comprobante{ID: c.ID, Tipo: d.Tipo, PuntoVenta: d.PuntoVenta, Numero: d.Numero, Fecha: d.Fecha,
			CUITEmisor: d.CUITEmisor, RazonSocial: d.RazonSocialEmisor, DocTipoRec: d.DocTipoReceptor, DocNroRec: d.DocNroReceptor,
			Moneda: d.Moneda, Neto: d.NetoGravado, IVA: d.IVATotal(), Total: d.Total, EsTique: d.EsTique,
			Estado: string(c.Estado), Constatacion: estadoConstatacion(c.Constatacion), Archivo: c.Archivo})
	}
	as := make([]conciliar.Recibido, 0, len(rec.Comprobantes))
	for _, r := range rec.Comprobantes {
		as = append(as, conciliar.Recibido{Fecha: r.Fecha, Tipo: r.CbteTipo, PuntoVenta: r.PuntoVenta, NumeroDesde: r.NumeroDesde, NumeroHasta: r.NumeroHasta,
			CAE: r.CAE, CUITEmisor: r.CUITEmisor, Emisor: r.Emisor, Moneda: r.Moneda, TipoCambio: r.TipoCambio,
			Neto: modelo.DesdeFloat(r.NetoGravado), IVA: modelo.DesdeFloat(r.IVA), Total: modelo.DesdeFloat(r.Total), EmisorApocrifo: r.EmisorApocrifo})
	}
	res.Resultado = conciliar.Conciliar(fs, as, conciliar.Opciones{Cliente: cliente, Desde: desde, Hasta: hasta, SinSincronizar: res.SinSincronizar})
	res.Estado = ConcLista
	switch {
	case len(res.SinSincronizar) > 0 && !rec.Cobertura.Sincronizado:
		res.Mensaje = "Arcanum todavía no bajó Mis Comprobantes de este CUIT."
	case len(res.SinSincronizar) > 0:
		res.Mensaje = "Arcanum no bajó todo el período: lo que cae en los días sin sincronizar puede figurar como «Falta cargar» o «No figura en ARCA» sin serlo."
	}
	if res.Truncado {
		res.Mensaje = "Arcanum tiene más recibidos de los que manda de una vez (" + fmt.Sprint(rec.Total) + "): conciliá un período más corto. " + res.Mensaje
	}
	return res, nil
}

// estadoErrorArcanum traduce un error de Arcanum a un estado y un mensaje claro (nunca el detalle crudo).
func estadoErrorArcanum(err error, cliente string) (string, string) {
	if arcanum.EsErrConexion(err) {
		return ConcArcanumCaido, "Arcanum no responde ahora: probá en un rato. Si sigue, un admin puede revisar la conexión en Configuración, Arcanum."
	}
	st, msg, ok := arcanum.ErrHTTP(err)
	switch {
	case ok && (st == http.StatusUnauthorized):
		return ConcArcanumError, "Arcanum rechazó la clave que usa Factorum: un admin tiene que cargar una clave vigente en Configuración, Arcanum."
	case ok && st == http.StatusForbidden:
		return ConcSinPermiso, "La clave de Arcanum que usa Factorum no tiene permiso para " + arca.FormatearCUIT(cliente) + ": pedile a un admin de Arcanum que sume ese CUIT a la clave."
	case ok && st == http.StatusNotFound:
		return ConcArcanumViejo, "Esta versión de Arcanum no tiene la conciliación: hace falta Arcanum 2.3.0 o superior."
	case ok && st == http.StatusBadRequest:
		return ConcArcanumError, "Arcanum no aceptó el pedido: " + msg
	}
	return ConcArcanumError, "Arcanum no pudo devolver los recibidos ahora: probá en un rato."
}
