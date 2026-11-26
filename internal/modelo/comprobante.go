package modelo

import "strconv"

// Origen dice de dónde salió un dato. Es lo que permite contestar "¿de dónde sacaste este número?".
type Origen string

const (
	OrigenQR      Origen = "qr"      // código QR de ARCA (RG 4892): el dato oficial
	OrigenTexto   Origen = "texto"   // capa de texto del PDF
	OrigenOCR     Origen = "ocr"     // OCR clásico (Tesseract) sobre la imagen
	OrigenVision  Origen = "vision"  // modelo de visión (Ollama u otro compatible con OpenAI)
	OrigenManual  Origen = "manual"  // corrección de una persona
	OrigenArcanum Origen = "arcanum" // padrón o constatación de ARCA vía Arcanum
	OrigenCalculo Origen = "calculo" // deducido de otros campos (por ejemplo, la letra desde el tipo)
)

// Comprobante es una factura, nota de débito o nota de crédito leída de un documento.
// Fechas en ISO (AAAA-MM-DD). CUITs y documentos, solo dígitos.
type Comprobante struct {
	Tipo       int    `json:"tipo"`        // código ARCA (1 = Factura A, 6 = Factura B, 11 = Factura C...)
	Letra      string `json:"letra"`       // A, B, C, M, E
	PuntoVenta int    `json:"punto_venta"` //
	Numero     int64  `json:"numero"`      //
	Fecha      string `json:"fecha"`       // fecha de emisión

	CUITEmisor         string `json:"cuit_emisor"`
	RazonSocialEmisor  string `json:"razon_social_emisor"`
	CondicionIVAEmisor string `json:"condicion_iva_emisor"`

	DocTipoReceptor     int    `json:"doc_tipo_receptor"` // 80 CUIT, 96 DNI, 99 consumidor final
	DocNroReceptor      string `json:"doc_nro_receptor"`
	RazonSocialReceptor string `json:"razon_social_receptor"`

	Moneda     string  `json:"moneda"` // PES, DOL...
	Cotizacion float64 `json:"cotizacion"`

	NetoGravado   Importe `json:"neto_gravado"`
	NoGravado     Importe `json:"no_gravado"`
	Exento        Importe `json:"exento"`
	IVA27         Importe `json:"iva_27"`
	IVA21         Importe `json:"iva_21"`
	IVA105        Importe `json:"iva_10_5"`
	IVA5          Importe `json:"iva_5"`
	IVA25         Importe `json:"iva_2_5"`
	OtrosTributos Importe `json:"otros_tributos"`
	Total         Importe `json:"total"`
	// En B y C el IVA no se discrimina: el papel trae un "Subtotal" (con IVA incluido en la B).
	// Va aparte para no hacerlo pasar por neto gravado.
	Subtotal     Importe `json:"subtotal"`
	IVAContenido Importe `json:"iva_contenido"` // Régimen de Transparencia Fiscal (Ley 27.743), solo informativo

	CAE        string `json:"cae"`
	VtoCAE     string `json:"vto_cae"`
	TipoCodAut string `json:"tipo_cod_aut"` // E = CAE, A = CAEA

	// Tickets de compra (tique y tique factura de controlador fiscal). EsTique sale del tipo; la hora
	// solo la imprimen los tiques. Rubro es el tipo de gasto (supermercado, combustible...): ver el
	// paquete rubro. Sus orígenes dicen de dónde salió: texto/ocr (palabras clave), arcanum
	// (actividad del padrón) o manual (corrección de una persona, que se recuerda por CUIT).
	EsTique bool   `json:"es_tique"`
	Hora    string `json:"hora,omitempty"` // HH:MM o HH:MM:SS
	Rubro   string `json:"rubro,omitempty"`

	// Origenes: campo (nombre JSON) -> de dónde salió.
	Origenes map[string]Origen `json:"origenes"`
	// Paginas del documento que corresponden a este comprobante (desde 1).
	Paginas []int `json:"paginas,omitempty"`
}

// IVATotal suma todas las alícuotas.
func (c *Comprobante) IVATotal() Importe {
	return c.IVA27 + c.IVA21 + c.IVA105 + c.IVA5 + c.IVA25
}

// SumaComponentes es lo que debería dar el total.
func (c *Comprobante) SumaComponentes() Importe {
	return c.NetoGravado + c.NoGravado + c.Exento + c.IVATotal() + c.OtrosTributos
}

// Marcar registra el origen de un campo.
func (c *Comprobante) Marcar(campo string, o Origen) {
	if c.Origenes == nil {
		c.Origenes = map[string]Origen{}
	}
	c.Origenes[campo] = o
}

// Origen de un campo ("" si nunca se cargó).
func (c *Comprobante) Origen(campo string) Origen {
	if c.Origenes == nil {
		return ""
	}
	return c.Origenes[campo]
}

// Clave identifica un comprobante en ARCA: emisor, tipo, punto de venta y número.
func (c *Comprobante) Clave() string {
	if c.CUITEmisor == "" || c.Tipo == 0 || c.Numero == 0 {
		return ""
	}
	return c.CUITEmisor + "-" + strconv.Itoa(c.Tipo) + "-" + strconv.Itoa(c.PuntoVenta) + "-" + strconv.FormatInt(c.Numero, 10)
}

// Nivel de un hallazgo.
type Nivel string

const (
	NivelError Nivel = "error" // no se puede usar sin revisar
	NivelAviso Nivel = "aviso" // conviene mirarlo
	NivelInfo  Nivel = "info"  // dato útil, no bloquea
)

// Hallazgo es el resultado de un control: "neto + IVA + otros no da el total".
type Hallazgo struct {
	Codigo  string `json:"codigo"`
	Nivel   Nivel  `json:"nivel"`
	Mensaje string `json:"mensaje"`
	Campo   string `json:"campo,omitempty"`
}

// Estado de un comprobante después de procesarlo.
type Estado string

const (
	EstadoOK       Estado = "ok"       // todo cierra y los datos clave vienen de fuentes exactas
	EstadoRevisar  Estado = "revisar"  // hay avisos, o datos que salieron de OCR o de IA
	EstadoError    Estado = "error"    // faltan datos clave o hay errores
	EstadoAprobado Estado = "aprobado" // una persona lo revisó
)

// Resultado de procesar un documento: puede traer más de un comprobante (un PDF con varias facturas).
type Resultado struct {
	Archivo      string             `json:"archivo"`
	SHA256       string             `json:"sha256"`
	Paginas      int                `json:"paginas"`
	Comprobantes []ResultadoCbte    `json:"comprobantes"`
	Extractores  []string           `json:"extractores"` // en el orden en que se usaron
	DuracionMS   int64              `json:"duracion_ms"`
	Error        string             `json:"error,omitempty"`
	Detalle      map[string]float64 `json:"detalle_ms,omitempty"` // tiempo por etapa
}

// ResultadoCbte es un comprobante con sus controles.
type ResultadoCbte struct {
	Comprobante Comprobante `json:"comprobante"`
	Hallazgos   []Hallazgo  `json:"hallazgos"`
	Estado      Estado      `json:"estado"`
	Confianza   float64     `json:"confianza"` // 0..1
}
