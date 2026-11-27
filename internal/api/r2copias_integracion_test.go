package api_test

// El pase de lo del disco a R2 y las copias de la base, desde la pantalla, y la ida y vuelta
// completa: base con datos -> copia -> base nueva -> «factorum restaurar» -> todo igual y Factorum
// funciona sobre la base restaurada.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/pruebas"
	"github.com/diegoparras/factorum/internal/store"
)

// esperar repite f hasta que devuelve true.
func esperar(t *testing.T, que string, f func() bool) {
	t.Helper()
	limite := time.Now().Add(60 * time.Second)
	for !f() {
		if time.Now().After(limite) {
			t.Fatalf("no pasó: %s", que)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (e *entorno) cargarLote(t *testing.T, archivos ...string) int64 {
	t.Helper()
	body, tipo := multipartDe(t, map[string]string{"cliente_cuit": "20-22222222-3", "constatar": "false"}, archivos...)
	st, m, raw := e.pedir(t, "POST", "/api/lotes", claveOp, body, tipo)
	if st != 202 {
		t.Fatalf("crear lote: %d %s", st, raw)
	}
	id := int64(m["lote"].(map[string]any)["id"].(float64))
	esperar(t, "que termine el lote", func() bool {
		_, m, _ := e.pedir(t, "GET", fmt.Sprintf("/api/lotes/%d", id), claveLect, nil, "")
		return m["lote"].(map[string]any)["estado"] == "terminado"
	})
	return id
}

func (e *entorno) primerDocumento(t *testing.T) int64 {
	var d int64
	if err := e.app.Store.Pool.QueryRow(context.Background(), `SELECT min(id) FROM documentos`).Scan(&d); err != nil {
		t.Fatal(err)
	}
	return d
}

// seVenLosArchivos: el original y la primera página de cada documento se pueden ver.
func (e *entorno) seVenLosArchivos(t *testing.T, cuando string) {
	t.Helper()
	rows, err := e.app.Store.Pool.Query(context.Background(), `SELECT id FROM documentos WHERE estado IN ('listo','repetido') AND borrado_en IS NULL ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var docs []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		docs = append(docs, id)
	}
	rows.Close()
	if len(docs) == 0 {
		t.Fatalf("%s: no hay documentos", cuando)
	}
	for _, d := range docs {
		st, _, raw := e.pedir(t, "GET", fmt.Sprintf("/api/documentos/%d/archivo", d), claveLect, nil, "")
		if st != 200 || len(raw) < 100 {
			t.Fatalf("%s: el original de %d: %d", cuando, d, st)
		}
		if st, _, _ := e.pedir(t, "GET", fmt.Sprintf("/api/documentos/%d/pagina/1", d), claveLect, nil, ""); st != 200 {
			t.Fatalf("%s: la página de %d: %d", cuando, d, st)
		}
	}
}

func (n *navegador) esperarPase(estado string) map[string]any {
	n.t.Helper()
	var p map[string]any
	esperar(n.t, "pase en "+estado, func() bool {
		st, m := n.pedir("GET", "/api/r2/pase", nil)
		if st != 200 {
			n.t.Fatalf("pase: %d %v", st, m)
		}
		p = m["pase"].(map[string]any)
		return p["estado"] == estado && p["trabajando"] == false
	})
	return p
}

func num(m map[string]any, k ...string) float64 {
	var v any = m
	for _, x := range k {
		v = v.(map[string]any)[x]
	}
	f, _ := v.(float64)
	return f
}

func TestPaseAR2YCopiasDesdeLaPantalla(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	sup := e.crearSuperadmin(t)
	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	e.cargarLote(t, "pdf/01_FA_21.pdf", "pdf/02_FA_mixta.pdf", "pdf/03_FC_servicios.pdf", "pdf/05_NCA.pdf", "foto_fb.png")
	var enDisco int
	_ = e.disco.Recorrer(func(string, int64) error { enDisco++; return nil })
	if enDisco < 8 {
		t.Fatalf("en el disco hay %d objetos", enDisco)
	}

	// Solo un superadmin.
	if st, _ := adm.pedir("GET", "/api/r2/pase", nil); st != 403 {
		t.Fatalf("admin ve el pase: %d", st)
	}
	if st, _ := adm.pedir("GET", "/api/copias", nil); st != 403 {
		t.Fatalf("admin ve las copias: %d", st)
	}
	if st, m := sup.pedir("POST", "/api/r2/pase", map[string]string{"accion": "iniciar"}); st != 409 || !strings.Contains(m["error"].(string), "R2 no está activo") {
		t.Fatalf("sin R2: %d %v", st, m)
	}

	// Se activa R2 desde Configuración (en el servidor, el almacén se reconfigura en caliente).
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
	e.seVenLosArchivos(t, "con R2 recién activado")

	if st, m := sup.pedir("POST", "/api/r2/pase", map[string]string{"accion": "iniciar"}); st != 200 {
		t.Fatalf("iniciar: %d %v", st, m)
	}
	p := sup.esperarPase("terminada")
	if num(p, "hechos", "objetos") != float64(enDisco) || num(p, "errores", "objetos") != 0 || p["puede_liberar"] != true {
		t.Fatalf("terminado: %v", p)
	}
	if len(falso.Claves(t)) != enDisco {
		t.Fatalf("en R2: %d", len(falso.Claves(t)))
	}
	e.seVenLosArchivos(t, "después del pase")

	if st, m := sup.pedir("POST", "/api/r2/pase", map[string]string{"accion": "liberar", "confirmacion": "si"}); st != 409 || !strings.Contains(m["error"].(string), "liberar") {
		t.Fatalf("liberar sin escribir «liberar»: %d %v", st, m)
	}
	if st, m := sup.pedir("POST", "/api/r2/pase", map[string]string{"accion": "liberar", "confirmacion": "liberar"}); st != 200 {
		t.Fatalf("liberar: %d %v", st, m)
	}
	p = sup.esperarPase("liberada")
	if num(p, "liberados", "objetos") != float64(enDisco) {
		t.Fatalf("liberado: %v", p)
	}
	var quedan int
	_ = e.disco.Recorrer(func(string, int64) error { quedan++; return nil })
	if quedan != 0 {
		t.Fatalf("en el disco quedaron %d", quedan)
	}
	// Ahora todo sale de R2.
	e.seVenLosArchivos(t, "con el disco liberado")
	_, m := sup.pedir("GET", "/api/auditoria?accion=r2.", nil)
	var acciones []string
	for _, ev := range m["eventos"].([]any) {
		acciones = append(acciones, ev.(map[string]any)["accion"].(string))
	}
	if strings.Join(acciones, " ") != "r2.liberar_fin r2.liberar_inicio r2.migrar_fin r2.migrar_inicio" {
		t.Fatalf("auditoría del pase: %v", acciones)
	}

	// Copias: con R2 activo van al bucket.
	if st, m := sup.pedir("POST", "/api/copias", nil); st != 202 {
		t.Fatalf("hacer copia: %d %v", st, m)
	}
	var c map[string]any
	esperar(t, "la copia", func() bool {
		_, m := sup.pedir("GET", "/api/copias", nil)
		l := m["copias"].([]any)
		if len(l) == 0 {
			return false
		}
		c = l[0].(map[string]any)
		return c["estado"] == copias.OK
	})
	if c["destino"] != "r2" || c["disponible"] != true || c["por"] != "diego@ejemplo.com" {
		t.Fatalf("copia: %v", c)
	}
	st, _, raw := sup.pedirCrudo("GET", fmt.Sprintf("/api/copias/%d/archivo", int64(c["id"].(float64))), nil)
	if st != 200 || int64(len(raw)) != int64(c["tamano"].(float64)) {
		t.Fatalf("descargar: %d %d", st, len(raw))
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != c["sha256"] {
		t.Fatal("lo descargado no es la copia")
	}
	if _, err := copias.Leer(bytes.NewReader(raw), e.app.Almacen); err != nil {
		t.Fatalf("la copia descargada no se abre: %v", err)
	}

	// Si la última copia falló, Inicio se lo avisa a un superadmin (y a nadie más).
	if _, m := sup.pedir("GET", "/api/panel", nil); m["copia_fallida"] != nil {
		t.Fatal("no falló nada todavía")
	}
	if _, err := e.app.Store.Pool.Exec(context.Background(), `INSERT INTO copias (estado, destino, error) VALUES ('error', 'r2', 'R2 no respondió')`); err != nil {
		t.Fatal(err)
	}
	if _, m := sup.pedir("GET", "/api/panel", nil); m["copia_fallida"] == nil || m["copia_fallida"].(map[string]any)["error"] != "R2 no respondió" {
		t.Fatalf("aviso en Inicio: %v", m["copia_fallida"])
	}
	if _, m := adm.pedir("GET", "/api/panel", nil); m["copia_fallida"] != nil {
		t.Fatal("un admin no ve el aviso de las copias")
	}
}

// binario compila factorum (para probar los comandos de consola de verdad).
func binario(t *testing.T) string {
	t.Helper()
	b := filepath.Join(t.TempDir(), "factorum")
	cmd := exec.Command("go", "build", "-o", b, "./cmd/factorum")
	cmd.Dir = filepath.Join("..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compilando: %v\n%s", err, out)
	}
	return b
}

func manifiestoDe(t *testing.T, p *pgxpool.Pool, s *almacen.Almacen) *copias.Manifiesto {
	t.Helper()
	m, err := copias.Volcar(context.Background(), p, s, &bytes.Buffer{}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCopiaRestaurarYFactorumArranca(t *testing.T) {
	ctx := context.Background()
	dirAlm := t.TempDir()
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t), dirAlmacen: dirAlm})
	sup := e.crearSuperadmin(t)
	e.altaUsuario(t, sup, "ana@ejemplo.com", "lectura", []string{cuitX})
	_, m := sup.pedir("POST", "/api/claves", map[string]any{"nombre": "Para Arcanum", "rol": "usuario"})
	claveNueva, _ := m["clave"].(string)
	if claveNueva == "" {
		t.Fatalf("clave: %v", m)
	}
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"general.nombre": "Estudio de prueba", "telegram.token": "123456789:AAsecreto-de-telegram"}}); st != 200 {
		t.Fatalf("configuración: %d %v", st, m)
	}
	lote := e.cargarLote(t, "pdf/01_FA_21.pdf", "pdf/04_FB_consumidor.pdf", "pdf/08_dos_facturas.pdf", "foto_fb.png")
	_, m = sup.pedir("GET", fmt.Sprintf("/api/comprobantes?lote=%d", lote), nil)
	cbtes := m["comprobantes"].([]any)
	primero := int64(cbtes[0].(map[string]any)["id"].(float64))
	if st, m := sup.pedir("PATCH", fmt.Sprintf("/api/comprobantes/%d", primero), map[string]any{"campos": map[string]any{"rubro": "servicios"}, "aprobar": true}); st != 200 {
		t.Fatalf("corregir: %d %v", st, m)
	}
	totalCbtes := len(cbtes)

	// La copia, desde la pantalla, y la descarga (el archivo cifrado).
	if st, _ := sup.pedir("POST", "/api/copias", nil); st != 202 {
		t.Fatal("hacer copia")
	}
	var c map[string]any
	esperar(t, "la copia", func() bool {
		_, m := sup.pedir("GET", "/api/copias", nil)
		l := m["copias"].([]any)
		if len(l) > 0 {
			c = l[0].(map[string]any)
		}
		return c != nil && c["estado"] == copias.OK
	})
	if c["destino"] != "disco" {
		t.Fatalf("sin R2 va al disco: %v", c)
	}
	_, _, raw := sup.pedirCrudo("GET", fmt.Sprintf("/api/copias/%d/archivo", int64(c["id"].(float64))), nil)
	archivo := filepath.Join(t.TempDir(), "factorum.copia")
	if err := os.WriteFile(archivo, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := copias.Leer(bytes.NewReader(raw), e.app.Almacen)
	if err != nil {
		t.Fatal(err)
	}
	for _, tb := range []string{"lotes", "documentos", "comprobantes", "correcciones", "usuarios", "sesiones", "claves_api", "configuracion", "auditoria"} {
		var hay bool
		for _, x := range original.Tablas {
			hay = hay || (x.Nombre == tb && x.Filas > 0)
		}
		if !hay {
			t.Fatalf("la copia no tiene datos de %s", tb)
		}
	}

	// Ida y vuelta exacta (con la biblioteca): todas las tablas con las mismas filas y el mismo hash.
	url3 := pruebas.BaseNueva(t)
	st3, err := store.Abrir(ctx, url3, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer st3.Cerrar()
	if _, err := copias.Restaurar(ctx, st3.Pool, func() (io.ReadCloser, error) { return os.Open(archivo) }, e.app.Almacen,
		copias.OpcionesRestaurar{Confirmar: true, Migraciones: store.Migraciones()}); err != nil {
		t.Fatal(err)
	}
	vuelta := manifiestoDe(t, st3.Pool, e.app.Almacen)
	if len(vuelta.Tablas) != len(original.Tablas) {
		t.Fatalf("tablas: %d y %d", len(original.Tablas), len(vuelta.Tablas))
	}
	for i, x := range original.Tablas {
		y := vuelta.Tablas[i]
		if x.Nombre != y.Nombre || x.Filas != y.Filas || x.SHA256 != y.SHA256 {
			t.Errorf("la tabla %s no quedó igual (%d filas y %d)", x.Nombre, x.Filas, y.Filas)
		}
	}

	// Por consola, como en producción: «factorum restaurar» en otra base nueva.
	bin := binario(t)
	url2 := pruebas.BaseNueva(t)
	entorno := append(os.Environ(), "FACTORUM_DATABASE_URL="+url2, "FACTORUM_MASTER_KEY="+strings.Repeat("07", 32), "FACTORUM_DATOS="+t.TempDir())
	correr := func(args ...string) string {
		cmd := exec.Command(bin, args...)
		cmd.Env = entorno
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("factorum %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	out := correr("restaurar", archivo)
	if !strings.Contains(out, "agregá --confirmar") {
		t.Fatalf("sin confirmar: %s", out)
	}
	out = correr("restaurar", archivo, "--confirmar")
	if !strings.Contains(out, "Listo: la base quedó como en la copia") {
		t.Fatalf("restaurar: %s", out)
	}
	// Con otra clave maestra no se puede.
	cmd := exec.Command(bin, "restaurar", archivo, "--confirmar")
	cmd.Env = append(entorno, "FACTORUM_MASTER_KEY="+strings.Repeat("08", 32))
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "otra FACTORUM_MASTER_KEY") {
		t.Fatalf("con otra clave: %v %s", err, out)
	}

	// Factorum arranca sobre la base restaurada (con el mismo almacén) y funciona.
	e2 := armarOpc(t, opcEntorno{url: url2, dirAlmacen: dirAlm})
	diego := e2.entrar(t, "diego@ejemplo.com", "una clave muy larga")
	_, m = diego.pedir("GET", "/api/comprobantes", nil)
	if int(m["total"].(float64)) != totalCbtes {
		t.Fatalf("comprobantes restaurados: %v de %d", m["total"], totalCbtes)
	}
	_, m = diego.pedir("GET", fmt.Sprintf("/api/comprobantes/%d", primero), nil)
	if cb := m["comprobante"].(map[string]any); cb["rubro"] != "servicios" || cb["estado"] != "aprobado" {
		t.Fatalf("la corrección y la aprobación: %v %v", cb["rubro"], cb["estado"])
	}
	e2.seVenLosArchivos(t, "en la base restaurada")
	if st, _, _ := e2.pedir(t, "GET", "/api/lotes", claveNueva, nil, ""); st != 200 {
		t.Fatalf("la clave de API restaurada: %d", st)
	}
	_, m = diego.pedir("GET", "/api/configuracion", nil)
	var nombre any
	var token bool
	for _, x := range m["campos"].([]any) {
		f := x.(map[string]any)
		switch f["clave"] {
		case "general.nombre":
			nombre = f["valor"]
		case "telegram.token":
			token = f["configurado"] == true && f["ultimos"] == "gram"
		}
	}
	if nombre != "Estudio de prueba" || !token {
		t.Fatalf("configuración restaurada: %v %v", nombre, token)
	}
	ana := e2.entrar(t, "ana@ejemplo.com", "otra clave bien larga")
	if st, _ := ana.pedir("GET", "/api/comprobantes", nil); st != 200 {
		t.Fatal("ana entra")
	}
	// Se puede seguir cargando: los números nuevos no chocan con los restaurados.
	nuevo := e2.cargarLote(t, "pdf/05_NCA.pdf")
	if nuevo <= lote {
		t.Fatalf("el lote nuevo es %d y el restaurado %d", nuevo, lote)
	}
	_, m = diego.pedir("GET", "/api/auditoria?accion=copia.", nil)
	var restaurar bool
	for _, ev := range m["eventos"].([]any) {
		restaurar = restaurar || ev.(map[string]any)["accion"] == "copia.restaurar"
	}
	if !restaurar {
		t.Fatal("la restauración queda en la auditoría")
	}
}
