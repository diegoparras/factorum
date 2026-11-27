package libroiva

import (
	"errors"
	"strconv"
	"strings"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Importación definitiva de bienes: «no se registrará la Factura del exterior, sino el Despacho de
// Importación» (Especificaciones, consideraciones particulares). Cada despacho (tipo 066) va con el
// diseño «Compras e Importación de Bienes - Cabecera» (325) —fecha de oficialización, punto de venta
// y número en cero, el número de despacho en el campo 5— y sus alícuotas con el diseño
// «Importaciones de Bienes - Alícuotas» (50), que se identifican por el despacho.

var (
	errMasDe9   = errors.New("más de 9 alícuotas")
	errDespacho = errors.New("el número de despacho no tiene 16 caracteres")
)

func (o *Opciones) analizarDespacho(e *Entrada) *analisis {
	c := &e.C
	desp := arca.NormalizarDespacho(c.Despacho)
	clave := ""
	if desp != "" {
		clave = "despacho:" + desp
	}
	a := &analisis{id: e.ID, estado: e.Estado, clave: clave, base: baseCompra(e, clave)}
	a.base.Despacho = desp
	if c.Tipo != modelo.TipoDespacho {
		a.add("tipo_no_admitido", "%s: en importaciones van solo los despachos de importación (066).", nombreTipo(c.Tipo))
	}
	switch {
	case desp == "":
		a.add("sin_despacho", "")
	case !arca.DespachoValido(desp):
		a.add("despacho_invalido", "%q.", desp)
	default:
		if _, _, _, _, _, ok := arca.PartesDespacho(desp); !ok {
			a.add("despacho_forma", "%s.", desp)
		}
	}
	cuit := arca.SoloDigitos(c.CUITEmisor)
	if !arca.CUITValido(cuit) && !arca.CUITPais(cuit) {
		if cuit == "" {
			a.add("sin_cuit_emisor", "Falta la identificación del proveedor del exterior: código 80 con la CUIT país de la tabla de ARCA.")
		} else {
			a.add("sin_cuit_emisor", "El CUIT %s no es válido.", arca.FormatearCUIT(cuit))
		}
	}
	a.importesBasicos(c)
	moneda, cot := a.moneda(c.Moneda, c.Cotizacion)
	r := &Registro{ID: e.ID, Clave: a.base.Clave, Tipo: c.Tipo, NombreTipo: nombreTipo(c.Tipo), Despacho: desp, Fecha: c.Fecha,
		CUITEmisor: cuit, Emisor: c.RazonSocialEmisor, DocTipo: 80, DocNro: cuit, Contraparte: c.RazonSocialEmisor, Moneda: moneda, Cotizacion: cot,
		Discrimina: true, NoGravado: c.NoGravado, Exento: c.Exento, Total: c.Total, CodigoOperacion: " ", Estado: string(e.Estado), Archivo: e.Archivo}
	a.reg = r
	a.periodo(c.Fecha, c.PeriodoIVA, o.Periodo)
	if a.alicuotasA(c) {
		// Sin IVA: alícuota 0 % con el código de operación de importación del exterior.
		r.CodigoOperacion = "X"
	}
	if c.Total > 0 {
		a.totales(c.Total, c.SumaComponentes(), toleranciaCbte(c))
	}
	a.percepciones(c)
	a.constatacion(e, o.Conciliacion)
	if !a.bloqueado {
		if err := r.renderizarDespacho(); err != nil {
			a.add("no_entra", "%s.", err)
		}
	}
	return a
}

// renderizarDespacho escribe el registro de cabecera (diseño de compras) y las alícuotas de
// importaciones.
func (r *Registro) renderizarDespacho() error {
	if len(r.Alicuotas) > 9 {
		return errMasDe9
	}
	if len(r.Despacho) != 16 {
		return errDespacho
	}
	var w escritor
	cuit, _ := strconv.ParseInt(r.DocNro, 10, 64)
	valores := []string{
		strings.ReplaceAll(r.Fecha, "-", ""), // fecha de oficialización
		w.num("tipo", int64(r.Tipo), 3),
		strings.Repeat("0", 5),  // punto de venta: cero en un documento aduanero (Especificaciones, campo 3)
		strings.Repeat("0", 20), // número: cero en un documento aduanero (campo 4)
		r.Despacho,
		"80",
		w.num("CUIT", cuit, 20),
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
		strings.Repeat("0", 11),
		strings.Repeat(" ", 30),
		w.imp("IVA comisión", 0),
	}
	var vAlis [][]string
	for _, a := range r.Alicuotas {
		vAlis = append(vAlis, []string{r.Despacho, w.imp("neto gravado", a.Neto), a.Codigo, w.imp("IVA", a.IVA)})
	}
	if err := w.err(); err != nil {
		return err
	}
	return r.escribir(CamposCbte, valores, CamposImportacionAlicuota, vAlis)
}

// GenerarImportaciones arma el libro de importaciones de bienes del período con los despachos del
// cliente.
func GenerarImportaciones(es []Entrada, o Opciones) (*Resultado, error) {
	res, err := iniciar(LibroImportaciones, len(es), &o)
	if err != nil {
		return nil, err
	}
	as := make([]*analisis, len(es))
	for i := range es {
		as[i] = o.analizarDespacho(&es[i])
	}
	armarResultado(res, as, o)
	return res, nil
}
