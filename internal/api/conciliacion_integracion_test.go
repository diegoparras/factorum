package api_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/store"
)

func numero(v any) float64 { f, _ := v.(float64); return f }

func fechaMas(f string, n int) string {
	t, _ := time.Parse("2006-01-02", f)
	return t.AddDate(0, 0, n).Format("2006-01-02")
}

// Conciliación de punta a punta: facturas de verdad cargadas en Factorum, Mis Comprobantes del
// Arcanum falso armado a partir de lo leído (una igual, una con otro total, una que falta cargar),
// la API con su caché, el alcance por CUIT, los estados sin Arcanum o sin sincronizar, el Excel, el
// CSV y la herramienta MCP.
func TestConciliacionPuntaAPunta(t *testing.T) {
	e := armar(t)
	const cliente = "20222222223"
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cliente, "constatar": "true", "nombre": "Compras"},
		"pdf/01_FA_21.pdf", "pdf/05_NCA.pdf", "pdf/03_FC_servicios.pdf", "pdf/07_FA_dolares.pdf")
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	e.esperarLote(t, int64(m["lote"].(map[string]any)["id"].(float64)))
	cbs := e.cbtes(t, "cliente="+cliente+"&limite=100")
	if len(cbs) != 4 {
		t.Fatalf("esperaba 4 comprobantes: %d", len(cbs))
	}
	porArchivo := map[string]map[string]any{}
	var fechas []string
	for _, c := range cbs {
		porArchivo[c["archivo"].(string)] = c
		fechas = append(fechas, c["fecha"].(string))
	}
	sort.Strings(fechas)
	desde, hasta := fechas[0], fechas[len(fechas)-1]
	fila := func(c map[string]any, total float64) map[string]any {
		return map[string]any{"fecha": c["fecha"], "cbteTipo": c["tipo"], "puntoVenta": c["punto_venta"], "numeroDesde": c["numero"], "numeroHasta": c["numero"],
			"cae": c["cae"], "docTipo": 80, "cuitEmisor": c["cuit_emisor"], "emisor": "DESDE ARCA SA", "moneda": "PES", "tipoCambio": 1,
			"netoGravado": c["neto_gravado"], "noGravado": 0, "exento": 0, "iva": numero(c["iva_21"]) + numero(c["iva_10_5"]) + numero(c["iva_27"]), "total": total, "emisorApocrifo": nil}
	}
	fa, nc := porArchivo["01_FA_21.pdf"], porArchivo["05_NCA.pdf"]
	falta := map[string]any{"fecha": desde, "cbteTipo": 1, "puntoVenta": 9, "numeroDesde": 999, "numeroHasta": 999, "cae": "76000000000009", "docTipo": 80,
		"cuitEmisor": "30500000001", "emisor": "NO MANDADA SA", "moneda": "PES", "tipoCambio": 1, "netoGravado": 1000, "noGravado": 0, "exento": 0, "iva": 210, "total": 1210, "emisorApocrifo": nil}
	pedidos := map[string]string{}
	recibidosFalsos.Store(func(cuit, d, h string) (int, any) {
		pedidos[cuit] = d + "|" + h
		switch cuit {
		case "30711111118":
			return 403, map[string]any{"ok": false, "error": "No autorizado para ese CUIT"}
		case "30722222226":
			return 200, map[string]any{"ok": true, "cuit": cuit, "desde": d, "hasta": h, "total": 0, "tope": 20000, "truncado": false,
				"cobertura": map[string]any{"sincronizado": false, "cubre": false, "rangos": []any{}, "faltan": []any{map[string]any{"desde": d, "hasta": h}}}, "comprobantes": []any{}}
		}
		return 200, map[string]any{"ok": true, "cuit": cuit, "desde": d, "hasta": h, "total": 3, "tope": 20000, "truncado": false,
			"cobertura": map[string]any{"sincronizado": true, "cubre": true, "rangos": []any{map[string]any{"desde": fechaMas(desde, -90), "hasta": fechaMas(hasta, 60)}}, "faltan": []any{},
				"ultimaSincronizacion": map[string]any{"corrida": 7, "terminada": "2026-11-26T10:00:00.000Z", "desde": fechaMas(desde, -90), "hasta": fechaMas(hasta, 60), "cantidad": 3}},
			"comprobantes": []any{fila(fa, numero(fa["total"])), fila(nc, numero(nc["total"])+100), falta}}
	})
	defer recibidosFalsos.Store((func(string, string, string) (int, any))(nil))
	recibidosLlamadas.Store(0)

	ruta := fmt.Sprintf("/api/conciliacion?cliente=%s&desde=%s&hasta=%s", cliente, desde, hasta)
	st, m, raw = e.pedir(t, "GET", ruta, claveLect, nil, "")
	if st != 200 || m["estado"] != "lista" || m["cubre_todo"] != true {
		t.Fatalf("conciliación: %d %s", st, raw)
	}
	if pedidos[cliente] != fechaMas(desde, -31)+"|"+fechaMas(hasta, 31) {
		t.Fatalf("a Arcanum se le pide el período con margen: %q", pedidos[cliente])
	}
	grupos := m["resultado"].(map[string]any)["grupos"].(map[string]any)
	lista := func(g string) []map[string]any {
		var out []map[string]any
		for _, x := range grupos[g].([]any) {
			out = append(out, x.(map[string]any))
		}
		return out
	}
	idDe := func(it map[string]any) float64 { return numero(it["factorum"].(map[string]any)["id"]) }
	if co := lista("coinciden"); len(co) != 1 || idDe(co[0]) != numero(fa["id"]) {
		t.Fatalf("coinciden: %v", co)
	}
	if di := lista("diferencias"); len(di) != 1 || idDe(di[0]) != numero(nc["id"]) || di[0]["diferencias"].([]any)[0].(map[string]any)["campo"] != "total" {
		t.Fatalf("diferencias: %v", di)
	}
	if fc := lista("falta_cargar"); len(fc) != 1 || numero(fc[0]["numero"]) != 999 || fc[0]["factorum"] != nil {
		t.Fatalf("falta cargar: %v", fc)
	}
	nf := lista("no_figura")
	if len(nf) != 2 {
		t.Fatalf("no figura: %v", nf)
	}
	for _, it := range nf {
		id := idDe(it)
		if id == numero(porArchivo["07_FA_dolares.pdf"]["id"]) && (it["causa"] != "exterior" || it["legitima"] != true) {
			t.Errorf("la de dólares tiene que ser del exterior: %v", it)
		}
	}
	tot := m["resultado"].(map[string]any)["totales"].(map[string]any)
	if numero(tot["no_figura"].(map[string]any)["cantidad_moneda_extranjera"]) != 1 {
		t.Errorf("la moneda extranjera va aparte: %v", tot["no_figura"])
	}

	// Caché: el segundo pedido no vuelve a Arcanum; refrescar sí.
	_, m, _ = e.pedir(t, "GET", ruta, claveLect, nil, "")
	if m["de_cache"] != true || recibidosLlamadas.Load() != 1 {
		t.Fatalf("caché: %v, llamadas %d", m["de_cache"], recibidosLlamadas.Load())
	}
	_, m, _ = e.pedir(t, "GET", ruta+"&refrescar=1", claveLect, nil, "")
	if m["de_cache"] != false || recibidosLlamadas.Load() != 2 {
		t.Fatalf("refrescar: %v, llamadas %d", m["de_cache"], recibidosLlamadas.Load())
	}

	// Lo que está en la papelera no cuenta: la que coincidía pasa a «Falta cargar».
	st, _, raw = e.pedir(t, "POST", "/api/comprobantes/borrar", claveOp, strings.NewReader(fmt.Sprintf(`{"ids":[%v]}`, fa["id"])), "application/json")
	if st != 200 {
		t.Fatalf("borrar: %d %s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveLect, nil, "")
	grupos = m["resultado"].(map[string]any)["grupos"].(map[string]any)
	if co, fc := lista("coinciden"), lista("falta_cargar"); len(co) != 0 || len(fc) != 2 {
		t.Fatalf("con la factura en la papelera: coinciden %v, falta cargar %v", co, fc)
	}
	if st, _, raw := e.pedir(t, "POST", "/api/papelera/restaurar", claveOp, strings.NewReader(fmt.Sprintf(`{"items":[{"tipo":"comprobante","id":%v}]}`, fa["id"])), "application/json"); st != 200 {
		t.Fatalf("restaurar: %d %s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveLect, nil, "")
	grupos = m["resultado"].(map[string]any)["grupos"].(map[string]any)
	if co := lista("coinciden"); len(co) != 1 {
		t.Fatalf("restaurada, vuelve a coincidir: %v", co)
	}

	// Validaciones.
	for q, want := range map[string]int{
		"desde=" + desde + "&hasta=" + hasta:                        400, // sin cliente, con una clave que ve todos
		"cliente=" + cliente + "&desde=2026-12-01&hasta=2026-11-01": 400,
		"cliente=" + cliente + "&desde=2025-01-01&hasta=2026-11-01": 400,
		"cliente=" + cliente + "&desde=01/11/2026&hasta=2026-11-30": 400,
		"cliente=123&desde=2026-11-01&hasta=2026-11-30":             400,
	} {
		if st, _, raw := e.pedir(t, "GET", "/api/conciliacion?"+q, claveLect, nil, ""); st != want {
			t.Errorf("%s: %d %s", q, st, raw)
		}
	}

	// Estados con mensaje claro: la clave de Arcanum sin ese CUIT, y un CUIT sin sincronizar.
	_, m, _ = e.pedir(t, "GET", "/api/conciliacion?cliente=30711111118&desde=2026-11-01&hasta=2026-11-30", claveLect, nil, "")
	if m["estado"] != "sin_permiso" || !strings.Contains(m["mensaje"].(string), "30-71111111-8") || m["resultado"] != nil {
		t.Fatalf("sin permiso: %v", m)
	}
	_, m, _ = e.pedir(t, "GET", "/api/conciliacion?cliente=30722222226&desde=2026-11-01&hasta=2026-11-30", claveLect, nil, "")
	if m["estado"] != "sin_sincronizar" || !strings.Contains(m["mensaje"].(string), "sincronizá") {
		t.Fatalf("sin sincronizar: %v", m)
	}

	// Alcance por CUIT: una clave de lectura con otro cliente no ve este; sin cliente, usa el suyo.
	k, pref := auth.NuevaClaveAPI()
	if _, err := e.app.Store.CrearClave(context.Background(), store.ClaveNueva{Nombre: "solo uno", Prefijo: pref, Hash: auth.HashToken(k), Rol: auth.Lectura, CUITs: []string{"30722222226"}}); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := e.pedir(t, "GET", ruta, k, nil, ""); st != 403 {
		t.Fatalf("otro cliente: %d", st)
	}
	if st, m, _ := e.pedir(t, "GET", "/api/conciliacion?desde=2026-11-01&hasta=2026-11-30", k, nil, ""); st != 200 || m["cliente"] != "30722222226" {
		t.Fatalf("su único cliente: %d %v", st, m)
	}
	_, m, _ = e.pedir(t, "GET", "/api/conciliacion/clientes", k, nil, "")
	if cs := m["clientes"].([]any); len(cs) != 1 || cs[0].(map[string]any)["cuit"] != "30722222226" {
		t.Fatalf("clientes de la clave acotada: %v", m)
	}
	_, m, _ = e.pedir(t, "GET", "/api/conciliacion/clientes", claveLect, nil, "")
	if cs := m["clientes"].([]any); len(cs) != 1 || cs[0].(map[string]any)["cuit"] != cliente || numero(cs[0].(map[string]any)["comprobantes"]) != 4 {
		t.Fatalf("clientes: %v", m)
	}

	// Excel y CSV.
	st, _, xlsx := e.pedir(t, "GET", strings.Replace(ruta, "/api/conciliacion?", "/api/conciliacion.xlsx?", 1), claveLect, nil, "")
	if st != 200 {
		t.Fatalf("excel: %d %s", st, xlsx)
	}
	x, err := excelize.OpenReader(bytes.NewReader(xlsx))
	if err != nil {
		t.Fatal(err)
	}
	if hojas := strings.Join(x.GetSheetList(), "|"); hojas != "Resumen|Coinciden|Con diferencias|Falta cargar|No figura en ARCA" {
		t.Fatalf("hojas: %s", hojas)
	}
	falt, _ := x.GetRows("Falta cargar")
	if len(falt) != 2 || !strings.Contains(strings.Join(falt[1], "|"), "NO MANDADA SA") {
		t.Fatalf("falta cargar en el Excel: %v", falt)
	}
	st, _, csv := e.pedir(t, "GET", strings.Replace(ruta, "/api/conciliacion?", "/api/conciliacion.csv?", 1)+"&grupo=falta_cargar", claveLect, nil, "")
	if st != 200 || strings.Count(string(csv), "\n") != 2 || !strings.Contains(string(csv), ";999;") {
		t.Fatalf("csv de falta cargar: %d %s", st, csv)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/conciliacion.xlsx?cliente=30722222226&desde=2026-11-01&hasta=2026-11-30", claveLect, nil, ""); st != http.StatusConflict {
		t.Fatalf("exportar sin sincronizar: %d", st)
	}
	var n int
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria WHERE accion = 'conciliacion.exportar'`).Scan(&n)
	if n != 2 {
		t.Fatalf("auditoría de exportaciones: %d", n)
	}

	// MCP: la herramienta conciliar, recortada por grupo.
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"conciliar","arguments":{"cliente":"%s","desde":"%s","hasta":"%s","grupo":"falta_cargar"}}}`, cliente, desde, hasta)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+claveLect)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, _ = out.ReadFrom(res.Body)
	res.Body.Close()
	s := out.String()
	if !strings.Contains(s, `\"grupos\":{\"falta_cargar\":{\"cantidad\":1`) || !strings.Contains(s, "NO MANDADA SA") || !strings.Contains(s, `\"totales\"`) {
		t.Fatalf("MCP conciliar: %s", s)
	}

	// Sin Arcanum: mensaje claro, sin error.
	e.app.SetArcanum(nil)
	_, m, _ = e.pedir(t, "GET", ruta, claveLect, nil, "")
	if m["estado"] != "sin_arcanum" || !strings.Contains(m["mensaje"].(string), "Arcanum") || numero(m["en_factorum"]) != 4 {
		t.Fatalf("sin arcanum: %v", m)
	}
}
