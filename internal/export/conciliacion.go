package export

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/conciliar"
	"github.com/diegoparras/factorum/internal/modelo"
)

// InfoConciliacion es lo que va arriba del resumen: de quién, de cuándo y qué tan completo está ARCA.
type InfoConciliacion struct {
	Cliente        string
	Nombre         string // razón social del cliente, si se conoce
	Desde, Hasta   string
	Sincronizacion string // «última sincronización de Arcanum: ...», ya armado
	SinSincronizar []conciliar.Periodo
	Aviso          string
}

// NombreCausa es el texto corto de cada causa de «No figura en ARCA».
var NombreCausa = map[string]string{
	conciliar.CausaTique:         "Tique de controlador fiscal",
	conciliar.CausaExterior:      "Moneda extranjera o exterior",
	conciliar.CausaOtroReceptor:  "Otro receptor o consumidor final",
	conciliar.CausaNoSincronizad: "Período sin sincronizar",
	conciliar.CausaNota:          "Nota de crédito o débito",
	conciliar.CausaIncompleto:    "Faltan datos para cruzarlo",
	conciliar.CausaSinExplicar:   "Sin explicación: constatalo",
}

var nombreConstatacion = map[string]string{"valido": "válido en ARCA", "invalido": "ARCA no lo reconoce", "apocrifo": "emisor apócrifo", "controlado": "emisor controlado", "error": "no se pudo constatar"}

var nombreCampo = map[string]string{"total": "Total", "neto_gravado": "Neto gravado", "iva": "IVA", "fecha": "Fecha", "moneda": "Moneda", "punto_venta": "Punto de venta", "tipo": "Tipo"}

func fechaAR(f string) string {
	if len(f) != 10 {
		return f
	}
	return f[8:10] + "/" + f[5:7] + "/" + f[0:4]
}

// DetalleDiferencias arma el texto de lo que no cierra («Total: Factorum 1.210,00, ARCA 1.310,00 (-100,00)»).
func DetalleDiferencias(it *conciliar.Item) string {
	var p []string
	for _, d := range it.Diferencias {
		n := nombreCampo[d.Campo]
		switch d.Campo {
		case "total", "neto_gravado", "iva":
			vf, _ := modelo.ParsearImporte(d.Factorum)
			va, _ := modelo.ParsearImporte(d.Arca)
			p = append(p, fmt.Sprintf("%s: Factorum %s, ARCA %s (%s)", n, vf.AR(), va.AR(), d.Importe.AR()))
		case "fecha":
			p = append(p, fmt.Sprintf("Fecha: Factorum %s, ARCA %s (%+d días)", fechaAR(d.Factorum), fechaAR(d.Arca), d.Dias))
		default:
			p = append(p, fmt.Sprintf("%s: Factorum %s, ARCA %s", n, d.Factorum, d.Arca))
		}
	}
	return strings.Join(p, "; ")
}

func signoTipo(tipo int) modelo.Importe {
	if tp, ok := arca.Tipo(tipo); ok && tp.Clase == arca.ClaseNotaCredito {
		return -1
	}
	return 1
}

type colConc struct {
	titulo string
	ancho  float64
	monto  bool
	valor  func(it *conciliar.Item) any
}

func montoDe(tipo int, v modelo.Importe) any {
	if v < 0 {
		v = -v
	}
	return (signoTipo(tipo) * v).Float()
}

var (
	ccFecha  = colConc{"Fecha", 11, false, func(it *conciliar.Item) any { return fechaAR(it.Fecha) }}
	ccTipo   = colConc{"Comprobante", 24, false, func(it *conciliar.Item) any { return it.NombreTipo }}
	ccCodigo = colConc{"Tipo", 6, false, func(it *conciliar.Item) any { return it.Tipo }}
	ccPV     = colConc{"Punto de venta", 9, false, func(it *conciliar.Item) any { return it.PuntoVenta }}
	ccNumero = colConc{"Número", 11, false, func(it *conciliar.Item) any { return it.Numero }}
	ccCUIT   = colConc{"CUIT emisor", 14, false, func(it *conciliar.Item) any { return arca.FormatearCUIT(it.CUITEmisor) }}
	ccEmisor = colConc{"Emisor", 32, false, func(it *conciliar.Item) any { return it.Emisor }}
	ccMoneda = colConc{"Moneda", 7, false, func(it *conciliar.Item) any { return it.Moneda }}
	ccTotalA = colConc{"Total ARCA", 14, true, func(it *conciliar.Item) any {
		return siArca(it, func(a *conciliar.LadoArca) any { return montoDe(a.Tipo, a.Total) })
	}}
	ccNetoA = colConc{"Neto ARCA", 14, true, func(it *conciliar.Item) any {
		return siArca(it, func(a *conciliar.LadoArca) any { return montoDe(a.Tipo, a.Neto) })
	}}
	ccIVAA = colConc{"IVA ARCA", 13, true, func(it *conciliar.Item) any {
		return siArca(it, func(a *conciliar.LadoArca) any { return montoDe(a.Tipo, a.IVA) })
	}}
	ccTotalF = colConc{"Total Factorum", 14, true, func(it *conciliar.Item) any {
		return siF(it, func(f *conciliar.LadoFactorum) any { return montoDe(f.Tipo, f.Total) })
	}}
	ccFechaF = colConc{"Fecha en Factorum", 11, false, func(it *conciliar.Item) any {
		return siF(it, func(f *conciliar.LadoFactorum) any { return fechaAR(f.Fecha) })
	}}
	ccLeidoF = colConc{"Leído en Factorum", 18, false, func(it *conciliar.Item) any {
		return siF(it, func(f *conciliar.LadoFactorum) any {
			return fmt.Sprintf("%d %05d-%08d", f.Tipo, f.PuntoVenta, f.Numero)
		})
	}}
	ccDifTotal = colConc{"Diferencia de total", 14, true, func(it *conciliar.Item) any {
		for _, d := range it.Diferencias {
			if d.Campo == "total" {
				return (signoTipo(it.Tipo) * d.Importe).Float()
			}
		}
		return ""
	}}
	ccDetalle = colConc{"Qué no cierra", 60, false, func(it *conciliar.Item) any { return DetalleDiferencias(it) }}
	ccCasi    = colConc{"Casi igual", 9, false, func(it *conciliar.Item) any { return siNo(it.CasiIgual) }}
	ccCAE     = colConc{"CAE", 16, false, func(it *conciliar.Item) any { return siArca(it, func(a *conciliar.LadoArca) any { return a.CAE }) }}
	ccApoc    = colConc{"Emisor apócrifo", 10, false, func(it *conciliar.Item) any {
		if it.Arca != nil && it.Arca.EmisorApocrifo != nil && *it.Arca.EmisorApocrifo {
			return "sí"
		}
		return ""
	}}
	ccCausa = colConc{"Causa", 28, false, func(it *conciliar.Item) any { return NombreCausa[it.Causa] }}
	ccLegit = colConc{"Legítima", 9, false, func(it *conciliar.Item) any { return siNo(it.Legitima) }}
	ccConst = colConc{"Constatación", 20, false, func(it *conciliar.Item) any {
		return siF(it, func(f *conciliar.LadoFactorum) any { return nombreConstatacion[f.Constatacion] })
	}}
	ccEstado = colConc{"Estado en Factorum", 11, false, func(it *conciliar.Item) any { return siF(it, func(f *conciliar.LadoFactorum) any { return f.Estado }) }}
	ccNota   = colConc{"Nota", 60, false, func(it *conciliar.Item) any { return it.Nota }}
	ccDup    = colConc{"Duplicados", 14, false, func(it *conciliar.Item) any { return duplicadosTexto(it) }}
	ccID     = colConc{"ID Factorum", 10, false, func(it *conciliar.Item) any { return siF(it, func(f *conciliar.LadoFactorum) any { return f.ID }) }}
	ccArch   = colConc{"Archivo", 26, false, func(it *conciliar.Item) any { return siF(it, func(f *conciliar.LadoFactorum) any { return f.Archivo }) }}
	ccGrupo  = colConc{"Grupo", 16, false, func(it *conciliar.Item) any { return conciliar.NombreGrupo[it.Grupo] }}
)

// Columnas de cada hoja.
var columnasConc = map[string][]colConc{
	conciliar.Coinciden:   {ccFecha, ccTipo, ccCodigo, ccPV, ccNumero, ccCUIT, ccEmisor, ccMoneda, ccTotalA, ccNetoA, ccIVAA, ccNota, ccDup, ccID, ccArch},
	conciliar.Diferencias: {ccFecha, ccTipo, ccCodigo, ccPV, ccNumero, ccCUIT, ccEmisor, ccMoneda, ccTotalA, ccTotalF, ccDifTotal, ccDetalle, ccCasi, ccLeidoF, ccFechaF, ccNota, ccDup, ccID, ccArch},
	conciliar.FaltaCargar: {ccFecha, ccTipo, ccCodigo, ccPV, ccNumero, ccCUIT, ccEmisor, ccMoneda, ccNetoA, ccIVAA, ccTotalA, ccCAE, ccApoc, ccNota},
	conciliar.NoFigura:    {ccFecha, ccTipo, ccCodigo, ccPV, ccNumero, ccCUIT, ccEmisor, ccMoneda, ccTotalF, ccCausa, ccLegit, ccNota, ccConst, ccEstado, ccDup, ccID, ccArch},
}

// Para el CSV, que es una sola tabla: el grupo primero y la unión de las columnas.
var columnasCSV = []colConc{ccGrupo, ccFecha, ccTipo, ccCodigo, ccPV, ccNumero, ccCUIT, ccEmisor, ccMoneda, ccTotalA, ccNetoA, ccIVAA, ccTotalF, ccDifTotal, ccDetalle, ccCasi,
	ccLeidoF, ccFechaF, ccCAE, ccApoc, ccCausa, ccLegit, ccConst, ccEstado, ccNota, ccDup, ccID, ccArch}

func siArca(it *conciliar.Item, f func(*conciliar.LadoArca) any) any {
	if it.Arca == nil {
		return ""
	}
	return f(it.Arca)
}

func siF(it *conciliar.Item, f func(*conciliar.LadoFactorum) any) any {
	if it.Factorum == nil {
		return ""
	}
	return f(it.Factorum)
}

func siNo(b bool) string {
	if b {
		return "sí"
	}
	return ""
}

func duplicadosTexto(it *conciliar.Item) string {
	var p []string
	if n := len(it.DuplicadosFactorum); n > 0 {
		ids := make([]string, n)
		for i, id := range it.DuplicadosFactorum {
			ids[i] = fmt.Sprint(id)
		}
		p = append(p, "en Factorum también "+strings.Join(ids, ", "))
	}
	if it.DuplicadosArca > 0 {
		p = append(p, fmt.Sprintf("%d más en ARCA", it.DuplicadosArca))
	}
	return strings.Join(p, "; ")
}

// gruposPedidos: todos, o solo uno («falta_cargar» para pedírselo al cliente).
func gruposPedidos(grupo string) []string {
	for _, g := range conciliar.Grupos {
		if g == grupo {
			return []string{g}
		}
	}
	return conciliar.Grupos
}

func extranjeraTexto(m map[string]modelo.Importe) string {
	var p []string
	for _, k := range []string{"DOL", "060"} {
		if v, ok := m[k]; ok {
			p = append(p, k+" "+v.AR())
		}
	}
	for k, v := range m {
		if k != "DOL" && k != "060" {
			p = append(p, k+" "+v.AR())
		}
	}
	return strings.Join(p, ", ")
}

// ConciliacionExcel escribe un .xlsx con el resumen y una hoja por grupo (o solo la del grupo pedido).
func ConciliacionExcel(w io.Writer, r *conciliar.Resultado, info InfoConciliacion, grupo string) error {
	f := excelize.NewFile()
	defer f.Close()
	negrita, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	titulo, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Size: 14}})
	encab, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"DDDDDD"}, Pattern: 1}})
	monto, _ := f.NewStyle(&excelize.Style{NumFmt: 4})
	aviso, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true, Color: "9C5700"}})

	const hr = "Resumen"
	f.SetSheetName("Sheet1", hr)
	_ = f.SetColWidth(hr, "A", "A", 34)
	_ = f.SetColWidth(hr, "B", "E", 18)
	fila := 1
	set := func(col string, v any, estilo int) {
		celda := fmt.Sprintf("%s%d", col, fila)
		_ = f.SetCellValue(hr, celda, v)
		if estilo != 0 {
			_ = f.SetCellStyle(hr, celda, celda, estilo)
		}
	}
	set("A", "Conciliación con ARCA (Mis Comprobantes)", titulo)
	fila += 2
	cli := arca.FormatearCUIT(info.Cliente)
	if info.Nombre != "" {
		cli += " " + info.Nombre
	}
	for _, kv := range [][2]string{{"Cliente", cli}, {"Período (fecha de emisión)", fechaAR(info.Desde) + " al " + fechaAR(info.Hasta)}, {"Arcanum", info.Sincronizacion}} {
		set("A", kv[0], negrita)
		set("B", kv[1], 0)
		fila++
	}
	if len(info.SinSincronizar) > 0 {
		var p []string
		for _, x := range info.SinSincronizar {
			p = append(p, fechaAR(x.Desde)+" al "+fechaAR(x.Hasta))
		}
		set("A", "Sin sincronizar en Arcanum: "+strings.Join(p, ", "), aviso)
		fila++
	}
	if info.Aviso != "" {
		set("A", info.Aviso, aviso)
		fila++
	}
	fila++
	for i, t := range []string{"Grupo", "Cantidad", "Total ARCA (pesos)", "Total Factorum (pesos)", "Moneda extranjera (aparte)"} {
		set(string(rune('A'+i)), t, encab)
	}
	fila++
	for _, g := range conciliar.Grupos {
		t := r.Totales[g]
		set("A", conciliar.NombreGrupo[g], 0)
		set("B", t.Cantidad, 0)
		if g != conciliar.NoFigura {
			set("C", t.PesosArca.Float(), monto)
		}
		if g != conciliar.FaltaCargar {
			set("D", t.PesosFactorum.Float(), monto)
		}
		set("E", extranjeraTexto(t.Extranjera), 0)
		fila++
	}
	if nf := r.Totales[conciliar.NoFigura]; nf.Cantidad > 0 {
		fila++
		set("A", "No figura en ARCA, por causa", negrita)
		fila++
		for _, c := range []string{conciliar.CausaTique, conciliar.CausaExterior, conciliar.CausaOtroReceptor, conciliar.CausaNoSincronizad, conciliar.CausaNota, conciliar.CausaIncompleto, conciliar.CausaSinExplicar} {
			if n := nf.PorCausa[c]; n > 0 {
				set("A", "  "+NombreCausa[c], 0)
				set("B", n, 0)
				fila++
			}
		}
	}
	if r.Duplicados.Factorum > 0 || r.Duplicados.Arca > 0 {
		fila++
		set("A", fmt.Sprintf("Duplicados: %d en Factorum, %d en ARCA (ver la columna Duplicados).", r.Duplicados.Factorum, r.Duplicados.Arca), 0)
		fila++
	}
	fila++
	set("A", "Las notas de crédito restan. Lo que está en moneda extranjera no se suma con los pesos. Tolerancia de redondeo: un centavo por importe.", 0)

	for _, g := range gruposPedidos(grupo) {
		hoja := conciliar.NombreGrupo[g]
		f.NewSheet(hoja)
		cols := columnasConc[g]
		for i, c := range cols {
			col, _ := excelize.ColumnNumberToName(i + 1)
			_ = f.SetColWidth(hoja, col, col, c.ancho)
			celda, _ := excelize.CoordinatesToCellName(i+1, 1)
			_ = f.SetCellValue(hoja, celda, c.titulo)
			_ = f.SetCellStyle(hoja, celda, celda, encab)
		}
		_ = f.SetPanes(hoja, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})
		its := r.Grupos[g]
		for n := range its {
			for i, c := range cols {
				celda, _ := excelize.CoordinatesToCellName(i+1, n+2)
				v := c.valor(&its[n])
				_ = f.SetCellValue(hoja, celda, v)
				if _, ok := v.(float64); ok && c.monto {
					_ = f.SetCellStyle(hoja, celda, celda, monto)
				}
			}
		}
		if len(its) > 0 {
			ult, _ := excelize.CoordinatesToCellName(len(cols), len(its)+1)
			_ = f.AutoFilter(hoja, "A1:"+ult, nil)
		}
	}
	if len(gruposPedidos(grupo)) == 1 {
		idx, _ := f.GetSheetIndex(conciliar.NombreGrupo[grupo])
		f.SetActiveSheet(idx)
	}
	return f.Write(w)
}

// ConciliacionCSV escribe una sola tabla con el grupo en la primera columna (separador «;», coma
// decimal y BOM, como el resto de los CSV de Factorum).
func ConciliacionCSV(w io.Writer, r *conciliar.Resultado, grupo string) error {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	cw := csv.NewWriter(&buf)
	cw.Comma = ';'
	enc := make([]string, len(columnasCSV))
	for i, c := range columnasCSV {
		enc[i] = c.titulo
	}
	_ = cw.Write(enc)
	for _, g := range gruposPedidos(grupo) {
		its := r.Grupos[g]
		for n := range its {
			reg := make([]string, len(columnasCSV))
			for i, c := range columnasCSV {
				switch x := c.valor(&its[n]).(type) {
				case float64:
					reg[i] = strings.Replace(fmt.Sprintf("%.2f", x), ".", ",", 1)
				default:
					reg[i] = celdaSegura(fmt.Sprint(x))
				}
			}
			_ = cw.Write(reg)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// celdaSegura: un texto que empieza con =, +, - o @ lo tomaría Excel como fórmula (un nombre de
// emisor o de archivo puede venir de afuera).
func celdaSegura(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
