package libroiva

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Entrada es un comprobante de compra (o un despacho de importación) guardado en Factorum.
type Entrada struct {
	ID           int64
	Estado       modelo.Estado
	Constatacion string // "", valido, invalido, apocrifo, controlado, error
	Archivo      string
	C            modelo.Comprobante
}

// claveCompra identifica un comprobante de compra: emisor, tipo, punto de venta y número.
func claveCompra(c *modelo.Comprobante) string {
	cuit := arca.SoloDigitos(c.CUITEmisor)
	if cuit == "" || c.Tipo == 0 || c.Numero == 0 {
		return ""
	}
	return fmt.Sprintf("%s-%d-%d-%d", cuit, c.Tipo, c.PuntoVenta, c.Numero)
}

func baseCompra(e *Entrada, clave string) Item {
	c := &e.C
	cuit := arca.SoloDigitos(c.CUITEmisor)
	if clave == "" {
		clave = "id:" + strconv.FormatInt(e.ID, 10)
	}
	return Item{ID: e.ID, Clave: clave, Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), PuntoVenta: c.PuntoVenta, Numero: c.Numero,
		Despacho: c.Despacho, Fecha: c.Fecha, CUITEmisor: cuit, Emisor: c.RazonSocialEmisor, Doc: cuit, Contraparte: c.RazonSocialEmisor,
		Total: c.Total, Moneda: c.Moneda, Estado: string(e.Estado)}
}

// percepciones pasa el detalle de los otros tributos al registro y deja en «Otros tributos» lo que
// no está separado. Avisa lo que queda sin detalle.
func (a *analisis) percepciones(c *modelo.Comprobante) {
	r := a.reg
	r.PercepcionIVA, r.PercepcionNacionales, r.PercepcionIIBB = c.PercepcionIVA, c.PercepcionNacionales, c.PercepcionIIBB
	r.PercepcionMunicipal, r.ImpuestosInternos = c.PercepcionMunicipal, c.ImpuestosInternos
	det := c.DetalleTributos()
	r.OtrosTributos = c.OtrosTributos - det
	for _, v := range []modelo.Importe{c.PercepcionIVA, c.PercepcionNacionales, c.PercepcionIIBB, c.PercepcionMunicipal, c.ImpuestosInternos} {
		if v < 0 {
			a.add("importe_negativo", "Tiene importes negativos.")
		}
	}
	switch {
	case det > c.OtrosTributos:
		a.add("detalle_tributos", "Percepciones e impuestos internos %s; otros tributos %s.", det.AR(), c.OtrosTributos.AR())
	case r.OtrosTributos > 0 && det > 0:
		a.add("otros_tributos", "Quedan %s sin detalle (de %s de otros tributos).", r.OtrosTributos.AR(), c.OtrosTributos.AR())
	case r.OtrosTributos > 0:
		a.add("otros_tributos", "Otros tributos %s.", c.OtrosTributos.AR())
	}
}

// alicuotasA arma las alícuotas de un comprobante que discrimina IVA (A, M, tique factura A o M,
// despacho de importación) con el neto gravado único que guarda Factorum. Devuelve si quedó sin IVA.
func (a *analisis) alicuotasA(c *modelo.Comprobante) (sinIVA bool) {
	r := a.reg
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
		return true
	case len(con) == 1:
		t := con[0]
		r.Alicuotas = []Alicuota{{Codigo: t.codigo, Tasa: t.nombre, Neto: c.NetoGravado, IVA: t.iva(c)}}
		if d := abs(ivaDe(c.NetoGravado, t.bp) - t.iva(c)); d > 1 {
			a.add("iva_no_cierra", "IVA %s %% de %s es %s y dice %s.", t.nombre, c.NetoGravado.AR(), ivaDe(c.NetoGravado, t.bp).AR(), t.iva(c).AR())
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
	return false
}

// importesBasicos controla lo que vale para compras e importaciones: total, negativos y nombre.
func (a *analisis) importesBasicos(c *modelo.Comprobante) {
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
}

func (o *Opciones) analizar(e *Entrada) *analisis {
	c := &e.C
	k := claveCompra(c)
	a := &analisis{id: e.ID, estado: e.Estado, clave: k, base: baseCompra(e, k)}
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
	case c.Tipo == modelo.TipoDespacho:
		a.add("tipo_no_admitido", "Despacho de importación: va en la pestaña «Importaciones», con su propio par de archivos.")
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
	a.importesBasicos(c)
	moneda, cot := a.moneda(c.Moneda, c.Cotizacion)

	discrimina := ok && (tp.Letra == "A" || tp.Letra == "M")
	r := &Registro{ID: e.ID, Clave: a.base.Clave, Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), PuntoVenta: c.PuntoVenta, Numero: c.Numero, Fecha: c.Fecha,
		CUITEmisor: cuit, Emisor: c.RazonSocialEmisor, DocTipo: 80, DocNro: cuit, Contraparte: c.RazonSocialEmisor, Moneda: moneda, Cotizacion: cot,
		Discrimina: discrimina, NotaCredito: ok && tp.Clase == arca.ClaseNotaCredito, Total: c.Total, CodigoOperacion: " ",
		Estado: string(e.Estado), Archivo: e.Archivo}
	a.reg = r
	a.periodo(c.Fecha, c.PeriodoIVA, o.Periodo)
	tol := toleranciaCbte(c)

	if discrimina {
		r.NoGravado, r.Exento = c.NoGravado, c.Exento
		if a.alicuotasA(c) {
			if c.Exento > 0 {
				r.CodigoOperacion = "E"
			} else {
				r.CodigoOperacion = "N"
			}
		}
		if c.Total > 0 {
			a.totales(c.Total, c.SumaComponentes(), tol)
		}
		a.percepciones(c)
	} else {
		if ok {
			a.add("sin_credito_fiscal", "")
			if c.Subtotal > 0 && abs(c.Subtotal+c.OtrosTributos-c.Total) > tol {
				a.add("suma_b_c", "Subtotal %s + otros tributos %s y el total es %s.", c.Subtotal.AR(), c.OtrosTributos.AR(), c.Total.AR())
			}
		}
		a.percepciones(c)
	}

	if rec := arca.SoloDigitos(c.DocNroReceptor); len(rec) == 11 && o.Cliente != "" && rec != o.Cliente {
		if discrimina {
			a.add("otro_receptor_a", "Es de %s.", arca.FormatearCUIT(rec))
		} else {
			a.add("otro_receptor", "Es de %s.", arca.FormatearCUIT(rec))
		}
	}
	a.constatacion(e, o.Conciliacion)

	if !a.bloqueado {
		if err := r.renderizarCompra(); err != nil {
			a.add("no_entra", "%s.", err)
		}
	}
	return a
}

// constatacion suma lo que dijeron la constatación y la conciliación.
func (a *analisis) constatacion(e *Entrada, k *Conciliacion) {
	switch e.Constatacion {
	case "apocrifo":
		a.add("apocrifo", "Lo dijo la constatación.")
	case "invalido":
		a.add("invalido", "")
	}
	if k != nil {
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
}

func iniciar(libro string, es int, o *Opciones) (*Resultado, error) {
	o.Cliente = arca.SoloDigitos(o.Cliente)
	if len(o.Cliente) != 11 {
		return nil, fmt.Errorf("el CUIT del cliente tiene que tener 11 dígitos")
	}
	desde, hasta, err := ValidarPeriodo(o.Periodo)
	if err != nil {
		return nil, err
	}
	o.Periodo = desde[:7]
	archivos := map[string][]string{
		LibroCompras:       {NombreCbte, NombreAlicuotas},
		LibroVentas:        {NombreVentasCbte, NombreVentasAlicuotas},
		LibroImportaciones: {NombreImportacionesCbte, NombreImportacionesAlicuota},
	}[libro]
	return &Resultado{Libro: libro, Cliente: o.Cliente, Periodo: o.Periodo, Desde: desde, Hasta: hasta, SoloAprobados: o.SoloAprobados,
		Archivos: archivos, Leidos: es, Registros: []Registro{}, Grupos: []Grupo{}}, nil
}

// Generar arma el libro de compras del período con los comprobantes del cliente. Cada comprobante
// va en su período (el mes de la fecha, o el que se le asignó: ver Comprobante.PeriodoIVA); los que
// no son del período (o no tienen fecha) se informan como problema y no van.
func Generar(es []Entrada, o Opciones) (*Resultado, error) {
	res, err := iniciar(LibroCompras, len(es), &o)
	if err != nil {
		return nil, err
	}
	as := make([]*analisis, len(es))
	for i := range es {
		as[i] = o.analizar(&es[i])
	}
	armarResultado(res, as, o)
	return res, nil
}

// renderizarCompra escribe el registro de comprobantes y los de alícuotas de compras según el
// diseño de ARCA.
func (r *Registro) renderizarCompra() error {
	if len(r.Alicuotas) > 9 {
		return fmt.Errorf("más de 9 alícuotas")
	}
	var w escritor
	cuit, _ := strconv.ParseInt(r.DocNro, 10, 64)
	tipo, pv, nro, doc := w.num("tipo", int64(r.Tipo), 3), w.num("punto de venta", int64(r.PuntoVenta), 5), w.num("número", r.Numero, 20), w.num("CUIT", cuit, 20)
	valores := []string{
		strings.ReplaceAll(r.Fecha, "-", ""),
		tipo, pv, nro,
		strings.Repeat("0", 16), // despacho de importación: ceros (Especificaciones, campo 5)
		"80",                    // CUIT
		doc,
		alfa(r.Contraparte, 30),
		w.imp("total", r.Total),
		w.imp("no gravado", r.NoGravado),
		w.imp("exento", r.Exento),
		w.imp("percepciones de IVA", r.PercepcionIVA),
		w.imp("percepciones de otros impuestos nacionales", r.PercepcionNacionales),
		w.imp("percepciones de Ingresos Brutos", r.PercepcionIIBB),
		w.imp("percepciones municipales", r.PercepcionMunicipal),
		w.imp("impuestos internos", r.ImpuestosInternos),
		alfa(r.Moneda, 3),
		w.tc(r.Moneda, r.Cotizacion),
		strconv.Itoa(len(r.Alicuotas)),
		r.CodigoOperacion,
		w.imp("crédito fiscal", r.CreditoFiscal),
		w.imp("otros tributos", r.OtrosTributos),
		strings.Repeat("0", 11), // CUIT emisor/corredor: solo para 033, 058, 059, 060 y 063
		strings.Repeat(" ", 30), // denominación del emisor/corredor
		w.imp("IVA comisión", 0),
	}
	var vAlis [][]string
	for _, a := range r.Alicuotas {
		vAlis = append(vAlis, []string{tipo, pv, nro, "80", doc, w.imp("neto gravado", a.Neto), a.Codigo, w.imp("IVA", a.IVA)})
	}
	if err := w.err(); err != nil {
		return err
	}
	return r.escribir(CamposCbte, valores, CamposAlicuota, vAlis)
}

// escribir arma las líneas y las guarda en el registro.
func (r *Registro) escribir(cCbte []Campo, valores []string, cAli []Campo, vAlis [][]string) error {
	l, err := armar(cCbte, valores)
	if err != nil {
		return err
	}
	var alis []string
	for _, v := range vAlis {
		x, err := armar(cAli, v)
		if err != nil {
			return err
		}
		alis = append(alis, x)
	}
	r.lineaCbte, r.lineasAlicuota = l, alis
	return nil
}
