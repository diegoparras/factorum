package libroiva

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/modelo"
)

// Venta es un comprobante emitido por el cliente según Mis Comprobantes (lo trae Arcanum). Los
// importes van en la moneda del comprobante.
type Venta struct {
	Fecha       string
	Tipo        int
	PuntoVenta  int
	Numero      int64
	NumeroHasta int64 // en un rango (tiques agrupados por día); si no, igual a Numero
	CAE         string
	DocTipo     int // 80 CUIT, 86 CUIL, 96 DNI, 99 sin identificar...
	DocNro      string
	Receptor    string
	Moneda      string
	Cotizacion  float64
	NetoGravado modelo.Importe
	NoGravado   modelo.Importe
	Exento      modelo.Importe
	IVA         modelo.Importe
	// OtrosTributos nil: Mis Comprobantes no lo trajo (se deduce del total).
	OtrosTributos *modelo.Importe
	Total         modelo.Importe
	// Alicuotas: el detalle por alícuota, o nil si no hay. FuenteAlicuotas dice de dónde salió:
	// "mis_comprobantes" o "arcanum" (lo que Arcanum emitió por WSFEv1).
	Alicuotas        []AlicuotaVenta
	FuenteAlicuotas  string
	DesgloseDistinto bool
	// Tributos separados (solo los tiene la emisión propia de Arcanum); nil si no hay.
	Tributos     []TributoVenta
	FechaVtoPago string // AAAAMMDD, si se conoce
}

// AlicuotaVenta es el neto y el IVA de una alícuota (código de ARCA: 3, 4, 5, 6, 8, 9).
type AlicuotaVenta struct {
	Codigo int
	Neto   modelo.Importe
	IVA    modelo.Importe
}

// TributoVenta es un tributo de la factura electrónica (tabla de tributos de WSFEv1).
type TributoVenta struct {
	ID      int
	Importe modelo.Importe
}

// MinimoIdentificarConsumidorFinal es el importe desde el que una venta a consumidor final tiene
// que identificar al comprador: $ 10.000.000 según la RG 5700/2025 (Boletín Oficial del 27/05/2025,
// que cambió el Anexo II de la RG 1415). Las Especificaciones del Libro IVA Digital (revisión
// 30/07/2025) todavía dicen $ 20.932. Ver docs/libro-iva-digital.md.
const MinimoIdentificarConsumidorFinal modelo.Importe = 10_000_000_00

// campoTributo dice en qué campo del registro de ventas va cada tributo de WSFEv1 (códigos de
// FEParamGetTiposTributos). Lo que no está en la tabla va en «Otros tributos».
//
//	1 Impuestos nacionales, 6 Percepción de IVA, 9 Otras percepciones -> 13 (impuestos nacionales)
//	5 IIBB, 7 Percepción de IIBB                                      -> 14 (Ingresos Brutos)
//	3 Impuestos municipales, 8 Percepciones municipales               -> 15 (municipales)
//	4 Impuestos internos                                              -> 16 (impuestos internos)
//	13 Percepción de IVA a no categorizados                            -> 11 (no categorizados)
//	2 Impuestos provinciales, 99 Otros                                 -> 21 (otros tributos)
var campoTributo = map[int]int{1: 13, 6: 13, 9: 13, 5: 14, 7: 14, 3: 15, 8: 15, 4: 16, 13: 11}

func claveVenta(v *Venta) string {
	if v.Tipo == 0 || v.Numero == 0 {
		return ""
	}
	return fmt.Sprintf("%d-%d-%d", v.Tipo, v.PuntoVenta, v.Numero)
}

// docComprador decide el código y el número del comprador y el nombre que va en el registro.
// Consumidor final sin identificar (código 99 o sin número): número 0 y «CONSUMIDOR FINAL», o
// «VENTA GLOBAL DIARIA» si es un rango de comprobantes (Especificaciones, campos 6 a 8).
func docComprador(v *Venta) (int, string, string, bool) {
	nro := arca.SoloDigitos(v.DocNro)
	if strings.Trim(nro, "0") == "" {
		nro = ""
	}
	tipo := v.DocTipo
	if tipo == 0 || tipo == 99 || nro == "" {
		nombre := strings.TrimSpace(v.Receptor)
		if v.NumeroHasta > v.Numero {
			nombre = "VENTA GLOBAL DIARIA"
		} else if nombre == "" || strings.Contains(strings.ToUpper(Latin1(nombre)), "CONSUMIDOR") {
			nombre = "CONSUMIDOR FINAL"
		}
		return 99, "0", nombre, true
	}
	return tipo, nro, strings.TrimSpace(v.Receptor), false
}

func (o *Opciones) analizarVenta(v *Venta) *analisis {
	k := claveVenta(v)
	docTipo, docNro, nombre, cf := docComprador(v)
	hasta := v.NumeroHasta
	if hasta < v.Numero {
		hasta = v.Numero
	}
	clave := k
	if clave == "" {
		clave = fmt.Sprintf("venta:%s-%d-%d-%d", v.Fecha, v.Tipo, v.PuntoVenta, v.Numero)
	}
	a := &analisis{estado: modelo.EstadoOK, clave: k,
		base: Item{Clave: clave, Tipo: v.Tipo, NombreTipo: nombreTipo(v.Tipo), PuntoVenta: v.PuntoVenta, Numero: v.Numero, Fecha: v.Fecha,
			Doc: docNro, Contraparte: nombre, Total: v.Total, Moneda: v.Moneda, Estado: string(modelo.EstadoOK)}}
	tp, ok := arca.Tipo(v.Tipo)
	switch {
	case v.Tipo == 0:
		a.add("tipo_no_admitido", "No se sabe qué tipo de comprobante es.")
	case !ok:
		a.add("tipo_no_admitido", "El código %d no está entre los que Factorum conoce.", v.Tipo)
	case tp.Letra == "C":
		a.add("tipo_no_admitido", "%s: la emite un monotributista o un exento; el libro de ventas de IVA es de responsables inscriptos (los exentos tienen su propio diseño de registro).", tp.Nombre)
	case v.Tipo == 49 || v.Tipo == modelo.TipoDespacho:
		a.add("tipo_no_admitido", "%s no es una venta.", tp.Nombre)
	}
	if v.PuntoVenta <= 0 || v.PuntoVenta > 99999 {
		a.add("sin_numero", "Punto de venta %d.", v.PuntoVenta)
	} else if v.PuntoVenta >= 9998 {
		a.add("punto_venta_alto", "Punto de venta %05d.", v.PuntoVenta)
	}
	if v.Numero <= 0 {
		a.add("sin_numero", "Sin número de comprobante.")
	}
	for _, x := range []modelo.Importe{v.NetoGravado, v.NoGravado, v.Exento, v.IVA, v.Total} {
		if x < 0 {
			a.add("importe_negativo", "Tiene importes negativos.")
		}
	}
	if v.OtrosTributos != nil && *v.OtrosTributos < 0 {
		a.add("importe_negativo", "Otros tributos negativos.")
	}
	moneda, cot := a.moneda(v.Moneda, v.Cotizacion)

	// El comprador.
	switch {
	case cf:
		enPesos := v.Total
		if moneda != "PES" && cot > 0 {
			enPesos = modelo.DesdeFloat(v.Total.Float() * cot)
		}
		if enPesos >= MinimoIdentificarConsumidorFinal && v.NumeroHasta <= v.Numero {
			a.add("consumidor_final_identificar", "Total %s %s.", moneda, v.Total.AR())
		}
	case (docTipo == 80 || docTipo == 86) && !arca.CUITValido(docNro) && !arca.CUITPais(docNro):
		a.add("doc_comprador", "Código %d con el número %s.", docTipo, docNro)
	case docTipo == 96 && (len(docNro) < 6 || len(docNro) > 9):
		a.add("doc_comprador", "DNI %s.", docNro)
	}
	if !cf && strings.TrimSpace(Latin1(nombre)) == "" {
		a.add("sin_razon_social", "")
	}

	r := &Registro{Clave: clave, Tipo: v.Tipo, NombreTipo: nombreTipo(v.Tipo), PuntoVenta: v.PuntoVenta, Numero: v.Numero, NumeroHasta: hasta,
		Fecha: v.Fecha, DocTipo: docTipo, DocNro: docNro, Contraparte: nombre, Moneda: moneda, Cotizacion: cot, Discrimina: true,
		NotaCredito: ok && tp.Clase == arca.ClaseNotaCredito, NoGravado: v.NoGravado, Exento: v.Exento, IVA: v.IVA, Total: v.Total,
		CodigoOperacion: " ", Estado: string(modelo.EstadoOK)}
	a.reg = r
	a.periodo(v.Fecha, "", o.Periodo) // en ventas, el período es siempre el mes de la fecha

	if !a.tiene("tipo_no_admitido") {
		a.alicuotasVenta(v, ok && tp.Letra == "E")
		a.tributosVenta(v, ok && tp.Letra == "E")
	}
	if !a.bloqueado {
		if err := r.renderizarVenta(v.FechaVtoPago); err != nil {
			a.add("no_entra", "%s.", err)
		}
	}
	return a
}

// alicuotasVenta arma las alícuotas de una venta: el detalle de Mis Comprobantes o de Arcanum, o el
// que se puede deducir de los totales. En ventas también las B y los tiques llevan alícuotas: «aún
// tratándose de compradores consumidores finales, sujetos exentos y monotributistas igualmente
// deberá consignarse el IVA contenido» (Especificaciones, ventas alícuotas, campo 6).
func (a *analisis) alicuotasVenta(v *Venta, exportacion bool) {
	r := a.reg
	switch v.FuenteAlicuotas {
	case "arcanum":
		a.add("desglose_arcanum", "")
	}
	if v.DesgloseDistinto {
		a.add("desglose_distinto", "")
	}
	if exportacion {
		a.add("exportacion", "")
	}
	switch {
	case len(v.Alicuotas) > 0:
		var neto, iva modelo.Importe
		for _, x := range v.Alicuotas {
			t, ok := tasaDe(strconv.Itoa(x.Codigo))
			if !ok {
				a.add("alicuotas_no_cierran", "Alícuota con código %d desconocido.", x.Codigo)
				return
			}
			if x.Neto < 0 || x.IVA < 0 {
				a.add("importe_negativo", "Alícuota con importes negativos.")
			}
			r.Alicuotas = append(r.Alicuotas, Alicuota{Codigo: t.codigo, Tasa: t.nombre, Neto: x.Neto, IVA: x.IVA})
			neto += x.Neto
			iva += x.IVA
		}
		ordenarAlicuotas(r.Alicuotas)
		tol := tolerancia(neto, iva) + modelo.Importe(len(v.Alicuotas))
		if abs(neto-v.NetoGravado) > tol || abs(iva-v.IVA) > tol {
			a.add("alicuotas_no_cierran", "Las alícuotas suman neto %s e IVA %s; el comprobante dice %s y %s.", neto.AR(), iva.AR(), v.NetoGravado.AR(), v.IVA.AR())
			return
		}
		r.IVA = iva
	case v.NetoGravado == 0 && v.IVA == 0:
		if v.Exento == 0 && v.NoGravado == 0 && v.Total != 0 && !exportacion {
			a.add("sin_desglose", "Total %s sin neto, IVA, exento ni no gravado.", v.Total.AR())
			return
		}
		r.Alicuotas = []Alicuota{{Codigo: AlicuotaCero, Tasa: "0"}}
	case v.NetoGravado > 0 && v.IVA == 0:
		a.add("neto_sin_iva", "Neto gravado %s sin IVA.", v.NetoGravado.AR())
		return
	case v.NetoGravado == 0 && v.IVA > 0:
		a.add("iva_sin_neto", "IVA %s sin neto gravado.", v.IVA.AR())
		return
	default:
		// Una sola alícuota que explique el IVA: neto por la tasa, al centavo (con un margen chico
		// por el redondeo de cada ítem de la factura).
		var cand []tasa
		margen := max(2, modelo.Importe(int64(v.IVA)/2000))
		for _, t := range tasas {
			if abs(ivaDe(v.NetoGravado, t.bp)-v.IVA) <= margen {
				cand = append(cand, t)
			}
		}
		if len(cand) != 1 {
			a.add("sin_desglose", "Neto %s e IVA %s: no es una sola alícuota.", v.NetoGravado.AR(), v.IVA.AR())
			return
		}
		t := cand[0]
		r.Alicuotas = []Alicuota{{Codigo: t.codigo, Tasa: t.nombre, Neto: v.NetoGravado, IVA: v.IVA}}
		a.add("alicuota_deducida", "%s %%.", t.nombre)
	}

	// Código de operación: solo si todas las alícuotas son 0 %.
	cero := true
	for _, x := range r.Alicuotas {
		if x.Codigo != AlicuotaCero {
			cero = false
		}
	}
	if cero {
		switch {
		case exportacion:
			r.CodigoOperacion = "X"
		case v.Exento > 0:
			r.CodigoOperacion = "E"
		case v.NoGravado > 0:
			r.CodigoOperacion = "N"
		default:
			a.add("codigo_operacion", "Neto al 0 %% de %s sin exento ni no gravado.", r.NetoGravado().AR())
		}
	}
}

func ordenarAlicuotas(as []Alicuota) {
	for i := 1; i < len(as); i++ {
		for j := i; j > 0 && as[j].Codigo < as[j-1].Codigo; j-- {
			as[j], as[j-1] = as[j-1], as[j]
		}
	}
}

// tributosVenta reparte los otros tributos: si se conoce el detalle (emisión propia), cada uno en su
// campo; si no, todo en «Otros tributos». Si Mis Comprobantes no trajo los otros tributos, son lo
// que falta para llegar al total. Después controla que la suma dé el total.
func (a *analisis) tributosVenta(v *Venta, exportacion bool) {
	r := a.reg
	if a.bloqueado {
		return
	}
	base := r.NetoGravado() + v.NoGravado + v.Exento + r.IVA
	var otros modelo.Importe
	tol := tolerancia(r.NetoGravado(), v.NoGravado, v.Exento, r.IVA)
	if v.OtrosTributos != nil {
		otros = *v.OtrosTributos
	} else if resto := v.Total - base; resto > tol && !(exportacion && base == 0) {
		otros = resto
		a.add("otros_tributos_deducidos", "%s.", resto.AR())
	}
	if otros != 0 {
		tol++
	}
	if v.Tributos != nil {
		var suma modelo.Importe
		for _, t := range v.Tributos {
			suma += t.Importe
		}
		if suma == otros {
			for _, t := range v.Tributos {
				switch campoTributo[t.ID] {
				case 11:
					r.PercepcionNoCategorizados += t.Importe
				case 13:
					r.PercepcionNacionales += t.Importe
				case 14:
					r.PercepcionIIBB += t.Importe
				case 15:
					r.PercepcionMunicipal += t.Importe
				case 16:
					r.ImpuestosInternos += t.Importe
				}
			}
		}
	}
	r.OtrosTributos = otros - r.Percepciones()
	if r.OtrosTributos > 0 && v.OtrosTributos != nil {
		a.add("otros_tributos", "Otros tributos %s.", r.OtrosTributos.AR())
	}
	if exportacion && base+otros == 0 {
		return // una exportación que solo trae el total: va el total (ver «exportacion»)
	}
	a.totales(v.Total, base+otros, tol)
}

// renderizarVenta escribe el registro de ventas y sus alícuotas según el diseño de ARCA.
func (r *Registro) renderizarVenta(vtoPago string) error {
	if len(r.Alicuotas) > 9 {
		return fmt.Errorf("más de 9 alícuotas")
	}
	var w escritor
	doc, _ := strconv.ParseInt(r.DocNro, 10, 64)
	if len(r.DocNro) > 18 {
		return fmt.Errorf("documento del comprador demasiado largo")
	}
	vto := "00000000"
	if t, err := time.Parse("20060102", vtoPago); err == nil {
		vto = t.Format("20060102")
	}
	tipo, pv, nro := w.num("tipo", int64(r.Tipo), 3), w.num("punto de venta", int64(r.PuntoVenta), 5), w.num("número", r.Numero, 20)
	valores := []string{
		strings.ReplaceAll(r.Fecha, "-", ""),
		tipo, pv, nro,
		w.num("número hasta", r.NumeroHasta, 20),
		w.num("código de documento", int64(r.DocTipo), 2),
		w.num("documento del comprador", doc, 20),
		alfa(r.Contraparte, 30),
		w.imp("total", r.Total),
		w.imp("no gravado", r.NoGravado),
		w.imp("percepción a no categorizados", r.PercepcionNoCategorizados),
		w.imp("exento", r.Exento),
		w.imp("percepciones de impuestos nacionales", r.PercepcionNacionales),
		w.imp("percepciones de Ingresos Brutos", r.PercepcionIIBB),
		w.imp("percepciones municipales", r.PercepcionMunicipal),
		w.imp("impuestos internos", r.ImpuestosInternos),
		alfa(r.Moneda, 3),
		w.tc(r.Moneda, r.Cotizacion),
		strconv.Itoa(len(r.Alicuotas)),
		r.CodigoOperacion,
		w.imp("otros tributos", r.OtrosTributos),
		vto,
	}
	var vAlis [][]string
	for _, a := range r.Alicuotas {
		vAlis = append(vAlis, []string{tipo, pv, nro, w.imp("neto gravado", a.Neto), a.Codigo, w.imp("IVA", a.IVA)})
	}
	if err := w.err(); err != nil {
		return err
	}
	return r.escribir(CamposVentasCbte, valores, CamposVentasAlicuota, vAlis)
}

// GenerarVentas arma el libro de ventas del período con los emitidos del cliente según Mis
// Comprobantes. Los que no son del mes se informan como problema y no van.
func GenerarVentas(vs []Venta, o Opciones) (*Resultado, error) {
	res, err := iniciar(LibroVentas, len(vs), &o)
	if err != nil {
		return nil, err
	}
	as := make([]*analisis, len(vs))
	for i := range vs {
		as[i] = o.analizarVenta(&vs[i])
	}
	armarResultado(res, as, o)
	return res, nil
}
