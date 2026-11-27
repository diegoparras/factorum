package api_test

// La papelera de punta a punta, contra Postgres real: borrar lotes y comprobantes sueltos (con
// permisos y alcance), que nada de lo que lista, cuenta, resume o exporta lo vea, restaurar, volver
// a subir lo borrado, el conflicto de archivo igual, el borrado definitivo con los archivos del disco
// y de R2 (y los compartidos), las copias de la base con cosas en la papelera y cambiar el cliente.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/pruebas"
	"github.com/diegoparras/factorum/internal/store"
)

// cargarDe carga un lote con la clave de rol usuario y espera a que termine (y a la constatación).
func (e *entorno) cargarDe(t *testing.T, cliente string, constatar bool, archivos ...string) int64 {
	t.Helper()
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cliente, "constatar": fmt.Sprint(constatar), "nombre": "Lote " + cliente}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	id := int64(m["lote"].(map[string]any)["id"].(float64))
	e.esperarFin(t, id, constatar)
	return id
}

// esperarFin espera que el lote termine (y, si se constata, que no le falte nada).
func (e *entorno) esperarFin(t *testing.T, id int64, constatar bool) {
	t.Helper()
	if constatar {
		e.esperarLote(t, id)
		return
	}
	esperar(t, fmt.Sprintf("que termine el lote %d", id), func() bool {
		_, m, _ := e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", id), claveLect, nil, "")
		l, _ := m["lote"].(map[string]any)
		return l != nil && l["estado"] == "terminado"
	})
}

func idsDe(cbs []map[string]any) []int64 {
	var res []int64
	for _, c := range cbs {
		res = append(res, int64(c["id"].(float64)))
	}
	return res
}

// mcp llama a una herramienta de MCP con la clave de lectura y devuelve el texto de la respuesta.
func (e *entorno) mcp(t *testing.T, herramienta, args string) string {
	t.Helper()
	cuerpo := fmt.Sprintf(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, herramienta, args)
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", strings.NewReader(cuerpo))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+claveLect)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b)
}

func items(t *testing.T, n *navegador) []map[string]any {
	t.Helper()
	st, m := n.pedir("GET", "/api/papelera", nil)
	if st != 200 {
		t.Fatalf("papelera: %d %v", st, m)
	}
	var res []map[string]any
	for _, x := range m["items"].([]any) {
		res = append(res, x.(map[string]any))
	}
	return res
}

func ref(tipo string, id int64) map[string]any { return map[string]any{"tipo": tipo, "id": id} }

func TestPapeleraPuntaAPunta(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	ctx := context.Background()
	sup := e.crearSuperadmin(t)
	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	ana, _ := e.altaUsuario(t, sup, "ana@ejemplo.com", "usuario", []string{cuitX})
	beto, _ := e.altaUsuario(t, sup, "beto@ejemplo.com", "usuario", []string{cuitY})
	lector, _ := e.altaUsuario(t, sup, "lector@ejemplo.com", "lectura", nil)

	loteX := e.cargarDe(t, cuitX, false, "pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf", "pdf/08_dos_facturas.pdf")
	loteY := e.cargarDe(t, cuitY, false, "pdf/05_NCA.pdf")
	cbsX := e.cbtes(t, fmt.Sprintf("lote=%d", loteX))
	if len(cbsX) != 4 {
		t.Fatalf("comprobantes del lote X: %d", len(cbsX))
	}
	var de08 []int64
	var doc08 float64
	for _, c := range cbsX {
		if c["archivo"] == "08_dos_facturas.pdf" {
			de08 = append(de08, int64(c["id"].(float64)))
			doc08 = c["documento_id"].(float64)
		}
	}
	cbY := idsDe(e.cbtes(t, fmt.Sprintf("lote=%d", loteY)))
	_, m, _ := e.pedir(t, "GET", "/api/panel", claveLect, nil, "")
	totalAntes := m["panel"].(map[string]any)["total"].(float64)

	// --- Permisos ---
	if st, _ := lector.pedir("POST", "/api/comprobantes/borrar", map[string]any{"ids": de08}); st != 403 {
		t.Fatalf("lectura no borra: %d", st)
	}
	if st, _ := ana.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteX), nil); st != 403 {
		t.Fatalf("usuario no borra lotes: %d", st)
	}
	if st, _ := e.pedirJSON(t, "DELETE", fmt.Sprintf("/api/lotes/%d", loteX), claveOp, nil); st != 403 {
		t.Fatalf("una clave de rol usuario no borra lotes: %d", st)
	}
	if st, _ := lector.pedir("GET", "/api/papelera", nil); st != 403 {
		t.Fatalf("lectura no ve la papelera: %d", st)
	}
	if st, m := ana.pedir("POST", "/api/comprobantes/borrar", map[string]any{"ids": append(slices.Clone(de08), cbY...)}); st != 404 || !strings.Contains(m["error"].(string), fmt.Sprint(cbY[0])) {
		t.Fatalf("ana no borra lo de Y (y entonces no borra nada): %d %v", st, m)
	}
	if n := len(e.cbtes(t, fmt.Sprintf("lote=%d", loteX))); n != 4 {
		t.Fatalf("no se borró ninguno: %d", n)
	}

	// --- Borrar sueltos: los dos de 08 (el documento se queda vacío y se va con ellos) ---
	st, m := ana.pedir("POST", "/api/comprobantes/borrar", map[string]any{"ids": de08})
	if st != 200 || num(m, "borrado", "comprobantes") != 2 || num(m, "borrado", "documentos") != 1 || num(m, "dias") != 30 {
		t.Fatalf("borrar sueltos: %d %v", st, m)
	}
	if n := len(e.cbtes(t, fmt.Sprintf("lote=%d", loteX))); n != 2 {
		t.Fatalf("quedan dos en el lote: %d", n)
	}
	if st, _, _ := e.pedir(t, "GET", fmt.Sprintf("/api/comprobantes/%d", de08[0]), claveLect, nil, ""); st != 404 {
		t.Fatalf("un comprobante de la papelera: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", fmt.Sprintf("/api/documentos/%v/archivo", doc08), claveLect, nil, ""); st != 404 {
		t.Fatalf("el archivo de un documento de la papelera: %d", st)
	}
	if st, _ := ana.pedir("PATCH", fmt.Sprintf("/api/comprobantes/%d", de08[0]), map[string]any{"aprobar": true}); st != 404 {
		t.Fatalf("aprobar algo de la papelera: %d", st)
	}
	// Ningún listado, resumen, export, panel ni MCP lo ve.
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", loteX), claveLect, nil, "")
	if l := m["lote"].(map[string]any); l["documentos"].(float64) != 2 {
		t.Fatalf("el lote cuenta los documentos vivos: %v", l["documentos"])
	}
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/resumen?lote=%d", loteX), claveLect, nil, "")
	if m["resumen"].(map[string]any)["cantidad"].(float64) != 2 {
		t.Fatalf("resumen: %v", m["resumen"])
	}
	_, _, raw := e.pedir(t, "GET", fmt.Sprintf("/api/export.csv?lote=%d", loteX), claveLect, nil, "")
	if strings.Contains(string(raw), "08_dos_facturas.pdf") || !strings.Contains(string(raw), "01_FA_21.pdf") {
		t.Fatalf("el export no lleva lo de la papelera: %.400s", raw)
	}
	_, m, _ = e.pedir(t, "GET", "/api/panel", claveLect, nil, "")
	if m["panel"].(map[string]any)["total"].(float64) != totalAntes-2 {
		t.Fatalf("panel: %v y antes %v", m["panel"].(map[string]any)["total"], totalAntes)
	}
	if out := e.mcp(t, "buscar_comprobantes", fmt.Sprintf(`{"lote":%d}`, loteX)); !strings.Contains(out, `\"total\":2`) {
		t.Fatalf("MCP buscar_comprobantes: %.400s", out)
	}
	if out := e.mcp(t, "comprobante", fmt.Sprintf(`{"id":%d}`, de08[0])); !strings.Contains(out, `"isError":true`) {
		t.Fatalf("MCP comprobante de la papelera: %.300s", out)
	}
	// La papelera: ana ve sus dos; beto, nada.
	if it := items(t, ana); len(it) != 2 || it[0]["tipo"] != "comprobante" || it[0]["borrado_por"] != "ana@ejemplo.com" || it[0]["dias_restantes"].(float64) != 30 {
		t.Fatalf("papelera de ana: %v", it)
	}
	if it := items(t, beto); len(it) != 0 {
		t.Fatalf("beto no ve lo de ana: %v", it)
	}

	// --- Volver a subir lo borrado: no es repetido, se procesa como nuevo ---
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cuitX, "constatar": "false"}, "pdf/08_dos_facturas.pdf")
	st, m, raw = e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 || m["repetidos"].(float64) != 0 {
		t.Fatalf("volver a subir: %d %s", st, raw)
	}
	loteNuevo := int64(m["lote"].(map[string]any)["id"].(float64))
	e.esperarFin(t, loteNuevo, false)
	nuevos := e.cbtes(t, fmt.Sprintf("lote=%d", loteNuevo))
	if len(nuevos) != 2 {
		t.Fatalf("lo vuelto a subir se leyó de nuevo: %d", len(nuevos))
	}
	for _, c := range nuevos {
		if c["duplicado_de"] != nil {
			t.Fatalf("lo de la papelera no hace duplicado: %v", c)
		}
	}
	// Restaurar el viejo dejaría dos iguales: no.
	st, m = ana.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("comprobante", de08[0])}})
	if st != 409 || !strings.Contains(m["error"].(string), "Ya hay uno igual cargado después") || !strings.Contains(m["error"].(string), "borrá ese primero") {
		t.Fatalf("restaurar con el mismo archivo cargado después: %d %v", st, m)
	}

	// --- Borrar el lote entero (admin, con la confirmación en la pantalla) ---
	st, m = adm.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteX), nil)
	if st != 200 || num(m, "borrado", "documentos") != 2 || num(m, "borrado", "comprobantes") != 2 {
		t.Fatalf("borrar el lote: %d %v", st, m)
	}
	if st, _ := adm.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteX), nil); st != 404 {
		t.Fatalf("borrarlo otra vez: %d", st)
	}
	if st, _, _ := e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", loteX), claveLect, nil, ""); st != 404 {
		t.Fatalf("el lote de la papelera: %d", st)
	}
	_, m, _ = e.pedir(t, "GET", "/api/lotes", claveLect, nil, "")
	if m["total"].(float64) != 2 {
		t.Fatalf("lotes: %v", m["total"])
	}
	if out := e.mcp(t, "estado_lote", fmt.Sprintf(`{"id":%d}`, loteX)); !strings.Contains(out, `"isError":true`) {
		t.Fatalf("MCP estado_lote de la papelera: %.300s", out)
	}
	it := items(t, ana)
	if len(it) != 3 || it[0]["tipo"] != "lote" || it[0]["borrado_por"] != "admin@ejemplo.com" || it[0]["comprobantes"].(float64) != 2 {
		t.Fatalf("papelera con el lote: %v", it)
	}
	if it[1]["lote_borrado"] != true {
		t.Fatalf("los sueltos dicen que su lote también está en la papelera: %v", it[1])
	}

	// --- Restaurar: ana el lote (vuelve como estaba); el suelto sigue con su conflicto ---
	st, m = ana.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("lote", loteX), ref("comprobante", de08[1])}})
	if st != 200 || m["restaurados"].(float64) != 1 || len(m["rechazados"].([]any)) != 1 {
		t.Fatalf("restaurar: %d %v", st, m)
	}
	if n := len(e.cbtes(t, fmt.Sprintf("lote=%d", loteX))); n != 2 {
		t.Fatalf("volvió el lote con sus dos: %d", n)
	}
	e.seVenLosArchivos(t, "después de restaurar")
	if st, m := beto.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("comprobante", de08[1])}}); st != 404 {
		t.Fatalf("beto no restaura lo de X: %d %v", st, m)
	}

	// --- Vaciar y Borrar ya: solo un superadmin con sesión, escribiendo «borrar» ---
	if st, _ := adm.pedir("DELETE", "/api/papelera", map[string]any{"confirmacion": "borrar"}); st != 403 {
		t.Fatalf("admin no vacía: %d", st)
	}
	if st, _ := e.pedirJSON(t, "DELETE", "/api/papelera", claveOp, map[string]any{"confirmacion": "borrar"}); st != 403 {
		t.Fatalf("una clave no vacía: %d", st)
	}
	if st, _ := sup.pedir("POST", "/api/papelera/borrar", map[string]any{"items": []any{ref("comprobante", de08[0])}, "confirmacion": "si"}); st != 400 {
		t.Fatalf("sin escribir «borrar»: %d", st)
	}
	st, m = sup.pedir("POST", "/api/papelera/borrar", map[string]any{"items": []any{ref("comprobante", de08[0])}, "confirmacion": "borrar"})
	if st != 200 || num(m, "borrado", "comprobantes") != 1 || num(m, "borrado", "documentos") != 0 {
		t.Fatalf("borrar ya uno: %d %v", st, m)
	}
	st, m = sup.pedir("DELETE", "/api/papelera", map[string]any{"confirmacion": "BORRAR"})
	if st != 200 || num(m, "borrado", "comprobantes") != 1 || num(m, "borrado", "documentos") != 1 {
		t.Fatalf("vaciar: %d %v", st, m)
	}
	if it := items(t, sup); len(it) != 0 {
		t.Fatalf("quedó algo: %v", it)
	}

	// --- Auditoría ---
	_, m = sup.pedir("GET", "/api/auditoria?limite=200", nil)
	var acciones []string
	for _, ev := range m["eventos"].([]any) {
		acciones = append(acciones, ev.(map[string]any)["accion"].(string))
	}
	for _, a := range []string{"comprobante.borrar", "lote.borrar", "papelera.restaurar", "papelera.borrar_ya", "papelera.vaciar"} {
		if !slices.Contains(acciones, a) {
			t.Errorf("falta %s en la auditoría: %v", a, acciones)
		}
	}
	_ = ctx
}

// Un lote que se está procesando: se cancela lo que falta en la cola, no se guarda nada después y
// al restaurarlo se termina de procesar.
func TestBorrarLoteEnProceso(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	ctx := context.Background()
	sup := e.crearSuperadmin(t)
	archivos := []string{"pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf", "pdf/03_FC_servicios.pdf", "pdf/04_FB_consumidor.pdf", "pdf/05_NCA.pdf",
		"pdf/06_FA_no_cierra.pdf", "pdf/07_FA_dolares.pdf", "pdf/08_dos_facturas.pdf", "pdf/09_FA_adulterada.pdf", "foto_fb.png"}
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cuitX, "constatar": "false"}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	id := int64(m["lote"].(map[string]any)["id"].(float64))
	st, m = sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", id), nil)
	if st != 200 {
		t.Fatalf("borrar en proceso: %d %v", st, m)
	}
	// La cola se calma: nada de ese lote queda por correr, y nada vivo aparece después.
	esperar(t, "que no quede nada en la cola", func() bool {
		var n int
		_ = e.app.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind='procesar_documento' AND state IN ('available','running','retryable','scheduled')`).Scan(&n)
		return n == 0
	})
	time.Sleep(300 * time.Millisecond)
	var vivos int
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM comprobantes WHERE lote_id=$1 AND borrado_en IS NULL`, id).Scan(&vivos)
	if vivos != 0 {
		t.Fatalf("después de borrar se guardaron %d comprobantes vivos", vivos)
	}
	st, m = sup.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("lote", id)}})
	if st != 200 {
		t.Fatalf("restaurar: %d %v", st, m)
	}
	e.esperarFin(t, id, false)
	_, m, _ = e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", id), claveLect, nil, "")
	if l := m["lote"].(map[string]any); l["documentos"].(float64) != 10 || l["procesados"].(float64) != 10 {
		t.Fatalf("el lote restaurado terminó de procesarse: %v", l)
	}
	if n := len(e.cbtes(t, fmt.Sprintf("lote=%d", id))); n != 11 {
		t.Fatalf("comprobantes: %d", n)
	}
}

// El borrado definitivo a los N días: borra lo vencido y los archivos que no usa nadie más, del disco
// y de R2; deja los compartidos.
func TestPurgaConDiscoYR2(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	ctx := context.Background()
	sup := e.crearSuperadmin(t)
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"papelera.dias": 5}}); st != 200 {
		t.Fatalf("días de la papelera: %d %v", st, m)
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"papelera.dias": 400}}); st != 400 {
		t.Fatalf("400 días no: %d", st)
	}
	loteA := e.cargarDe(t, cuitX, false, "pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf")
	sha := func(archivo string) string {
		var s string
		if err := e.app.Store.Pool.QueryRow(ctx, `SELECT sha256 FROM documentos WHERE nombre=$1 ORDER BY id LIMIT 1`, archivo).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	sha01, sha02 := sha("01_FA_21.pdf"), sha("02_FA_mixta.pdf")

	// R2 activo y lo del disco pasado (queda en los dos hasta liberar).
	falso := pruebas.S3Falso(t)
	e.aj.Suscribir(func(_, n *config.Config) {
		if n.R2Endpoint != "" {
			s3, err := almacen.NuevoS3(n.R2Endpoint, n.R2Bucket, n.R2AccessKey, n.R2SecretKey, n.R2Region, n.R2Prefijo)
			if err != nil {
				t.Error(err)
				return
			}
			e.app.Almacen.CambiarBackend(s3, e.disco)
		}
	})
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"r2.endpoint": falso.URL, "r2.bucket": falso.Bucket,
		"r2.access_key": falso.Access, "r2.secret_key": falso.Secret, "r2.prefijo": "fct/"}}); st != 200 {
		t.Fatalf("activar R2: %d %v", st, m)
	}
	if st, m := sup.pedir("POST", "/api/r2/pase", map[string]string{"accion": "iniciar"}); st != 200 {
		t.Fatalf("pase: %d %v", st, m)
	}
	sup.esperarPase("terminada")
	// Lote B: 03 (nuevo, solo en R2) y 01 otra vez (repetido: comparte el archivo con A).
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": cuitX, "constatar": "false"}, "pdf/03_FC_servicios.pdf", "pdf/01_FA_21.pdf")
	_, m, _ := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if m["repetidos"].(float64) != 1 {
		t.Fatalf("01 es repetido: %v", m)
	}
	loteB := int64(m["lote"].(map[string]any)["id"].(float64))
	e.esperarFin(t, loteB, false)
	sha03 := sha("03_FC_servicios.pdf")

	enDisco := func(s string) bool { ok, _ := e.disco.Existe(ctx, "originales/"+s); return ok }
	enR2 := func(s string) bool { return slices.Contains(falso.Claves(t), "fct/originales/"+s) }
	if !enDisco(sha01) || !enR2(sha01) || !enDisco(sha02) || !enR2(sha02) || enDisco(sha03) || !enR2(sha03) {
		t.Fatal("dónde está cada archivo antes de borrar")
	}

	if st, _ := sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteA), nil); st != 200 {
		t.Fatal("borrar A")
	}
	// Al día 4 no vence (son 5 días).
	if p, err := e.app.PurgarPapelera(ctx, time.Now().Add(4*24*time.Hour)); err != nil || !p.Vacia() {
		t.Fatalf("al día 4: %+v %v", p, err)
	}
	// Mientras corre el pase a R2, las filas se borran pero los archivos esperan.
	if _, err := e.app.Store.Pool.Exec(ctx, `UPDATE r2_migracion SET estado='corriendo'`); err != nil {
		t.Fatal(err)
	}
	p, err := e.app.PurgarPapelera(ctx, time.Now().Add(6*24*time.Hour))
	if err != nil || p.Lotes != 1 || p.Documentos != 2 || p.Comprobantes != 2 || p.Archivos != 0 {
		t.Fatalf("al día 6, con el pase corriendo: %+v %v", p, err)
	}
	if !enDisco(sha02) || !enR2(sha02) {
		t.Fatal("con el pase corriendo no se toca el almacén")
	}
	if _, err := e.app.Store.Pool.Exec(ctx, `UPDATE r2_migracion SET estado='terminada'`); err != nil {
		t.Fatal(err)
	}
	p, err = e.app.PurgarPapelera(ctx, time.Now().Add(6*24*time.Hour))
	if err != nil || p.Archivos != 1 {
		t.Fatalf("después del pase: %+v %v", p, err)
	}
	// 02 no lo usaba nadie más: fuera del disco, de R2 y del pase. 01 lo usa el repetido de B.
	if enDisco(sha02) || enR2(sha02) || slices.Contains(falso.Claves(t), "fct/resultados/"+sha02+".json") {
		t.Fatal("02 tenía que irse del disco y de R2")
	}
	var r2 int
	_ = e.app.Store.Pool.QueryRow(ctx, `SELECT count(*) FROM r2_objetos WHERE clave LIKE '%'||$1||'%'`, sha02).Scan(&r2)
	if r2 != 0 {
		t.Fatalf("02 sigue en el pase a R2: %d", r2)
	}
	if !enDisco(sha01) || !enR2(sha01) {
		t.Fatal("01 lo sigue usando el lote B")
	}
	if st, _ := sup.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("lote", loteA)}}); st != 404 {
		t.Fatalf("lo purgado no se restaura: %d", st)
	}
	e.seVenLosArchivos(t, "con lo compartido")

	// B también: ahora sí se van 01 (del disco y de R2) y 03 (de R2).
	if st, _ := sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteB), nil); st != 200 {
		t.Fatal("borrar B")
	}
	p, err = e.app.PurgarPapelera(ctx, time.Now().Add(6*24*time.Hour))
	if err != nil || p.Lotes != 1 || p.Archivos != 2 {
		t.Fatalf("purgar B: %+v %v", p, err)
	}
	if enDisco(sha01) || enR2(sha01) || enR2(sha03) {
		t.Fatal("01 y 03 tenían que irse")
	}
	_, m = sup.pedir("GET", "/api/auditoria?accion=papelera.purgar", nil)
	if ev := m["eventos"].([]any); len(ev) != 3 || ev[0].(map[string]any)["actor"] != "Factorum" {
		t.Fatalf("auditoría del borrado automático: %v", ev)
	}
	// Volver a subir lo purgado: se guarda de nuevo.
	lote := e.cargarDe(t, cuitX, false, "pdf/01_FA_21.pdf")
	if n := len(e.cbtes(t, fmt.Sprintf("lote=%d", lote))); n != 1 || !enR2(sha01) {
		t.Fatalf("lo purgado se vuelve a subir: %d %v", n, enR2(sha01))
	}
}

// La copia de la base lleva lo de la papelera (y restaurada, sigue en la papelera y se puede
// restaurar).
func TestCopiaConPapelera(t *testing.T) {
	ctx := context.Background()
	dirAlm := t.TempDir()
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t), dirAlmacen: dirAlm})
	sup := e.crearSuperadmin(t)
	loteA := e.cargarDe(t, cuitX, false, "pdf/01_FA_21.pdf", "pdf/08_dos_facturas.pdf")
	loteB := e.cargarDe(t, cuitX, false, "pdf/05_NCA.pdf")
	var suelto int64
	for _, c := range e.cbtes(t, fmt.Sprintf("lote=%d", loteA)) {
		if c["archivo"] == "01_FA_21.pdf" {
			suelto = int64(c["id"].(float64))
		}
	}
	if st, _ := sup.pedir("POST", "/api/comprobantes/borrar", map[string]any{"ids": []int64{suelto}}); st != 200 {
		t.Fatal("borrar suelto")
	}
	if st, _ := sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", loteB), nil); st != 200 {
		t.Fatal("borrar B")
	}
	antes := items(t, sup)

	var copia bytes.Buffer
	if _, err := copias.Volcar(ctx, e.app.Store.Pool, e.app.Almacen, &copia, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	url2 := pruebas.BaseNueva(t)
	st2, err := store.Abrir(ctx, url2, 4)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copias.Restaurar(ctx, st2.Pool, func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(copia.Bytes())), nil }, e.app.Almacen,
		copias.OpcionesRestaurar{Confirmar: true, Migraciones: store.Migraciones()}); err != nil {
		t.Fatal(err)
	}
	st2.Cerrar()
	e2 := armarOpc(t, opcEntorno{url: url2, dirAlmacen: dirAlm})
	diego := e2.entrar(t, "diego@ejemplo.com", "una clave muy larga")
	despues := items(t, diego)
	if len(despues) != len(antes) || len(despues) != 2 {
		t.Fatalf("la papelera restaurada: %v y antes %v", despues, antes)
	}
	for i := range antes {
		if antes[i]["tipo"] != despues[i]["tipo"] || antes[i]["id"] != despues[i]["id"] || antes[i]["borrado_en"] != despues[i]["borrado_en"] || antes[i]["borrado_por"] != despues[i]["borrado_por"] {
			t.Fatalf("%v y %v", antes[i], despues[i])
		}
	}
	_, m := diego.pedir("GET", "/api/comprobantes", nil)
	if m["total"].(float64) != 2 {
		t.Fatalf("en la base restaurada no se ve lo de la papelera: %v", m["total"])
	}
	if st, m := diego.pedir("POST", "/api/papelera/restaurar", map[string]any{"items": []any{ref("lote", loteB), ref("comprobante", suelto)}}); st != 200 || m["restaurados"].(float64) != 2 {
		t.Fatalf("restaurar en la base restaurada: %d %v", st, m)
	}
	e2.seVenLosArchivos(t, "lo restaurado de la papelera, en la base restaurada")
}

// Cambiar el cliente de un lote: valida el CUIT y el alcance, pasa los comprobantes y vuelve a
// constatar con el CUIT consultante nuevo lo que se había constatado con el viejo.
func TestCambiarCliente(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	sup := e.crearSuperadmin(t)
	ana, _ := e.altaUsuario(t, sup, "ana@ejemplo.com", "usuario", []string{cuitX})
	lector, _ := e.altaUsuario(t, sup, "lector@ejemplo.com", "lectura", nil)
	if !arca.CUITValido(cuitZ) {
		t.Fatal("cuitZ tiene que ser válido")
	}
	lote := e.cargarDe(t, cuitX, true, "pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf")
	cbs := e.cbtes(t, fmt.Sprintf("lote=%d", lote))
	clave := func(c map[string]any) string {
		return fmt.Sprintf("%v-%v-%v", c["tipo"], c["punto_venta"], c["numero"])
	}
	for _, c := range cbs {
		if v, _ := consultantesVistos.Load(clave(c)); v != cuitX || !strings.Contains(fmt.Sprint(c["hallazgos"]), "constatado") {
			t.Fatalf("constatado con el cliente: %v %v", v, c["hallazgos"])
		}
	}
	ruta := fmt.Sprintf("/api/lotes/%d", lote)
	if st, _ := lector.pedir("PATCH", ruta, map[string]any{"cliente_cuit": cuitZ}); st != 403 {
		t.Fatalf("lectura no cambia el cliente: %d", st)
	}
	if st, m := sup.pedir("PATCH", ruta, map[string]any{"cliente_cuit": "20-33333333-5"}); st != 400 || !strings.Contains(m["error"].(string), "dígito verificador") {
		t.Fatalf("CUIT inválido: %d %v", st, m)
	}
	if st, m := ana.pedir("PATCH", ruta, map[string]any{"cliente_cuit": cuitZ}); st != 403 || !strings.Contains(m["error"].(string), "no está entre tus clientes") {
		t.Fatalf("ana no lo pasa a un cliente que no ve: %d %v", st, m)
	}
	st, m := sup.pedir("PATCH", ruta, map[string]any{"cliente_cuit": arca.FormatearCUIT(cuitZ)})
	if st != 200 || m["lote"].(map[string]any)["cliente_cuit"] != cuitZ || num(m, "cambio", "comprobantes") != 2 || num(m, "cambio", "reconstatar") != 2 || m["cambio"].(map[string]any)["constatacion_encolada"] != true {
		t.Fatalf("cambiar: %d %v", st, m)
	}
	if st, _ := ana.pedir("GET", ruta, nil); st != 404 {
		t.Fatalf("ana ya no lo ve: %d", st)
	}
	// Se vuelve a constatar con el CUIT nuevo.
	esperar(t, "la constatación con el CUIT nuevo", func() bool {
		for _, c := range e.cbtes(t, fmt.Sprintf("lote=%d", lote)) {
			if v, _ := consultantesVistos.Load(clave(c)); v != cuitZ || c["constatacion"] == nil || strings.Contains(fmt.Sprint(c["hallazgos"]), "reconstatar") {
				return false
			}
		}
		return true
	})
	if n := len(e.cbtes(t, "cliente="+cuitZ)); n != 2 {
		t.Fatalf("los comprobantes pasaron al cliente nuevo: %d", n)
	}
	_, m = sup.pedir("GET", "/api/auditoria?accion=lote.cambiar_cliente", nil)
	if ev := m["eventos"].([]any); len(ev) != 1 || !strings.Contains(fmt.Sprint(ev[0]), cuitZ) {
		t.Fatalf("auditoría: %v", ev)
	}
}

// Los chats: el «estado» no ve un lote borrado y lo nuevo no se suma a él.
func TestChatConLoteBorrado(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	ctx := context.Background()
	sup := e.crearSuperadmin(t)
	pdf, err := os.ReadFile(filepath.Join("..", "..", "testdata", "pdf", "01_FA_21.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	msg := canal.Mensaje{Canal: "webhook", Chat: "chat-1", Remitente: "5491100000000", Archivos: []canal.Archivo{{Nombre: "a.pdf", Datos: pdf}}}
	if r := e.app.ManejarMensaje(ctx, msg); !strings.Contains(r, "Recibí 1") {
		t.Fatalf("cargar por el chat: %q", r)
	}
	l, err := e.app.Store.UltimoLoteDeChat(ctx, "webhook", "chat-1")
	if err != nil {
		t.Fatal(err)
	}
	e.esperarFin(t, l.ID, l.Constatar)
	if st, _ := sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", l.ID), nil); st != 200 {
		t.Fatal("borrar")
	}
	if r := e.app.ManejarMensaje(ctx, canal.Mensaje{Canal: "webhook", Chat: "chat-1", Texto: "estado"}); !strings.Contains(r, "Todavía no me mandaste") {
		t.Fatalf("estado: %q", r)
	}
	// Lo que llega ahora va a un lote nuevo (y no es repetido: el de antes está en la papelera).
	if r := e.app.ManejarMensaje(ctx, msg); !strings.Contains(r, "Recibí 1") {
		t.Fatalf("otra vez: %q", r)
	}
	n, _ := e.app.Store.UltimoLoteDeChat(ctx, "webhook", "chat-1")
	if n == nil || n.ID == l.ID {
		t.Fatalf("lote nuevo: %v", n)
	}
}
