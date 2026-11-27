package libroiva

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// InstruccionImportar es lo que hay que elegir en la pantalla de importación del Libro IVA Digital
// para que los importes en moneda extranjera se lean bien (los archivos van en la moneda original).
const InstruccionImportar = "Al importar, elegí «Los importes están expresados en la Moneda Original del Comprobante»: lo que está en moneda extranjera va en su moneda con la cotización del comprobante (con todo en pesos, las dos opciones dan lo mismo)."

func (r *Resultado) nombreCbte() string {
	if len(r.Archivos) == 2 {
		return r.Archivos[0]
	}
	return NombreCbte
}

func (r *Resultado) nombreAlicuotas() string {
	if len(r.Archivos) == 2 {
		return r.Archivos[1]
	}
	return NombreAlicuotas
}

// Zip escribe el ZIP con los dos archivos de ARCA y el resumen legible.
func (r *Resultado) Zip(w io.Writer, nombreCliente string, generado time.Time) error {
	z := zip.NewWriter(w)
	for _, f := range []struct {
		nombre string
		datos  []byte
	}{{r.nombreCbte(), r.Comprobantes}, {r.nombreAlicuotas(), r.Alicuotas}, {NombreResumen, r.ResumenCSV(nombreCliente, generado)}} {
		fw, err := z.CreateHeader(&zip.FileHeader{Name: f.nombre, Method: zip.Deflate, Modified: generado})
		if err != nil {
			return err
		}
		if _, err := fw.Write(f.datos); err != nil {
			return err
		}
	}
	return z.Close()
}

func ar(v modelo.Importe) string { return strings.ReplaceAll(v.String(), ".", ",") }

// celda evita que una planilla tome un texto como fórmula.
func celda(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

func fechaAR(f string) string {
	if len(f) == 10 && f[4] == '-' {
		return f[8:] + "/" + f[5:7] + "/" + f[:4]
	}
	return f
}

var nombreNivel = map[string]string{NivelBloquea: "No va", NivelFuerte: "Advertencia fuerte", NivelAdvertencia: "Advertencia", NivelInfo: "Dato"}

// ResumenCSV arma el resumen para una persona: totales por alícuota, los comprobantes que van y
// los problemas. Con «;» y coma decimal, en UTF-8 con BOM (abre bien en Excel en castellano).
// Las notas de crédito restan.
func (r *Resultado) ResumenCSV(nombreCliente string, generado time.Time) []byte {
	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	cw := csv.NewWriter(&buf)
	cw.Comma = ';'
	fila := func(c ...string) { _ = cw.Write(c) }
	s := &r.Resumen
	cli := arca.FormatearCUIT(r.Cliente)
	if nombreCliente != "" {
		cli += " " + nombreCliente
	}
	opcion := "Todos los comprobantes, con advertencias"
	if r.SoloAprobados {
		opcion = "Solo los listos (bien o aprobados, sin advertencias fuertes)"
	}
	libro := r.Libro
	if libro == "" {
		libro = LibroCompras
	}
	ventas := libro == LibroVentas
	fila("Libro IVA Digital: " + libro)
	fila("Cliente", celda(cli))
	fila("Período", r.Periodo)
	fila("Generado", generado.Format("02/01/2006 15:04"))
	fila("Qué va", opcion)
	fila("Comprobantes", strconv.Itoa(s.Comprobantes))
	if s.DeMesesAnteriores > 0 {
		fila("Entraron de meses anteriores", strconv.Itoa(s.DeMesesAnteriores))
	}
	fila("Registros de alícuotas", strconv.Itoa(s.LineasAlicuotas))
	fila("Quedaron afuera", strconv.Itoa(r.Excluidos))
	fila("Al importar", InstruccionImportar)
	fila()
	fila("Totales en pesos (las notas de crédito restan)")
	fila("Alícuota", "Código", "Registros", "Neto gravado", "IVA")
	for _, a := range s.PorAlicuota {
		fila(a.Tasa+" %", a.Codigo, strconv.Itoa(a.Lineas), ar(a.Neto), ar(a.IVA))
	}
	fila("Neto gravado", "", "", ar(s.NetoGravado))
	fila("No gravado", "", "", ar(s.NoGravado))
	fila("Exento", "", "", ar(s.Exento))
	if ventas {
		fila("IVA (débito fiscal)", "", "", ar(s.IVA))
	} else {
		fila("IVA", "", "", ar(s.IVA))
		fila("Crédito fiscal computable", "", "", ar(s.CreditoFiscal))
	}
	if s.Percepciones != 0 {
		fila("Percepciones e impuestos internos", "", "", ar(s.Percepciones))
	}
	fila("Otros tributos", "", "", ar(s.OtrosTributos))
	fila("Total", "", "", ar(s.Total))
	if len(s.Extranjera) > 0 {
		fila()
		fila("En moneda extranjera (en su moneda, no se suman con los pesos)")
		fila("Moneda", "Comprobantes", "IVA", "Crédito fiscal computable", "Total")
		for _, m := range s.Extranjera {
			fila(m.Moneda, strconv.Itoa(m.Cantidad), ar(m.IVA), ar(m.CreditoFiscal), ar(m.Total))
		}
	}
	fila()
	fila("Comprobantes que van")
	contra, doc := "Emisor", "CUIT emisor"
	if ventas {
		contra, doc = "Comprador", "Documento comprador"
	}
	fila("Fecha", "Código", "Comprobante", "Punto de venta", "Número", "Despacho", doc, contra, "Moneda", "Cotización",
		"Neto gravado", "No gravado", "Exento", "IVA", "Crédito fiscal", "Percepciones", "Otros tributos", "Total", "Código de operación", "Alícuotas",
		"De un mes anterior", "Estado", "ID Factorum")
	for i := range r.Registros {
		x := &r.Registros[i]
		sg := modelo.Importe(1)
		if x.NotaCredito {
			sg = -1
		}
		var alis []string
		for _, a := range x.Alicuotas {
			alis = append(alis, fmt.Sprintf("%s %%: %s / %s", a.Tasa, ar(sg*a.Neto), ar(sg*a.IVA)))
		}
		docNro := x.DocNro
		if x.DocTipo == 80 || x.DocTipo == 86 {
			docNro = arca.FormatearCUIT(x.DocNro)
		}
		id, ant := "", ""
		if x.ID > 0 {
			id = strconv.FormatInt(x.ID, 10)
		}
		if x.DeMesAnterior {
			ant = "Sí"
		}
		fila(fechaAR(x.Fecha), fmt.Sprintf("%03d", x.Tipo), x.NombreTipo, fmt.Sprintf("%05d", x.PuntoVenta), strconv.FormatInt(x.Numero, 10), x.Despacho,
			docNro, celda(x.Contraparte), x.Moneda, strings.Replace(strconv.FormatFloat(x.Cotizacion, 'f', -1, 64), ".", ",", 1),
			ar(sg*x.NetoGravado()), ar(sg*x.NoGravado), ar(sg*x.Exento), ar(sg*x.IVA), ar(sg*x.CreditoFiscal), ar(sg*x.Percepciones()), ar(sg*x.OtrosTributos), ar(sg*x.Total),
			strings.TrimSpace(x.CodigoOperacion), strings.Join(alis, " | "), ant, x.Estado, id)
	}
	if len(r.Grupos) > 0 {
		fila()
		fila("Problemas")
		fila("Nivel", "Problema", "Comprobante", "Fecha", doc, contra, "Detalle", "Va en el archivo", "ID Factorum")
		for _, g := range r.Grupos {
			for _, it := range g.Items {
				va := "Sí"
				if it.Excluido {
					va = "No"
				}
				ident := fmt.Sprintf("%s %05d-%08d", it.NombreTipo, it.PuntoVenta, it.Numero)
				if it.Despacho != "" {
					ident = it.NombreTipo + " " + it.Despacho
				}
				d := it.Doc
				if len(d) == 11 {
					d = arca.FormatearCUIT(d)
				}
				id := ""
				if it.ID > 0 {
					id = strconv.FormatInt(it.ID, 10)
				}
				fila(nombreNivel[g.Nivel], g.Titulo, ident, fechaAR(it.Fecha), d, celda(it.Contraparte), celda(it.Detalle), va, id)
			}
		}
	}
	cw.Flush()
	return buf.Bytes()
}
