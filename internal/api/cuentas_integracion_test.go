package api_test

// Usuarios, sesiones, claves de API, alcance por CUIT, configuración, auditoría y el pase de la
// suite, contra Postgres real (FACTORUM_TEST_DB).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/api"
	"github.com/diegoparras/factorum/internal/config"
)

const (
	cuitX = "20222222223"
	cuitY = "30711111111"
	cuitZ = "20333333334"
)

// navegador es un cliente con cookies que manda lo que manda la UI (X-Factorum y Origin).
type navegador struct {
	t       *testing.T
	e       *entorno
	c       *http.Client
	origen  string
	sinCSRF bool
	ip      string // X-Forwarded-For (el servidor le cree a un pedido que viene de 127.0.0.1)
}

func (e *entorno) navegador(t *testing.T) *navegador {
	jar, _ := cookiejar.New(nil)
	return &navegador{t: t, e: e, origen: e.srv.URL, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (n *navegador) pedir(metodo, ruta string, cuerpo any) (int, map[string]any) {
	n.t.Helper()
	st, m, _ := n.pedirCrudo(metodo, ruta, cuerpo)
	return st, m
}

func (n *navegador) pedirCrudo(metodo, ruta string, cuerpo any) (int, map[string]any, []byte) {
	n.t.Helper()
	var rd io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(metodo, n.e.srv.URL+ruta, rd)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if !n.sinCSRF {
		req.Header.Set("X-Factorum", "1")
		req.Header.Set("Origin", n.origen)
	}
	if n.ip != "" {
		req.Header.Set("X-Forwarded-For", n.ip)
	}
	res, err := n.c.Do(req)
	if err != nil {
		n.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return res.StatusCode, m, b
}

// pedirJSON con una clave de API.
func (e *entorno) pedirJSON(t *testing.T, metodo, ruta, clave string, cuerpo any) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	tipo := ""
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		rd, tipo = bytes.NewReader(b), "application/json"
	}
	st, m, _ := e.pedir(t, metodo, ruta, clave, rd, tipo)
	return st, m
}

func (e *entorno) codigoArranque(t *testing.T) string {
	b, err := os.ReadFile(filepath.Join(e.cfg.DirDatos, api.ArchivoArranque))
	if err != nil {
		t.Fatalf("el código de arranque tiene que estar en el directorio de datos: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// crearSuperadmin arranca la instalación y devuelve el navegador con la sesión del superadmin.
func (e *entorno) crearSuperadmin(t *testing.T) *navegador {
	t.Helper()
	sup := e.navegador(t)
	st, m := sup.pedir("POST", "/api/arranque", map[string]string{"codigo": e.codigoArranque(t), "email": "diego@ejemplo.com", "nombre": "Diego", "clave": "una clave muy larga"})
	if st != 201 {
		t.Fatalf("arranque: %d %v", st, m)
	}
	return sup
}

// entrar crea una sesión con email y contraseña.
func (e *entorno) entrar(t *testing.T, email, clave string) *navegador {
	t.Helper()
	n := e.navegador(t)
	if st, m := n.pedir("POST", "/api/sesion", map[string]string{"email": email, "clave": clave}); st != 200 {
		t.Fatalf("entrar %s: %d %v", email, st, m)
	}
	return n
}

// altaUsuario crea a alguien y le cambia la contraseña temporal; devuelve su sesión.
func (e *entorno) altaUsuario(t *testing.T, por *navegador, email, rol string, cuits any) (*navegador, int64) {
	t.Helper()
	st, m := por.pedir("POST", "/api/usuarios", map[string]any{"email": email, "nombre": strings.Split(email, "@")[0], "rol": rol, "cuits": cuits})
	if st != 201 {
		t.Fatalf("alta %s: %d %v", email, st, m)
	}
	temp := m["clave_temporal"].(string)
	id := int64(m["usuario"].(map[string]any)["id"].(float64))
	n := e.entrar(t, email, temp)
	if st, m := n.pedir("POST", "/api/sesion/clave", map[string]string{"actual": temp, "nueva": "otra clave bien larga"}); st != 200 {
		t.Fatalf("cambiar la temporal: %d %v", st, m)
	}
	return n, id
}

// sembrar carga un lote, un documento y un comprobante por cliente, directo en la base.
func (e *entorno) sembrar(t *testing.T, cuits ...string) map[string][3]int64 {
	t.Helper()
	ctx := context.Background()
	res := map[string][3]int64{}
	for i, c := range cuits {
		var l, d, cb int64
		db := e.app.Store.Pool
		if err := db.QueryRow(ctx, `INSERT INTO lotes (nombre, cliente_cuit) VALUES ($1,$2) RETURNING id`, "Lote "+c, c).Scan(&l); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `INSERT INTO documentos (lote_id, nombre, tamano, sha256, estado) VALUES ($1,$2,1,$3,'listo') RETURNING id`, l, c+".pdf", fmt.Sprintf("sha%d", i)).Scan(&d); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `INSERT INTO comprobantes (documento_id, lote_id, cliente_cuit, tipo, punto_venta, numero, fecha, cuit_emisor, total, datos, estado)
			VALUES ($1,$2,$3,1,1,$4,'2026-11-01','30708888881',10000,'{"tipo":1,"punto_venta":1,"total":"100.00","neto_gravado":"82.64","iva_21":"17.36"}','revisar') RETURNING id`, d, l, c, i+1).Scan(&cb); err != nil {
			t.Fatal(err)
		}
		res[c] = [3]int64{l, d, cb}
	}
	return res
}

func ids(t *testing.T, m map[string]any, campo string) map[float64]bool {
	t.Helper()
	res := map[float64]bool{}
	l, _ := m[campo].([]any)
	for _, x := range l {
		res[x.(map[string]any)["id"].(float64)] = true
	}
	return res
}

func TestArranqueSesionesYUsuarios(t *testing.T) {
	e := armar(t)
	anon := e.navegador(t)

	// --- Arranque: sin usuarios, el código está en el log y en el directorio de datos ---
	if _, m := anon.pedir("GET", "/api/arranque", nil); m["necesita"] != true {
		t.Fatalf("arranque pendiente: %v", m)
	}
	if _, m, _ := e.pedir(t, "GET", "/healthz", "", nil, ""); m["arranque"] != true {
		t.Errorf("healthz avisa el arranque: %v", m)
	}
	cod := e.codigoArranque(t)
	sinCSRF := e.navegador(t)
	sinCSRF.sinCSRF = true
	if st, _ := sinCSRF.pedir("POST", "/api/arranque", map[string]string{"codigo": cod}); st != 403 {
		t.Errorf("arranque sin X-Factorum: %d", st)
	}
	if st, _ := anon.pedir("POST", "/api/arranque", map[string]string{"codigo": "MALO", "email": "a@b.com", "clave": "una clave muy larga"}); st != 401 {
		t.Errorf("código malo: %d", st)
	}
	if st, _ := anon.pedir("POST", "/api/arranque", map[string]string{"codigo": cod, "email": "diego@ejemplo.com", "clave": "corta"}); st != 400 {
		t.Errorf("contraseña corta: %d", st)
	}
	sup := e.crearSuperadmin(t)
	if _, err := os.Stat(filepath.Join(e.cfg.DirDatos, api.ArchivoArranque)); err == nil {
		t.Error("el código de arranque se tiene que borrar al usarse")
	}
	if st, _ := anon.pedir("POST", "/api/arranque", map[string]string{"codigo": cod, "email": "otro@ejemplo.com", "clave": "una clave muy larga"}); st != 409 {
		t.Errorf("segundo arranque: %d", st)
	}
	_, m := sup.pedir("GET", "/api/sesion", nil)
	if idn := m["identidad"].(map[string]any); idn["rol"] != "superadmin" || idn["email"] != "diego@ejemplo.com" || m["permisos"].(map[string]any)["configuracion"] != true {
		t.Fatalf("yo: %v", m)
	}

	// --- Entrar y salir ---
	if st, _ := anon.pedir("POST", "/api/sesion", map[string]string{"email": "DIEGO@ejemplo.com", "clave": "mala mala mala"}); st != 401 {
		t.Errorf("contraseña mala: %d", st)
	}
	otra := e.entrar(t, "Diego@Ejemplo.com", "una clave muy larga") // el email no distingue mayúsculas
	if st, _ := otra.pedir("DELETE", "/api/sesion", nil); st != 200 {
		t.Errorf("salir: %d", st)
	}
	if st, _ := otra.pedir("GET", "/api/sesion", nil); st != 401 {
		t.Errorf("después de salir: %d", st)
	}

	// --- CSRF: con cookie, lo que modifica pide la cabecera y un Origin propio ---
	ajeno := *sup
	ajeno.origen = "https://malo.ejemplo.com"
	if st, _ := ajeno.pedir("POST", "/api/usuarios", map[string]any{"email": "x@ejemplo.com", "rol": "lectura"}); st != 403 {
		t.Errorf("origen ajeno: %d", st)
	}
	sinCab := *sup
	sinCab.sinCSRF = true
	if st, _ := sinCab.pedir("POST", "/api/usuarios", map[string]any{"email": "x@ejemplo.com", "rol": "lectura"}); st != 403 {
		t.Errorf("sin X-Factorum: %d", st)
	}
	if st, _ := sinCab.pedir("GET", "/api/usuarios", nil); st != 200 {
		t.Errorf("un GET no la necesita: %d", st)
	}

	// --- Alta con contraseña temporal: obliga a cambiarla ---
	st, m := sup.pedir("POST", "/api/usuarios", map[string]any{"email": "ana@ejemplo.com", "nombre": "Ana", "rol": "admin"})
	if st != 201 || len(m["clave_temporal"].(string)) < 10 || m["usuario"].(map[string]any)["debe_cambiar"] != true {
		t.Fatalf("alta: %d %v", st, m)
	}
	temp := m["clave_temporal"].(string)
	anaID := int64(m["usuario"].(map[string]any)["id"].(float64))
	if st, _ := sup.pedir("POST", "/api/usuarios", map[string]any{"email": "ANA@ejemplo.com", "rol": "lectura"}); st != 409 {
		t.Errorf("email repetido: %d", st)
	}
	ana := e.entrar(t, "ana@ejemplo.com", temp)
	if st, m := ana.pedir("GET", "/api/lotes", nil); st != 403 || m["debe_cambiar"] != true {
		t.Errorf("con la temporal no puede seguir: %d %v", st, m)
	}
	if st, _ := ana.pedir("POST", "/api/sesion/clave", map[string]string{"actual": temp, "nueva": "corta"}); st != 400 {
		t.Errorf("nueva corta: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/sesion/clave", map[string]string{"actual": temp, "nueva": "la clave nueva de ana"}); st != 200 {
		t.Fatalf("cambiar: %d", st)
	}
	if st, _ := ana.pedir("GET", "/api/lotes", nil); st != 200 {
		t.Errorf("ya cambió: %d", st)
	}

	// --- Un admin no toca superadmins ni da un rol mayor al suyo ---
	if st, _ := ana.pedir("POST", "/api/usuarios", map[string]any{"email": "s2@ejemplo.com", "rol": "superadmin"}); st != 403 {
		t.Errorf("admin crea superadmin: %d", st)
	}
	if st, _ := ana.pedir("PATCH", "/api/usuarios/1", map[string]any{"nombre": "Otro"}); st != 403 {
		t.Errorf("admin edita superadmin: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/usuarios/1/restablecer", nil); st != 403 {
		t.Errorf("admin restablece superadmin: %d", st)
	}
	if st, _ := ana.pedir("GET", "/api/configuracion", nil); st != 200 {
		t.Errorf("admin ve los clientes: %d", st)
	}
	if st, _ := ana.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"telegram.token": "1:x"}}); st != 403 {
		t.Errorf("admin cambia la configuración: %d", st)
	}
	if st, _ := ana.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"clientes.canales": "5491155550000 = 20-22222222-3"}}); st != 200 {
		t.Errorf("admin cambia los clientes: %d", st)
	}
	if got := e.app.Config().CanalClienteCUIT["5491155550000"]; got != cuitX {
		t.Errorf("los clientes se recargan en caliente: %q", got)
	}

	// --- El último superadmin no se puede ir ---
	if st, _ := sup.pedir("PATCH", "/api/usuarios/1", map[string]any{"rol": "admin"}); st != 409 {
		t.Errorf("bajar al último superadmin: %d", st)
	}
	if st, _ := sup.pedir("PATCH", "/api/usuarios/1", map[string]any{"activo": false}); st != 403 {
		t.Errorf("desactivarse a sí mismo: %d", st)
	}

	// --- Desactivar cierra las sesiones; reactivar; restablecer ---
	if st, _ := sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", anaID), map[string]any{"activo": false}); st != 200 {
		t.Fatalf("desactivar: %d", st)
	}
	if st, _ := ana.pedir("GET", "/api/lotes", nil); st != 401 {
		t.Errorf("la sesión de una desactivada no sirve: %d", st)
	}
	if st, _ := anon.pedir("POST", "/api/sesion", map[string]string{"email": "ana@ejemplo.com", "clave": "la clave nueva de ana"}); st != 403 {
		t.Errorf("entrar desactivada: %d", st)
	}
	_, _ = sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", anaID), map[string]any{"activo": true})
	st, m = sup.pedir("POST", fmt.Sprintf("/api/usuarios/%d/restablecer", anaID), nil)
	if st != 200 || m["clave_temporal"] == nil {
		t.Fatalf("restablecer: %d %v", st, m)
	}
	e.entrar(t, "ana@ejemplo.com", m["clave_temporal"].(string))

	// --- Lectura no ve la administración ---
	lec, _ := e.altaUsuario(t, sup, "lu@ejemplo.com", "lectura", nil)
	if st, _ := lec.pedir("GET", "/api/usuarios", nil); st != 403 {
		t.Errorf("lectura lista usuarios: %d", st)
	}
	if st, _ := lec.pedir("GET", "/api/auditoria", nil); st != 403 {
		t.Errorf("lectura ve la auditoría: %d", st)
	}
	if st, _ := lec.pedir("GET", "/api/canales/whatsapp/qr.png", nil); st != 403 {
		t.Errorf("lectura ve el QR de WhatsApp: %d", st)
	}

	// --- Auditoría: entradas, fallidas (sin la contraseña), usuarios, configuración ---
	st, m, raw := sup.pedirCrudo("GET", "/api/auditoria?limite=200", nil)
	if st != 200 {
		t.Fatalf("auditoría: %d", st)
	}
	for _, acc := range []string{"sesion.entrar", "sesion.fallida", "sesion.salir", "usuario.crear", "usuario.desactivar", "usuario.reactivar", "usuario.restablecer", "usuario.cambiar_clave", "config.cambiar"} {
		if !strings.Contains(string(raw), `"`+acc+`"`) {
			t.Errorf("falta %s en la auditoría", acc)
		}
	}
	for _, secreto := range []string{"mala mala mala", "una clave muy larga", temp, "la clave nueva de ana"} {
		if strings.Contains(string(raw), secreto) {
			t.Errorf("la auditoría tiene una contraseña: %q", secreto)
		}
	}
	_, m = sup.pedir("GET", "/api/auditoria?accion=sesion.&persona=ana", nil)
	for _, x := range m["eventos"].([]any) {
		ev := x.(map[string]any)
		if !strings.HasPrefix(ev["accion"].(string), "sesion.") || !strings.Contains(ev["actor"].(string), "ana") {
			t.Errorf("filtro de auditoría: %v", ev)
		}
	}
	if m["total"].(float64) < 2 {
		t.Errorf("entradas de ana: %v", m["total"])
	}
}

func TestAlcancePorCUITYClaves(t *testing.T) {
	e := armar(t)
	sup := e.crearSuperadmin(t)
	sem := e.sembrar(t, cuitX, cuitY)
	cbX, cbY := sem[cuitX][2], sem[cuitY][2]

	// Bea solo ve a X.
	bea, _ := e.altaUsuario(t, sup, "bea@ejemplo.com", "usuario", []string{"20-22222222-3"})
	_, m := bea.pedir("GET", "/api/comprobantes", nil)
	if v := ids(t, m, "comprobantes"); len(v) != 1 || !v[float64(cbX)] {
		t.Errorf("Bea ve: %v", v)
	}
	_, m = bea.pedir("GET", "/api/lotes", nil)
	if v := ids(t, m, "lotes"); len(v) != 1 || !v[float64(sem[cuitX][0])] || m["total"].(float64) != 1 {
		t.Errorf("lotes de Bea: %v", m)
	}
	for _, ruta := range []string{fmt.Sprintf("/api/comprobantes/%d", cbY), fmt.Sprintf("/api/lotes/%d", sem[cuitY][0]),
		fmt.Sprintf("/api/documentos/%d/archivo", sem[cuitY][1]), fmt.Sprintf("/api/documentos/%d/pagina/1", sem[cuitY][1])} {
		if st, _ := bea.pedir("GET", ruta, nil); st != 404 {
			t.Errorf("Bea no ve %s: %d", ruta, st)
		}
	}
	if st, _ := bea.pedir("PATCH", fmt.Sprintf("/api/comprobantes/%d", cbY), map[string]any{"aprobar": true}); st != 404 {
		t.Errorf("Bea no aprueba lo de Y: %d", st)
	}
	if st, _ := bea.pedir("POST", fmt.Sprintf("/api/comprobantes/%d/constatar", cbY), nil); st != 404 {
		t.Errorf("Bea no constata lo de Y: %d", st)
	}
	st, m := bea.pedir("PATCH", fmt.Sprintf("/api/comprobantes/%d", cbX), map[string]any{"aprobar": true, "usuario": "otro"})
	if st != 200 || m["comprobante"].(map[string]any)["revisado_por"] != "bea@ejemplo.com" {
		t.Errorf("Bea aprueba lo suyo y queda su email (no el del pedido): %d %v", st, m["comprobante"])
	}
	_, m = bea.pedir("GET", "/api/resumen", nil)
	if m["resumen"].(map[string]any)["cantidad"].(float64) != 1 {
		t.Errorf("resumen de Bea: %v", m["resumen"])
	}
	_, m = bea.pedir("GET", "/api/panel", nil)
	if p := m["panel"].(map[string]any); p["total"].(float64) != 1 || len(p["lotes"].([]any)) != 1 {
		t.Errorf("panel de Bea: %v", p)
	}
	_, _, raw := bea.pedirCrudo("GET", "/api/export.csv", nil)
	if strings.Count(strings.TrimSpace(string(raw)), "\n") != 1 {
		t.Errorf("export de Bea: %s", raw)
	}
	if st, _ := bea.pedir("GET", "/metrics", nil); st != 403 {
		t.Errorf("métricas con restricción: %d", st)
	}
	// Carga: no puede cargar para Y; con un solo CUIT, se usa ese.
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cuitY}, "pdf/01_FA_21.pdf")
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/lotes", body)
	req.Header.Set("Content-Type", tipo)
	req.Header.Set("X-Factorum", "1")
	req.Header.Set("Origin", e.srv.URL)
	res, _ := bea.c.Do(req)
	if res.StatusCode != 403 {
		t.Errorf("Bea carga para Y: %d", res.StatusCode)
	}
	res.Body.Close()

	// El total de todos lo ve el superadmin.
	_, m = sup.pedir("GET", "/api/comprobantes", nil)
	if len(ids(t, m, "comprobantes")) != 2 {
		t.Errorf("el superadmin ve todo")
	}

	// --- Claves de API ---
	if st, _ := bea.pedir("POST", "/api/claves", map[string]any{"nombre": "x", "rol": "lectura"}); st != 403 {
		t.Errorf("un usuario no crea claves: %d", st)
	}
	ana, _ := e.altaUsuario(t, sup, "ana@ejemplo.com", "admin", []string{cuitX, cuitZ})
	if st, _ := ana.pedir("POST", "/api/claves", map[string]any{"nombre": "toda", "rol": "lectura"}); st != 403 {
		t.Errorf("admin con clientes no da una clave de todos: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/claves", map[string]any{"nombre": "y", "rol": "lectura", "cuits": []string{cuitY}}); st != 403 {
		t.Errorf("admin da un CUIT que no tiene: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/claves", map[string]any{"nombre": "sup", "rol": "superadmin", "cuits": []string{cuitX}}); st != 403 {
		t.Errorf("admin da una clave superadmin: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/claves", map[string]any{"nombre": "s", "rol": "lectura", "cuits": []string{cuitX}, "suite": true, "suite_tope": "admin"}); st != 403 {
		t.Errorf("tope admin lo pone solo un superadmin: %d", st)
	}
	st, m = ana.pedir("POST", "/api/claves", map[string]any{"nombre": "Claude de Ana", "rol": "lectura", "cuits": []string{cuitX}})
	if st != 201 {
		t.Fatalf("crear clave: %d %v", st, m)
	}
	clave := m["clave"].(string)
	kid := m["datos"].(map[string]any)["id"].(float64)
	if !strings.HasPrefix(clave, "fct_") || m["datos"].(map[string]any)["prefijo"] != clave[:12] {
		t.Errorf("clave: %v", m)
	}
	_, _, raw = ana.pedirCrudo("GET", "/api/claves", nil)
	if strings.Contains(string(raw), clave) {
		t.Error("la clave completa se muestra una sola vez")
	}
	st, mm, _ := e.pedir(t, "GET", "/api/comprobantes", clave, nil, "")
	if v := ids(t, mm, "comprobantes"); st != 200 || len(v) != 1 || !v[float64(cbX)] {
		t.Errorf("la clave ve solo X: %d %v", st, v)
	}
	if st, _ := e.pedirJSON(t, "PATCH", fmt.Sprintf("/api/comprobantes/%d", cbX), clave, map[string]any{"aprobar": true}); st != 403 {
		t.Errorf("clave de lectura no aprueba: %d", st)
	}
	// MCP con la clave: solo X, sin procesar_factura.
	mcp := func(cuerpo string) string {
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
	if out := mcp(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); strings.Contains(out, "procesar_factura") || !strings.Contains(out, "buscar_comprobantes") {
		t.Errorf("MCP lectura: %.300s", out)
	}
	out := mcp(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"buscar_comprobantes","arguments":{}}}`)
	if !strings.Contains(out, `\"total\":1`) {
		t.Errorf("MCP con alcance: %.400s", out)
	}
	if out := mcp(fmt.Sprintf(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"comprobante","arguments":{"id":%d}}}`, cbY)); !strings.Contains(out, "no encontrado") {
		t.Errorf("MCP no ve lo de Y: %.300s", out)
	}
	// MCP no acepta la cookie del navegador.
	req, _ = http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	res, _ = sup.c.Do(req)
	if res.StatusCode != 401 {
		t.Errorf("MCP con cookie: %d", res.StatusCode)
	}
	res.Body.Close()

	// Revocar.
	if st, _ := ana.pedir("DELETE", fmt.Sprintf("/api/claves/%v", kid), nil); st != 200 {
		t.Fatalf("revocar: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", "/api/comprobantes", clave, nil, ""); st != 401 {
		t.Errorf("clave revocada: %d", st)
	}
	// Una clave vencida no anda; la de una persona desactivada, tampoco.
	_, m = ana.pedir("POST", "/api/claves", map[string]any{"nombre": "otra", "rol": "usuario", "cuits": []string{cuitX}})
	otra := m["clave"].(string)
	if st, _, _ := e.pedir(t, "GET", "/api/lotes", otra, nil, ""); st != 200 {
		t.Errorf("clave nueva: %d", st)
	}
	_, _ = e.app.Store.Pool.Exec(context.Background(), `UPDATE claves_api SET vence = now() - interval '1 minute' WHERE prefijo = $1`, otra[:12])
	if st, _, _ := e.pedir(t, "GET", "/api/lotes", otra, nil, ""); st != 401 {
		t.Errorf("clave vencida: %d", st)
	}
	if st, _ := ana.pedir("POST", "/api/claves", map[string]any{"nombre": "vieja", "rol": "lectura", "cuits": []string{cuitX}, "vence": "2020-01-01"}); st != 400 {
		t.Errorf("vencimiento en el pasado: %d", st)
	}
	_, m = sup.pedir("POST", "/api/claves", map[string]any{"nombre": "de sup", "rol": "lectura"})
	deSup := m["clave"].(string)
	if st, _ := ana.pedir("DELETE", fmt.Sprintf("/api/claves/%v", m["datos"].(map[string]any)["id"]), nil); st != 200 {
		t.Errorf("admin revoca una clave de lectura de otro: %d", st)
	}
	_ = deSup

	// La auditoría registra correcciones, aprobaciones, exportaciones y claves.
	_, _, raw = sup.pedirCrudo("GET", "/api/auditoria?limite=200", nil)
	for _, acc := range []string{"comprobante.aprobar", "exportar", "clave.crear", "clave.revocar"} {
		if !strings.Contains(string(raw), `"`+acc+`"`) {
			t.Errorf("falta %s en la auditoría", acc)
		}
	}
	if strings.Contains(string(raw), clave) || strings.Contains(string(raw), otra) {
		t.Error("la auditoría tiene una clave completa")
	}
}

func TestConfiguracionDesdeLaPantalla(t *testing.T) {
	e := armar(t)
	sup := e.crearSuperadmin(t)

	// Un secreto nunca vuelve completo; en la base va cifrado.
	token := "123456789:AAEXAMPLEtokendeprueba"
	st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"telegram.token": token, "telegram.permitidos": "111\n222"}})
	if st != 200 {
		t.Fatalf("guardar: %d %v", st, m)
	}
	_, _, raw := sup.pedirCrudo("GET", "/api/configuracion", nil)
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), `"ultimos":"ueba"`) {
		t.Errorf("el token no puede salir por la API: %s", raw)
	}
	var crudo []byte
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT secreto FROM configuracion WHERE clave='telegram.token'`).Scan(&crudo)
	if len(crudo) == 0 || bytes.Contains(crudo, []byte("AAEXAMPLE")) {
		t.Error("el token tiene que ir cifrado en la base")
	}
	if c := e.app.Config(); c.TelegramToken != token || len(c.TelegramPermitidos) != 2 {
		t.Errorf("recarga en caliente: %q %v", c.TelegramToken, c.TelegramPermitidos)
	}
	// Mandar el secreto vacío no lo borra; null sí.
	_, _ = sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"telegram.token": ""}})
	if e.app.Config().TelegramToken != token {
		t.Error("un secreto vacío no se toca")
	}
	_, _ = sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"telegram.token": nil}})
	if e.app.Config().TelegramToken != "" {
		t.Error("null borra")
	}
	// Validaciones.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"general.url_publica": "ftp://x"}}); st != 400 {
		t.Errorf("URL pública inválida: %d %v", st, m)
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"ia.url": "http://ollama:11434/v1"}}); st != 400 {
		t.Errorf("IA sin modelo: %d", st)
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"arcanum.cuit_consultante": "20222222224"}}); st != 400 {
		t.Errorf("CUIT inválido: %d", st)
	}
	// Lo que fija el entorno gana y no se cambia.
	e.fijadas.Store("FACTORUM_NOMBRE", "Del entorno")
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"general.nombre": "Otro"}}); st != 409 || !strings.Contains(m["error"].(string), "FACTORUM_NOMBRE") {
		t.Errorf("campo bloqueado: %d %v", st, m)
	}
	_, m = sup.pedir("GET", "/api/configuracion", nil)
	for _, x := range m["campos"].([]any) {
		if c := x.(map[string]any); c["clave"] == "general.nombre" && (c["bloqueado"] != true || c["env"] != "FACTORUM_NOMBRE") {
			t.Errorf("nombre bloqueado: %v", c)
		}
	}

	// Probar: el modelo de visión con un endpoint compatible con OpenAI de mentira, sin guardar.
	ia := chatDePrueba(t, "qwen3-vl:4b-instruct", "secreta")
	cambios := map[string]any{"ia.proveedor": "otro", "ia.url": ia.URL + "/v1", "ia.modelo": "qwen3-vl:4b-instruct", "ia.api_key": "secreta"}
	_, m = sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": cambios})
	if m["exito"] != true {
		t.Errorf("probar IA: %v", m)
	}
	cambios["ia.modelo"] = "otro"
	if _, m = sup.pedir("POST", "/api/configuracion/probar/ia", map[string]any{"cambios": cambios}); m["exito"] != false || !strings.Contains(m["mensaje"].(string), "otro") {
		t.Errorf("probar IA con un modelo que no está: %v", m)
	}
	if e.app.Config().LLMBaseURL != "" {
		t.Error("probar no guarda")
	}
	_, m = sup.pedir("POST", "/api/configuracion/probar/arcanum", map[string]any{})
	if m["exito"] != true || !strings.Contains(m["mensaje"].(string), "9.9.9") {
		t.Errorf("probar Arcanum: %v", m)
	}
	_, m = sup.pedir("POST", "/api/configuracion/probar/telegram", map[string]any{})
	if m["exito"] != false || !strings.Contains(m["mensaje"].(string), "token") {
		t.Errorf("probar Telegram sin token: %v", m)
	}
	// La auditoría dice qué cambió, nunca el valor.
	_, _, raw = sup.pedirCrudo("GET", "/api/auditoria?accion=config.cambiar", nil)
	if !strings.Contains(string(raw), "telegram.token") || strings.Contains(string(raw), "AAEXAMPLE") {
		t.Errorf("auditoría de la configuración: %s", raw)
	}
}

func TestSuitePase(t *testing.T) {
	e := armar(t)
	sup := e.crearSuperadmin(t)
	pedirPase := func(clave string, cuerpo map[string]any) (int, map[string]any) {
		t.Helper()
		return e.pedirJSON(t, "POST", "/api/suite/pase", clave, cuerpo)
	}
	nuevaClave := func(cuerpo map[string]any) string {
		t.Helper()
		st, m := sup.pedir("POST", "/api/claves", cuerpo)
		if st != 201 {
			t.Fatalf("clave: %d %v", st, m)
		}
		return m["clave"].(string)
	}
	suite := nuevaClave(map[string]any{"nombre": "Arcanum", "rol": "lectura", "suite": true})
	sinFlag := nuevaClave(map[string]any{"nombre": "sin suite", "rol": "usuario"})
	cuerpo := map[string]any{"app": "arcanum", "email": "nueva@ejemplo.com", "nombre": "Nueva", "rol": "operador", "cuits": nil}

	// Sin URL pública: 409.
	if st, m := pedirPase(suite, cuerpo); st != 409 || m["error"] != "Falta la URL pública de Factorum" {
		t.Errorf("sin URL pública: %d %v", st, m)
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"general.url_publica": e.srv.URL}}); st != 200 {
		t.Fatal("URL pública")
	}
	// Clave sin la marca, clave del entorno sin tope, sesión del navegador: 403.
	if st, m := pedirPase(sinFlag, cuerpo); st != 403 || m["error"] != "Esta clave no está habilitada para abrir sesiones de la suite" {
		t.Errorf("clave sin la marca: %d %v", st, m)
	}
	if st, _ := pedirPase(claveOp, cuerpo); st != 403 {
		t.Errorf("FACTORUM_API_KEY sin tope: %d", st)
	}
	if st, _ := sup.pedir("POST", "/api/suite/pase", cuerpo); st != 403 {
		t.Errorf("con sesión: %d", st)
	}

	entrar := func(url string) (*navegador, string) {
		t.Helper()
		n := e.navegador(t)
		ruta := strings.TrimPrefix(url, e.srv.URL)
		req, _ := http.NewRequest("GET", e.srv.URL+ruta, nil)
		res, err := n.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return n, res.Header.Get("Location")
	}
	rolDe := func(n *navegador) (string, any) {
		_, m := n.pedir("GET", "/api/sesion", nil)
		idn, _ := m["identidad"].(map[string]any)
		if idn == nil {
			return "", nil
		}
		return idn["rol"].(string), idn["cuits"]
	}

	// Pase válido: crea a la persona (origen arcanum) con el rol traducido.
	st, m := pedirPase(suite, cuerpo)
	if st != 200 || !strings.HasPrefix(m["url"].(string), e.srv.URL+"/suite/entrar?pase=") || m["vence"] == nil {
		t.Fatalf("pase: %d %v", st, m)
	}
	url := m["url"].(string)
	n, loc := entrar(url)
	if loc != "/" {
		t.Errorf("entrar lleva a /: %q", loc)
	}
	if rol, _ := rolDe(n); rol != "usuario" {
		t.Errorf("operador de Arcanum es usuario: %q", rol)
	}
	u, err := e.app.Store.UsuarioPorEmail(context.Background(), "nueva@ejemplo.com")
	if err != nil || u.Origen != "arcanum" || u.TieneClave {
		t.Errorf("persona creada: %+v %v", u, err)
	}
	// Reusado.
	if n2, loc := entrar(url); loc != "/?aviso=pase" {
		t.Errorf("pase reusado: %q", loc)
	} else if r, _ := rolDe(n2); r != "" {
		t.Error("un pase reusado no abre sesión")
	}
	// Vencido.
	_, m = pedirPase(suite, cuerpo)
	_, _ = e.app.Store.Pool.Exec(context.Background(), `UPDATE pases_suite SET vence = now() - interval '1 second' WHERE usado IS NULL`)
	if _, loc := entrar(m["url"].(string)); loc != "/?aviso=pase" {
		t.Errorf("pase vencido: %q", loc)
	}
	// Rol topeado: alguien nuevo que es superadmin en Arcanum llega como usuario (el tope por defecto).
	_, m = pedirPase(suite, map[string]any{"app": "arcanum", "email": "jefa@ejemplo.com", "rol": "superadmin"})
	if n, _ := entrar(m["url"].(string)); func() string { r, _ := rolDe(n); return r }() != "usuario" {
		t.Error("el rol queda topeado por la clave")
	}
	// Una persona existente usa su rol de acá (no se escala por venir de otra app).
	_, _ = e.altaUsuario(t, sup, "lu@ejemplo.com", "lectura", nil)
	_, m = pedirPase(suite, map[string]any{"app": "arcanum", "email": "LU@ejemplo.com", "rol": "admin"})
	if n, _ := entrar(m["url"].(string)); func() string { r, _ := rolDe(n); return r }() != "lectura" {
		t.Error("una persona existente entra con su rol de Factorum")
	}
	// Un admin de acá, con una clave de tope usuario, entra como usuario.
	_, _ = e.altaUsuario(t, sup, "ana@ejemplo.com", "admin", nil)
	_, m = pedirPase(suite, map[string]any{"app": "arcanum", "email": "ana@ejemplo.com", "rol": "admin"})
	if n, _ := entrar(m["url"].(string)); func() string { r, _ := rolDe(n); return r }() != "usuario" {
		t.Error("el rol de una persona existente también se topea")
	}
	// Una clave con tope superadmin (la crea un superadmin a propósito) deja entrar como admin.
	alta := nuevaClave(map[string]any{"nombre": "Arcanum alta", "rol": "lectura", "suite": true, "suite_tope": "superadmin"})
	_, m = pedirPase(alta, map[string]any{"app": "arcanum", "email": "ana@ejemplo.com", "rol": "operador"})
	if n, _ := entrar(m["url"].(string)); func() string { r, _ := rolDe(n); return r }() != "admin" {
		t.Error("con tope alto manda el rol de Factorum")
	}
	// Desactivada: 403.
	_, _ = sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", u.ID), map[string]any{"activo": false})
	if st, _ := pedirPase(suite, cuerpo); st != 403 {
		t.Errorf("desactivada: %d", st)
	}
	// CUITs: se intersecan con los de la clave.
	soloX := nuevaClave(map[string]any{"nombre": "Arcanum X", "rol": "lectura", "cuits": []string{cuitX}, "suite": true})
	_, m = pedirPase(soloX, map[string]any{"app": "arcanum", "email": "mixta@ejemplo.com", "rol": "operador", "cuits": []string{cuitX, cuitY}})
	if n, _ := entrar(m["url"].(string)); func() string { _, c := rolDe(n); return fmt.Sprint(c) }() != "["+cuitX+"]" {
		t.Error("los CUITs se intersecan con los de la clave")
	}
	if u, _ := e.app.Store.UsuarioPorEmail(context.Background(), "mixta@ejemplo.com"); len(u.CUITs) != 2 {
		t.Errorf("la persona se crea con los CUITs que trae: %v", u.CUITs)
	}
	_, _ = e.altaUsuario(t, sup, "ye@ejemplo.com", "usuario", []string{cuitY})
	if st, _ := pedirPase(soloX, map[string]any{"app": "arcanum", "email": "ye@ejemplo.com", "rol": "operador"}); st != 403 {
		t.Errorf("sin CUITs en común: %d", st)
	}
	// Sin creación automática.
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"arcanum.suite_crear_usuarios": false}}); st != 200 {
		t.Fatal("apagar la creación")
	}
	if st, _ := pedirPase(suite, map[string]any{"app": "arcanum", "email": "nadie2@ejemplo.com", "rol": "operador"}); st != 403 {
		t.Errorf("sin creación automática: %d", st)
	}
	// La auditoría registra los pases emitidos y usados, nunca el token.
	_, _, raw := sup.pedirCrudo("GET", "/api/auditoria?accion=suite.&limite=200", nil)
	if !strings.Contains(string(raw), "suite.pase_emitido") || !strings.Contains(string(raw), "suite.pase_usado") {
		t.Errorf("auditoría de la suite: %.500s", raw)
	}
	if tok := strings.SplitN(url, "pase=", 2)[1]; strings.Contains(string(raw), tok) {
		t.Error("el token del pase no puede quedar en la auditoría")
	}

	// --- Origen: abrir Arcanum ---
	_, m = sup.pedir("GET", "/api/suite/apps", nil)
	apps := m["apps"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["app"] != "arcanum" || apps[0].(map[string]any)["disponible"] != true {
		t.Errorf("apps: %v", m)
	}
	st, m = sup.pedir("POST", "/api/suite/abrir/arcanum", nil)
	if st != 200 || m["url"] != "https://arcanum.ejemplo.com/suite/entrar?pase=abc" {
		t.Fatalf("abrir Arcanum: %d %v", st, m)
	}
	p := paseArcanum.Load().(map[string]any)
	if p["app"] != "factorum" || p["email"] != "diego@ejemplo.com" || p["rol"] != "superadmin" || p["cuits"] != nil {
		t.Errorf("lo que se manda a Arcanum: %v", p)
	}
	if st, _ := e.pedirJSON(t, "POST", "/api/suite/abrir/arcanum", claveOp, nil); st != 403 {
		t.Errorf("abrir con una clave: %d", st)
	}
	// Si Arcanum tiene URL pública, la dirección del pase tiene que ser de ahí.
	_, _ = sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"arcanum.url_publica": "https://otro.ejemplo.com"}})
	if st, _ := sup.pedir("POST", "/api/suite/abrir/arcanum", nil); st != 502 {
		t.Errorf("pase a otra dirección: %d", st)
	}
}

// FACTORUM_API_KEY se habilita para la suite con FACTORUM_API_KEY_SUITE_TOPE (es la que usa Arcanum).
func TestSuitePaseConLaClaveDelEntorno(t *testing.T) {
	e := armarConCfg(t, nil, func(c *config.Config) {
		c.APIKeySuiteTope = "admin"
		c.URLPublica = "https://factorum.ejemplo.com"
	})
	st, m := e.pedirJSON(t, "POST", "/api/suite/pase", claveOp, map[string]any{"app": "arcanum", "email": "a@ejemplo.com", "rol": "admin", "cuits": nil})
	if st != 200 || !strings.HasPrefix(m["url"].(string), "https://factorum.ejemplo.com/suite/entrar?pase=") {
		t.Fatalf("pase con la clave del entorno: %d %v", st, m)
	}
	u, _ := e.app.Store.UsuarioPorEmail(context.Background(), "a@ejemplo.com")
	if u == nil || u.Rol.String() != "admin" {
		t.Errorf("rol: %+v", u)
	}
	if st, _ := e.pedirJSON(t, "POST", "/api/suite/pase", claveLect, map[string]any{"app": "arcanum", "email": "b@ejemplo.com", "rol": "lectura"}); st != 403 {
		t.Errorf("la de lectura no está habilitada: %d", st)
	}
}

// Límite de intentos: después de cinco fallos hay que esperar.
func TestLimiteDeIntentos(t *testing.T) {
	e := armar(t)
	e.crearSuperadmin(t)
	n := e.navegador(t)
	for i := 0; i < 5; i++ {
		if st, _ := n.pedir("POST", "/api/sesion", map[string]string{"email": "diego@ejemplo.com", "clave": "mala mala mala"}); st != 401 {
			t.Fatalf("intento %d: %d", i, st)
		}
	}
	st, m := n.pedir("POST", "/api/sesion", map[string]string{"email": "diego@ejemplo.com", "clave": "una clave muy larga"})
	if st != 429 || !strings.Contains(m["error"].(string), "Probá de nuevo") {
		t.Errorf("tiene que esperar aunque acierte: %d %v", st, m)
	}
}
