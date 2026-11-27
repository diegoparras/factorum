package libroiva

import (
	"fmt"
	"strings"
)

// Campo de un diseño de registro: posiciones desde y hasta, las dos incluidas, contando desde 1.
type Campo struct {
	Nro    int
	Nombre string
	Desde  int
	Hasta  int
}

// Largo del campo.
func (c Campo) Largo() int { return c.Hasta - c.Desde + 1 }

// Diseño de registro «Compras e Importación de Bienes - Cabecera» (LIBRO_IVA_DIGITAL_COMPRAS_CBTE,
// longitud 325), Anexo I de ARCA: arca.gob.ar/iva/documentos/libro-iva-digital-diseno-registros.pdf.
var CamposCbte = []Campo{
	{1, "Fecha de comprobante o fecha de oficialización", 1, 8},
	{2, "Tipo de comprobante", 9, 11},
	{3, "Punto de venta", 12, 16},
	{4, "Número de comprobante", 17, 36},
	{5, "Despacho de importación", 37, 52},
	{6, "Código de documento del vendedor", 53, 54},
	{7, "Número de identificación del vendedor", 55, 74},
	{8, "Apellido y nombre o denominación del vendedor", 75, 104},
	{9, "Importe total de la operación", 105, 119},
	{10, "Importe total de conceptos que no integran el precio neto gravado", 120, 134},
	{11, "Importe de operaciones exentas", 135, 149},
	{12, "Importe de percepciones o pagos a cuenta del Impuesto al Valor Agregado", 150, 164},
	{13, "Importe de percepciones o pagos a cuenta de otros impuestos nacionales", 165, 179},
	{14, "Importe de percepciones de Ingresos Brutos", 180, 194},
	{15, "Importe de percepciones de Impuestos Municipales", 195, 209},
	{16, "Importe de Impuestos Internos", 210, 224},
	{17, "Código de moneda", 225, 227},
	{18, "Tipo de cambio", 228, 237},
	{19, "Cantidad de alícuotas de IVA", 238, 238},
	{20, "Código de operación", 239, 239},
	{21, "Crédito Fiscal Computable", 240, 254},
	{22, "Otros Tributos", 255, 269},
	{23, "CUIT emisor/corredor", 270, 280},
	{24, "Denominación del emisor/corredor", 281, 310},
	{25, "IVA comisión", 311, 325},
}

// Diseño de registro «Compras - Alícuotas» (LIBRO_IVA_DIGITAL_COMPRAS_ALICUOTAS, longitud 84).
var CamposAlicuota = []Campo{
	{1, "Tipo de comprobante", 1, 3},
	{2, "Punto de venta", 4, 8},
	{3, "Número de comprobante", 9, 28},
	{4, "Código de documento del vendedor", 29, 30},
	{5, "Número de identificación del vendedor", 31, 50},
	{6, "Importe neto gravado", 51, 65},
	{7, "Alícuota de IVA", 66, 69},
	{8, "Impuesto liquidado", 70, 84},
}

// Diseño de registro «Ventas - Cabecera» (LIBRO_IVA_DIGITAL_VENTAS_CBTE, longitud 266), Anexo I.
var CamposVentasCbte = []Campo{
	{1, "Fecha de comprobante", 1, 8},
	{2, "Tipo de comprobante", 9, 11},
	{3, "Punto de venta", 12, 16},
	{4, "Número de comprobante", 17, 36},
	{5, "Número de comprobante hasta", 37, 56},
	{6, "Código de documento del comprador", 57, 58},
	{7, "Número de identificación del comprador", 59, 78},
	{8, "Apellido y nombre o denominación del comprador", 79, 108},
	{9, "Importe total de la operación", 109, 123},
	{10, "Importe total de conceptos que no integran el precio neto gravado", 124, 138},
	{11, "Percepción a no categorizados", 139, 153},
	{12, "Importe de operaciones exentas", 154, 168},
	{13, "Importe de percepciones o pagos a cuenta de impuestos Nacionales", 169, 183},
	{14, "Importe de percepciones de Ingresos Brutos", 184, 198},
	{15, "Importe de percepciones impuestos Municipales", 199, 213},
	{16, "Importe impuestos internos", 214, 228},
	{17, "Código de moneda", 229, 231},
	{18, "Tipo de cambio", 232, 241},
	{19, "Cantidad de alícuotas de IVA", 242, 242},
	{20, "Código de operación", 243, 243},
	{21, "Otros Tributos", 244, 258},
	{22, "Fecha de Vencimiento o Pago", 259, 266},
}

// Diseño de registro «Ventas - Alícuotas» (LIBRO_IVA_DIGITAL_VENTAS_ALICUOTAS, longitud 62).
var CamposVentasAlicuota = []Campo{
	{1, "Tipo de comprobante", 1, 3},
	{2, "Punto de venta", 4, 8},
	{3, "Número de comprobante", 9, 28},
	{4, "Importe neto gravado", 29, 43},
	{5, "Alícuota de IVA", 44, 47},
	{6, "Impuesto Liquidado", 48, 62},
}

// Diseño de registro «Importaciones de Bienes - Alícuotas» (LIBRO_IVA_DIGITAL_IMPORTACION_BIENES_ALICUOTA,
// longitud 50), Anexo I. La cabecera de los despachos es la de compras (CamposCbte).
var CamposImportacionAlicuota = []Campo{
	{1, "Despacho de importación", 1, 16},
	{2, "Importe neto gravado", 17, 31},
	{3, "Alícuota de IVA", 32, 35},
	{4, "Impuesto liquidado", 36, 50},
}

// Longitudes de los registros según el diseño.
const (
	LargoCbte                = 325
	LargoAlicuota            = 84
	LargoVentasCbte          = 266
	LargoVentasAlicuota      = 62
	LargoImportacionAlicuota = 50
)

// armar junta los valores de un registro controlando que cada uno tenga el largo de su campo.
func armar(campos []Campo, valores []string) (string, error) {
	if len(valores) != len(campos) {
		return "", fmt.Errorf("registro con %d campos, el diseño tiene %d", len(valores), len(campos))
	}
	var b strings.Builder
	for i, c := range campos {
		if len(valores[i]) != c.Largo() {
			return "", fmt.Errorf("campo %d (%s): %d posiciones, el diseño pide %d", c.Nro, c.Nombre, len(valores[i]), c.Largo())
		}
		b.WriteString(valores[i])
	}
	return b.String(), nil
}
