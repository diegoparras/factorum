// Package libroiva arma los archivos de importación de las compras del Libro IVA Digital de ARCA
// (RG 4597 y modificatorias): LIBRO_IVA_DIGITAL_COMPRAS_CBTE (un registro de 325 posiciones por
// comprobante) y LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS (uno de 84 por cada alícuota de IVA), con
// los problemas que conviene resolver antes de importarlos y el resumen del período.
//
// Es puro: recibe los comprobantes ya leídos (los de un cliente y un mes) y no habla con la base
// ni con la red. Los importes van en centavos (modelo.Importe). El diseño de registro, sus fuentes
// oficiales y las decisiones en los casos dudosos están en docs/libro-iva-digital.md.
package libroiva

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Nombres de los archivos dentro del ZIP: la «denominación del archivo» de cada diseño de registro.
const (
	NombreCbte      = "LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt"
	NombreAlicuotas = "LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS.txt"
	NombreResumen   = "resumen.csv"
)

// Entrada es un comprobante de compra guardado en Factorum.
type Entrada struct {
	ID           int64
	Estado       modelo.Estado
	Constatacion string // "", valido, invalido, apocrifo, controlado, error
	Archivo      string
	C            modelo.Comprobante
}

// Conciliacion es lo que dijo la conciliación con Mis Comprobantes, si se pudo hacer. Solo avisa:
// nunca saca un comprobante del archivo.
type Conciliacion struct {
	NoFigura    map[int64]string // no está en los recibidos de ARCA y no hay causa legítima: id -> nota
	Alarma      map[int64]bool   // ARCA marca al emisor como apócrifo
	Diferencias map[int64]string // está en ARCA con otros datos: id -> qué no cierra
}

// Opciones de la exportación.
type Opciones struct {
	Cliente string // CUIT del cliente (el que compra, el que presenta el libro)
	Periodo string // AAAA-MM
	// SoloAprobados deja afuera lo que está «para revisar» o «con errores» sin aprobar y lo que
	// tiene una advertencia fuerte (emisor apócrifo, ARCA no lo reconoce, a nombre de otro CUIT).
	SoloAprobados bool
	Conciliacion  *Conciliacion
}

// Niveles de un problema.
const (
	NivelBloquea     = "bloquea"     // no va al archivo, nunca
	NivelFuerte      = "fuerte"      // va solo si se exportan todos, y no debería computarse
	NivelAdvertencia = "advertencia" // va; conviene mirarlo
	NivelInfo        = "info"        // dato útil
)

var ordenNivel = map[string]int{NivelBloquea: 0, NivelFuerte: 1, NivelAdvertencia: 2, NivelInfo: 3}

type defProblema struct {
	nivel, titulo, ayuda string
}

// Problemas que se detectan, en el orden en que se muestran dentro de cada nivel.
var codigosProblema = []string{
	"sin_cuit_emisor", "sin_fecha", "fuera_del_periodo", "tipo_no_admitido", "sin_numero", "sin_total", "importe_negativo",
	"moneda_desconocida", "moneda_sin_cotizacion", "neto_sin_iva", "iva_sin_neto", "sin_neto", "importes_no_cierran",
	"alicuotas_no_cierran", "no_entra", "duplicado",
	"apocrifo", "invalido", "otro_receptor_a",
	"sin_aprobar", "no_figura_arca", "diferencia_arca", "otros_tributos", "otro_receptor", "redondeo", "iva_no_cierra",
	"suma_b_c", "sin_razon_social", "punto_venta_alto",
	"moneda_extranjera", "neto_calculado", "sin_credito_fiscal",
}

var problemas = map[string]defProblema{
	"sin_cuit_emisor":       {NivelBloquea, "Sin CUIT válido del emisor", "El registro exige el CUIT del vendedor (código 80). Corregilo en Revisar."},
	"sin_fecha":             {NivelBloquea, "Sin fecha", "Sin fecha de emisión no se puede ubicar en un período. Completala en Revisar."},
	"fuera_del_periodo":     {NivelBloquea, "Fuera del período", "La fecha de emisión no es de este mes."},
	"tipo_no_admitido":      {NivelBloquea, "Tipo de comprobante que no va", "El tipo no se reconoce o no va en las compras ordinarias."},
	"sin_numero":            {NivelBloquea, "Sin punto de venta o número", "El registro exige el punto de venta y el número."},
	"sin_total":             {NivelBloquea, "Sin total", "El total es cero."},
	"importe_negativo":      {NivelBloquea, "Importes negativos", "Las notas de crédito van con importes positivos: el tipo de comprobante dice que restan."},
	"moneda_desconocida":    {NivelBloquea, "Moneda desconocida", "La moneda no está en la tabla de monedas de ARCA."},
	"moneda_sin_cotizacion": {NivelBloquea, "Moneda extranjera sin cotización", "El registro exige el tipo de cambio. Cargalo en Revisar (campo Cotización)."},
	"neto_sin_iva":          {NivelBloquea, "Neto gravado sin IVA", "Un comprobante A o M con neto gravado tiene que traer el IVA. Si es exento o no gravado, pasá el importe a ese campo."},
	"iva_sin_neto":          {NivelBloquea, "IVA sin neto gravado", "Falta el neto gravado."},
	"sin_neto":              {NivelBloquea, "Sin neto, no gravado ni exento", "Un comprobante A o M necesita al menos uno de los tres."},
	"importes_no_cierran":   {NivelBloquea, "Los importes no cierran", "Neto + no gravado + exento + IVA + otros tributos tiene que dar el total: ARCA lo controla en los comprobantes A y M."},
	"alicuotas_no_cierran":  {NivelBloquea, "El neto no se puede repartir entre las alícuotas", "Con varias alícuotas, Factorum calcula el neto de cada una desde su IVA; acá no da el neto gravado. Revisá el neto y el IVA de cada alícuota."},
	"no_entra":              {NivelBloquea, "Un dato no entra en el registro", "Un importe o un número es más largo de lo que admite el diseño de ARCA."},
	"duplicado":             {NivelBloquea, "Cargado más de una vez", "Va una sola vez: estos son las copias que sobran. Borralas o dejalas, no cambian el archivo."},
	"apocrifo":              {NivelFuerte, "Emisor apócrifo", "ARCA marca al emisor como apócrifo: su crédito fiscal no se computa. No lo exportes salvo que sepas lo que hacés."},
	"invalido":              {NivelFuerte, "ARCA no reconoce el comprobante", "La constatación dio que no existe con esos datos. Revisalo antes de computarlo."},
	"otro_receptor_a":       {NivelFuerte, "A nombre de otro CUIT", "El comprobante A es de otro comprador: su crédito fiscal no es de este cliente."},
	"sin_aprobar":           {NivelAdvertencia, "Sin aprobar", "Está «para revisar» o «con errores» y nadie lo aprobó. Con «Solo los listos» queda afuera."},
	"no_figura_arca":        {NivelAdvertencia, "No figura en ARCA", "Según la conciliación, no está en los recibidos de Mis Comprobantes y no hay una causa legítima. Conviene constatarlo."},
	"diferencia_arca":       {NivelAdvertencia, "Distinto en ARCA", "Según la conciliación, ARCA lo tiene con otros datos."},
	"otros_tributos":        {NivelAdvertencia, "Otros tributos sin detalle", "Factorum guarda los otros tributos sin separar: van en el campo «Otros tributos». Si son percepciones de IVA, de Ganancias, de Ingresos Brutos o impuestos internos, pasalas a su campo en el Libro IVA Digital: la percepción de IVA solo se computa en el suyo."},
	"otro_receptor":         {NivelAdvertencia, "A nombre de otro CUIT", "El comprobante es de otro comprador. Revisá si va en el libro de este cliente."},
	"redondeo":              {NivelAdvertencia, "Diferencia de redondeo", "La suma de los importes difiere del total en centavos. ARCA puede observarla al importar."},
	"iva_no_cierra":         {NivelAdvertencia, "El IVA no da con el neto", "El IVA no es la alícuota aplicada al neto gravado. Revisalo: el crédito fiscal sale del IVA."},
	"suma_b_c":              {NivelAdvertencia, "El subtotal no da el total", "En un comprobante B o C solo se informa el total; revisalo igual."},
	"sin_razon_social":      {NivelAdvertencia, "Sin razón social del emisor", "El campo va en blanco. Conviene completarla en Revisar."},
	"punto_venta_alto":      {NivelAdvertencia, "Punto de venta 9998 o mayor", "Las especificaciones de ARCA piden un punto de venta menor a 9998. Revisá que esté bien leído."},
	"moneda_extranjera":     {NivelInfo, "En moneda extranjera", "Va en la moneda original con su cotización. Al importar, elegí «Los importes están expresados en la Moneda Original del Comprobante»."},
	"neto_calculado":        {NivelInfo, "Neto por alícuota calculado", "Factorum guarda un solo neto gravado: el de cada alícuota sale de su IVA y la diferencia de redondeo va a la mayor."},
	"sin_credito_fiscal":    {NivelInfo, "Sin crédito fiscal", "B, C y tiques no discriminan el IVA: van con cantidad de alícuotas 0, sin registros de alícuotas y crédito fiscal 0."},
}

// Item es un comprobante dentro de un grupo de problemas.
type Item struct {
	ID         int64          `json:"id"`
	Tipo       int            `json:"tipo"`
	NombreTipo string         `json:"nombre_tipo"`
	PuntoVenta int            `json:"punto_venta"`
	Numero     int64          `json:"numero"`
	Fecha      string         `json:"fecha"`
	CUITEmisor string         `json:"cuit_emisor"`
	Emisor     string         `json:"emisor"`
	Total      modelo.Importe `json:"total"`
	Moneda     string         `json:"moneda"`
	Estado     string         `json:"estado"`
	Detalle    string         `json:"detalle,omitempty"`
	Excluido   bool           `json:"excluido"` // con las opciones pedidas, no va al archivo
}

// Grupo de problemas del mismo tipo.
type Grupo struct {
	Codigo string `json:"codigo"`
	Nivel  string `json:"nivel"`
	Titulo string `json:"titulo"`
	Ayuda  string `json:"ayuda"`
	Items  []Item `json:"items"`
}

// Alicuota es un registro del archivo de alícuotas.
type Alicuota struct {
	Codigo string         `json:"codigo"` // 0003 a 0009
	Tasa   string         `json:"tasa"`   // "21", "10,5"...
	Neto   modelo.Importe `json:"neto_gravado"`
	IVA    modelo.Importe `json:"iva"`
}

// Registro es un comprobante que va al archivo, con lo que se escribe.
type Registro struct {
	ID              int64          `json:"id"`
	Tipo            int            `json:"tipo"`
	NombreTipo      string         `json:"nombre_tipo"`
	PuntoVenta      int            `json:"punto_venta"`
	Numero          int64          `json:"numero"`
	Fecha           string         `json:"fecha"`
	CUITEmisor      string         `json:"cuit_emisor"`
	Emisor          string         `json:"emisor"`
	Moneda          string         `json:"moneda"` // código de ARCA: PES, DOL, 060...
	Cotizacion      float64        `json:"cotizacion"`
	Discrimina      bool           `json:"discrimina_iva"`
	NotaCredito     bool           `json:"nota_credito"`
	Alicuotas       []Alicuota     `json:"alicuotas"`
	NoGravado       modelo.Importe `json:"no_gravado"`
	Exento          modelo.Importe `json:"exento"`
	OtrosTributos   modelo.Importe `json:"otros_tributos"`
	IVA             modelo.Importe `json:"iva"`
	CreditoFiscal   modelo.Importe `json:"credito_fiscal"`
	Total           modelo.Importe `json:"total"`
	CodigoOperacion string         `json:"codigo_operacion"`
	Estado          string         `json:"estado"`
	Archivo         string         `json:"archivo"`

	lineaCbte      string
	lineasAlicuota []string
}

// NetoGravado suma el neto de sus alícuotas.
func (r *Registro) NetoGravado() modelo.Importe {
	var n modelo.Importe
	for _, a := range r.Alicuotas {
		n += a.Neto
	}
	return n
}

// TotalAlicuota suma una alícuota en el período.
type TotalAlicuota struct {
	Codigo string         `json:"codigo"`
	Tasa   string         `json:"tasa"`
	Lineas int            `json:"registros"`
	Neto   modelo.Importe `json:"neto_gravado"`
	IVA    modelo.Importe `json:"iva"`
}

// TotalMoneda suma lo que está en una moneda extranjera, en esa moneda.
type TotalMoneda struct {
	Moneda        string         `json:"moneda"`
	Cantidad      int            `json:"cantidad"`
	IVA           modelo.Importe `json:"iva"`
	CreditoFiscal modelo.Importe `json:"credito_fiscal"`
	Total         modelo.Importe `json:"total"`
}

// Resumen de lo que va al archivo. Los importes son en pesos y las notas de crédito restan; la
// moneda extranjera va aparte, en su moneda.
type Resumen struct {
	Comprobantes     int             `json:"comprobantes"`
	LineasAlicuotas  int             `json:"registros_alicuotas"`
	NotasCredito     int             `json:"notas_credito"`
	SinCreditoFiscal int             `json:"sin_credito_fiscal"` // B, C y tiques
	PorAlicuota      []TotalAlicuota `json:"por_alicuota"`
	NetoGravado      modelo.Importe  `json:"neto_gravado"`
	NoGravado        modelo.Importe  `json:"no_gravado"`
	Exento           modelo.Importe  `json:"exento"`
	IVA              modelo.Importe  `json:"iva"`
	CreditoFiscal    modelo.Importe  `json:"credito_fiscal"`
	OtrosTributos    modelo.Importe  `json:"otros_tributos"`
	Total            modelo.Importe  `json:"total"`
	Extranjera       []TotalMoneda   `json:"moneda_extranjera"`
	PorTipo          map[string]int  `json:"por_tipo"`
}

// Resultado de armar el libro de un período.
type Resultado struct {
	Cliente       string `json:"cliente"`
	Periodo       string `json:"periodo"`
	Desde         string `json:"desde"`
	Hasta         string `json:"hasta"`
	SoloAprobados bool   `json:"solo_aprobados"`

	Leidos    int        `json:"leidos"`    // comprobantes que se miraron
	Registros []Registro `json:"registros"` // los que van al archivo, en el orden del archivo
	Excluidos int        `json:"excluidos"` // los que no van, con las opciones pedidas
	// Cuántos irían con cada opción: todos (sin los que bloquean) o solo los listos.
	CantidadTodos  int `json:"cantidad_todos"`
	CantidadListos int `json:"cantidad_listos"`

	Grupos  []Grupo `json:"problemas"`
	Resumen Resumen `json:"resumen"`

	// Los dos archivos, en ISO 8859-1 y con CRLF al final de cada registro.
	Comprobantes []byte `json:"-"`
	Alicuotas    []byte `json:"-"`
}

type tasa struct {
	codigo string
	bp     int64 // en centésimos de punto: 21 % = 2100
	nombre string
	iva    func(c *modelo.Comprobante) modelo.Importe
}

// Tabla «Alícuotas del IVA» de las Tablas del Sistema, en el orden de su código.
var tasas = []tasa{
	{"0004", 1050, "10,5", func(c *modelo.Comprobante) modelo.Importe { return c.IVA105 }},
	{"0005", 2100, "21", func(c *modelo.Comprobante) modelo.Importe { return c.IVA21 }},
	{"0006", 2700, "27", func(c *modelo.Comprobante) modelo.Importe { return c.IVA27 }},
	{"0008", 500, "5", func(c *modelo.Comprobante) modelo.Importe { return c.IVA5 }},
	{"0009", 250, "2,5", func(c *modelo.Comprobante) modelo.Importe { return c.IVA25 }},
}

// AlicuotaCero es el código de la alícuota 0 %.
const AlicuotaCero = "0003"

// ValidarPeriodo controla AAAA-MM y devuelve el primer y el último día del mes.
func ValidarPeriodo(p string) (string, string, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(p))
	if err != nil || t.Year() < 2000 {
		return "", "", fmt.Errorf("elegí el período: el mes con el formato AAAA-MM")
	}
	return t.Format("2006-01-02"), t.AddDate(0, 1, -1).Format("2006-01-02"), nil
}

type analisis struct {
	e         *Entrada
	reg       *Registro
	probs     []Problema
	bloqueado bool
	fuerte    bool
}

// Problema es lo que se encontró en un comprobante.
type Problema struct {
	Codigo  string
	Detalle string
}

func (a *analisis) add(codigo, formato string, args ...any) {
	if _, ok := problemas[codigo]; !ok {
		panic("libroiva: problema sin definir " + codigo)
	}
	for _, p := range a.probs {
		if p.Codigo == codigo {
			return
		}
	}
	d := formato
	if len(args) > 0 {
		d = fmt.Sprintf(formato, args...)
	}
	a.probs = append(a.probs, Problema{codigo, d})
	switch problemas[codigo].nivel {
	case NivelBloquea:
		a.bloqueado = true
	case NivelFuerte:
		a.fuerte = true
	}
}

// listo: se puede exportar con «Solo los listos».
func (a *analisis) listo() bool {
	return !a.bloqueado && !a.fuerte && (a.e.Estado == modelo.EstadoOK || a.e.Estado == modelo.EstadoAprobado)
}

func abs(v modelo.Importe) modelo.Importe {
	if v < 0 {
		return -v
	}
	return v
}

// tolerancia de redondeo para comparar sumas: un centavo por componente, como mínimo dos (la misma
// regla que los controles de Factorum).
func tolerancia(c *modelo.Comprobante) modelo.Importe {
	n := modelo.Importe(0)
	for _, v := range []modelo.Importe{c.NetoGravado, c.NoGravado, c.Exento, c.IVA27, c.IVA21, c.IVA105, c.IVA5, c.IVA25, c.OtrosTributos} {
		if v != 0 {
			n++
		}
	}
	return max(n, 2)
}

// netoDesdeIVA redondea iva / tasa al centavo.
func netoDesdeIVA(iva modelo.Importe, bp int64) modelo.Importe {
	return modelo.Importe((int64(iva)*10000*2 + bp) / (2 * bp))
}

// tolNeto es cuánto se puede mover el neto de una alícuota sin que cambie su IVA redondeado.
func tolNeto(bp int64) modelo.Importe {
	return modelo.Importe((5000+bp-1)/bp + 1)
}

func nombreTipo(tipo int) string {
	if t, ok := arca.Tipo(tipo); ok {
		return t.Nombre
	}
	return "Comprobante " + strconv.Itoa(tipo)
}

func (o *Opciones) analizar(e *Entrada, desde, hasta string) *analisis {
	a := &analisis{e: e}
	c := &e.C
	tp, ok := arca.Tipo(c.Tipo)
	switch {
	case c.Tipo == 0:
		a.add("tipo_no_admitido", "No se sabe qué tipo de comprobante es.")
	case !ok:
		a.add("tipo_no_admitido", "El código %d no está entre los que Factorum sabe leer.", c.Tipo)
	case tp.Letra == "E":
		a.add("tipo_no_admitido", "%s: es un comprobante de exportación, no una compra.", tp.Nombre)
	case c.Tipo == 49:
		a.add("tipo_no_admitido", "Bienes usados: va en la pestaña «Bienes usados» del Libro IVA Digital, con su propio par de archivos.")
	}

	switch f, err := time.Parse("2006-01-02", c.Fecha); {
	case c.Fecha == "":
		a.add("sin_fecha", "")
	case err != nil:
		a.add("sin_fecha", "La fecha %q no se entiende.", c.Fecha)
	case c.Fecha < desde || c.Fecha > hasta:
		a.add("fuera_del_periodo", "Es del %s.", f.Format("02/01/2006"))
	}

	cuit := arca.SoloDigitos(c.CUITEmisor)
	if !arca.CUITValido(cuit) {
		if cuit == "" {
			a.add("sin_cuit_emisor", "No tiene CUIT del emisor.")
		} else {
			a.add("sin_cuit_emisor", "El CUIT %s no es válido.", arca.FormatearCUIT(cuit))
		}
	}
	if c.PuntoVenta <= 0 || c.PuntoVenta > 99999 {
		a.add("sin_numero", "Punto de venta %d.", c.PuntoVenta)
	} else if c.PuntoVenta >= 9998 {
		a.add("punto_venta_alto", "Punto de venta %05d.", c.PuntoVenta)
	}
	if c.Numero <= 0 {
		a.add("sin_numero", "Sin número de comprobante.")
	}
	if c.Total <= 0 {
		if c.Total < 0 {
			a.add("importe_negativo", "Total %s.", c.Total.AR())
		} else {
			a.add("sin_total", "")
		}
	}
	for _, v := range []modelo.Importe{c.NetoGravado, c.NoGravado, c.Exento, c.IVA27, c.IVA21, c.IVA105, c.IVA5, c.IVA25, c.OtrosTributos} {
		if v < 0 {
			a.add("importe_negativo", "Tiene importes negativos.")
		}
	}
	if strings.TrimSpace(Latin1(c.RazonSocialEmisor)) == "" {
		a.add("sin_razon_social", "")
	}

	moneda, okMon := CodigoMoneda(c.Moneda)
	cot := 1.0
	if !okMon {
		a.add("moneda_desconocida", "Moneda %q.", c.Moneda)
	} else if moneda != "PES" {
		cot = c.Cotizacion
		if _, err := tipoCambio(cot); err != nil || cot == 1 {
			a.add("moneda_sin_cotizacion", "Moneda %s, cotización %v.", moneda, c.Cotizacion)
		} else {
			a.add("moneda_extranjera", "Moneda %s, cotización %s.", moneda, strconv.FormatFloat(cot, 'f', -1, 64))
		}
	}

	discrimina := ok && (tp.Letra == "A" || tp.Letra == "M")
	r := &Registro{ID: e.ID, Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), PuntoVenta: c.PuntoVenta, Numero: c.Numero, Fecha: c.Fecha,
		CUITEmisor: cuit, Emisor: c.RazonSocialEmisor, Moneda: moneda, Cotizacion: cot, Discrimina: discrimina,
		NotaCredito: ok && tp.Clase == arca.ClaseNotaCredito, Total: c.Total, OtrosTributos: c.OtrosTributos,
		CodigoOperacion: " ", Estado: string(e.Estado), Archivo: e.Archivo}
	a.reg = r
	tol := tolerancia(c)

	if discrimina {
		r.NoGravado, r.Exento = c.NoGravado, c.Exento
		iva := c.IVATotal()
		r.IVA, r.CreditoFiscal = iva, iva
		var con []tasa
		for _, t := range tasas {
			if t.iva(c) != 0 {
				con = append(con, t)
			}
		}
		switch {
		case c.NetoGravado > 0 && iva == 0:
			a.add("neto_sin_iva", "Neto gravado %s sin IVA.", c.NetoGravado.AR())
		case iva > 0 && c.NetoGravado == 0:
			a.add("iva_sin_neto", "IVA %s sin neto gravado.", iva.AR())
		case len(con) == 0:
			if c.Exento == 0 && c.NoGravado == 0 {
				a.add("sin_neto", "")
			}
			// Sin IVA: un registro con alícuota 0 % y neto 0, y el código de operación (campo 20).
			r.Alicuotas = []Alicuota{{Codigo: AlicuotaCero, Tasa: "0"}}
			if c.Exento > 0 {
				r.CodigoOperacion = "E"
			} else {
				r.CodigoOperacion = "N"
			}
		case len(con) == 1:
			t := con[0]
			r.Alicuotas = []Alicuota{{Codigo: t.codigo, Tasa: t.nombre, Neto: c.NetoGravado, IVA: t.iva(c)}}
			if d := abs(modelo.Importe((int64(c.NetoGravado)*t.bp*2+10000)/20000) - t.iva(c)); d > 1 {
				a.add("iva_no_cierra", "IVA %s %% de %s es %s y dice %s.", t.nombre, c.NetoGravado.AR(),
					modelo.Importe((int64(c.NetoGravado)*t.bp*2+10000)/20000).AR(), t.iva(c).AR())
			}
		default:
			var suma, margen modelo.Importe
			mayor := 0
			for i, t := range con {
				n := netoDesdeIVA(t.iva(c), t.bp)
				r.Alicuotas = append(r.Alicuotas, Alicuota{Codigo: t.codigo, Tasa: t.nombre, Neto: n, IVA: t.iva(c)})
				suma += n
				margen += tolNeto(t.bp)
				if n > r.Alicuotas[mayor].Neto {
					mayor = i
				}
			}
			resto := c.NetoGravado - suma
			if abs(resto) > margen {
				a.add("alicuotas_no_cierran", "Neto gravado %s; desde el IVA de cada alícuota da %s (diferencia %s).", c.NetoGravado.AR(), suma.AR(), resto.AR())
			} else {
				r.Alicuotas[mayor].Neto += resto
				var p []string
				for _, x := range r.Alicuotas {
					p = append(p, x.Tasa+" %: "+x.Neto.AR())
				}
				a.add("neto_calculado", "%s.", strings.Join(p, "; "))
			}
		}
		if c.Total > 0 {
			suma := c.NetoGravado + c.NoGravado + c.Exento + iva + c.OtrosTributos
			if d := c.Total - suma; d != 0 {
				if abs(d) <= tol {
					a.add("redondeo", "La suma da %s y el total es %s.", suma.AR(), c.Total.AR())
				} else {
					a.add("importes_no_cierran", "La suma da %s y el total es %s (diferencia %s).", suma.AR(), c.Total.AR(), d.AR())
				}
			}
		}
	} else if ok {
		a.add("sin_credito_fiscal", "")
		if c.Subtotal > 0 && abs(c.Subtotal+c.OtrosTributos-c.Total) > tol {
			a.add("suma_b_c", "Subtotal %s + otros tributos %s y el total es %s.", c.Subtotal.AR(), c.OtrosTributos.AR(), c.Total.AR())
		}
	}
	if c.OtrosTributos > 0 {
		a.add("otros_tributos", "Otros tributos %s.", c.OtrosTributos.AR())
	}

	if rec := arca.SoloDigitos(c.DocNroReceptor); len(rec) == 11 && o.Cliente != "" && rec != o.Cliente {
		if discrimina {
			a.add("otro_receptor_a", "Es de %s.", arca.FormatearCUIT(rec))
		} else {
			a.add("otro_receptor", "Es de %s.", arca.FormatearCUIT(rec))
		}
	}
	switch e.Constatacion {
	case "apocrifo":
		a.add("apocrifo", "Lo dijo la constatación.")
	case "invalido":
		a.add("invalido", "")
	}
	if k := o.Conciliacion; k != nil {
		if k.Alarma[e.ID] {
			a.add("apocrifo", "Lo dice Mis Comprobantes.")
		}
		if n, ok := k.NoFigura[e.ID]; ok {
			a.add("no_figura_arca", "%s", n)
		}
		if n, ok := k.Diferencias[e.ID]; ok {
			a.add("diferencia_arca", "%s", n)
		}
	}

	if !a.bloqueado {
		if err := r.renderizar(); err != nil {
			a.add("no_entra", "%s.", err)
		}
	}
	return a
}

// clave identifica un comprobante: emisor, tipo, punto de venta y número.
func clave(c *modelo.Comprobante) string {
	cuit := arca.SoloDigitos(c.CUITEmisor)
	if cuit == "" || c.Tipo == 0 || c.Numero == 0 {
		return ""
	}
	return fmt.Sprintf("%s-%d-%d-%d", cuit, c.Tipo, c.PuntoVenta, c.Numero)
}

// prioridad para elegir qué copia de un duplicado va: la aprobada, la que está bien, la más vieja.
func prioridad(a *analisis) int {
	switch a.e.Estado {
	case modelo.EstadoAprobado:
		return 0
	case modelo.EstadoOK:
		return 1
	case modelo.EstadoRevisar:
		return 2
	}
	return 3
}

// Generar arma el libro de compras del período con los comprobantes del cliente. Los que no son
// del período (o no tienen fecha) se informan como problema y no van.
func Generar(es []Entrada, o Opciones) (*Resultado, error) {
	o.Cliente = arca.SoloDigitos(o.Cliente)
	if len(o.Cliente) != 11 {
		return nil, fmt.Errorf("el CUIT del cliente tiene que tener 11 dígitos")
	}
	desde, hasta, err := ValidarPeriodo(o.Periodo)
	if err != nil {
		return nil, err
	}
	res := &Resultado{Cliente: o.Cliente, Periodo: desde[:7], Desde: desde, Hasta: hasta, SoloAprobados: o.SoloAprobados,
		Leidos: len(es), Registros: []Registro{}, Grupos: []Grupo{}}
	as := make([]*analisis, len(es))
	for i := range es {
		as[i] = o.analizar(&es[i], desde, hasta)
	}

	// Duplicados: mismo emisor, tipo, punto de venta y número. Va uno solo.
	porClave := map[string][]*analisis{}
	for _, a := range as {
		if k := clave(&a.e.C); k != "" && !a.bloqueado {
			porClave[k] = append(porClave[k], a)
		}
	}
	for _, g := range porClave {
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool {
			if p, q := prioridad(g[i]), prioridad(g[j]); p != q {
				return p < q
			}
			return g[i].e.ID < g[j].e.ID
		})
		for _, a := range g[1:] {
			// La copia que sobra solo se informa como duplicada: lo demás ya se ve en la que va.
			a.probs = nil
			a.add("duplicado", "Es el mismo que el %d, que es el que va.", g[0].e.ID)
		}
	}

	for _, a := range as {
		if !a.bloqueado && !a.listo() && (a.e.Estado == modelo.EstadoRevisar || a.e.Estado == modelo.EstadoError) {
			a.add("sin_aprobar", "Está %s.", map[modelo.Estado]string{modelo.EstadoRevisar: "para revisar", modelo.EstadoError: "con errores"}[a.e.Estado])
		}
	}

	var incluidos []*analisis
	excluido := map[*analisis]bool{}
	for _, a := range as {
		if !a.bloqueado {
			res.CantidadTodos++
			if a.listo() {
				res.CantidadListos++
			}
		}
		if a.bloqueado || (o.SoloAprobados && !a.listo()) {
			excluido[a] = true
			res.Excluidos++
			continue
		}
		incluidos = append(incluidos, a)
	}
	sort.SliceStable(incluidos, func(i, j int) bool {
		x, y := incluidos[i].reg, incluidos[j].reg
		if x.Fecha != y.Fecha {
			return x.Fecha < y.Fecha
		}
		if x.Tipo != y.Tipo {
			return x.Tipo < y.Tipo
		}
		if x.PuntoVenta != y.PuntoVenta {
			return x.PuntoVenta < y.PuntoVenta
		}
		if x.Numero != y.Numero {
			return x.Numero < y.Numero
		}
		return x.ID < y.ID
	})

	var cb, al strings.Builder
	for _, a := range incluidos {
		r := a.reg
		cb.WriteString(r.lineaCbte + FinDeLinea)
		for _, l := range r.lineasAlicuota {
			al.WriteString(l + FinDeLinea)
		}
		res.Registros = append(res.Registros, *r)
	}
	res.Comprobantes, res.Alicuotas = []byte(cb.String()), []byte(al.String())
	res.Resumen = resumir(res.Registros)
	res.Grupos = agrupar(as, excluido)
	return res, nil
}

func agrupar(as []*analisis, excluido map[*analisis]bool) []Grupo {
	idx := map[string]*Grupo{}
	for _, a := range as {
		c := &a.e.C
		for _, p := range a.probs {
			g := idx[p.Codigo]
			if g == nil {
				d := problemas[p.Codigo]
				g = &Grupo{Codigo: p.Codigo, Nivel: d.nivel, Titulo: d.titulo, Ayuda: d.ayuda}
				idx[p.Codigo] = g
			}
			g.Items = append(g.Items, Item{ID: a.e.ID, Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), PuntoVenta: c.PuntoVenta, Numero: c.Numero,
				Fecha: c.Fecha, CUITEmisor: arca.SoloDigitos(c.CUITEmisor), Emisor: c.RazonSocialEmisor, Total: c.Total, Moneda: a.reg.Moneda,
				Estado: string(a.e.Estado), Detalle: p.Detalle, Excluido: excluido[a]})
		}
	}
	res := []Grupo{}
	for _, nivel := range []string{NivelBloquea, NivelFuerte, NivelAdvertencia, NivelInfo} {
		for _, k := range codigosProblema {
			if g := idx[k]; g != nil && g.Nivel == nivel {
				sort.SliceStable(g.Items, func(i, j int) bool {
					if g.Items[i].Fecha != g.Items[j].Fecha {
						return g.Items[i].Fecha < g.Items[j].Fecha
					}
					return g.Items[i].ID < g.Items[j].ID
				})
				res = append(res, *g)
			}
		}
	}
	return res
}

func resumir(rs []Registro) Resumen {
	s := Resumen{PorTipo: map[string]int{}, PorAlicuota: []TotalAlicuota{}, Extranjera: []TotalMoneda{}}
	porAli := map[string]*TotalAlicuota{}
	porMon := map[string]*TotalMoneda{}
	for i := range rs {
		r := &rs[i]
		s.Comprobantes++
		s.LineasAlicuotas += len(r.Alicuotas)
		s.PorTipo[r.NombreTipo]++
		if r.NotaCredito {
			s.NotasCredito++
		}
		if !r.Discrimina {
			s.SinCreditoFiscal++
		}
		sg := modelo.Importe(1)
		if r.NotaCredito {
			sg = -1
		}
		if r.Moneda != "PES" {
			m := porMon[r.Moneda]
			if m == nil {
				m = &TotalMoneda{Moneda: r.Moneda}
				porMon[r.Moneda] = m
			}
			m.Cantidad++
			m.IVA += sg * r.IVA
			m.CreditoFiscal += sg * r.CreditoFiscal
			m.Total += sg * r.Total
			continue
		}
		for _, a := range r.Alicuotas {
			t := porAli[a.Codigo]
			if t == nil {
				t = &TotalAlicuota{Codigo: a.Codigo, Tasa: a.Tasa}
				porAli[a.Codigo] = t
			}
			t.Lineas++
			t.Neto += sg * a.Neto
			t.IVA += sg * a.IVA
		}
		s.NetoGravado += sg * r.NetoGravado()
		s.NoGravado += sg * r.NoGravado
		s.Exento += sg * r.Exento
		s.IVA += sg * r.IVA
		s.CreditoFiscal += sg * r.CreditoFiscal
		s.OtrosTributos += sg * r.OtrosTributos
		s.Total += sg * r.Total
	}
	for _, c := range append([]string{AlicuotaCero}, codigosTasa()...) {
		if t := porAli[c]; t != nil {
			s.PorAlicuota = append(s.PorAlicuota, *t)
		}
	}
	var ms []string
	for k := range porMon {
		ms = append(ms, k)
	}
	sort.Strings(ms)
	for _, k := range ms {
		s.Extranjera = append(s.Extranjera, *porMon[k])
	}
	return s
}

func codigosTasa() []string {
	var r []string
	for _, t := range tasas {
		r = append(r, t.codigo)
	}
	return r
}

// renderizar escribe el registro de comprobantes y los de alícuotas según el diseño de ARCA.
func (r *Registro) renderizar() error {
	if len(r.Alicuotas) > 9 {
		return fmt.Errorf("más de 9 alícuotas")
	}
	ceros := func(v modelo.Importe) string { s, _ := importe(v); return s }
	campos := map[string]string{}
	var errs []string
	num := func(nombre string, v int64, n int) string {
		s, err := numero(v, n)
		if err != nil {
			errs = append(errs, nombre+": "+err.Error())
			return strings.Repeat("0", n)
		}
		return s
	}
	imp := func(nombre string, v modelo.Importe) string {
		s, err := importe(v)
		if err != nil {
			errs = append(errs, nombre+": "+err.Error())
			return strings.Repeat("0", 15)
		}
		return s
	}
	tc := "0001000000"
	if r.Moneda != "PES" {
		s, err := tipoCambio(r.Cotizacion)
		if err != nil {
			errs = append(errs, "tipo de cambio: "+err.Error())
		} else {
			tc = s
		}
	}
	cuit, _ := strconv.ParseInt(r.CUITEmisor, 10, 64)
	campos["tipo"] = num("tipo", int64(r.Tipo), 3)
	campos["pv"] = num("punto de venta", int64(r.PuntoVenta), 5)
	campos["numero"] = num("número", r.Numero, 20)
	campos["doc"] = num("CUIT", cuit, 20)
	valores := []string{
		strings.ReplaceAll(r.Fecha, "-", ""),
		campos["tipo"],
		campos["pv"],
		campos["numero"],
		strings.Repeat("0", 16), // despacho de importación: ceros (Especificaciones, campo 5)
		"80",                    // CUIT
		campos["doc"],
		alfa(r.Emisor, 30),
		imp("total", r.Total),
		imp("no gravado", r.NoGravado),
		imp("exento", r.Exento),
		ceros(0), // percepciones de IVA: Factorum no las separa (ver otros tributos)
		ceros(0), // percepciones de otros impuestos nacionales
		ceros(0), // percepciones de Ingresos Brutos
		ceros(0), // percepciones municipales
		ceros(0), // impuestos internos
		alfa(r.Moneda, 3),
		tc,
		strconv.Itoa(len(r.Alicuotas)),
		r.CodigoOperacion,
		imp("crédito fiscal", r.CreditoFiscal),
		imp("otros tributos", r.OtrosTributos),
		strings.Repeat("0", 11), // CUIT emisor/corredor: solo para 033, 058, 059, 060 y 063
		strings.Repeat(" ", 30), // denominación del emisor/corredor
		ceros(0),                // IVA comisión
	}
	var vAlis [][]string
	for _, a := range r.Alicuotas {
		vAlis = append(vAlis, []string{campos["tipo"], campos["pv"], campos["numero"], "80", campos["doc"],
			imp("neto gravado", a.Neto), a.Codigo, imp("IVA", a.IVA)})
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	l, err := armar(CamposCbte, valores)
	if err != nil {
		return err
	}
	var alis []string
	for _, v := range vAlis {
		x, err := armar(CamposAlicuota, v)
		if err != nil {
			return err
		}
		alis = append(alis, x)
	}
	r.lineaCbte, r.lineasAlicuota = l, alis
	return nil
}
