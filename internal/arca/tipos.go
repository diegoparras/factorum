package arca

// Clase de comprobante: define el signo en un libro de IVA.
type Clase string

const (
	ClaseFactura     Clase = "factura"
	ClaseNotaDebito  Clase = "nota_debito"
	ClaseNotaCredito Clase = "nota_credito"
	ClaseRecibo      Clase = "recibo"
	ClaseOtro        Clase = "otro"
)

// TipoCbte describe un código de comprobante de ARCA (tabla de tipos de WSFEv1).
type TipoCbte struct {
	Codigo int
	Letra  string
	Clase  Clase
	Nombre string
	FCE    bool // Factura de Crédito Electrónica MiPyME
}

var tipos = map[int]TipoCbte{
	1:   {1, "A", ClaseFactura, "Factura A", false},
	2:   {2, "A", ClaseNotaDebito, "Nota de Débito A", false},
	3:   {3, "A", ClaseNotaCredito, "Nota de Crédito A", false},
	4:   {4, "A", ClaseRecibo, "Recibo A", false},
	5:   {5, "A", ClaseOtro, "Nota de Venta al Contado A", false},
	6:   {6, "B", ClaseFactura, "Factura B", false},
	7:   {7, "B", ClaseNotaDebito, "Nota de Débito B", false},
	8:   {8, "B", ClaseNotaCredito, "Nota de Crédito B", false},
	9:   {9, "B", ClaseRecibo, "Recibo B", false},
	10:  {10, "B", ClaseOtro, "Nota de Venta al Contado B", false},
	11:  {11, "C", ClaseFactura, "Factura C", false},
	12:  {12, "C", ClaseNotaDebito, "Nota de Débito C", false},
	13:  {13, "C", ClaseNotaCredito, "Nota de Crédito C", false},
	15:  {15, "C", ClaseRecibo, "Recibo C", false},
	19:  {19, "E", ClaseFactura, "Factura E", false},
	20:  {20, "E", ClaseNotaDebito, "Nota de Débito E", false},
	21:  {21, "E", ClaseNotaCredito, "Nota de Crédito E", false},
	49:  {49, "", ClaseOtro, "Comprobante de Compra de Bienes Usados", false},
	51:  {51, "M", ClaseFactura, "Factura M", false},
	52:  {52, "M", ClaseNotaDebito, "Nota de Débito M", false},
	53:  {53, "M", ClaseNotaCredito, "Nota de Crédito M", false},
	54:  {54, "M", ClaseRecibo, "Recibo M", false},
	201: {201, "A", ClaseFactura, "Factura de Crédito Electrónica MiPyME A", true},
	202: {202, "A", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME A", true},
	203: {203, "A", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME A", true},
	206: {206, "B", ClaseFactura, "Factura de Crédito Electrónica MiPyME B", true},
	207: {207, "B", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME B", true},
	208: {208, "B", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME B", true},
	211: {211, "C", ClaseFactura, "Factura de Crédito Electrónica MiPyME C", true},
	212: {212, "C", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME C", true},
	213: {213, "C", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME C", true},
}

// Tipo devuelve la descripción de un código; ok=false si no está en la tabla.
func Tipo(codigo int) (TipoCbte, bool) {
	t, ok := tipos[codigo]
	return t, ok
}

// TipoDesde arma el código a partir de la clase y la letra que se leen en el papel.
// Devuelve 0 si la combinación no existe.
func TipoDesde(clase Clase, letra string, fce bool) int {
	for _, t := range tipos {
		if t.Clase == clase && t.Letra == letra && t.FCE == fce {
			return t.Codigo
		}
	}
	return 0
}

// DocTipos de receptor más comunes.
var DocTipos = map[int]string{80: "CUIT", 86: "CUIL", 87: "CDI", 89: "LE", 90: "LC", 94: "Pasaporte", 96: "DNI", 99: "Consumidor Final"}
