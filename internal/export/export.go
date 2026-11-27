// Package export arma el Excel y el CSV de comprobantes, y el resumen por alícuota.
// Las notas de crédito van en negativo, para que las sumas den el neto del período.
package export

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Fila es lo mínimo que se exporta de un comprobante guardado.
type Fila struct {
	ID          int64
	Archivo     string
	ClienteCUIT string
	Estado      modelo.Estado
	Hallazgos   []modelo.Hallazgo
	Constatado  string // "", "valido", "invalido", "apocrifo"
	C           modelo.Comprobante
}

func signo(c *modelo.Comprobante) modelo.Importe {
	if tp, ok := arca.Tipo(c.Tipo); ok && tp.Clase == arca.ClaseNotaCredito {
		return -1
	}
	return 1
}

type columna struct {
	titulo string
	valor  func(f *Fila) any
	ancho  float64
	monto  bool
}

func imp(get func(c *modelo.Comprobante) modelo.Importe) func(f *Fila) any {
	return func(f *Fila) any { return (get(&f.C) * signo(&f.C)).Float() }
}

var columnas = []columna{
	{"Fecha", func(f *Fila) any { return f.C.Fecha }, 11, false},
	{"Hora", func(f *Fila) any { return f.C.Hora }, 9, false},
	{"Tipo", func(f *Fila) any { return f.C.Tipo }, 6, false},
	{"Comprobante", func(f *Fila) any {
		if tp, ok := arca.Tipo(f.C.Tipo); ok {
			return tp.Nombre
		}
		return ""
	}, 22, false},
	{"Letra", func(f *Fila) any { return f.C.Letra }, 6, false},
	{"Punto de venta", func(f *Fila) any { return f.C.PuntoVenta }, 8, false},
	{"Número", func(f *Fila) any { return f.C.Numero }, 10, false},
	{"CUIT emisor", func(f *Fila) any { return arca.FormatearCUIT(f.C.CUITEmisor) }, 14, false},
	{"Razón social emisor", func(f *Fila) any { return f.C.RazonSocialEmisor }, 32, false},
	{"Condición IVA emisor", func(f *Fila) any { return f.C.CondicionIVAEmisor }, 20, false},
	{"Rubro", func(f *Fila) any { return f.C.Rubro }, 13, false},
	{"Doc. receptor", func(f *Fila) any { return f.C.DocNroReceptor }, 14, false},
	{"Razón social receptor", func(f *Fila) any { return f.C.RazonSocialReceptor }, 28, false},
	{"Neto gravado", imp(func(c *modelo.Comprobante) modelo.Importe { return c.NetoGravado }), 14, true},
	{"No gravado", imp(func(c *modelo.Comprobante) modelo.Importe { return c.NoGravado }), 12, true},
	{"Exento", imp(func(c *modelo.Comprobante) modelo.Importe { return c.Exento }), 12, true},
	{"IVA 2,5%", imp(func(c *modelo.Comprobante) modelo.Importe { return c.IVA25 }), 12, true},
	{"IVA 5%", imp(func(c *modelo.Comprobante) modelo.Importe { return c.IVA5 }), 12, true},
	{"IVA 10,5%", imp(func(c *modelo.Comprobante) modelo.Importe { return c.IVA105 }), 12, true},
	{"IVA 21%", imp(func(c *modelo.Comprobante) modelo.Importe { return c.IVA21 }), 12, true},
	{"IVA 27%", imp(func(c *modelo.Comprobante) modelo.Importe { return c.IVA27 }), 12, true},
	{"Otros tributos", imp(func(c *modelo.Comprobante) modelo.Importe { return c.OtrosTributos }), 13, true},
	{"Subtotal (B/C)", imp(func(c *modelo.Comprobante) modelo.Importe { return c.Subtotal }), 13, true},
	{"Total", imp(func(c *modelo.Comprobante) modelo.Importe { return c.Total }), 14, true},
	{"Moneda", func(f *Fila) any { return f.C.Moneda }, 7, false},
	{"Cotización", func(f *Fila) any { return f.C.Cotizacion }, 10, false},
	{"CAE", func(f *Fila) any { return f.C.CAE }, 16, false},
	{"Vto. CAE", func(f *Fila) any { return f.C.VtoCAE }, 11, false},
	{"Estado", func(f *Fila) any { return string(f.Estado) }, 9, false},
	{"Constatación ARCA", func(f *Fila) any { return f.Constatado }, 12, false},
	{"Observaciones", func(f *Fila) any { return observaciones(f.Hallazgos) }, 50, false},
	{"Origen de los datos", func(f *Fila) any { return origenes(&f.C) }, 18, false},
	{"Archivo", func(f *Fila) any { return f.Archivo }, 24, false},
	{"Cliente", func(f *Fila) any { return arca.FormatearCUIT(f.ClienteCUIT) }, 14, false},
	{"ID Factorum", func(f *Fila) any { return f.ID }, 9, false},
}

func observaciones(h []modelo.Hallazgo) string {
	var p []string
	for _, x := range h {
		if x.Nivel != modelo.NivelInfo {
			p = append(p, x.Mensaje)
		}
	}
	return strings.Join(p, " | ")
}

func origenes(c *modelo.Comprobante) string {
	vistos := map[modelo.Origen]bool{}
	var p []string
	for _, k := range []string{"total", "cuit_emisor", "neto_gravado", "iva_21", "razon_social_emisor"} {
		if o := c.Origen(k); o != "" && !vistos[o] {
			vistos[o] = true
			p = append(p, string(o))
		}
	}
	return strings.Join(p, ", ")
}

// Excel escribe un .xlsx con la hoja de comprobantes y la de resumen.
func Excel(w io.Writer, filas []Fila) error {
	f := excelize.NewFile()
	defer f.Close()
	const hoja = "Comprobantes"
	f.SetSheetName("Sheet1", hoja)
	sw, err := f.NewStreamWriter(hoja)
	if err != nil {
		return err
	}
	negrita, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"DDDDDD"}, Pattern: 1}})
	monto, _ := f.NewStyle(&excelize.Style{NumFmt: 4}) // #,##0.00
	for i, c := range columnas {
		_ = sw.SetColWidth(i+1, i+1, c.ancho)
	}
	if err := sw.SetPanes(&excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	enc := make([]any, len(columnas))
	for i, c := range columnas {
		enc[i] = excelize.Cell{StyleID: negrita, Value: c.titulo}
	}
	if err := sw.SetRow("A1", enc); err != nil {
		return err
	}
	for n := range filas {
		fila := make([]any, len(columnas))
		for i, c := range columnas {
			v := c.valor(&filas[n])
			if c.monto {
				fila[i] = excelize.Cell{StyleID: monto, Value: v}
			} else {
				fila[i] = v
			}
		}
		celda, _ := excelize.CoordinatesToCellName(1, n+2)
		if err := sw.SetRow(celda, fila); err != nil {
			return err
		}
	}
	if err := sw.Flush(); err != nil {
		return err
	}
	if len(filas) > 0 {
		ult, _ := excelize.CoordinatesToCellName(len(columnas), len(filas)+1)
		_ = f.AutoFilter(hoja, "A1:"+ult, nil)
	}

	// Resumen.
	r := Resumir(filas)
	const hr = "Resumen"
	f.NewSheet(hr)
	_ = f.SetColWidth(hr, "A", "A", 28)
	_ = f.SetColWidth(hr, "B", "B", 18)
	filasR := [][2]any{
		{"Comprobantes", r.Cantidad},
		{"Neto gravado", r.NetoGravado.Float()}, {"No gravado", r.NoGravado.Float()}, {"Exento", r.Exento.Float()},
		{"IVA 2,5%", r.IVA25.Float()}, {"IVA 5%", r.IVA5.Float()}, {"IVA 10,5%", r.IVA105.Float()}, {"IVA 21%", r.IVA21.Float()}, {"IVA 27%", r.IVA27.Float()},
		{"IVA total", r.IVATotal.Float()}, {"Otros tributos", r.OtrosTributos.Float()}, {"Subtotal B/C", r.Subtotal.Float()}, {"Total", r.Total.Float()},
		{"", ""}, {"Para revisar", r.PorEstado["revisar"]}, {"Con errores", r.PorEstado["error"]}, {"En moneda extranjera (no sumados)", r.MonedaExtranjera},
		{"", ""}, {"Tickets de compra", r.Tickets.Cantidad}, {"Total en tickets", r.Tickets.Total.Float()},
	}
	var rubros []string
	for k := range r.PorRubro {
		rubros = append(rubros, k)
	}
	sort.Strings(rubros)
	if len(rubros) > 0 {
		filasR = append(filasR, [2]any{"", ""}, [2]any{"Total por rubro", ""})
		for _, k := range rubros {
			nombre := k
			if k == SinRubro {
				nombre = "sin rubro"
			}
			filasR = append(filasR, [2]any{"  " + nombre, r.PorRubro[k].Float()})
		}
	}
	for i, x := range filasR {
		_ = f.SetCellValue(hr, fmt.Sprintf("A%d", i+1), x[0])
		_ = f.SetCellValue(hr, fmt.Sprintf("B%d", i+1), x[1])
		if _, ok := x[1].(float64); ok {
			_ = f.SetCellStyle(hr, fmt.Sprintf("B%d", i+1), fmt.Sprintf("B%d", i+1), monto)
		}
	}
	_ = f.SetCellValue(hr, fmt.Sprintf("A%d", len(filasR)+2), "Las notas de crédito restan. Los comprobantes en moneda extranjera no se suman.")
	return f.Write(w)
}

// CSV con ";" y coma decimal, en UTF-8 con BOM: abre bien en Excel en castellano.
func CSV(w io.Writer, filas []Fila) error {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	cw := csv.NewWriter(&buf)
	cw.Comma = ';'
	enc := make([]string, len(columnas))
	for i, c := range columnas {
		enc[i] = c.titulo
	}
	_ = cw.Write(enc)
	for n := range filas {
		reg := make([]string, len(columnas))
		for i, c := range columnas {
			v := c.valor(&filas[n])
			switch x := v.(type) {
			case float64:
				reg[i] = strings.Replace(fmt.Sprintf("%.2f", x), ".", ",", 1)
				if !c.monto {
					reg[i] = strings.TrimRight(strings.TrimRight(strings.Replace(fmt.Sprintf("%.4f", x), ".", ",", 1), "0"), ",")
				}
			default:
				reg[i] = fmt.Sprint(x)
			}
		}
		_ = cw.Write(reg)
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// Resumen por alícuota.
type Resumen struct {
	Cantidad         int            `json:"cantidad"`
	NetoGravado      modelo.Importe `json:"neto_gravado"`
	NoGravado        modelo.Importe `json:"no_gravado"`
	Exento           modelo.Importe `json:"exento"`
	IVA25            modelo.Importe `json:"iva_2_5"`
	IVA5             modelo.Importe `json:"iva_5"`
	IVA105           modelo.Importe `json:"iva_10_5"`
	IVA21            modelo.Importe `json:"iva_21"`
	IVA27            modelo.Importe `json:"iva_27"`
	IVATotal         modelo.Importe `json:"iva_total"`
	OtrosTributos    modelo.Importe `json:"otros_tributos"`
	Subtotal         modelo.Importe `json:"subtotal_b_c"`
	Total            modelo.Importe `json:"total"`
	MonedaExtranjera int            `json:"moneda_extranjera"`
	PorEstado        map[string]int `json:"por_estado"`
	PorTipo          map[string]int `json:"por_tipo"`
	// Tickets de compra (tiques de controlador fiscal): cuántos y cuánto suman.
	Tickets ResumenTickets `json:"tickets"`
	// PorRubro: total por rubro de gasto (las notas de crédito restan; sin rubro va en "sin_rubro").
	// Con el filtro clase=tickets son los "gastos con ticket" por rubro.
	PorRubro map[string]modelo.Importe `json:"por_rubro"`
}

// ResumenTickets cuenta los tiques de controlador fiscal.
type ResumenTickets struct {
	Cantidad int            `json:"cantidad"`
	Total    modelo.Importe `json:"total"`
}

// SinRubro es la clave de por_rubro para lo que no tiene rubro.
const SinRubro = "sin_rubro"

func Resumir(filas []Fila) Resumen {
	r := Resumen{PorEstado: map[string]int{}, PorTipo: map[string]int{}, PorRubro: map[string]modelo.Importe{}}
	for i := range filas {
		f := &filas[i]
		c := &f.C
		r.Cantidad++
		r.PorEstado[string(f.Estado)]++
		nombre := fmt.Sprint(c.Tipo)
		if tp, ok := arca.Tipo(c.Tipo); ok {
			nombre = tp.Nombre
		}
		r.PorTipo[nombre]++
		if c.Moneda != "" && c.Moneda != "PES" {
			r.MonedaExtranjera++
			continue
		}
		s := signo(c)
		r.NetoGravado += s * c.NetoGravado
		r.NoGravado += s * c.NoGravado
		r.Exento += s * c.Exento
		r.IVA25 += s * c.IVA25
		r.IVA5 += s * c.IVA5
		r.IVA105 += s * c.IVA105
		r.IVA21 += s * c.IVA21
		r.IVA27 += s * c.IVA27
		r.OtrosTributos += s * c.OtrosTributos
		r.Subtotal += s * c.Subtotal
		r.Total += s * c.Total
		if c.EsTique || arca.EsTique(c.Tipo) {
			r.Tickets.Cantidad++
			r.Tickets.Total += s * c.Total
		}
		rb := c.Rubro
		if rb == "" {
			rb = SinRubro
		}
		r.PorRubro[rb] += s * c.Total
	}
	r.IVATotal = r.IVA25 + r.IVA5 + r.IVA105 + r.IVA21 + r.IVA27
	return r
}
