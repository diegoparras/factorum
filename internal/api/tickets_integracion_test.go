package api_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/extractor"
)

func (e *entorno) esperarLote(t *testing.T, id int64) {
	t.Helper()
	limite := time.Now().Add(120 * time.Second)
	for {
		_, m, _ := e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", id), claveLect, nil, "")
		l := m["lote"].(map[string]any)
		if l["estado"] == "terminado" {
			var pend int
			_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM comprobantes WHERE lote_id=$1 AND constatacion IS NULL`, id).Scan(&pend)
			if pend == 0 {
				return
			}
		}
		if time.Now().After(limite) {
			t.Fatalf("el lote no terminó: %v", l)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (e *entorno) cbtes(t *testing.T, query string) []map[string]any {
	t.Helper()
	st, m, raw := e.pedir(t, "GET", "/api/comprobantes?"+query, claveLect, nil, "")
	if st != 200 {
		t.Fatalf("comprobantes?%s: %d %s", query, st, raw)
	}
	var res []map[string]any
	for _, x := range m["comprobantes"].([]any) {
		res = append(res, x.(map[string]any))
	}
	return res
}

func origen(c map[string]any, campo string) string {
	o, _ := c["origenes"].(map[string]any)
	s, _ := o[campo].(string)
	return s
}

// Tickets de punta a punta: fotos de tickets por OCR, constatación (apócrifos en lugar de WSCDC),
// rubro en tres capas (palabras clave, padrón, corrección recordada por CUIT), filtros, resumen,
// exportación y MCP.
func TestTicketsPuntaAPunta(t *testing.T) {
	ocr := extractor.NuevoTesseract("spa", 60*time.Second)
	if ocr == nil {
		t.Skip("sin tesseract")
	}
	e := armarCon(t, ocr)
	apocLlamadas.Store(0)

	archivos := []string{"tickets/01_super_tfb_foto.jpg", "tickets/02_ypf_tique_foto.jpg", "tickets/03_farmacia_tfc_foto.jpg", "tickets/04_parrilla_tfa.png",
		"pdf/01_FA_21.pdf", "pdf/05_NCA.pdf"}
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": "20222222223", "constatar": "true", "nombre": "Gastos"}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	id := int64(m["lote"].(map[string]any)["id"].(float64))
	t0 := time.Now()
	e.esperarLote(t, id)
	t.Logf("lote 1: %s", time.Since(t0))

	// --- Filtro por clase ---
	tickets := e.cbtes(t, fmt.Sprintf("lote=%d&clase=tickets", id))
	facturas := e.cbtes(t, fmt.Sprintf("lote=%d&clase=facturas", id))
	if len(tickets) != 4 || len(facturas) != 2 {
		t.Fatalf("tickets %d, facturas %d", len(tickets), len(facturas))
	}
	if st, _, _ := e.pedir(t, "GET", "/api/comprobantes?clase=boletas", claveLect, nil, ""); st != 400 {
		t.Errorf("clase inválida: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/comprobantes?rubro=joyeria", claveLect, nil, ""); st != 400 {
		t.Errorf("rubro inválido: %d", st)
	}
	porCUIT := map[string]map[string]any{}
	for _, c := range tickets {
		if c["es_tique"] != true {
			t.Errorf("es_tique: %v", c)
		}
		porCUIT[c["cuit_emisor"].(string)] = c
	}
	for _, c := range facturas {
		if c["es_tique"] != false {
			t.Errorf("una factura no es tique: %v", c["tipo"])
		}
	}

	// --- Constatación: los tiques van a la base de apócrifos, no a WSCDC ---
	ypf := porCUIT["30708888881"]
	if ypf == nil {
		t.Fatalf("no se leyó el CUIT de la estación de servicio: %v", porCUIT)
	}
	k, _ := ypf["constatacion"].(map[string]any)
	if k == nil || k["valido"] != nil || k["apoc"] == nil || k["apoc"].(map[string]any)["consultado"] != true {
		t.Errorf("constatación del tique: %v", k)
	}
	if h := fmt.Sprint(ypf["hallazgos"]); !strings.Contains(h, "apoc_controlado") || !strings.Contains(h, "tique_sin_constatacion") || strings.Contains(h, "sin_cae") {
		t.Errorf("hallazgos del tique: %s", h)
	}
	if super := porCUIT[cuitSuperApocrifo]; super == nil || super["estado"] != "error" || !strings.Contains(fmt.Sprint(super["hallazgos"]), "emisor_apocrifo") {
		t.Errorf("el super apócrifo: %v", super)
	}
	if apocLlamadas.Load() < 4 {
		t.Errorf("consultas a apócrifos: %d", apocLlamadas.Load())
	}

	// --- Rubro: padrón (capa 2) sobre palabras clave (capa 1) ---
	if ypf["rubro"] != "combustible" || origen(ypf, "rubro") != "arcanum" {
		t.Errorf("YPF: rubro %v (%s)", ypf["rubro"], origen(ypf, "rubro"))
	}
	if p := porCUIT["30715554441"]; p == nil || p["rubro"] != "gastronomia" || origen(p, "rubro") != "texto" && origen(p, "rubro") != "ocr" {
		t.Errorf("parrilla: %v", p)
	}
	if f := porCUIT["27272727274"]; f == nil || f["rubro"] != "farmacia" || origen(f, "rubro") != "ocr" {
		t.Errorf("farmacia: %v", f)
	}
	var fa map[string]any
	for _, c := range facturas {
		if c["rubro"] != "supermercado" || origen(c, "rubro") != "arcanum" {
			t.Errorf("la factura toma el rubro del padrón: %v (%s)", c["rubro"], origen(c, "rubro"))
		}
		if c["tipo"].(float64) == 1 {
			fa = c
		}
	}
	if len(e.cbtes(t, fmt.Sprintf("lote=%d&rubro=combustible", id))) != 1 {
		t.Error("filtro por rubro")
	}

	// --- Rubro: la corrección de una persona (capa 3) se recuerda por CUIT ---
	parche := `{"campos":{"rubro":"joyeria"}}`
	if st, _, _ := e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%v", fa["id"]), claveOp, strings.NewReader(parche), "application/json"); st != 400 {
		t.Errorf("rubro inválido: %d", st)
	}
	parche = `{"campos":{"rubro":"Servicios"},"usuario":"diego"}`
	st, m, raw = e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%v", fa["id"]), claveOp, strings.NewReader(parche), "application/json")
	if st != 200 || m["comprobante"].(map[string]any)["rubro"] != "servicios" {
		t.Fatalf("corregir rubro: %d %s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", "/api/rubros", claveLect, nil, "")
	if pc := fmt.Sprint(m["por_cuit"]); !strings.Contains(pc, "30711111111") || !strings.Contains(pc, "servicios") || !strings.Contains(pc, "diego") {
		t.Errorf("rubros por CUIT: %v", m)
	}
	// La nota de crédito del mismo emisor se actualiza sola.
	for _, c := range e.cbtes(t, fmt.Sprintf("lote=%d&clase=facturas", id)) {
		if c["rubro"] != "servicios" || origen(c, "rubro") != "manual" {
			t.Errorf("el rubro no se pasó a los otros comprobantes del emisor: %v (%s)", c["rubro"], origen(c, "rubro"))
		}
	}
	// Un comprobante nuevo de ese emisor toma el rubro guardado (le gana al padrón).
	body, tipo = multipartDe(t, map[string]string{"cliente_cuit": "20222222223"}, "pdf/02_FA_mixta.pdf")
	_, m, _ = e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	id2 := int64(m["lote"].(map[string]any)["id"].(float64))
	t0 = time.Now()
	e.esperarLote(t, id2)
	t.Logf("lote 2: %s", time.Since(t0))
	if c := e.cbtes(t, fmt.Sprintf("lote=%d", id2)); len(c) != 1 || c[0]["rubro"] != "servicios" || origen(c[0], "rubro") != "manual" {
		t.Errorf("el rubro recordado no se aplicó: %v", c)
	}

	// --- Resumen, exportación y MCP ---
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/resumen?lote=%d", id), claveLect, nil, "")
	r := m["resumen"].(map[string]any)
	tk := r["tickets"].(map[string]any)
	pr := r["por_rubro"].(map[string]any)
	if tk["cantidad"].(float64) != 4 || tk["total"].(float64) <= 0 || pr["combustible"] == nil || pr["servicios"] == nil {
		t.Errorf("resumen: %v", r)
	}
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/resumen?lote=%d&clase=tickets", id), claveLect, nil, "")
	if r := m["resumen"].(map[string]any); r["cantidad"].(float64) != 4 || r["por_rubro"].(map[string]any)["servicios"] != nil {
		t.Errorf("resumen de tickets: %v", r)
	}
	st, _, raw = e.pedir(t, "GET", fmt.Sprintf("/api/export.csv?lote=%d&clase=tickets", id), claveLect, nil, "")
	if s := string(raw); st != 200 || !strings.Contains(s, ";Rubro;") || !strings.Contains(s, "combustible") || !strings.Contains(s, "Tique") {
		t.Errorf("csv: %d %.400s", st, raw)
	}
	mcp := func(cuerpo string) string {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+claveLect)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := res.Body.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return b.String()
	}
	out := mcp(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"buscar_comprobantes","arguments":{"lote":%d,"clase":"tickets"}}}`, id))
	if !strings.Contains(out, `\"total\":4`) || !strings.Contains(out, `\"es_tique\":true`) || !strings.Contains(out, `\"rubro\":\"combustible\"`) {
		t.Errorf("MCP buscar_comprobantes: %.600s", out)
	}

	// --- Verificar un ticket ---
	body, tipo = multipartDe(t, map[string]string{"_campo": "archivo"}, "tickets/02_ypf_tique.png")
	st, m, raw = e.pedir(t, "POST", "/api/verificar", claveLect, body, tipo)
	if st != 200 || m["veredicto"] != "tique_controlado" || !strings.Contains(fmt.Sprint(m["mensaje"]), "no se constatan en ARCA") {
		t.Errorf("verificar ticket: %d %.500s", st, raw)
	}
	body, tipo = multipartDe(t, map[string]string{"_campo": "archivo"}, "tickets/01_super_tfb.png")
	_, m, _ = e.pedir(t, "POST", "/api/verificar", claveLect, body, tipo)
	if m["veredicto"] != "apocrifa" {
		t.Errorf("verificar ticket apócrifo: %v", m["veredicto"])
	}
}
