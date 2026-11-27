// Package libroiva arma los archivos de importación del Libro IVA Digital de ARCA (RG 4597 y
// modificatorias; desde noviembre de 2025, el «Módulo Registración Electrónica de Operaciones» de
// IVA Simple) para un cliente y un mes:
//
//   - compras: LIBRO_IVA_DIGITAL_COMPRAS_CBTE (325 posiciones por comprobante) y
//     LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS (84 por alícuota), con lo que Factorum leyó;
//   - ventas: LIBRO_IVA_DIGITAL_VENTAS_CBTE (266) y LIBRO_IVA_DIGITAL_VENTAS_ALICUOTAS (62), con los
//     emitidos de Mis Comprobantes que trae Arcanum;
//   - importaciones de bienes: los despachos (tipo 066) con el diseño de cabecera de compras (325,
//     con el número de despacho) y LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA (50).
//
// Cada uno trae los problemas que conviene resolver antes de importarlos y el resumen del período.
//
// Es puro: recibe los comprobantes ya leídos y no habla con la base ni con la red. Los importes van
// en centavos (modelo.Importe). El diseño de registro, sus fuentes oficiales y las decisiones en
// los casos dudosos están en docs/libro-iva-digital.md.
package libroiva

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Libros que se arman.
const (
	LibroCompras       = "compras"
	LibroVentas        = "ventas"
	LibroImportaciones = "importaciones"
)

// LibroValido dice si el nombre de un libro se entiende.
func LibroValido(l string) bool {
	return l == LibroCompras || l == LibroVentas || l == LibroImportaciones
}

// Nombres de los archivos dentro de cada ZIP: la «denominación del archivo» de cada diseño de
// registro, con .txt. La cabecera de importaciones usa el diseño de compras («Compras e
// Importación de Bienes - Cabecera») con otro nombre, para que no pise al de compras si se
// descomprimen los dos ZIP en la misma carpeta (ver docs/libro-iva-digital.md).
const (
	NombreCbte                  = "LIBRO_IVA_DIGITAL_COMPRAS_CBTE.txt"
	NombreAlicuotas             = "LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS.txt"
	NombreVentasCbte            = "LIBRO_IVA_DIGITAL_VENTAS_CBTE.txt"
	NombreVentasAlicuotas       = "LIBRO_IVA_DIGITAL_VENTAS_ALICUOTAS.txt"
	NombreImportacionesCbte     = "LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_CBTE.txt"
	NombreImportacionesAlicuota = "LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA.txt"
	NombreResumen               = "resumen.csv"
)

// Conciliacion es lo que dijo la conciliación con Mis Comprobantes, si se pudo hacer. Solo avisa:
// nunca saca un comprobante del archivo.
type Conciliacion struct {
	NoFigura    map[int64]string // no está en los recibidos de ARCA y no hay causa legítima: id -> nota
	Alarma      map[int64]bool   // ARCA marca al emisor como apócrifo
	Diferencias map[int64]string // está en ARCA con otros datos: id -> qué no cierra
}

// Opciones de la exportación.
type Opciones struct {
	Cliente string // CUIT del cliente (el que presenta el libro)
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
	"sin_cuit_emisor", "sin_fecha", "fuera_del_periodo", "tipo_no_admitido", "sin_numero", "sin_despacho", "despacho_invalido",
	"sin_total", "importe_negativo", "moneda_desconocida", "moneda_sin_cotizacion", "neto_sin_iva", "iva_sin_neto", "sin_neto",
	"sin_desglose", "codigo_operacion", "importes_no_cierran", "alicuotas_no_cierran", "detalle_tributos", "no_entra", "duplicado",
	"apocrifo", "invalido", "otro_receptor_a",
	"sin_aprobar", "no_figura_arca", "diferencia_arca", "otros_tributos", "otros_tributos_deducidos", "otro_receptor", "redondeo",
	"iva_no_cierra", "suma_b_c", "sin_razon_social", "doc_comprador", "consumidor_final_identificar", "desglose_distinto",
	"exportacion", "despacho_forma", "punto_venta_alto",
	"de_mes_anterior", "moneda_extranjera", "neto_calculado", "alicuota_deducida", "desglose_arcanum", "sin_credito_fiscal",
}

var problemas = map[string]defProblema{
	"sin_cuit_emisor":              {NivelBloquea, "Sin CUIT válido del emisor", "El registro exige el CUIT del vendedor (código 80). Corregilo en Revisar."},
	"sin_fecha":                    {NivelBloquea, "Sin fecha", "Sin fecha de emisión no se puede ubicar en un período. Completala en Revisar."},
	"fuera_del_periodo":            {NivelBloquea, "Fuera del período", "No es de este mes ni se pasó a este período."},
	"tipo_no_admitido":             {NivelBloquea, "Tipo de comprobante que no va", "El tipo no se reconoce o no va en este libro."},
	"sin_numero":                   {NivelBloquea, "Sin punto de venta o número", "El registro exige el punto de venta y el número."},
	"sin_despacho":                 {NivelBloquea, "Sin número de despacho", "Un despacho de importación va con su número (16 caracteres). Cargalo en Revisar."},
	"despacho_invalido":            {NivelBloquea, "Número de despacho inválido", "El diseño de ARCA pide 16 caracteres, letras y números. Corregilo en Revisar."},
	"sin_total":                    {NivelBloquea, "Sin total", "El total es cero."},
	"importe_negativo":             {NivelBloquea, "Importes negativos", "Las notas de crédito van con importes positivos: el tipo de comprobante dice que restan."},
	"moneda_desconocida":           {NivelBloquea, "Moneda desconocida", "La moneda no está en la tabla de monedas de ARCA."},
	"moneda_sin_cotizacion":        {NivelBloquea, "Moneda extranjera sin cotización", "El registro exige el tipo de cambio. Cargalo en Revisar (campo Cotización)."},
	"neto_sin_iva":                 {NivelBloquea, "Neto gravado sin IVA", "Un comprobante que discrimina IVA con neto gravado tiene que traer el IVA. Si es exento o no gravado, pasá el importe a ese campo."},
	"iva_sin_neto":                 {NivelBloquea, "IVA sin neto gravado", "Falta el neto gravado."},
	"sin_neto":                     {NivelBloquea, "Sin neto, no gravado ni exento", "Un comprobante A o M necesita al menos uno de los tres."},
	"sin_desglose":                 {NivelBloquea, "Sin detalle por alícuota", "El libro de ventas pide el neto y el IVA de cada alícuota. Mis Comprobantes no los trajo, Arcanum no emitió este comprobante y no se pueden deducir del neto y el IVA totales. Volvé a sincronizar los emitidos en Arcanum o cargalo a mano en el Libro IVA Digital."},
	"codigo_operacion":             {NivelBloquea, "Falta el código de operación", "Con alícuota 0 % el registro pide el código de operación (exento, no gravado, exportación...) y no hay con qué saber cuál es."},
	"importes_no_cierran":          {NivelBloquea, "Los importes no cierran", "Neto + no gravado + exento + IVA + otros tributos tiene que dar el total: ARCA lo controla."},
	"alicuotas_no_cierran":         {NivelBloquea, "El neto no se puede repartir entre las alícuotas", "El neto y el IVA de cada alícuota no dan el neto gravado y el IVA del comprobante. Revisá el neto y el IVA de cada alícuota."},
	"detalle_tributos":             {NivelBloquea, "Las percepciones suman más que los otros tributos", "Los otros tributos son el total y las percepciones e impuestos internos, su detalle. Corregilo en Revisar."},
	"no_entra":                     {NivelBloquea, "Un dato no entra en el registro", "Un importe o un número es más largo de lo que admite el diseño de ARCA."},
	"duplicado":                    {NivelBloquea, "Cargado más de una vez", "Va una sola vez: estos son las copias que sobran. Borralas o dejalas, no cambian el archivo."},
	"apocrifo":                     {NivelFuerte, "Emisor apócrifo", "ARCA marca al emisor como apócrifo: su crédito fiscal no se computa. No lo exportes salvo que sepas lo que hacés."},
	"invalido":                     {NivelFuerte, "ARCA no reconoce el comprobante", "La constatación dio que no existe con esos datos. Revisalo antes de computarlo."},
	"otro_receptor_a":              {NivelFuerte, "A nombre de otro CUIT", "El comprobante A es de otro comprador: su crédito fiscal no es de este cliente."},
	"sin_aprobar":                  {NivelAdvertencia, "Sin aprobar", "Está «para revisar» o «con errores» y nadie lo aprobó. Con «Solo los listos» queda afuera."},
	"no_figura_arca":               {NivelAdvertencia, "No figura en ARCA", "Según la conciliación, no está en los recibidos de Mis Comprobantes y no hay una causa legítima. Conviene constatarlo."},
	"diferencia_arca":              {NivelAdvertencia, "Distinto en ARCA", "Según la conciliación, ARCA lo tiene con otros datos."},
	"otros_tributos":               {NivelAdvertencia, "Otros tributos sin detalle", "Lo que no está separado va en el campo «Otros tributos». Si son percepciones de IVA, de Ganancias, de Ingresos Brutos o impuestos internos, separalas en Revisar (o pasalas a su campo en el Libro IVA Digital): la percepción de IVA solo se computa en el suyo."},
	"otros_tributos_deducidos":     {NivelAdvertencia, "Otros tributos deducidos", "Mis Comprobantes no trajo los otros tributos: son la diferencia entre el total y el neto, el no gravado, el exento y el IVA. Revisalos en el Libro IVA Digital (sobre todo las percepciones)."},
	"otro_receptor":                {NivelAdvertencia, "A nombre de otro CUIT", "El comprobante es de otro comprador. Revisá si va en el libro de este cliente."},
	"redondeo":                     {NivelAdvertencia, "Diferencia de redondeo", "La suma de los importes difiere del total en centavos. ARCA puede observarla al importar."},
	"iva_no_cierra":                {NivelAdvertencia, "El IVA no da con el neto", "El IVA no es la alícuota aplicada al neto gravado. Revisalo: el crédito fiscal sale del IVA."},
	"suma_b_c":                     {NivelAdvertencia, "El subtotal no da el total", "En un comprobante B o C solo se informa el total; revisalo igual."},
	"sin_razon_social":             {NivelAdvertencia, "Sin nombre de la contraparte", "El campo va en blanco. Conviene completarlo."},
	"doc_comprador":                {NivelAdvertencia, "Documento del comprador raro", "El número no es un CUIT, CUIL o DNI válido según su tipo. Va como lo tiene ARCA; revisalo."},
	"consumidor_final_identificar": {NivelAdvertencia, "Consumidor final sin identificar", "Desde la RG 5700/2025, una venta a consumidor final de $ 10.000.000 o más tiene que identificar al comprador. Este comprobante va con el código 99 (sin identificar)."},
	"desglose_distinto":            {NivelAdvertencia, "El detalle de ARCA y el de Arcanum no coinciden", "Mis Comprobantes y lo que Arcanum emitió por WSFEv1 tienen otro neto o IVA por alícuota. Va el de Mis Comprobantes; revisalo."},
	"exportacion":                  {NivelAdvertencia, "Exportación", "Va con alícuota 0 % y código de operación X (exportación al exterior). Si Mis Comprobantes trae solo el total, va solo el total (sin repartirlo en exento o no gravado): revisalo al importar. Si es a zona franca, cambiá el código a Z en el Libro IVA Digital."},
	"despacho_forma":               {NivelAdvertencia, "Número de despacho con otra forma", "Tiene 16 caracteres pero no la forma usual (año, aduana, tipo de destinación, número y control, como 26001IC04123456K). Revisalo."},
	"punto_venta_alto":             {NivelAdvertencia, "Punto de venta 9998 o mayor", "Las especificaciones de ARCA piden un punto de venta menor a 9998. Revisá que esté bien leído."},
	"de_mes_anterior":              {NivelInfo, "Entró de un mes anterior", "Va en este período aunque su fecha sea de antes: llegó después de presentar su mes, o alguien eligió este período en Revisar. En el registro va con su fecha."},
	"moneda_extranjera":            {NivelInfo, "En moneda extranjera", "Va en la moneda original con su cotización. Al importar, elegí «Los importes están expresados en la Moneda Original del Comprobante»."},
	"neto_calculado":               {NivelInfo, "Neto por alícuota calculado", "Factorum guarda un solo neto gravado: el de cada alícuota sale de su IVA y la diferencia de redondeo va a la mayor."},
	"alicuota_deducida":            {NivelInfo, "Alícuota deducida", "Mis Comprobantes no trajo el detalle por alícuota: el IVA es justo una alícuota del neto, así que va como una sola alícuota."},
	"desglose_arcanum":             {NivelInfo, "Detalle de la emisión de Arcanum", "Mis Comprobantes no trajo el detalle por alícuota: va el de lo que Arcanum emitió por WSFEv1 (el mismo comprobante, con el mismo total)."},
	"sin_credito_fiscal":           {NivelInfo, "Sin crédito fiscal", "B, C y tiques no discriminan el IVA: van con cantidad de alícuotas 0, sin registros de alícuotas y crédito fiscal 0."},
}

// Item es un comprobante dentro de un grupo de problemas.
type Item struct {
	ID          int64          `json:"id,omitempty"` // el de Factorum (0 en ventas: vienen de ARCA)
	Clave       string         `json:"clave"`
	Tipo        int            `json:"tipo"`
	NombreTipo  string         `json:"nombre_tipo"`
	PuntoVenta  int            `json:"punto_venta"`
	Numero      int64          `json:"numero"`
	Despacho    string         `json:"despacho,omitempty"`
	Fecha       string         `json:"fecha"`
	CUITEmisor  string         `json:"cuit_emisor,omitempty"`
	Emisor      string         `json:"emisor,omitempty"`
	Doc         string         `json:"doc,omitempty"` // la contraparte: CUIT del emisor en compras, documento del comprador en ventas
	Contraparte string         `json:"contraparte"`
	Total       modelo.Importe `json:"total"`
	Moneda      string         `json:"moneda"`
	Estado      string         `json:"estado"`
	Detalle     string         `json:"detalle,omitempty"`
	Excluido    bool           `json:"excluido"` // con las opciones pedidas, no va al archivo
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
	ID          int64  `json:"id,omitempty"`
	Clave       string `json:"clave"` // identifica el comprobante entre exportaciones
	Tipo        int    `json:"tipo"`
	NombreTipo  string `json:"nombre_tipo"`
	PuntoVenta  int    `json:"punto_venta"`
	Numero      int64  `json:"numero"`
	NumeroHasta int64  `json:"numero_hasta,omitempty"` // ventas
	Despacho    string `json:"despacho,omitempty"`     // importaciones
	Fecha       string `json:"fecha"`
	// DeMesAnterior: la fecha es de un mes anterior al período (llegó tarde o se pasó a mano).
	DeMesAnterior bool `json:"de_mes_anterior,omitempty"`
	// CUITEmisor y Emisor: el vendedor en compras e importaciones (vacíos en ventas).
	CUITEmisor string `json:"cuit_emisor,omitempty"`
	Emisor     string `json:"emisor,omitempty"`
	// La contraparte: el vendedor en compras, el comprador en ventas.
	DocTipo     int            `json:"doc_tipo"`
	DocNro      string         `json:"doc_nro"`
	Contraparte string         `json:"contraparte"`
	Moneda      string         `json:"moneda"` // código de ARCA: PES, DOL, 060...
	Cotizacion  float64        `json:"cotizacion"`
	Discrimina  bool           `json:"discrimina_iva"`
	NotaCredito bool           `json:"nota_credito"`
	Alicuotas   []Alicuota     `json:"alicuotas"`
	NoGravado   modelo.Importe `json:"no_gravado"`
	Exento      modelo.Importe `json:"exento"`
	// Percepciones e impuestos internos separados (campos 12 a 16 de compras; 11 y 13 a 16 de ventas).
	PercepcionNoCategorizados modelo.Importe `json:"percepcion_no_categorizados,omitempty"`
	PercepcionIVA             modelo.Importe `json:"percepcion_iva,omitempty"`
	PercepcionNacionales      modelo.Importe `json:"percepcion_nacionales,omitempty"`
	PercepcionIIBB            modelo.Importe `json:"percepcion_iibb,omitempty"`
	PercepcionMunicipal       modelo.Importe `json:"percepcion_municipal,omitempty"`
	ImpuestosInternos         modelo.Importe `json:"impuestos_internos,omitempty"`
	// OtrosTributos es lo que va en el campo «Otros tributos»: lo que no está separado.
	OtrosTributos   modelo.Importe `json:"otros_tributos"`
	IVA             modelo.Importe `json:"iva"`
	CreditoFiscal   modelo.Importe `json:"credito_fiscal"`
	Total           modelo.Importe `json:"total"`
	CodigoOperacion string         `json:"codigo_operacion"`
	Estado          string         `json:"estado"`
	Archivo         string         `json:"archivo,omitempty"`

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

// Percepciones suma las percepciones e impuestos internos separados.
func (r *Registro) Percepciones() modelo.Importe {
	return r.PercepcionNoCategorizados + r.PercepcionIVA + r.PercepcionNacionales + r.PercepcionIIBB + r.PercepcionMunicipal + r.ImpuestosInternos
}

// Huella resume lo que se escribe del comprobante (su registro y sus alícuotas): si cambia la
// huella, cambió lo que va al archivo.
func (r *Registro) Huella() string {
	h := sha256.Sum256([]byte(r.lineaCbte + "\n" + strings.Join(r.lineasAlicuota, "\n")))
	return hex.EncodeToString(h[:12])
}

// Descripcion nombra el comprobante para una persona: «Factura A 00002-00001544, DISTRIBUIDORA SA».
func (r *Registro) Descripcion() string {
	return describir(r.NombreTipo, r.PuntoVenta, r.Numero, r.Despacho, r.Contraparte)
}

func describir(nombreTipo string, pv int, numero int64, despacho, contraparte string) string {
	id := fmt.Sprintf("%05d-%08d", pv, numero)
	if despacho != "" {
		id = despacho
	}
	s := nombreTipo + " " + id
	if contraparte != "" {
		s += ", " + contraparte
	}
	return s
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
// moneda extranjera va aparte, en su moneda. En ventas, el IVA es el débito fiscal y el crédito
// fiscal va en 0.
type Resumen struct {
	Comprobantes      int             `json:"comprobantes"`
	LineasAlicuotas   int             `json:"registros_alicuotas"`
	NotasCredito      int             `json:"notas_credito"`
	SinCreditoFiscal  int             `json:"sin_credito_fiscal"` // B, C y tiques (compras)
	DeMesesAnteriores int             `json:"de_meses_anteriores"`
	PorAlicuota       []TotalAlicuota `json:"por_alicuota"`
	NetoGravado       modelo.Importe  `json:"neto_gravado"`
	NoGravado         modelo.Importe  `json:"no_gravado"`
	Exento            modelo.Importe  `json:"exento"`
	IVA               modelo.Importe  `json:"iva"`
	CreditoFiscal     modelo.Importe  `json:"credito_fiscal"`
	Percepciones      modelo.Importe  `json:"percepciones"` // percepciones e impuestos internos separados
	OtrosTributos     modelo.Importe  `json:"otros_tributos"`
	Total             modelo.Importe  `json:"total"`
	Extranjera        []TotalMoneda   `json:"moneda_extranjera"`
	PorTipo           map[string]int  `json:"por_tipo"`
}

// Resultado de armar un libro de un período.
type Resultado struct {
	Libro         string   `json:"libro"`
	Cliente       string   `json:"cliente"`
	Periodo       string   `json:"periodo"`
	Desde         string   `json:"desde"`
	Hasta         string   `json:"hasta"`
	SoloAprobados bool     `json:"solo_aprobados"`
	Archivos      []string `json:"archivos"` // los dos .txt del ZIP, en orden: comprobantes y alícuotas

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

// tasaDe busca una alícuota por su código ("0005" o "5").
func tasaDe(codigo string) (tasa, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(codigo))
	if err != nil {
		return tasa{}, false
	}
	c := fmt.Sprintf("%04d", n)
	if c == AlicuotaCero {
		return tasa{codigo: AlicuotaCero, nombre: "0"}, true
	}
	for _, t := range tasas {
		if t.codigo == c {
			return t, true
		}
	}
	return tasa{}, false
}

// ValidarPeriodo controla AAAA-MM y devuelve el primer y el último día del mes.
func ValidarPeriodo(p string) (string, string, error) {
	t, err := time.Parse("2006-01", strings.TrimSpace(p))
	if err != nil || t.Year() < 2000 {
		return "", "", fmt.Errorf("elegí el período: el mes con el formato AAAA-MM")
	}
	return t.Format("2006-01-02"), t.AddDate(0, 1, -1).Format("2006-01-02"), nil
}

// analisis es un comprobante mirado para un libro: lo que se escribiría y sus problemas.
type analisis struct {
	id        int64
	estado    modelo.Estado
	clave     string // para los duplicados; "" si no se puede identificar
	base      Item   // datos para los grupos de problemas
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

func (a *analisis) tiene(codigo string) bool {
	for _, p := range a.probs {
		if p.Codigo == codigo {
			return true
		}
	}
	return false
}

// listo: se puede exportar con «Solo los listos».
func (a *analisis) listo() bool {
	return !a.bloqueado && !a.fuerte && (a.estado == modelo.EstadoOK || a.estado == modelo.EstadoAprobado)
}

func abs(v modelo.Importe) modelo.Importe {
	if v < 0 {
		return -v
	}
	return v
}

// tolerancia de redondeo para comparar sumas: un centavo por componente, como mínimo dos (la misma
// regla que los controles de Factorum).
func tolerancia(vs ...modelo.Importe) modelo.Importe {
	n := modelo.Importe(0)
	for _, v := range vs {
		if v != 0 {
			n++
		}
	}
	return max(n, 2)
}

func toleranciaCbte(c *modelo.Comprobante) modelo.Importe {
	return tolerancia(c.NetoGravado, c.NoGravado, c.Exento, c.IVA27, c.IVA21, c.IVA105, c.IVA5, c.IVA25, c.OtrosTributos)
}

// netoDesdeIVA redondea iva / tasa al centavo.
func netoDesdeIVA(iva modelo.Importe, bp int64) modelo.Importe {
	return modelo.Importe((int64(iva)*10000*2 + bp) / (2 * bp))
}

// ivaDe es la tasa aplicada al neto, redondeada al centavo.
func ivaDe(neto modelo.Importe, bp int64) modelo.Importe {
	return modelo.Importe((int64(neto)*bp*2 + 10000) / 20000)
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

// moneda controla la moneda y la cotización; devuelve el código de ARCA y la cotización a usar.
func (a *analisis) moneda(m string, cotizacion float64) (string, float64) {
	moneda, ok := CodigoMoneda(m)
	cot := 1.0
	if !ok {
		a.add("moneda_desconocida", "Moneda %q.", m)
	} else if moneda != "PES" {
		cot = cotizacion
		if _, err := tipoCambio(cot); err != nil || cot == 1 {
			a.add("moneda_sin_cotizacion", "Moneda %s, cotización %v.", moneda, cotizacion)
		} else {
			a.add("moneda_extranjera", "Moneda %s, cotización %s.", moneda, strconv.FormatFloat(cot, 'f', -1, 64))
		}
	}
	return moneda, cot
}

// periodo controla que el comprobante vaya en el período: el suyo (el mes de la fecha o el que se
// le asignó) tiene que ser el pedido. Si la fecha es de un mes anterior, lo avisa.
func (a *analisis) periodo(fecha, periodo, pedido string) {
	f, err := time.Parse("2006-01-02", fecha)
	switch {
	case fecha == "":
		a.add("sin_fecha", "")
		return
	case err != nil:
		a.add("sin_fecha", "La fecha %q no se entiende.", fecha)
		return
	}
	if periodo == "" {
		periodo = fecha[:7]
	}
	switch {
	case periodo != pedido:
		if periodo != fecha[:7] {
			a.add("fuera_del_periodo", "Es del %s y va en el libro de %s.", f.Format("02/01/2006"), NombreMes(periodo))
		} else {
			a.add("fuera_del_periodo", "Es del %s.", f.Format("02/01/2006"))
		}
	case fecha[:7] < pedido:
		a.add("de_mes_anterior", "Es del %s (%s).", f.Format("02/01/2006"), NombreMes(fecha[:7]))
		if a.reg != nil {
			a.reg.DeMesAnterior = true
		}
	case fecha[:7] > pedido:
		a.add("fuera_del_periodo", "Es del %s: un período del libro no puede ser anterior a la fecha del comprobante.", f.Format("02/01/2006"))
	}
}

// totales controla que los componentes den el total: un poco de diferencia es redondeo, más no va.
func (a *analisis) totales(total, suma, tol modelo.Importe) {
	if d := total - suma; d != 0 {
		if abs(d) <= tol {
			a.add("redondeo", "La suma da %s y el total es %s.", suma.AR(), total.AR())
		} else {
			a.add("importes_no_cierran", "La suma da %s y el total es %s (diferencia %s).", suma.AR(), total.AR(), d.AR())
		}
	}
}

// armar junta lo analizado en el resultado: duplicados (va uno solo: el que no tiene problemas que
// bloqueen, el aprobado, el que está bien, el primero), sin aprobar, qué va con las opciones, el
// orden del archivo, los archivos, el resumen y los grupos de problemas.
func armarResultado(res *Resultado, as []*analisis, o Opciones) {
	porClave := map[string][]*analisis{}
	var claves []string
	for _, a := range as {
		if a.clave != "" {
			if _, ok := porClave[a.clave]; !ok {
				claves = append(claves, a.clave)
			}
			porClave[a.clave] = append(porClave[a.clave], a)
		}
	}
	for _, k := range claves {
		g := porClave[k]
		if len(g) < 2 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool {
			if g[i].bloqueado != g[j].bloqueado {
				return !g[i].bloqueado
			}
			if p, q := prioridad(g[i]), prioridad(g[j]); p != q {
				return p < q
			}
			return g[i].id < g[j].id
		})
		for _, a := range g[1:] {
			// La copia que sobra solo se informa como duplicada: lo demás ya se ve en la otra.
			a.probs, a.fuerte = nil, false
			ref := g[0].base.Descripcion()
			if g[0].id > 0 {
				ref = "el " + strconv.FormatInt(g[0].id, 10)
			}
			if g[0].bloqueado {
				a.add("duplicado", "Es el mismo que %s.", ref)
			} else {
				a.add("duplicado", "Es el mismo que %s, que es el que va.", ref)
			}
		}
	}

	for _, a := range as {
		if !a.bloqueado && !a.listo() && (a.estado == modelo.EstadoRevisar || a.estado == modelo.EstadoError) {
			a.add("sin_aprobar", "Está %s.", map[modelo.Estado]string{modelo.EstadoRevisar: "para revisar", modelo.EstadoError: "con errores"}[a.estado])
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
		if x.Despacho != y.Despacho {
			return x.Despacho < y.Despacho
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
}

// Descripcion de un ítem, como la del registro.
func (it Item) Descripcion() string {
	return describir(it.NombreTipo, it.PuntoVenta, it.Numero, it.Despacho, it.Contraparte)
}

// prioridad para elegir qué copia de un duplicado va: la aprobada, la que está bien, la más vieja.
func prioridad(a *analisis) int {
	switch a.estado {
	case modelo.EstadoAprobado:
		return 0
	case modelo.EstadoOK:
		return 1
	case modelo.EstadoRevisar:
		return 2
	}
	return 3
}

func agrupar(as []*analisis, excluido map[*analisis]bool) []Grupo {
	idx := map[string]*Grupo{}
	for _, a := range as {
		for _, p := range a.probs {
			g := idx[p.Codigo]
			if g == nil {
				d := problemas[p.Codigo]
				g = &Grupo{Codigo: p.Codigo, Nivel: d.nivel, Titulo: d.titulo, Ayuda: d.ayuda}
				idx[p.Codigo] = g
			}
			it := a.base
			it.Detalle, it.Excluido = p.Detalle, excluido[a]
			if a.reg != nil {
				it.Moneda = a.reg.Moneda
			}
			g.Items = append(g.Items, it)
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
					if g.Items[i].ID != g.Items[j].ID {
						return g.Items[i].ID < g.Items[j].ID
					}
					return g.Items[i].Clave < g.Items[j].Clave
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
		if r.DeMesAnterior {
			s.DeMesesAnteriores++
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
		s.Percepciones += sg * r.Percepciones()
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

// escritor junta los valores de un registro y anota lo que no entra.
type escritor struct{ errs []string }

func (w *escritor) num(nombre string, v int64, n int) string {
	s, err := numero(v, n)
	if err != nil {
		w.errs = append(w.errs, nombre+": "+err.Error())
		return strings.Repeat("0", n)
	}
	return s
}

func (w *escritor) imp(nombre string, v modelo.Importe) string {
	s, err := importe(v)
	if err != nil {
		w.errs = append(w.errs, nombre+": "+err.Error())
		return strings.Repeat("0", 15)
	}
	return s
}

func (w *escritor) tc(moneda string, cot float64) string {
	if moneda == "PES" {
		return "0001000000"
	}
	s, err := tipoCambio(cot)
	if err != nil {
		w.errs = append(w.errs, "tipo de cambio: "+err.Error())
		return "0001000000"
	}
	return s
}

func (w *escritor) err() error {
	if len(w.errs) > 0 {
		return fmt.Errorf("%s", strings.Join(w.errs, "; "))
	}
	return nil
}
