package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/libroiva"
)

// bajarZip baja un ZIP del libro y devuelve el estado, los archivos y el nombre que propone.
func (e *entorno) bajarZip(t *testing.T, ruta string) (int, map[string][]byte, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+ruta, nil)
	req.Header.Set("X-API-Key", claveOp)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return resp.StatusCode, nil, string(b)
	}
	return 200, zipLibro(t, b), resp.Header.Get("Content-Disposition")
}

func (e *entorno) subir(t *testing.T, cliente string, archivos ...string) []map[string]any {
	t.Helper()
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cliente}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	id := int64(m["lote"].(map[string]any)["id"].(float64))
	e.esperarLote(t, id)
	return e.cbtes(t, fmt.Sprintf("lote=%d&limite=100", id))
}

func (e *entorno) corregirCbte(t *testing.T, id int64, campos string) (int, map[string]any) {
	t.Helper()
	st, m, _ := e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", id), claveOp, strings.NewReader(`{"campos":`+campos+`}`), "application/json")
	return st, m
}

func hallazgo(c map[string]any, codigo string) string {
	hs, _ := c["hallazgos"].([]any)
	for _, h := range hs {
		if x := h.(map[string]any); x["codigo"] == codigo {
			return x["mensaje"].(string)
		}
	}
	return ""
}

// Períodos del Libro IVA Digital de punta a punta: exportar registra y deja el mes presentado; lo
// que llega después va al primer período abierto, con su hallazgo; la papelera; el período a mano
// con su corrección; «cambió desde la presentación»; un despacho cargado a mano en importaciones;
// y las ventas de Arcanum.
func TestLibroIVAPeriodosVentasEImportaciones(t *testing.T) {
	e := armar(t)
	const cliente = "20222222223"
	ctx := context.Background()
	nov := e.subir(t, cliente, "pdf/01_FA_21.pdf", "pdf/05_NCA.pdf")
	if len(nov) != 2 || !strings.HasPrefix(nov[0]["fecha"].(string), "2026-11") {
		t.Fatalf("comprobantes de noviembre: %v", nov)
	}
	ruta := "/api/libro-iva?cliente=" + cliente + "&periodo=2026-11&conciliar=0"
	_, m, raw := e.pedir(t, "GET", ruta, claveOp, nil, "")
	if p := m["presentacion"].(map[string]any); p["estado"] != "abierto" || p["veces"].(float64) != 0 {
		t.Fatalf("antes de exportar, abierto: %s", raw)
	}
	todos := int(m["cantidad_todos"].(float64))

	// Exportar registra: queda presentado, con quién, los hashes y los comprobantes.
	st, fs, nombre := e.bajarZip(t, strings.Replace(ruta, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1))
	if st != 200 || !strings.Contains(nombre, "libro-iva-compras-") || len(lineasDe(t, fs[libroiva.NombreCbte], libroiva.LargoCbte)) != todos {
		t.Fatalf("zip de compras: %d %s", st, nombre)
	}
	var arch, cbs string
	var usuario string
	if err := e.app.Store.Pool.QueryRow(ctx, `SELECT archivos::text, comprobantes::text, usuario FROM libro_iva_exportaciones WHERE cliente_cuit=$1 AND periodo='2026-11' AND libro='compras'`, cliente).Scan(&arch, &cbs, &usuario); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(arch, libroiva.NombreCbte) || !strings.Contains(arch, `"sha256"`) || !strings.Contains(cbs, `"huella"`) || usuario == "" {
		t.Fatalf("exportación registrada: %s %s %q", arch, cbs, usuario)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	p := m["presentacion"].(map[string]any)
	if p["estado"] != "presentado" || p["cambio"] != false || p["ultima"].(map[string]any)["usuario"] == "" {
		t.Fatalf("presentado sin cambios: %v", p)
	}

	// Una factura de noviembre que llega después de presentar: va en diciembre, con el hallazgo.
	tarde := e.subir(t, cliente, "pdf/07_FA_dolares.pdf")[0]
	idTarde := int64(tarde["id"].(float64))
	if tarde["periodo_iva"] != "2026-12" || !strings.Contains(hallazgo(tarde, "llego_tarde"), "Llegó después de presentar noviembre de 2026: va en diciembre de 2026") {
		t.Fatalf("la que llegó tarde: %v %v", tarde["periodo_iva"], tarde["hallazgos"])
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	if int(m["cantidad_todos"].(float64)) != todos || m["presentacion"].(map[string]any)["cambio"] != false {
		t.Fatalf("noviembre no cambia: %v", m["presentacion"])
	}
	rutaDic := "/api/libro-iva?cliente=" + cliente + "&periodo=2026-12&conciliar=0"
	_, m, _ = e.pedir(t, "GET", rutaDic, claveOp, nil, "")
	if g := grupoLibro(m, "de_mes_anterior"); !idsGrupo(g)[idTarde] || m["resumen"].(map[string]any)["de_meses_anteriores"].(float64) != 1 ||
		m["presentacion"].(map[string]any)["estado"] != "abierto" {
		t.Fatalf("diciembre, entró de un mes anterior: %v", m["problemas"])
	}

	// Papelera: lo que se exportó en noviembre vuelve a noviembre; la que llegó tarde, a diciembre.
	fa := int64(nov[0]["id"].(float64))
	for _, id := range []int64{fa, idTarde} {
		if st, _, raw := e.pedir(t, "POST", "/api/comprobantes/borrar", claveOp, strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, id)), "application/json"); st != 200 {
			t.Fatalf("borrar: %d %s", st, raw)
		}
		if st, _, raw := e.pedir(t, "POST", "/api/papelera/restaurar", claveOp, strings.NewReader(fmt.Sprintf(`{"items":[{"tipo":"comprobante","id":%d}]}`, id)), "application/json"); st != 200 {
			t.Fatalf("restaurar: %d %s", st, raw)
		}
	}
	var pFa, pTarde string
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT periodo_iva FROM comprobantes WHERE id=$1`, fa).Scan(&pFa)
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT periodo_iva FROM comprobantes WHERE id=$1`, idTarde).Scan(&pTarde)
	if pFa != "2026-11" || pTarde != "2026-12" {
		t.Fatalf("después de la papelera: %s %s", pFa, pTarde)
	}

	// El período a mano: AAAA-MM, no antes de la fecha; la corrección queda registrada.
	if st, _ := e.corregirCbte(t, idTarde, `{"periodo_iva":"2026-13"}`); st != 400 {
		t.Fatalf("período inválido: %d", st)
	}
	if st, _ := e.corregirCbte(t, idTarde, `{"periodo_iva":"2026-10"}`); st != 400 {
		t.Fatalf("período anterior a la fecha: %d", st)
	}
	st, m = e.corregirCbte(t, idTarde, `{"periodo_iva":"2027-01"}`)
	c := m["comprobante"].(map[string]any)
	if st != 200 || c["periodo_iva"] != "2027-01" || origen(c, "periodo_iva") != "manual" || hallazgo(c, "llego_tarde") != "" {
		t.Fatalf("período a mano: %d %v", st, c)
	}
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/comprobantes/%d", idTarde), claveOp, nil, "")
	if ks := m["correcciones"].([]any); len(ks) != 1 || ks[0].(map[string]any)["campo"] != "periodo_iva" || ks[0].(map[string]any)["despues"] != "2027-01" {
		t.Fatalf("corrección del período: %v", m["correcciones"])
	}
	// Vacío vuelve al automático: noviembre está presentado, así que diciembre.
	if _, m = e.corregirCbte(t, idTarde, `{"periodo_iva":""}`); m["comprobante"].(map[string]any)["periodo_iva"] != "2026-12" {
		t.Fatalf("período automático: %v", m["comprobante"].(map[string]any)["periodo_iva"])
	}

	// Cambió desde la presentación: corregir el nombre del emisor de una de noviembre.
	if st, _ := e.corregirCbte(t, fa, `{"razon_social_emisor":"OTRO NOMBRE SA"}`); st != 200 {
		t.Fatalf("corregir: %d", st)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	p = m["presentacion"].(map[string]any)
	if p["cambio"] != true || len(p["cambios"].(map[string]any)["modificados"].([]any)) != 1 {
		t.Fatalf("cambió desde la presentación: %v", p)
	}
	// Volver a exportar se puede: dos veces y sin cambios.
	if st, _, _ := e.bajarZip(t, strings.Replace(ruta, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1)); st != 200 {
		t.Fatalf("volver a exportar: %d", st)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	if p := m["presentacion"].(map[string]any); p["veces"].(float64) != 2 || p["cambio"] != false {
		t.Fatalf("reexportado: %v", p)
	}

	// Un despacho cargado a mano: la nota de crédito pasa a ser un despacho de diciembre.
	nc := int64(nov[1]["id"].(float64))
	st, m = e.corregirCbte(t, nc, `{"tipo":"66","letra":"","despacho":"26 001 IC04 123456 K","fecha":"2026-12-03","cuit_emisor":"55000002206",`+
		`"razon_social_emisor":"SHENZHEN TRADING CO","neto_gravado":"100000","iva_21":"21000","iva_10_5":"0","iva_27":"0","exento":"0","no_gravado":"0",`+
		`"otros_tributos":"26000","percepcion_iva":"20000","percepcion_nacionales":"6000","total":"147000","moneda":"PES","cotizacion":"1"}`)
	if st != 200 || m["comprobante"].(map[string]any)["despacho"] != "26001IC04123456K" {
		t.Fatalf("despacho: %d %v", st, m)
	}
	if st, _ := e.corregirCbte(t, nc, `{"despacho":"123"}`); st != 400 {
		t.Fatalf("despacho corto: %d", st)
	}
	rutaImp := "/api/libro-iva?cliente=" + cliente + "&periodo=2026-12&libro=importaciones"
	_, m, raw = e.pedir(t, "GET", rutaImp+"&registros=1", claveOp, nil, "")
	if m["libro"] != "importaciones" || len(m["registros"].([]any)) != 1 || m["registros"].([]any)[0].(map[string]any)["despacho"] != "26001IC04123456K" {
		t.Fatalf("importaciones: %s", raw)
	}
	_, m, _ = e.pedir(t, "GET", rutaDic+"&registros=1", claveOp, nil, "")
	for _, r := range m["registros"].([]any) {
		if int64(r.(map[string]any)["id"].(float64)) == nc {
			t.Fatal("el despacho no va en compras")
		}
	}
	st, fs, nombre = e.bajarZip(t, strings.Replace(rutaImp, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1))
	if st != 200 || !strings.Contains(nombre, "libro-iva-importaciones-") {
		t.Fatalf("zip de importaciones: %d %s", st, nombre)
	}
	ic := lineasDe(t, fs[libroiva.NombreImportacionesCbte], libroiva.LargoCbte)
	ia := lineasDe(t, fs[libroiva.NombreImportacionesAlicuota], libroiva.LargoImportacionAlicuota)
	if len(ic) != 1 || len(ia) != 1 || ic[0][36:52] != "26001IC04123456K" || ia[0][:16] != "26001IC04123456K" || ic[0][149:164] != "000000002000000" {
		t.Fatalf("archivos de importaciones: %q %q", ic, ia)
	}

	// Ventas: sin Arcanum que las tenga (404), el aviso de la versión.
	rutaV := "/api/libro-iva?cliente=" + cliente + "&periodo=2026-11&libro=ventas"
	_, m, _ = e.pedir(t, "GET", rutaV, claveOp, nil, "")
	if f := m["fuente"].(map[string]any); f["estado"] != "arcanum_viejo" || !strings.Contains(f["mensaje"].(string), "2.4.0") {
		t.Fatalf("ventas con un Arcanum viejo: %v", f)
	}
	if st, _, _ := e.bajarZip(t, strings.Replace(rutaV, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1)); st != http.StatusConflict {
		t.Fatalf("zip de ventas sin Arcanum: %d", st)
	}
	emitidosFalsos.Store(func(cuit, d, h string) (int, any) {
		return 200, map[string]any{"ok": true, "cuit": cuit, "desde": d, "hasta": h, "total": 2, "tope": 20000, "truncado": false,
			"cobertura": map[string]any{"sincronizado": true, "cubre": false, "rangos": []any{map[string]any{"desde": "2026-01-01", "hasta": "2026-11-20"}},
				"faltan": []any{map[string]any{"desde": "2026-11-21", "hasta": "2026-11-30"}}},
			"comprobantes": []any{
				map[string]any{"fecha": "2026-11-05", "cbteTipo": 1, "puntoVenta": 3, "numeroDesde": 10, "numeroHasta": 10, "cae": "76000000000010", "docTipo": 80,
					"docNro": "30711111111", "receptor": "CLIENTE SA", "moneda": "PES", "tipoCambio": 1, "netoGravado": 1000, "noGravado": 0, "exento": 0,
					"iva": 210, "otrosTributos": 0, "total": 1210, "alicuotas": []any{map[string]any{"id": 5, "netoGravado": 1000, "iva": 210}},
					"fuenteAlicuotas": "mis_comprobantes", "desgloseDistinto": false, "tributos": nil, "emisionArcanum": false, "fechaVtoPago": nil},
				map[string]any{"fecha": "2026-11-06", "cbteTipo": 6, "puntoVenta": 3, "numeroDesde": 20, "numeroHasta": 20, "cae": "76000000000020", "docTipo": 99,
					"docNro": "0", "receptor": "", "moneda": "PES", "tipoCambio": 1, "netoGravado": 500, "noGravado": 0, "exento": 0,
					"iva": 105, "otrosTributos": nil, "total": 605, "alicuotas": nil, "fuenteAlicuotas": nil, "desgloseDistinto": false, "tributos": nil},
			}}
	})
	defer emitidosFalsos.Store((func(string, string, string) (int, any))(nil))
	_, m, raw = e.pedir(t, "GET", rutaV+"&registros=1", claveOp, nil, "")
	f := m["fuente"].(map[string]any)
	if f["estado"] != "lista" || f["cubre_todo"] != false || !strings.Contains(f["mensaje"].(string), "no bajó todos los emitidos") {
		t.Fatalf("ventas, cobertura parcial: %s", raw)
	}
	regs := m["registros"].([]any)
	if len(regs) != 2 || regs[1].(map[string]any)["contraparte"] != "CONSUMIDOR FINAL" || grupoLibro(m, "alicuota_deducida") == nil {
		t.Fatalf("ventas: %s", raw)
	}
	st, fs, nombre = e.bajarZip(t, strings.Replace(rutaV, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1))
	if st != 200 || !strings.Contains(nombre, "libro-iva-ventas-") {
		t.Fatalf("zip de ventas: %d %s", st, nombre)
	}
	vc := lineasDe(t, fs[libroiva.NombreVentasCbte], libroiva.LargoVentasCbte)
	va := lineasDe(t, fs[libroiva.NombreVentasAlicuotas], libroiva.LargoVentasAlicuota)
	if len(vc) != 2 || len(va) != 2 || vc[1][56:58] != "99" {
		t.Fatalf("archivos de ventas: %q %q", vc, va)
	}
	if !strings.Contains(string(fs[libroiva.NombreResumen]), "Libro IVA Digital: ventas") {
		t.Fatalf("resumen de ventas: %s", fs[libroiva.NombreResumen])
	}
	var nv int
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM libro_iva_exportaciones WHERE cliente_cuit=$1 AND libro='ventas'`, cliente).Scan(&nv)
	if nv != 1 {
		t.Fatalf("exportación de ventas: %d", nv)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/libro-iva?cliente="+cliente+"&periodo=2026-11&libro=otro", claveOp, nil, ""); st != 400 {
		t.Fatalf("libro desconocido: %d", st)
	}
	var aud int
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM auditoria WHERE accion='libro_iva.exportar' AND detalle ? 'archivos'`).Scan(&aud)
	if aud != 4 {
		t.Fatalf("auditoría con los hashes: %d", aud)
	}
}
