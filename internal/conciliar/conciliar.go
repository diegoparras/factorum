// Package conciliar cruza las facturas de compra que un cliente le mandó al estudio (lo cargado en
// Factorum) con lo que ARCA dice que ese CUIT recibió (Mis Comprobantes, vía Arcanum). Es puro: no
// habla con la base ni con la red. Los importes van en centavos (modelo.Importe) y se comparan con
// una tolerancia de redondeo por importe.
//
// Cada comprobante termina en uno de cuatro grupos:
//   - coinciden: está en los dos, con el mismo emisor, tipo, punto de venta, número, fecha, moneda
//     y total (y neto e IVA cuando el comprobante los discrimina y ARCA los trae);
//   - diferencias: está en los dos pero algo no cierra (se dice qué y cuánto), o es «casi igual»:
//     mismo emisor y número con otro punto de venta o tipo, típico de un error de lectura. Eso es
//     una sugerencia, nunca una coincidencia;
//   - falta_cargar: está en ARCA y no en Factorum (el cliente no la mandó);
//   - no_figura: está en Factorum y no en ARCA, con la causa cuando es legítima (tique, exterior,
//     período sin sincronizar, nota de crédito o débito, otro receptor) o la sugerencia de
//     constatarlo.
package conciliar

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Grupos, en el orden en que se muestran.
const (
	Coinciden   = "coinciden"
	Diferencias = "diferencias"
	FaltaCargar = "falta_cargar"
	NoFigura    = "no_figura"
)

// Grupos lista los grupos en orden.
var Grupos = []string{Coinciden, Diferencias, FaltaCargar, NoFigura}

// NombreGrupo es el título de cada grupo para una persona.
var NombreGrupo = map[string]string{
	Coinciden:   "Coinciden",
	Diferencias: "Con diferencias",
	FaltaCargar: "Falta cargar",
	NoFigura:    "No figura en ARCA",
}

// Causas de «No figura en ARCA».
const (
	CausaTique         = "tique"           // tique de controlador fiscal: no va a Mis Comprobantes
	CausaExterior      = "exterior"        // moneda extranjera, factura E o emisor del exterior
	CausaOtroReceptor  = "otro_receptor"   // a nombre de otro CUIT o a consumidor final
	CausaNoSincronizad = "no_sincronizado" // fecha en un tramo que Arcanum todavía no bajó
	CausaNota          = "nota"            // nota de crédito o débito
	CausaIncompleto    = "incompleto"      // le faltan datos para cruzarlo (CUIT, tipo o número)
	CausaSinExplicar   = "sin_explicacion"
)

// Legitima dice si la causa explica la ausencia sin que sea una alarma.
var Legitima = map[string]bool{CausaTique: true, CausaExterior: true, CausaOtroReceptor: true, CausaNoSincronizad: true, CausaNota: true}

// ToleranciaDefecto es el redondeo aceptado por importe: un centavo.
const ToleranciaDefecto modelo.Importe = 1

// Comprobante es lo cargado en Factorum (un comprobante leído de un archivo).
type Comprobante struct {
	ID           int64
	Tipo         int
	PuntoVenta   int
	Numero       int64
	Fecha        string // AAAA-MM-DD
	CUITEmisor   string
	RazonSocial  string
	DocTipoRec   int    // 80 CUIT, 96 DNI, 99 consumidor final
	DocNroRec    string // documento del receptor leído
	Moneda       string
	Neto         modelo.Importe
	IVA          modelo.Importe
	Total        modelo.Importe
	EsTique      bool
	Estado       string // ok, revisar, error, aprobado
	Constatacion string // "", valido, invalido, apocrifo, controlado, error
	Archivo      string
}

// Recibido es una fila de Mis Comprobantes (recibidos) tal como la trae Arcanum.
type Recibido struct {
	Fecha          string
	Tipo           int
	PuntoVenta     int
	NumeroDesde    int64
	NumeroHasta    int64
	CAE            string
	CUITEmisor     string
	Emisor         string
	Moneda         string
	TipoCambio     float64
	Neto           modelo.Importe
	IVA            modelo.Importe
	Total          modelo.Importe
	EmisorApocrifo *bool
}

// Periodo es un rango de fechas, ambos extremos incluidos.
type Periodo struct {
	Desde string `json:"desde"`
	Hasta string `json:"hasta"`
}

// Opciones de la conciliación.
type Opciones struct {
	Cliente string // CUIT del cliente (el receptor)
	Desde   string // período pedido, por fecha de emisión
	Hasta   string
	// SinSincronizar son los tramos del período que Arcanum todavía no bajó.
	SinSincronizar []Periodo
	Tolerancia     modelo.Importe // 0 = ToleranciaDefecto
}

// LadoFactorum es lo que Factorum tiene de un comprobante.
type LadoFactorum struct {
	ID           int64          `json:"id"`
	Tipo         int            `json:"tipo"`
	PuntoVenta   int            `json:"punto_venta"`
	Numero       int64          `json:"numero"`
	Fecha        string         `json:"fecha"`
	Moneda       string         `json:"moneda"`
	Neto         modelo.Importe `json:"neto_gravado"`
	IVA          modelo.Importe `json:"iva"`
	Total        modelo.Importe `json:"total"`
	Estado       string         `json:"estado"`
	Constatacion string         `json:"constatacion"`
	Archivo      string         `json:"archivo"`
}

// LadoArca es lo que ARCA tiene de un comprobante.
type LadoArca struct {
	Tipo           int            `json:"tipo"`
	PuntoVenta     int            `json:"punto_venta"`
	NumeroDesde    int64          `json:"numero_desde"`
	NumeroHasta    int64          `json:"numero_hasta"`
	Fecha          string         `json:"fecha"`
	Moneda         string         `json:"moneda"`
	TipoCambio     float64        `json:"tipo_cambio"`
	Neto           modelo.Importe `json:"neto_gravado"`
	IVA            modelo.Importe `json:"iva"`
	Total          modelo.Importe `json:"total"`
	CAE            string         `json:"cae,omitempty"`
	EmisorApocrifo *bool          `json:"emisor_apocrifo"`
}

// Diferencia es un campo que no cierra entre los dos lados.
type Diferencia struct {
	Campo    string         `json:"campo"` // total, neto_gravado, iva, fecha, moneda, punto_venta, tipo
	Factorum string         `json:"factorum"`
	Arca     string         `json:"arca"`
	Importe  modelo.Importe `json:"diferencia,omitempty"` // Factorum menos ARCA, en los importes
	Dias     int            `json:"dias,omitempty"`       // Factorum menos ARCA, en la fecha
}

// Item es un comprobante conciliado.
type Item struct {
	Grupo      string         `json:"grupo"`
	Clave      string         `json:"clave"`
	Tipo       int            `json:"tipo"`
	NombreTipo string         `json:"nombre_tipo"`
	Clase      string         `json:"clase"` // factura, nota_credito, nota_debito, ...
	PuntoVenta int            `json:"punto_venta"`
	Numero     int64          `json:"numero"`
	Fecha      string         `json:"fecha"`
	CUITEmisor string         `json:"cuit_emisor"`
	Emisor     string         `json:"emisor"`
	Moneda     string         `json:"moneda"`
	Extranjera bool           `json:"moneda_extranjera"`
	Total      modelo.Importe `json:"total"` // el de ARCA si está; si no, el de Factorum (sin signo)

	Factorum *LadoFactorum `json:"factorum,omitempty"`
	Arca     *LadoArca     `json:"arca,omitempty"`

	Diferencias []Diferencia `json:"diferencias,omitempty"`
	// CasiIgual: mismo emisor y número con otro punto de venta o tipo. Es una sugerencia.
	CasiIgual bool   `json:"casi_igual,omitempty"`
	Nota      string `json:"nota,omitempty"`

	// Solo en «No figura en ARCA».
	Causa     string `json:"causa,omitempty"`
	Legitima  bool   `json:"legitima,omitempty"`
	Alarma    bool   `json:"alarma,omitempty"` // apócrifo o ARCA no lo reconoce
	Constatar bool   `json:"constatar,omitempty"`

	// Duplicados: el mismo comprobante cargado más de una vez en Factorum (otros ids) o repetido en ARCA.
	DuplicadosFactorum []int64 `json:"duplicados_factorum,omitempty"`
	DuplicadosArca     int     `json:"duplicados_arca,omitempty"`
}

// Total de un grupo. Las notas de crédito restan; la moneda extranjera va aparte, por moneda.
type Total struct {
	Cantidad           int                       `json:"cantidad"`
	Pesos              modelo.Importe            `json:"pesos"`
	PesosFactorum      modelo.Importe            `json:"pesos_factorum"`
	PesosArca          modelo.Importe            `json:"pesos_arca"`
	CantidadExtranjera int                       `json:"cantidad_moneda_extranjera"`
	Extranjera         map[string]modelo.Importe `json:"moneda_extranjera"`
	PorCausa           map[string]int            `json:"por_causa,omitempty"`
	Legitimas          int                       `json:"legitimas,omitempty"`
	Alarmas            int                       `json:"alarmas,omitempty"`
	CasiIguales        int                       `json:"casi_iguales,omitempty"`
}

// Resultado de una conciliación.
type Resultado struct {
	Cliente string            `json:"cliente"`
	Desde   string            `json:"desde"`
	Hasta   string            `json:"hasta"`
	Grupos  map[string][]Item `json:"grupos"`
	Totales map[string]*Total `json:"totales"`
	// Duplicados cuenta las filas repetidas de cada lado (las que sobran).
	Duplicados struct {
		Factorum int `json:"factorum"`
		Arca     int `json:"arca"`
	} `json:"duplicados"`
	// Incompletos: comprobantes de Factorum del período sin CUIT, tipo o número (van en No figura).
	Incompletos int `json:"incompletos"`
}

// NormalizarMoneda lleva los códigos que se ven en los comprobantes y en Mis Comprobantes a los de
// ARCA: peso PES, dólar DOL, euro 060.
func NormalizarMoneda(m string) string {
	switch s := strings.ToUpper(strings.TrimSpace(m)); s {
	case "", "PES", "$", "ARS", "PESOS":
		return "PES"
	case "DOL", "USD", "U$S", "US$", "U$D":
		return "DOL"
	case "EUR", "060":
		return "060"
	default:
		return s
	}
}

// Clave normaliza emisor, tipo, punto de venta y número. "" si falta el emisor, el tipo o el número.
func Clave(cuitEmisor string, tipo, pv int, numero int64) string {
	c := arca.SoloDigitos(cuitEmisor)
	if c == "" || tipo <= 0 || numero <= 0 || pv < 0 {
		return ""
	}
	return c + "-" + strconv.Itoa(tipo) + "-" + strconv.Itoa(pv) + "-" + strconv.FormatInt(numero, 10)
}

func claseDe(tipo int) arca.Clase {
	if t, ok := arca.Tipo(tipo); ok {
		return t.Clase
	}
	return arca.ClaseOtro
}

func nombreTipo(tipo int) string {
	if t, ok := arca.Tipo(tipo); ok {
		return t.Nombre
	}
	return "Comprobante " + strconv.Itoa(tipo)
}

// signo: las notas de crédito restan.
func signo(tipo int) modelo.Importe {
	if claseDe(tipo) == arca.ClaseNotaCredito {
		return -1
	}
	return 1
}

// discrimina dice si el comprobante trae neto e IVA por separado (letras A y M). En B y C el IVA va
// incluido: no se comparan neto ni IVA.
func discrimina(tipo int) bool {
	t, ok := arca.Tipo(tipo)
	return ok && (t.Letra == "A" || t.Letra == "M")
}

func abs(i modelo.Importe) modelo.Importe {
	if i < 0 {
		return -i
	}
	return i
}

func esTique(c *Comprobante) bool { return c.EsTique || arca.EsTique(c.Tipo) }

// emisorDelExterior: los CUIT genéricos de países (50, 51 y 55) los usan los proveedores del exterior.
func emisorDelExterior(cuit string) bool {
	c := arca.SoloDigitos(cuit)
	return len(c) == 11 && (strings.HasPrefix(c, "50") || strings.HasPrefix(c, "51") || strings.HasPrefix(c, "55"))
}

func dias(a, b string) (int, bool) {
	ta, e1 := time.Parse("2006-01-02", a)
	tb, e2 := time.Parse("2006-01-02", b)
	if e1 != nil || e2 != nil {
		return 0, false
	}
	return int(ta.Sub(tb).Hours() / 24), true
}

func enRango(f string, p Periodo) bool { return f != "" && f >= p.Desde && f <= p.Hasta }

func ladoF(c *Comprobante) *LadoFactorum {
	return &LadoFactorum{ID: c.ID, Tipo: c.Tipo, PuntoVenta: c.PuntoVenta, Numero: c.Numero, Fecha: c.Fecha, Moneda: NormalizarMoneda(c.Moneda),
		Neto: c.Neto, IVA: c.IVA, Total: c.Total, Estado: c.Estado, Constatacion: c.Constatacion, Archivo: c.Archivo}
}

func ladoA(r *Recibido) *LadoArca {
	return &LadoArca{Tipo: r.Tipo, PuntoVenta: r.PuntoVenta, NumeroDesde: r.NumeroDesde, NumeroHasta: r.NumeroHasta, Fecha: r.Fecha,
		Moneda: NormalizarMoneda(r.Moneda), TipoCambio: r.TipoCambio, Neto: r.Neto, IVA: r.IVA, Total: r.Total, CAE: r.CAE, EmisorApocrifo: r.EmisorApocrifo}
}

// comparar devuelve lo que no cierra entre un comprobante de Factorum y uno de ARCA.
func comparar(f *Comprobante, a *Recibido, tol modelo.Importe) []Diferencia {
	var d []Diferencia
	mf, ma := NormalizarMoneda(f.Moneda), NormalizarMoneda(a.Moneda)
	if mf != ma {
		d = append(d, Diferencia{Campo: "moneda", Factorum: mf, Arca: ma})
	} else {
		imp := func(campo string, vf, va modelo.Importe) {
			if x := abs(vf) - abs(va); abs(x) > tol {
				d = append(d, Diferencia{Campo: campo, Factorum: abs(vf).String(), Arca: abs(va).String(), Importe: x})
			}
		}
		imp("total", f.Total, a.Total)
		// Neto e IVA: solo si el comprobante los discrimina y ARCA los trae.
		if discrimina(a.Tipo) && discrimina(f.Tipo) {
			if a.Neto != 0 {
				imp("neto_gravado", f.Neto, a.Neto)
			}
			if a.IVA != 0 {
				imp("iva", f.IVA, a.IVA)
			}
		}
	}
	if f.Fecha != a.Fecha {
		n, _ := dias(f.Fecha, a.Fecha)
		d = append(d, Diferencia{Campo: "fecha", Factorum: f.Fecha, Arca: a.Fecha, Dias: n})
	}
	return d
}

// puntaje para elegir, entre varios de Factorum con la misma clave, el que mejor cierra con ARCA.
func puntaje(f *Comprobante, a *Recibido, tol modelo.Importe) int {
	p := 0
	if len(comparar(f, a, tol)) == 0 {
		p += 100
	}
	if abs(abs(f.Total)-abs(a.Total)) <= tol {
		p += 10
	}
	if f.Fecha == a.Fecha {
		p += 5
	}
	switch f.Estado {
	case "aprobado":
		p += 3
	case "ok":
		p += 2
	case "revisar":
		p++
	}
	return p
}

// Conciliar cruza lo cargado en Factorum con lo recibido en ARCA. Conviene pasarle los dos lados con
// un margen de fechas alrededor del período: así un comprobante con la fecha mal leída se empareja
// igual (y queda en «Con diferencias»). Lo que queda afuera del período en los dos lados se descarta.
func Conciliar(fs []Comprobante, as []Recibido, o Opciones) *Resultado {
	tol := o.Tolerancia
	if tol <= 0 {
		tol = ToleranciaDefecto
	}
	per := Periodo{o.Desde, o.Hasta}
	res := &Resultado{Cliente: arca.SoloDigitos(o.Cliente), Desde: o.Desde, Hasta: o.Hasta, Grupos: map[string][]Item{}, Totales: map[string]*Total{}}
	for _, g := range Grupos {
		res.Grupos[g] = []Item{}
	}

	// Factorum por clave (en el orden en que vinieron: el primero es el más viejo si vienen por id).
	porClaveF := map[string][]*Comprobante{}
	var clavesF []string
	var incompletos []*Comprobante
	for i := range fs {
		c := &fs[i]
		k := Clave(c.CUITEmisor, c.Tipo, c.PuntoVenta, c.Numero)
		if k == "" {
			incompletos = append(incompletos, c)
			continue
		}
		if _, ok := porClaveF[k]; !ok {
			clavesF = append(clavesF, k)
		}
		porClaveF[k] = append(porClaveF[k], c)
	}
	// ARCA: las filas de un comprobante por clave; las de un rango de números, aparte.
	porClaveA := map[string][]*Recibido{}
	var clavesA []string
	var rangos []*Recibido
	for i := range as {
		r := &as[i]
		if r.NumeroHasta > r.NumeroDesde {
			rangos = append(rangos, r)
			continue
		}
		k := Clave(r.CUITEmisor, r.Tipo, r.PuntoVenta, r.NumeroDesde)
		if k == "" {
			continue
		}
		if _, ok := porClaveA[k]; !ok {
			clavesA = append(clavesA, k)
		}
		porClaveA[k] = append(porClaveA[k], r)
	}

	usadosF := map[string]bool{}
	usadosA := map[string]bool{}
	agregar := func(it Item, fechas ...string) {
		for _, f := range fechas {
			if enRango(f, per) {
				res.Grupos[it.Grupo] = append(res.Grupos[it.Grupo], it)
				return
			}
		}
	}
	duplicados := func(it *Item, fsK []*Comprobante, elegido *Comprobante, nA int) {
		for _, c := range fsK {
			if c != elegido {
				it.DuplicadosFactorum = append(it.DuplicadosFactorum, c.ID)
			}
		}
		if nA > 1 {
			it.DuplicadosArca = nA - 1
		}
	}

	// 1. Misma clave en los dos lados.
	for _, k := range clavesA {
		fsK, ok := porClaveF[k]
		if !ok {
			continue
		}
		a := porClaveA[k][0]
		mejor := fsK[0]
		for _, c := range fsK[1:] {
			if puntaje(c, a, tol) > puntaje(mejor, a, tol) {
				mejor = c
			}
		}
		it := base(mejor, a)
		it.Diferencias = comparar(mejor, a, tol)
		it.Grupo = Coinciden
		if len(it.Diferencias) > 0 {
			it.Grupo = Diferencias
		}
		duplicados(&it, fsK, mejor, len(porClaveA[k]))
		usadosF[k], usadosA[k] = true, true
		agregar(it, a.Fecha, mejor.Fecha)
	}

	// 2. Números dentro de un rango de ARCA (un renglón por varios comprobantes): coincide el número;
	// los importes de ARCA son del rango entero, así que no se comparan.
	rangoUsado := map[*Recibido]bool{}
	for _, k := range clavesF {
		if usadosF[k] {
			continue
		}
		c := porClaveF[k][0]
		for _, r := range rangos {
			if arca.SoloDigitos(r.CUITEmisor) == arca.SoloDigitos(c.CUITEmisor) && r.Tipo == c.Tipo && r.PuntoVenta == c.PuntoVenta &&
				c.Numero >= r.NumeroDesde && c.Numero <= r.NumeroHasta {
				it := base(c, r)
				it.Grupo = Coinciden
				it.Total = c.Total
				it.Nota = "En ARCA figura en un renglón del " + strconv.FormatInt(r.NumeroDesde, 10) + " al " + strconv.FormatInt(r.NumeroHasta, 10) + ": se controla el número, no los importes."
				duplicados(&it, porClaveF[k], c, 1)
				usadosF[k] = true
				rangoUsado[r] = true
				agregar(it, c.Fecha, r.Fecha)
				break
			}
		}
	}

	// 3. «Casi igual»: mismo emisor y número, otro punto de venta o tipo de la misma clase, y el mismo
	// total o la misma fecha. Típico de un error de lectura: va como sugerencia en «Con diferencias».
	pendA := map[string][]string{} // emisor-número -> claves de ARCA sin usar
	for _, k := range clavesA {
		if usadosA[k] {
			continue
		}
		a := porClaveA[k][0]
		en := arca.SoloDigitos(a.CUITEmisor) + "#" + strconv.FormatInt(a.NumeroDesde, 10)
		pendA[en] = append(pendA[en], k)
	}
	for _, k := range clavesF {
		if usadosF[k] {
			continue
		}
		c := porClaveF[k][0]
		var mejorK string
		mejorP := -1
		for _, ka := range pendA[arca.SoloDigitos(c.CUITEmisor)+"#"+strconv.FormatInt(c.Numero, 10)] {
			if usadosA[ka] {
				continue
			}
			a := porClaveA[ka][0]
			if claseDe(a.Tipo) != claseDe(c.Tipo) {
				continue
			}
			mismoTotal := NormalizarMoneda(a.Moneda) == NormalizarMoneda(c.Moneda) && abs(abs(c.Total)-abs(a.Total)) <= tol
			mismaFecha := a.Fecha == c.Fecha
			if !mismoTotal && !mismaFecha {
				continue
			}
			p := 0
			if mismoTotal {
				p += 2
			}
			if mismaFecha {
				p++
			}
			if p > mejorP {
				mejorK, mejorP = ka, p
			}
		}
		if mejorK == "" {
			continue
		}
		a := porClaveA[mejorK][0]
		it := base(c, a)
		it.Grupo = Diferencias
		it.CasiIgual = true
		if a.PuntoVenta != c.PuntoVenta {
			it.Diferencias = append(it.Diferencias, Diferencia{Campo: "punto_venta", Factorum: strconv.Itoa(c.PuntoVenta), Arca: strconv.Itoa(a.PuntoVenta)})
		}
		if a.Tipo != c.Tipo {
			it.Diferencias = append(it.Diferencias, Diferencia{Campo: "tipo", Factorum: nombreTipo(c.Tipo), Arca: nombreTipo(a.Tipo)})
		}
		it.Diferencias = append(it.Diferencias, comparar(c, a, tol)...)
		it.Nota = "Parece el mismo comprobante leído con otro " + queCambia(a, c) + ": revisalo en Factorum. Si no es el mismo, el de ARCA falta cargar."
		duplicados(&it, porClaveF[k], c, len(porClaveA[mejorK]))
		usadosF[k], usadosA[mejorK] = true, true
		agregar(it, a.Fecha, c.Fecha)
	}

	// 4. Lo que queda de ARCA: falta cargar.
	for _, k := range clavesA {
		if usadosA[k] {
			continue
		}
		a := porClaveA[k][0]
		it := baseArca(a)
		it.Grupo = FaltaCargar
		if len(porClaveA[k]) > 1 {
			it.DuplicadosArca = len(porClaveA[k]) - 1
		}
		agregar(it, a.Fecha)
	}
	for _, r := range rangos {
		if rangoUsado[r] {
			continue
		}
		it := baseArca(r)
		it.Grupo = FaltaCargar
		it.Nota = "En ARCA figura en un renglón del " + strconv.FormatInt(r.NumeroDesde, 10) + " al " + strconv.FormatInt(r.NumeroHasta, 10) + "."
		agregar(it, r.Fecha)
	}

	// 5. Lo que queda de Factorum: no figura en ARCA, con su causa.
	for _, k := range clavesF {
		if usadosF[k] {
			continue
		}
		fsK := porClaveF[k]
		c := fsK[0]
		it := baseFactorum(c)
		it.Grupo = NoFigura
		causa(&it, c, res.Cliente, o.SinSincronizar)
		duplicados(&it, fsK, c, 0)
		agregar(it, c.Fecha)
	}
	for _, c := range incompletos {
		if !enRango(c.Fecha, per) {
			continue
		}
		it := baseFactorum(c)
		it.Grupo = NoFigura
		it.Causa = CausaIncompleto
		it.Nota = "Le faltan datos para cruzarlo con ARCA (CUIT del emisor, tipo o número): revisalo en Factorum."
		res.Incompletos++
		agregar(it, c.Fecha)
	}

	for _, g := range Grupos {
		ordenar(res.Grupos[g])
		t := &Total{Extranjera: map[string]modelo.Importe{}}
		if g == NoFigura {
			t.PorCausa = map[string]int{}
		}
		for i := range res.Grupos[g] {
			it := &res.Grupos[g][i]
			sumar(t, it)
			res.Duplicados.Factorum += len(it.DuplicadosFactorum)
			res.Duplicados.Arca += it.DuplicadosArca
		}
		res.Totales[g] = t
	}
	return res
}

func queCambia(a *Recibido, c *Comprobante) string {
	switch {
	case a.PuntoVenta != c.PuntoVenta && a.Tipo != c.Tipo:
		return "punto de venta y tipo"
	case a.PuntoVenta != c.PuntoVenta:
		return "punto de venta"
	default:
		return "tipo"
	}
}

func base(c *Comprobante, a *Recibido) Item {
	it := baseArca(a)
	it.Factorum = ladoF(c)
	if it.Emisor == "" {
		it.Emisor = c.RazonSocial
	}
	return it
}

func baseArca(a *Recibido) Item {
	m := NormalizarMoneda(a.Moneda)
	return Item{
		Clave: Clave(a.CUITEmisor, a.Tipo, a.PuntoVenta, a.NumeroDesde), Tipo: a.Tipo, NombreTipo: nombreTipo(a.Tipo), Clase: string(claseDe(a.Tipo)),
		PuntoVenta: a.PuntoVenta, Numero: a.NumeroDesde, Fecha: a.Fecha, CUITEmisor: arca.SoloDigitos(a.CUITEmisor), Emisor: a.Emisor,
		Moneda: m, Extranjera: m != "PES", Total: abs(a.Total), Arca: ladoA(a),
	}
}

func baseFactorum(c *Comprobante) Item {
	m := NormalizarMoneda(c.Moneda)
	return Item{
		Clave: Clave(c.CUITEmisor, c.Tipo, c.PuntoVenta, c.Numero), Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), Clase: string(claseDe(c.Tipo)),
		PuntoVenta: c.PuntoVenta, Numero: c.Numero, Fecha: c.Fecha, CUITEmisor: arca.SoloDigitos(c.CUITEmisor), Emisor: c.RazonSocial,
		Moneda: m, Extranjera: m != "PES", Total: abs(c.Total), Factorum: ladoF(c),
	}
}

// causa explica por qué un comprobante de Factorum no está en ARCA. Primero lo que nunca va a estar
// (tique, exterior, otro receptor), después lo que ARCA todavía no bajó y las notas; si no hay
// explicación, se sugiere constatarlo y se marca la alarma si ya se constató mal.
func causa(it *Item, c *Comprobante, cliente string, sinSync []Periodo) {
	cl := claseDe(c.Tipo)
	tp, _ := arca.Tipo(c.Tipo)
	rec := arca.SoloDigitos(c.DocNroRec)
	switch {
	case esTique(c):
		it.Causa = CausaTique
		it.Nota = "Los tiques de controlador fiscal no van a Mis Comprobantes."
	case c.Tipo == 66:
		it.Causa = CausaExterior
		it.Nota = "Los despachos de importación no van a Mis Comprobantes: van en el libro de importaciones."
	case NormalizarMoneda(c.Moneda) != "PES" || tp.Letra == "E" || emisorDelExterior(c.CUITEmisor):
		it.Causa = CausaExterior
		it.Nota = "Comprobante en moneda extranjera o del exterior: puede no figurar en Mis Comprobantes."
	case c.DocTipoRec == 99 || (rec != "" && strings.Trim(rec, "0") == ""):
		it.Causa = CausaOtroReceptor
		it.Nota = "Está hecho a consumidor final: no identifica al cliente, así que no figura en sus recibidos."
	case len(rec) == 11 && cliente != "" && rec != cliente:
		it.Causa = CausaOtroReceptor
		it.Nota = "Está a nombre de otro CUIT (" + arca.FormatearCUIT(rec) + "), no del cliente."
	case enAlguno(c.Fecha, sinSync):
		it.Causa = CausaNoSincronizad
		it.Nota = "Arcanum todavía no bajó Mis Comprobantes de esa fecha: sincronizá ese período y volvé a conciliar."
	case cl == arca.ClaseNotaCredito || cl == arca.ClaseNotaDebito:
		it.Causa = CausaNota
		it.Nota = "Las notas de crédito y débito a veces aparecen con otra fecha o más tarde: confirmala con el emisor o constatala."
		it.Constatar = c.Constatacion == "" || c.Constatacion == "error"
	default:
		it.Causa = CausaSinExplicar
		it.Constatar = c.Constatacion == "" || c.Constatacion == "error"
		switch c.Constatacion {
		case "apocrifo":
			it.Alarma = true
			it.Nota = "El emisor figura en la base de apócrifos de ARCA: no lo computes."
		case "invalido":
			it.Alarma = true
			it.Nota = "ARCA no reconoce este comprobante con esos datos: revisalo o pedile el original al cliente."
		case "valido":
			it.Nota = "ARCA lo constató (existe), pero no figura en los recibidos del cliente: puede estar a nombre de otro receptor o aparecer más tarde."
		default:
			it.Nota = "No figura en ARCA: constatalo para saber si existe."
		}
	}
	it.Legitima = Legitima[it.Causa]
}

func enAlguno(f string, ps []Periodo) bool {
	for _, p := range ps {
		if enRango(f, p) {
			return true
		}
	}
	return false
}

func sumar(t *Total, it *Item) {
	t.Cantidad++
	if it.Grupo == NoFigura {
		t.PorCausa[it.Causa]++
		if it.Legitima {
			t.Legitimas++
		}
		if it.Alarma {
			t.Alarmas++
		}
	}
	if it.CasiIgual {
		t.CasiIguales++
	}
	s := signo(it.Tipo)
	if it.Extranjera {
		t.CantidadExtranjera++
		t.Extranjera[it.Moneda] += s * it.Total
	} else {
		t.Pesos += s * it.Total
	}
	if it.Factorum != nil && it.Factorum.Moneda == "PES" {
		t.PesosFactorum += signo(it.Factorum.Tipo) * abs(it.Factorum.Total)
	}
	if it.Arca != nil && it.Arca.Moneda == "PES" {
		t.PesosArca += signo(it.Arca.Tipo) * abs(it.Arca.Total)
	}
}

// ordenar por fecha, emisor, tipo, punto de venta y número.
func ordenar(its []Item) {
	sort.SliceStable(its, func(i, j int) bool {
		a, b := its[i], its[j]
		if a.Fecha != b.Fecha {
			return a.Fecha < b.Fecha
		}
		if a.CUITEmisor != b.CUITEmisor {
			return a.CUITEmisor < b.CUITEmisor
		}
		if a.Tipo != b.Tipo {
			return a.Tipo < b.Tipo
		}
		if a.PuntoVenta != b.PuntoVenta {
			return a.PuntoVenta < b.PuntoVenta
		}
		return a.Numero < b.Numero
	})
}

// Faltantes calcula los tramos de [desde, hasta] que no están en ninguno de los rangos bajados.
func Faltantes(desde, hasta string, bajados []Periodo) []Periodo {
	rs := append([]Periodo(nil), bajados...)
	sort.Slice(rs, func(i, j int) bool { return rs[i].Desde < rs[j].Desde })
	var out []Periodo
	cursor := desde
	for _, r := range rs {
		if r.Hasta < cursor || r.Desde > r.Hasta {
			continue
		}
		if r.Desde > hasta {
			break
		}
		if r.Desde > cursor {
			out = append(out, Periodo{cursor, masDias(r.Desde, -1)})
		}
		if c := masDias(r.Hasta, 1); c > cursor {
			cursor = c
		}
		if cursor > hasta {
			break
		}
	}
	if cursor <= hasta {
		out = append(out, Periodo{cursor, hasta})
	}
	return out
}

func masDias(f string, n int) string {
	t, err := time.Parse("2006-01-02", f)
	if err != nil {
		return f
	}
	return t.AddDate(0, 0, n).Format("2006-01-02")
}
