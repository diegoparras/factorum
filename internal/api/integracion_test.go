package api_test

// Prueba de punta a punta: Postgres real (FACTORUM_TEST_DB), cola River, almacén cifrado en disco,
// Arcanum falso, API HTTP y MCP. Sin FACTORUM_TEST_DB se salta.
//
//	FACTORUM_TEST_DB=postgres://factorum@localhost:5433/factorum_test?host=/tmp go test ./internal/api/

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/api"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/correo/correotest"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/mcpserver"
	"github.com/diegoparras/factorum/internal/pdfx"
	"github.com/diegoparras/factorum/internal/pipeline"
	"github.com/diegoparras/factorum/internal/store"
)

const (
	claveOp   = "clave-operador-de-prueba"
	claveLect = "clave-lectura-de-prueba"
)

// Para los tickets: el supermercado figura en la base de apócrifos; la estación de servicio y el
// emisor de las facturas A tienen actividad en el padrón (combustible y supermercado).
const cuitSuperApocrifo = "30712223339"

var actividadesFalsas = map[string]string{"30708888881": "473000", "30711111111": "471120"}

var apocLlamadas atomic.Int32

// paseArcanum guarda el último pedido de pase que recibió el Arcanum falso.
var paseArcanum atomic.Value

// arcanumFalso: el emisor 20123456786 es apócrifo; el resto, válido. El padrón conoce a todos.
func arcanumFalso(t *testing.T, llamadas *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "service": "arcanum", "version": "9.9.9"})
			return
		}
		if r.URL.Path == "/api/suite/pase" {
			if r.Header.Get("Authorization") != "Bearer arc" {
				w.WriteHeader(401)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Clave inválida"})
				return
			}
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			paseArcanum.Store(b)
			if b["email"] == "nadie@ejemplo.com" {
				w.WriteHeader(403)
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "Esa persona no tiene cuenta en Arcanum"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": "https://arcanum.ejemplo.com/suite/entrar?pase=abc", "vence": "2026-11-27T12:00:00Z"})
			return
		}
		if r.Header.Get("X-API-Key") != "arc" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/tenants":
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "tenants": []any{map[string]any{"cuit": "20222222223"}}})
		case r.URL.Path == "/api/constatacion":
			llamadas.Add(1)
			var b struct {
				Cuit  string           `json:"cuit"`
				Items []map[string]any `json:"items"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b.Cuit != "20222222223" {
				t.Errorf("consultante %q", b.Cuit)
			}
			var res []map[string]any
			for _, it := range b.Items {
				apoc := it["cuitEmisor"] == "20123456786"
				res = append(res, map[string]any{"valido": true, "resultado": "A", "observaciones": []any{},
					"apoc": map[string]any{"consultado": true, "esApocrifo": apoc}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "constatacion": map[string]any{"resultados": res}})
		case strings.HasPrefix(r.URL.Path, "/api/padron/a5/"):
			datos := map[string]any{"datosGenerales": map[string]any{"razonSocial": "DESDE EL PADRON SA"}}
			// Actividad principal (capa 2 del rubro) para algunos emisores.
			if act, ok := actividadesFalsas[strings.TrimPrefix(r.URL.Path, "/api/padron/a5/")]; ok {
				datos["datosRegimenGeneral"] = map[string]any{"actividad": []any{
					map[string]any{"idActividad": "960990", "descripcionActividad": "SERVICIOS PERSONALES N.C.P.", "orden": "2"},
					map[string]any{"idActividad": act, "descripcionActividad": "ACTIVIDAD " + act, "orden": "1"},
				}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "datos": datos})
		case strings.HasPrefix(r.URL.Path, "/api/apoc/"):
			apocLlamadas.Add(1)
			if r.URL.Query().Get("cuit") != "20222222223" {
				t.Errorf("consultante de apócrifos %q", r.URL.Query().Get("cuit"))
			}
			cuit := strings.TrimPrefix(r.URL.Path, "/api/apoc/")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cuit": cuit, "esApocrifo": cuit == cuitSuperApocrifo, "codigo": nil, "descripcion": nil})
		default:
			w.WriteHeader(404)
		}
	}))
}

type entorno struct {
	app     *app.App
	srv     *httptest.Server
	arc     *atomic.Int32
	api     *api.Server
	aj      *ajustes.Servicio
	fijadas *sync.Map
	cfg     *config.Config
}

func armar(t *testing.T) *entorno { return armarCon(t, nil) }

func armarCon(t *testing.T, ocr *extractor.Tesseract) *entorno { return armarConCfg(t, ocr, nil) }

func armarConCfg(t *testing.T, ocr *extractor.Tesseract, ajustar func(*config.Config)) *entorno {
	t.Helper()
	return armarConURL(t, ocr, func(c *config.Config, _ string) {
		if ajustar != nil {
			ajustar(c)
		}
	})
}

// armarConURL es como armarConCfg, pero ajustar recibe además la dirección en la que va a escuchar
// el servidor (para usarla de URL pública).
func armarConURL(t *testing.T, ocr *extractor.Tesseract, ajustar func(*config.Config, string), opciones ...func(*app.App)) *entorno {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	dir := "http://" + ln.Addr().String()
	url := os.Getenv("FACTORUM_TEST_DB")
	if url == "" {
		t.Skip("sin FACTORUM_TEST_DB")
	}
	motor := pdfx.NuevoPoppler(30 * time.Second)
	if motor.Disponible() != nil {
		t.Skip("sin poppler")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	st, err := store.Abrir(ctx, url, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Base limpia en cada corrida.
	if _, err := st.Pool.Exec(ctx, `TRUNCATE lotes, documentos, comprobantes, correcciones, rubros_cuit, usuarios, sesiones, claves_api, configuracion, auditoria, pases_suite, correo_mensajes, correo_adjuntos, tokens_cuenta, avisos_enviados RESTART IDENTITY CASCADE`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Cerrar)
	clave := bytes.Repeat([]byte{7}, 32)
	alm, err := almacen.Nuevo(&almacen.Disco{Dir: t.TempDir()}, clave)
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	arc := arcanumFalso(t, &n)
	t.Cleanup(arc.Close)
	cfg := &config.Config{APIKey: claveOp, APIKeyLect: claveLect, Workers: 4, MaxPaginas: 30, MaxArchivo: 25 << 20,
		ClaveMaestra: clave, DirDatos: t.TempDir(), Nombre: "Factorum", ConstatarPorDefecto: true, SuiteCrearUsuarios: true,
		ArcanumURL: arc.URL, ArcanumAPIKey: "arc", ArcanumConsultante: "20222222223", ArcanumTimeout: 10 * time.Second,
		CanalClienteCUIT: map[string]string{}}
	if ajustar != nil {
		ajustar(cfg, dir)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, tlsCorreo, err := correotest.Certificado()
	if err != nil {
		t.Fatal(err)
	}
	a := &app.App{Cfg: cfg, Store: st, Almacen: alm, Pipeline: pipeline.Nuevo(motor, ocr, nil, 2, 1), Log: log, TLSCorreo: tlsCorreo, Calma: time.Second}
	for _, o := range opciones {
		o(a)
	}
	a.SetArcanum(arcanum.Nuevo(arc.URL, "arc", "20222222223", 10*time.Second))
	if _, err := st.Pool.Exec(ctx, `TRUNCATE river_job`); err != nil && !strings.Contains(err.Error(), "does not exist") {
		t.Fatal(err)
	}
	if err := a.IniciarCola(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = a.River.Stop(sctx)
	})
	aj, err := ajustes.Nuevo(st, cfg, log)
	if err != nil {
		t.Fatal(err)
	}
	var fijadas sync.Map // variables de entorno de mentira: la prueba no depende del entorno real
	aj.Getenv = func(k string) string {
		v, _ := fijadas.Load(k)
		s, _ := v.(string)
		return s
	}
	if err := aj.Cargar(ctx); err != nil {
		t.Fatal(err)
	}
	aj.Suscribir(func(_, nuevo *config.Config) { a.SetConfig(nuevo) })
	s := &api.Server{App: a, Cfg: cfg, Log: log, Ajustes: aj}
	s.MCP = mcpserver.Handler(a, s.IdentificarClave)
	if err := s.PrepararArranque(ctx); err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: s.Handler()}}
	srv.Start()
	t.Cleanup(srv.Close)
	return &entorno{app: a, srv: srv, arc: &n, api: s, aj: aj, fijadas: &fijadas, cfg: cfg}
}

func (e *entorno) pedir(t *testing.T, metodo, ruta, clave string, cuerpo io.Reader, tipo string) (int, map[string]any, []byte) {
	t.Helper()
	req, _ := http.NewRequest(metodo, e.srv.URL+ruta, cuerpo)
	if clave != "" {
		req.Header.Set("X-API-Key", clave)
	}
	if tipo != "" {
		req.Header.Set("Content-Type", tipo)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return res.StatusCode, m, b
}

func multipartDe(t *testing.T, campos map[string]string, archivos ...string) (io.Reader, string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range campos {
		_ = mw.WriteField(k, v)
	}
	for _, a := range archivos {
		b, err := os.ReadFile(filepath.Join("..", "..", "testdata", a))
		if err != nil {
			t.Fatal(err)
		}
		campo := "archivos"
		if campos["_campo"] != "" {
			campo = campos["_campo"]
		}
		fw, _ := mw.CreateFormFile(campo, filepath.Base(a))
		_, _ = fw.Write(b)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestPuntaAPunta(t *testing.T) {
	e := armar(t)

	// --- Autenticación ---
	if st, _, _ := e.pedir(t, "GET", "/api/lotes", "", nil, ""); st != 401 {
		t.Fatalf("sin clave: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/lotes", "mala", nil, ""); st != 401 {
		t.Fatalf("clave mala: %d", st)
	}
	body, tipo := multipartDe(t, map[string]string{}, "pdf/01_FA_21.pdf")
	if st, _, _ := e.pedir(t, "POST", "/api/lotes", claveLect, body, tipo); st != 403 {
		t.Fatalf("lectura no puede subir: %d", st)
	}

	// --- Lote: 9 PDFs + una foto, constatando con Arcanum ---
	archivos := []string{"pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf", "pdf/03_FC_servicios.pdf", "pdf/04_FB_consumidor.pdf", "pdf/05_NCA.pdf",
		"pdf/06_FA_no_cierra.pdf", "pdf/07_FA_dolares.pdf", "pdf/08_dos_facturas.pdf", "pdf/09_FA_adulterada.pdf", "foto_fb.png"}
	body, tipo = multipartDe(t, map[string]string{"cliente_cuit": "20-22222222-3", "constatar": "true", "nombre": "Noviembre"}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	lote := m["lote"].(map[string]any)
	id := int64(lote["id"].(float64))
	// La foto es la misma factura que 04 (mismo número): no es "repetido" (otro archivo) pero sí duplicado.
	if m["recibidos"].(float64) != 10 {
		t.Fatalf("recibidos: %v", m)
	}

	// Esperar a que termine (procesar + constatar).
	limite := time.Now().Add(60 * time.Second)
	for {
		_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", id), claveLect, nil, "")
		l := m["lote"].(map[string]any)
		if l["estado"] == "terminado" && e.arc.Load() > 0 {
			var pend int
			_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM comprobantes WHERE lote_id=$1 AND constatacion IS NULL`, id).Scan(&pend)
			if pend == 0 {
				break
			}
		}
		if time.Now().After(limite) {
			t.Fatalf("el lote no terminó: %v", l)
		}
		time.Sleep(200 * time.Millisecond)
	}

	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/comprobantes?lote=%d&limite=100", id), claveLect, nil, "")
	cbs := m["comprobantes"].([]any)
	if len(cbs) != 11 { // 9 PDFs, uno con dos facturas, más la foto
		t.Fatalf("comprobantes: %d", len(cbs))
	}
	porNumero := map[string]map[string]any{}
	for _, x := range cbs {
		c := x.(map[string]any)
		porNumero[fmt.Sprintf("%v-%v-%v", c["tipo"], c["punto_venta"], c["numero"])] = c
	}
	// Apócrifo (emisor 20123456786): Factura C 5-91, dos veces (03 y 08): una es duplicado.
	fc := porNumero["11-5-91"]
	if fc["estado"] != "error" || !strings.Contains(fmt.Sprint(fc["hallazgos"]), "emisor_apocrifo") {
		t.Errorf("la C del apócrifo tiene que dar error: %v %v", fc["estado"], fc["hallazgos"])
	}
	fa := porNumero["1-2-1543"]
	if fa["estado"] != "ok" || !strings.Contains(fmt.Sprint(fa["hallazgos"]), "constatado") {
		t.Errorf("la A 2-1543 tiene que estar bien y constatada: %v %v", fa["estado"], fa["hallazgos"])
	}
	if adult := porNumero["1-2-1546"]; adult["estado"] != "error" {
		t.Errorf("la adulterada: %v", adult["estado"])
	}
	// La foto (solo QR) se completa con el padrón: la razón social sale de Arcanum.
	var foto map[string]any
	for _, x := range cbs {
		if c := x.(map[string]any); c["archivo"] == "foto_fb.png" {
			foto = c
		}
	}
	if foto == nil || foto["razon_social_emisor"] != "DESDE EL PADRON SA" || foto["origenes"].(map[string]any)["razon_social_emisor"] != "arcanum" {
		t.Errorf("la foto no se completó con el padrón: %v", foto)
	}
	if foto["duplicado_de"] == nil {
		t.Errorf("la foto es la misma B 3-20077 que 04: tenía que marcarse duplicado")
	}

	// --- Subir el mismo archivo otra vez: repetido, no se procesa ---
	body, tipo = multipartDe(t, map[string]string{"cliente_cuit": "20222222223"}, "pdf/01_FA_21.pdf")
	_, m, _ = e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if m["repetidos"].(float64) != 1 {
		t.Errorf("repetido: %v", m)
	}

	// --- Corrección con papel de trabajo ---
	nc := porNumero["6-5-91"]
	_ = nc
	no := porNumero["1-5-91"] // 06: no cierra (total 112.100 vs 121.000)
	cid := int64(no["id"].(float64))
	parche := `{"campos":{"iva_21":"12100","neto_gravado":"100000"},"usuario":"diego"}`
	st, m, raw = e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", cid), claveOp, strings.NewReader(parche), "application/json")
	if st != 200 {
		t.Fatalf("corregir: %d %s", st, raw)
	}
	c := m["comprobante"].(map[string]any)
	if c["estado"] == "error" && strings.Contains(fmt.Sprint(c["hallazgos"]), "suma_no_cierra") {
		t.Errorf("con IVA 12.100 la suma cierra: %v", c["hallazgos"])
	}
	if c["origenes"].(map[string]any)["iva_21"] != "manual" {
		t.Errorf("origen manual")
	}
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/comprobantes/%d", cid), claveLect, nil, "")
	if corr := m["correcciones"].([]any); len(corr) != 1 || corr[0].(map[string]any)["antes"] != "21000.00" {
		t.Errorf("papel de trabajo: %v", corr)
	}
	st, m, _ = e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", cid), claveOp, strings.NewReader(`{"aprobar":true,"usuario":"diego"}`), "application/json")
	if m["comprobante"].(map[string]any)["estado"] != "aprobado" {
		t.Errorf("aprobar: %v", m)
	}
	if st, _, _ := e.pedir(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", cid), claveOp, strings.NewReader(`{"campos":{"hackeo":"1"}}`), "application/json"); st != 400 {
		t.Errorf("campo no editable: %d", st)
	}

	// --- Archivo original (descifrado) ---
	st, _, raw = e.pedir(t, "GET", fmt.Sprintf("/api/documentos/%v/archivo", fa["documento_id"]), claveLect, nil, "")
	if st != 200 || !bytes.HasPrefix(raw, []byte("%PDF")) {
		t.Errorf("archivo original: %d", st)
	}

	// --- Export y resumen ---
	st, _, raw = e.pedir(t, "GET", fmt.Sprintf("/api/export.xlsx?lote=%d", id), claveLect, nil, "")
	if st != 200 || !bytes.HasPrefix(raw, []byte("PK")) {
		t.Errorf("xlsx: %d", st)
	}
	st, _, raw = e.pedir(t, "GET", fmt.Sprintf("/api/export.csv?lote=%d", id), claveLect, nil, "")
	if st != 200 || !strings.Contains(string(raw), "DISTRIBUIDORA EJEMPLO") || !strings.Contains(string(raw), "121000,00") {
		t.Errorf("csv: %d %.300s", st, raw)
	}
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/resumen?lote=%d", id), claveLect, nil, "")
	r := m["resumen"].(map[string]any)
	if r["cantidad"].(float64) != 11 || r["moneda_extranjera"].(float64) != 1 {
		t.Errorf("resumen: %v", r)
	}

	// --- Verificar (lectura puede, no guarda) ---
	body, tipo = multipartDe(t, map[string]string{"_campo": "archivo"}, "pdf/02_FA_mixta.pdf")
	st, m, raw = e.pedir(t, "POST", "/api/verificar", claveLect, body, tipo)
	if st != 200 || m["veredicto"] != "valida" {
		t.Errorf("verificar válida: %d %s", st, raw)
	}
	body, tipo = multipartDe(t, map[string]string{"_campo": "archivo"}, "pdf/03_FC_servicios.pdf")
	_, m, _ = e.pedir(t, "POST", "/api/verificar", claveLect, body, tipo)
	if m["veredicto"] != "apocrifa" {
		t.Errorf("verificar apócrifa: %v", m["veredicto"])
	}
	b, _ := json.Marshal(map[string]string{"nombre": "x.pdf", "contenido_base64": "JVBERi0xLjQgcm90bw=="})
	_, m, _ = e.pedir(t, "POST", "/api/verificar", claveLect, bytes.NewReader(b), "application/json")
	if m["veredicto"] != "no_verificable" {
		t.Errorf("PDF roto: %v", m)
	}

	// --- MCP ---
	mcp := func(clave string, cuerpo string) (int, string) {
		req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(cuerpo))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if clave != "" {
			req.Header.Set("Authorization", "Bearer "+clave)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if st, _ := mcp("", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); st != 401 {
		t.Errorf("MCP sin clave: %d", st)
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	if st, out := mcp(claveLect, init); st != 200 || !strings.Contains(out, "factorum") {
		t.Fatalf("MCP initialize: %d %s", st, out)
	}
	_, out := mcp(claveLect, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if !strings.Contains(out, "verificar_factura") || strings.Contains(out, "procesar_factura") {
		t.Errorf("lectura no ve procesar_factura: %s", out)
	}
	_, out = mcp(claveOp, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if !strings.Contains(out, "procesar_factura") {
		t.Errorf("operador ve procesar_factura")
	}
	_, out = mcp(claveLect, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pendientes_revision","arguments":{}}}`)
	if !strings.Contains(out, "emisor") || !strings.Contains(out, `\"total\"`) {
		t.Errorf("pendientes_revision: %.500s", out)
	}
	_, out = mcp(claveLect, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"resumen","arguments":{"lote":1}}}`)
	if !strings.Contains(out, "iva_21") {
		t.Errorf("resumen MCP: %.300s", out)
	}

	// --- Salud y métricas ---
	_, m, _ = e.pedir(t, "GET", "/healthz", "", nil, "")
	if m["base"] != true || m["cifrado"] != true || m["arcanum"] != true {
		t.Errorf("healthz: %v", m)
	}
	_, _, raw = e.pedir(t, "GET", "/metrics", claveLect, nil, "")
	if !strings.Contains(string(raw), "factorum_comprobantes") {
		t.Errorf("metrics: %s", raw)
	}
}

// El almacén nunca escribe un PDF en claro.
func TestAlmacenCifrado(t *testing.T) {
	dir := t.TempDir()
	alm, _ := almacen.Nuevo(&almacen.Disco{Dir: dir}, bytes.Repeat([]byte{1}, 32))
	datos, _ := os.ReadFile(filepath.Join("..", "..", "testdata", "pdf", "01_FA_21.pdf"))
	if _, err := alm.Guardar(context.Background(), "originales/abc123", datos); err != nil {
		t.Fatal(err)
	}
	crudo, _ := os.ReadFile(filepath.Join(dir, "originales", "ab", "abc123"))
	if bytes.Contains(crudo, []byte("%PDF")) || bytes.Contains(crudo, []byte("DISTRIBUIDORA")) {
		t.Fatal("el objeto guardado se puede leer en claro")
	}
	back, err := alm.Leer(context.Background(), "originales/abc123")
	if err != nil || !bytes.Equal(back, datos) {
		t.Fatal("ida y vuelta", err)
	}
	otra, _ := almacen.Nuevo(&almacen.Disco{Dir: dir}, bytes.Repeat([]byte{2}, 32))
	if _, err := otra.Leer(context.Background(), "originales/abc123"); err == nil {
		t.Fatal("con otra clave no tiene que abrir")
	}
	_ = pgx.ErrNoRows
}
