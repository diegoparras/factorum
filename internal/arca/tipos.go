package arca

import "sort"

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
	Tique  bool // emitido por un controlador fiscal (tique, tique factura): no se constata en ARCA
}

var tipos = map[int]TipoCbte{
	1:  {1, "A", ClaseFactura, "Factura A", false, false},
	2:  {2, "A", ClaseNotaDebito, "Nota de Débito A", false, false},
	3:  {3, "A", ClaseNotaCredito, "Nota de Crédito A", false, false},
	4:  {4, "A", ClaseRecibo, "Recibo A", false, false},
	5:  {5, "A", ClaseOtro, "Nota de Venta al Contado A", false, false},
	6:  {6, "B", ClaseFactura, "Factura B", false, false},
	7:  {7, "B", ClaseNotaDebito, "Nota de Débito B", false, false},
	8:  {8, "B", ClaseNotaCredito, "Nota de Crédito B", false, false},
	9:  {9, "B", ClaseRecibo, "Recibo B", false, false},
	10: {10, "B", ClaseOtro, "Nota de Venta al Contado B", false, false},
	11: {11, "C", ClaseFactura, "Factura C", false, false},
	12: {12, "C", ClaseNotaDebito, "Nota de Débito C", false, false},
	13: {13, "C", ClaseNotaCredito, "Nota de Crédito C", false, false},
	15: {15, "C", ClaseRecibo, "Recibo C", false, false},
	19: {19, "E", ClaseFactura, "Factura E", false, false},
	20: {20, "E", ClaseNotaDebito, "Nota de Débito E", false, false},
	21: {21, "E", ClaseNotaCredito, "Nota de Crédito E", false, false},
	49: {49, "", ClaseOtro, "Comprobante de Compra de Bienes Usados", false, false},
	// Despacho de importación: tabla «Tipo de Comprobante» de las Tablas del Sistema del Libro IVA
	// Digital («066 DESPACHO DE IMPORTACION»). No es de factura electrónica: no tiene letra, punto de
	// venta, número ni CAE; lo identifica el número de despacho.
	66:  {66, "", ClaseFactura, "Despacho de Importación", false, false},
	51:  {51, "M", ClaseFactura, "Factura M", false, false},
	52:  {52, "M", ClaseNotaDebito, "Nota de Débito M", false, false},
	53:  {53, "M", ClaseNotaCredito, "Nota de Crédito M", false, false},
	54:  {54, "M", ClaseRecibo, "Recibo M", false, false},
	201: {201, "A", ClaseFactura, "Factura de Crédito Electrónica MiPyME A", true, false},
	202: {202, "A", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME A", true, false},
	203: {203, "A", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME A", true, false},
	206: {206, "B", ClaseFactura, "Factura de Crédito Electrónica MiPyME B", true, false},
	207: {207, "B", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME B", true, false},
	208: {208, "B", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME B", true, false},
	211: {211, "C", ClaseFactura, "Factura de Crédito Electrónica MiPyME C", true, false},
	212: {212, "C", ClaseNotaDebito, "Nota de Débito Electrónica MiPyME C", true, false},
	213: {213, "C", ClaseNotaCredito, "Nota de Crédito Electrónica MiPyME C", true, false},

	// Tiques de controlador fiscal. Fuente: la tabla oficial de comprobantes de ARCA
	// (afip.gob.ar/fe/documentos/TABLACOMPROBANTES.xls, "(*) solo emitidos con controladores
	// fiscales"), que trae el 81, el 82, el 83 y del 109 al 117. Del 118 al 120 (letra M) no están
	// en esa tabla, pero sí en el listado de comprobantes de la RG 4290/2018 (Boletín Oficial del
	// 03/08/2018). Arcanum resta como notas de crédito el 110 y del 112 al 114 (tiposCbte.js).
	81:  {81, "A", ClaseFactura, "Tique Factura A", false, true},
	82:  {82, "B", ClaseFactura, "Tique Factura B", false, true},
	83:  {83, "", ClaseFactura, "Tique", false, true},
	109: {109, "C", ClaseFactura, "Tique C", false, true},
	110: {110, "", ClaseNotaCredito, "Tique Nota de Crédito", false, true},
	111: {111, "C", ClaseFactura, "Tique Factura C", false, true},
	112: {112, "A", ClaseNotaCredito, "Tique Nota de Crédito A", false, true},
	113: {113, "B", ClaseNotaCredito, "Tique Nota de Crédito B", false, true},
	114: {114, "C", ClaseNotaCredito, "Tique Nota de Crédito C", false, true},
	115: {115, "A", ClaseNotaDebito, "Tique Nota de Débito A", false, true},
	116: {116, "B", ClaseNotaDebito, "Tique Nota de Débito B", false, true},
	117: {117, "C", ClaseNotaDebito, "Tique Nota de Débito C", false, true},
	118: {118, "M", ClaseFactura, "Tique Factura M", false, true},
	119: {119, "M", ClaseNotaCredito, "Tique Nota de Crédito M", false, true},
	120: {120, "M", ClaseNotaDebito, "Tique Nota de Débito M", false, true},
}

// Tipo devuelve la descripción de un código; ok=false si no está en la tabla.
func Tipo(codigo int) (TipoCbte, bool) {
	t, ok := tipos[codigo]
	return t, ok
}

// EsTique dice si el código es de un tique de controlador fiscal.
func EsTique(codigo int) bool {
	t, ok := tipos[codigo]
	return ok && t.Tique
}

// CodigosTique devuelve los códigos de tique, ordenados.
func CodigosTique() []int {
	var res []int
	for c, t := range tipos {
		if t.Tique {
			res = append(res, c)
		}
	}
	sort.Ints(res)
	return res
}

// TiqueDesde arma el código de un tique con la clase y la letra impresas ("TIQUE FACTURA B",
// "TIQUE NOTA DE CREDITO A"). Sin letra, la factura es el 83 (Tique) y la nota de crédito el 110.
// Devuelve 0 si la combinación no existe.
func TiqueDesde(clase Clase, letra string) int {
	if letra == "" {
		switch clase {
		case ClaseFactura:
			return 83
		case ClaseNotaCredito:
			return 110
		}
		return 0
	}
	for _, c := range CodigosTique() {
		t := tipos[c]
		// El 109 (Tique C) no lo emite un controlador fiscal: "TIQUE FACTURA C" es el 111.
		if c != 109 && t.Clase == clase && t.Letra == letra {
			return c
		}
	}
	return 0
}

// TipoDesde arma el código a partir de la clase y la letra que se leen en el papel.
// Devuelve 0 si la combinación no existe. Nunca devuelve un tique: para eso está TiqueDesde.
func TipoDesde(clase Clase, letra string, fce bool) int {
	for _, t := range tipos {
		if !t.Tique && t.Codigo != 66 && t.Clase == clase && t.Letra == letra && t.FCE == fce {
			return t.Codigo
		}
	}
	return 0
}

// DocTipos de receptor más comunes.
var DocTipos = map[int]string{80: "CUIT", 86: "CUIL", 87: "CDI", 89: "LE", 90: "LC", 94: "Pasaporte", 96: "DNI", 99: "Consumidor Final"}
