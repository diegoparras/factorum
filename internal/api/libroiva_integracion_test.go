package api_test

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/libroiva"
	"github.com/diegoparras/factorum/internal/store"
)

// zipLibro abre el ZIP del Libro IVA Digital y devuelve sus archivos.
func zipLibro(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("ZIP: %v (%q)", err, b)
	}
	res := map[string][]byte{}
	for _, f := range z.File {
		rc, _ := f.Open()
		x, _ := io.ReadAll(rc)
		rc.Close()
		res[f.Name] = x
	}
	return res
}

func lineasDe(t *testing.T, b []byte, largo int) []string {
	t.Helper()
	if len(b) == 0 {
		return nil
	}
	if !bytes.HasSuffix(b, []byte("\r\n")) {
		t.Fatal("tiene que terminar en CRLF")
	}
	ls := strings.Split(strings.TrimSuffix(string(b), "\r\n"), "\r\n")
	for i, l := range ls {
		if len(l) != largo {
			t.Fatalf("línea %d: %d posiciones, el diseño pide %d", i+1, len(l), largo)
		}
	}
	return ls
}

func grupoLibro(m map[string]any, codigo string) map[string]any {
	ps, _ := m["problemas"].([]any)
	for _, p := range ps {
		if g := p.(map[string]any); g["codigo"] == codigo {
			return g
		}
	}
	return nil
}

func idsGrupo(g map[string]any) map[int64]bool {
	res := map[int64]bool{}
	if g == nil {
		return res
	}
	for _, it := range g["items"].([]any) {
		res[int64(it.(map[string]any)["id"].(float64))] = true
	}
	return res
}

// Libro IVA Digital de punta a punta: facturas y tickets de verdad, los problemas, el ZIP con las
// longitudes del diseño, solo los listos, la papelera, la conciliación como aviso, el alcance por
// CUIT, los roles, la auditoría y MCP.
func TestLibroIVAPuntaAPunta(t *testing.T) {
	ocr := extractor.NuevoTesseract("spa", 60*time.Second)
	e := armarCon(t, ocr)
	const cliente = "20222222223"
	archivos := []string{"pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf", "pdf/03_FC_servicios.pdf", "pdf/05_NCA.pdf", "pdf/07_FA_dolares.pdf", "pdf/08_dos_facturas.pdf"}
	if ocr != nil {
		archivos = append(archivos, "tickets/04_parrilla_tfa.png", "tickets/02_ypf_tique.png", "tickets/01_super_tfb.png")
	}
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cliente, "nombre": "Compras noviembre"}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	e.esperarLote(t, int64(m["lote"].(map[string]any)["id"].(float64)))
	porArchivo := map[string][]map[string]any{}
	for _, c := range e.cbtes(t, "cliente="+cliente+"&limite=100") {
		porArchivo[c["archivo"].(string)] = append(porArchivo[c["archivo"].(string)], c)
	}
	idDe := func(archivo string, i int) int64 { return int64(porArchivo[archivo][i]["id"].(float64)) }
	ruta := "/api/libro-iva?cliente=" + cliente + "&periodo=2026-11"

	// Lectura no exporta: el Libro IVA es de usuario para arriba.
	if st, _, _ := e.pedir(t, "GET", ruta, claveLect, nil, ""); st != http.StatusForbidden {
		t.Fatalf("lectura: %d", st)
	}
	st, m, raw = e.pedir(t, "GET", ruta, claveOp, nil, "")
	if st != 200 || m["periodo"] != "2026-11" || m["desde"] != "2026-11-01" || m["hasta"] != "2026-11-30" {
		t.Fatalf("libro: %d %s", st, raw)
	}
	if m["registros"] != nil {
		t.Fatalf("sin registros=1 no vienen los registros")
	}
	if c := m["conciliacion"].(map[string]any); c["estado"] == "lista" || c["estado"] == "no_pedida" {
		t.Fatalf("sin Mis Comprobantes en el Arcanum falso no hay conciliación: %v", c)
	}
	// 02_FA_mixta: el IVA de 10,5 % no da con el neto: el neto no se puede repartir y no va.
	if g := grupoLibro(m, "alicuotas_no_cierran"); !idsGrupo(g)[idDe("02_FA_mixta.pdf", 0)] || g["nivel"] != "bloquea" {
		t.Fatalf("alícuotas que no cierran: %v", g)
	}
	// 08_dos_facturas trae otra vez la C y la nota de crédito: van una sola vez.
	if g := grupoLibro(m, "duplicado"); len(idsGrupo(g)) != 2 {
		t.Fatalf("duplicados: %v", g)
	}
	// La C es a nombre de otro CUIT; la de dólares va en su moneda.
	if g := grupoLibro(m, "otro_receptor"); !idsGrupo(g)[idDe("03_FC_servicios.pdf", 0)] {
		t.Fatalf("otro receptor: %v", g)
	}
	if g := grupoLibro(m, "moneda_extranjera"); !idsGrupo(g)[idDe("07_FA_dolares.pdf", 0)] {
		t.Fatalf("moneda extranjera: %v", g)
	}
	if !strings.Contains(m["importar"].(string), "Moneda Original") {
		t.Fatalf("instrucción para importar: %v", m["importar"])
	}
	// El fake de Arcanum marca como apócrifos al emisor de la C y al del súper: advertencia fuerte, van
	// con «todos», y cada uno figura una vez (la copia de la C que sobra solo figura como duplicada).
	if g := grupoLibro(m, "apocrifo"); len(idsGrupo(g)) != len(g["items"].([]any)) || !idsGrupo(g)[idDe("03_FC_servicios.pdf", 0)] || g["nivel"] != "fuerte" {
		t.Fatalf("apócrifo: %v", g)
	}
	todos := int(m["cantidad_todos"].(float64))
	listos := int(m["cantidad_listos"].(float64))
	res := m["resumen"].(map[string]any)
	if int(res["comprobantes"].(float64)) != todos || listos >= todos {
		t.Fatalf("cantidades: todos %d, listos %d, resumen %v", todos, listos, res)
	}

	// El ZIP: los dos archivos de ARCA con las longitudes del diseño, y el resumen.
	rutaZip := strings.Replace(ruta, "/api/libro-iva?", "/api/libro-iva/archivos.zip?", 1)
	req, _ := http.NewRequest("GET", e.srv.URL+rutaZip, nil)
	req.Header.Set("X-API-Key", claveOp)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	zb, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/zip" ||
		!strings.Contains(resp.Header.Get("Content-Disposition"), "libro-iva-compras-"+cliente+"-2026-11.zip") {
		t.Fatalf("zip: %d %v %s", resp.StatusCode, resp.Header, zb)
	}
	fs := zipLibro(t, zb)
	cbs := lineasDe(t, fs[libroiva.NombreCbte], libroiva.LargoCbte)
	alis := lineasDe(t, fs[libroiva.NombreAlicuotas], libroiva.LargoAlicuota)
	if len(cbs) != todos || len(alis) != int(res["registros_alicuotas"].(float64)) {
		t.Fatalf("registros: %d comprobantes (esperaba %d), %d alícuotas", len(cbs), todos, len(alis))
	}
	if !strings.Contains(string(fs[libroiva.NombreResumen]), "Libro IVA Digital: compras") {
		t.Fatalf("resumen.csv: %s", fs[libroiva.NombreResumen])
	}
	// Ninguna línea es de la mixta (bloqueada).
	mixta := porArchivo["02_FA_mixta.pdf"][0]
	for _, l := range cbs {
		if l[8:11] == "001" && strings.TrimLeft(l[16:36], "0") == fmt.Sprint(mixta["numero"]) {
			t.Fatalf("la mixta no tiene que ir: %q", l)
		}
	}

	// Solo los listos: menos comprobantes.
	st, m, _ = e.pedir(t, "GET", ruta+"&solo_aprobados=1", claveOp, nil, "")
	if st != 200 || int(m["resumen"].(map[string]any)["comprobantes"].(float64)) != listos || m["solo_aprobados"] != true {
		t.Fatalf("solo los listos: %d %v", st, m["resumen"])
	}
	req, _ = http.NewRequest("GET", e.srv.URL+rutaZip+"&solo_aprobados=1", nil)
	req.Header.Set("X-API-Key", claveOp)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	zb, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || len(lineasDe(t, zipLibro(t, zb)[libroiva.NombreCbte], libroiva.LargoCbte)) != listos {
		t.Fatalf("zip de los listos: %d", resp.StatusCode)
	}

	// Aprobar la de dólares la pasa a lista.
	dol := idDe("07_FA_dolares.pdf", 0)
	if st, _, raw := e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", dol), claveOp, strings.NewReader(`{"aprobar":true}`), "application/json"); st != 200 {
		t.Fatalf("aprobar: %d %s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", ruta+"&registros=1", claveOp, nil, "")
	if int(m["cantidad_listos"].(float64)) != listos+1 {
		t.Fatalf("aprobada, va con los listos: %v (antes %d)", m["cantidad_listos"], listos)
	}
	if regs, _ := m["registros"].([]any); len(regs) != todos {
		t.Fatalf("con registros=1 vienen los registros: %d", len(regs))
	}

	// Lo que está en la papelera no sale.
	fa := idDe("01_FA_21.pdf", 0)
	if st, _, raw := e.pedir(t, "POST", "/api/comprobantes/borrar", claveOp, strings.NewReader(fmt.Sprintf(`{"ids":[%d]}`, fa)), "application/json"); st != 200 {
		t.Fatalf("borrar: %d %s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", ruta+"&registros=1", claveOp, nil, "")
	if int(m["cantidad_todos"].(float64)) != todos-1 {
		t.Fatalf("con la factura en la papelera: %v", m["cantidad_todos"])
	}
	for _, r := range m["registros"].([]any) {
		if int64(r.(map[string]any)["id"].(float64)) == fa {
			t.Fatal("lo borrado no va al libro")
		}
	}

	// Sin fecha: aparece como problema y no va.
	if _, err := e.app.Store.Pool.Exec(context.Background(), `UPDATE comprobantes SET fecha = NULL, datos = jsonb_set(datos, '{fecha}', '""') WHERE id = $1`, dol); err != nil {
		t.Fatal(err)
	}
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	if g := grupoLibro(m, "sin_fecha"); !idsGrupo(g)[dol] {
		t.Fatalf("sin fecha: %v", m["problemas"])
	}

	// La conciliación avisa (no saca nada): la de 10,5 % del 08 no figura en ARCA.
	recibidosFalsos.Store(func(cuit, d, h string) (int, any) {
		return 200, map[string]any{"ok": true, "cuit": cuit, "desde": d, "hasta": h, "total": 0, "tope": 20000, "truncado": false,
			"cobertura": map[string]any{"sincronizado": true, "cubre": true, "rangos": []any{map[string]any{"desde": "2026-01-01", "hasta": "2026-12-31"}}, "faltan": []any{},
				"ultimaSincronizacion": map[string]any{"corrida": 1, "terminada": "2026-11-26T10:00:00.000Z", "desde": "2026-01-01", "hasta": "2026-12-31", "cantidad": 0}},
			"comprobantes": []any{}}
	})
	defer recibidosFalsos.Store((func(string, string, string) (int, any))(nil))
	_, m, _ = e.pedir(t, "GET", ruta, claveOp, nil, "")
	if c := m["conciliacion"].(map[string]any); c["estado"] != "lista" {
		t.Fatalf("conciliación: %v", c)
	}
	nf := grupoLibro(m, "no_figura_arca")
	if nf == nil || nf["nivel"] != "advertencia" || len(idsGrupo(nf)) == 0 {
		t.Fatalf("no figura en ARCA: %v", m["problemas"])
	}
	if int(m["cantidad_todos"].(float64)) != todos-2 {
		t.Fatalf("la conciliación no saca nada del archivo: %v", m["cantidad_todos"])
	}
	_, m, _ = e.pedir(t, "GET", ruta+"&conciliar=0", claveOp, nil, "")
	if c := m["conciliacion"].(map[string]any); c["estado"] != "no_pedida" || grupoLibro(m, "no_figura_arca") != nil {
		t.Fatalf("sin conciliar: %v", c)
	}

	// Validaciones.
	for q, want := range map[string]int{
		"periodo=2026-11":                         400, // sin cliente, con una clave que ve todos
		"cliente=" + cliente + "&periodo=":        400,
		"cliente=" + cliente + "&periodo=13":      400,
		"cliente=" + cliente + "&periodo=2026-13": 400,
		"cliente=123&periodo=2026-11":             400,
	} {
		if st, _, raw := e.pedir(t, "GET", "/api/libro-iva?"+q, claveOp, nil, ""); st != want {
			t.Errorf("%s: %d %s", q, st, raw)
		}
	}
	// Un mes sin nada: el resumen en cero y el ZIP no se arma.
	st, m, _ = e.pedir(t, "GET", "/api/libro-iva?cliente="+cliente+"&periodo=2026-01&conciliar=0", claveOp, nil, "")
	if st != 200 || m["resumen"].(map[string]any)["comprobantes"].(float64) != 0 {
		t.Fatalf("mes vacío: %d %v", st, m)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/libro-iva/archivos.zip?cliente="+cliente+"&periodo=2026-01", claveOp, nil, ""); st != http.StatusConflict {
		t.Fatalf("zip de un mes vacío: %d", st)
	}

	// Alcance por CUIT: una clave de usuario de otro cliente no ve este; sin cliente, usa el suyo.
	k, pref := auth.NuevaClaveAPI()
	if _, err := e.app.Store.CrearClave(context.Background(), store.ClaveNueva{Nombre: "otro", Prefijo: pref, Hash: auth.HashToken(k), Rol: auth.Usuario, CUITs: []string{"30722222226"}}); err != nil {
		t.Fatal(err)
	}
	if st, _, _ := e.pedir(t, "GET", ruta, k, nil, ""); st != http.StatusForbidden {
		t.Fatalf("otro cliente: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", rutaZip, k, nil, ""); st != http.StatusForbidden {
		t.Fatalf("zip de otro cliente: %d", st)
	}
	if st, m, _ := e.pedir(t, "GET", "/api/libro-iva?periodo=2026-11&conciliar=0", k, nil, ""); st != 200 || m["cliente"] != "30722222226" || m["resumen"].(map[string]any)["comprobantes"].(float64) != 0 {
		t.Fatalf("su único cliente: %d %v", st, m)
	}

	// Auditoría: una por ZIP bajado.
	var n int
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria WHERE accion = 'libro_iva.exportar' AND detalle->>'periodo' = '2026-11'`).Scan(&n)
	if n != 2 {
		t.Fatalf("auditoría: %d", n)
	}

	// MCP: libro_iva_resumen con una clave de usuario; la de lectura no la tiene.
	mcpCon := func(clave, cuerpo string) string {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+clave)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return string(b)
	}
	out := mcpCon(claveOp, fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"libro_iva_resumen","arguments":{"cliente":"%s","periodo":"2026-11","limite":1}}}`, cliente))
	if !strings.Contains(out, `\"periodo\":\"2026-11\"`) || !strings.Contains(out, `\"credito_fiscal\"`) || !strings.Contains(out, `\"problemas\"`) || strings.Contains(out, "LIBRO_IVA_DIGITAL") {
		t.Fatalf("MCP libro_iva_resumen: %s", out)
	}
	if out := mcpCon(claveLect, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); strings.Contains(out, "libro_iva_resumen") || !strings.Contains(out, "conciliar") {
		t.Fatalf("lectura no tiene libro_iva_resumen: %s", out)
	}
}
