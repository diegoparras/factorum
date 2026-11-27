package api_test

// Inicio de sesión con Lockatus contra un proveedor OIDC de mentira (oidctest) y Postgres real
// (FACTORUM_TEST_DB).

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/oidc/oidctest"
)

const secretoLK = "secreto-del-cliente-de-prueba"

type entornoLockatus struct {
	*entornoCorreo
	lk    *oidctest.Falso
	lkURL string
}

// armarLockatus: Factorum con correo saliente, URL pública y Lockatus (de mentira) configurado.
func armarLockatus(t *testing.T, ajustar func(*config.Config)) *entornoLockatus {
	t.Helper()
	lk := oidctest.Nuevo("factorum", secretoLK)
	srv := httptest.NewServer(lk)
	t.Cleanup(srv.Close)
	lk.Cambiar(func(f *oidctest.Falso) { f.Emisor = srv.URL })
	e := armarCorreo(t, func(c *config.Config) {
		c.OIDCEmisor, c.OIDCClienteID, c.OIDCClienteSecreto = srv.URL, "factorum", secretoLK
		c.OIDCScope, c.OIDCClaimRol, c.OIDCRolPorDefecto, c.OIDCCrearUsuarios = "openid profile email", "role", "lectura", true
		if ajustar != nil {
			ajustar(c)
		}
	})
	return &entornoLockatus{entornoCorreo: e, lk: lk, lkURL: srv.URL}
}

var ipsLK atomic.Int32

// nav es un navegador con su propia IP (el límite de intentos es por IP).
func (e *entornoLockatus) nav(t *testing.T) *navegador {
	n := e.navegador(t)
	i := ipsLK.Add(1)
	n.ip = fmt.Sprintf("10.77.%d.%d", i/250, i%250+1)
	return n
}

// ir hace un GET sin seguir redirecciones y devuelve el status y a dónde manda.
func (n *navegador) ir(u string) (int, string) {
	n.t.Helper()
	if !strings.HasPrefix(u, "http") {
		u = n.e.srv.URL + u
	}
	req, _ := http.NewRequest("GET", u, nil)
	if n.ip != "" {
		req.Header.Set("X-Forwarded-For", n.ip)
	}
	res, err := n.c.Do(req)
	if err != nil {
		n.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode, res.Header.Get("Location")
}

// entrarLK hace el ida y vuelta con Lockatus entrando como p. Devuelve a dónde terminó ("/" si
// entró, "/?lockatus=..." si no) y la dirección del callback (para reusarla).
func (e *entornoLockatus) entrarLK(t *testing.T, n *navegador, p oidctest.Persona) (string, string) {
	t.Helper()
	e.lk.Cambiar(func(f *oidctest.Falso) { f.Auto = p })
	st, loc := n.ir("/api/auth/oidc/login")
	if st != http.StatusFound {
		return loc, ""
	}
	if !strings.HasPrefix(loc, e.lkURL+"/authorize?") {
		t.Fatalf("login manda a %q", loc)
	}
	au, _ := url.Parse(loc)
	q := au.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("nonce") == "" || q.Get("state") == "" ||
		q.Get("redirect_uri") != e.url+"/api/auth/oidc/callback" || q.Get("scope") != "openid profile email" {
		t.Fatalf("pedido a Lockatus incompleto: %v", q)
	}
	st, cb := n.ir(loc)
	if st != http.StatusFound || !strings.HasPrefix(cb, e.url+"/api/auth/oidc/callback?") {
		t.Fatalf("Lockatus de mentira: %d %q", st, cb)
	}
	st, fin := n.ir(cb)
	if st != http.StatusSeeOther {
		t.Fatalf("callback: %d %q", st, fin)
	}
	return fin, cb
}

func persona(email string, extra ...any) oidctest.Persona {
	p := oidctest.Persona{"sub": "sub-" + email, "email": email, "email_verified": true, "name": strings.Split(email, "@")[0]}
	for i := 0; i+1 < len(extra); i += 2 {
		p[extra[i].(string)] = extra[i+1]
	}
	return p
}

func (e *entornoLockatus) ultimoRechazo(t *testing.T) string {
	t.Helper()
	var m string
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT detalle->>'motivo' FROM auditoria WHERE accion='sesion.rechazo_lockatus' ORDER BY id DESC LIMIT 1`).Scan(&m)
	return m
}

func identidadDe(t *testing.T, n *navegador) map[string]any {
	t.Helper()
	st, m := n.pedir("GET", "/api/sesion", nil)
	if st != 200 {
		t.Fatalf("sesión: %d %v", st, m)
	}
	return m
}

func TestLockatusEntradaEIdentidad(t *testing.T) {
	e := armarLockatus(t, nil)
	sup := e.crearSuperadmin(t)
	e.sembrar(t, cuitX, cuitY)
	if _, m := e.navegador(t).pedir("GET", "/api/arranque", nil); m["lockatus"] != true || m["solo_lockatus"] != false {
		t.Fatalf("arranque: %v", m)
	}

	// Quien ya existe (el email no distingue mayúsculas) entra con SU rol: el claim no lo baja.
	diego := e.nav(t)
	fin, cb := e.entrarLK(t, diego, persona("Diego@Ejemplo.com", "role", "lectura"))
	if fin != "/" {
		t.Fatalf("diego: %q", fin)
	}
	m := identidadDe(t, diego)
	id := m["identidad"].(map[string]any)
	if id["rol"] != "superadmin" || id["origen"] != "lockatus" || id["email"] != "diego@ejemplo.com" || id["cuits"] != nil {
		t.Fatalf("diego por Lockatus: %v", id)
	}
	// El state, el código y los tokens no quedan en la auditoría.
	cbu, _ := url.Parse(cb)
	for _, x := range []string{cbu.Query().Get("state"), cbu.Query().Get("code")} {
		var n int
		_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria WHERE detalle::text LIKE '%'||$1||'%' OR objeto LIKE '%'||$1||'%'`, x).Scan(&n)
		if n != 0 {
			t.Fatal("la auditoría tiene el state o el código")
		}
	}
	var entradas int
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria WHERE accion='sesion.entrar_lockatus' AND actor='diego@ejemplo.com'`).Scan(&entradas)
	if entradas != 1 {
		t.Fatalf("auditoría de la entrada: %d", entradas)
	}
	// El callback sirve una sola vez.
	if st, fin := diego.ir(cb); st != 303 || fin != "/?lockatus=estado" || !strings.Contains(e.ultimoRechazo(t), "state") {
		t.Fatalf("callback reusado: %d %q %q", st, fin, e.ultimoRechazo(t))
	}

	// Nuevo con rol del claim: operador es usuario, sin clientes hasta que un admin se los asigne.
	ana := e.nav(t)
	if fin, _ := e.entrarLK(t, ana, persona("ana@cliente.com", "role", "operador")); fin != "/" {
		t.Fatalf("ana: %q", fin)
	}
	m = identidadDe(t, ana)
	id = m["identidad"].(map[string]any)
	if id["rol"] != "usuario" || fmt.Sprint(id["cuits"]) != "[]" || m["permisos"].(map[string]any)["sin_clientes"] != true ||
		m["cuenta"].(map[string]any)["origen"] != "lockatus" || m["cuenta"].(map[string]any)["tiene_clave"] != false {
		t.Fatalf("ana nueva: %v", m)
	}
	if st, m := ana.pedir("GET", "/api/comprobantes", nil); st != 403 || !strings.Contains(fmt.Sprint(m["error"]), "ningún cliente") {
		t.Fatalf("sin clientes no ve nada: %d %v", st, m)
	}
	// Nuevo admin por el claim (una lista): todos los clientes.
	beto := e.nav(t)
	if fin, _ := e.entrarLK(t, beto, persona("beto@cliente.com", "role", []any{"lector", "admin"})); fin != "/" {
		t.Fatalf("beto: %q", fin)
	}
	if id := identidadDe(t, beto)["identidad"].(map[string]any); id["rol"] != "admin" || id["cuits"] != nil {
		t.Fatalf("beto admin: %v", id)
	}
	if st, _ := beto.pedir("GET", "/api/usuarios", nil); st != 200 {
		t.Fatalf("admin ve usuarios: %d", st)
	}
	// Sin claim (o con uno desconocido): el rol por defecto, lectura.
	carla := e.nav(t)
	if fin, _ := e.entrarLK(t, carla, persona("carla@cliente.com", "role", "jefa")); fin != "/" {
		t.Fatalf("carla: %q", fin)
	}
	if id := identidadDe(t, carla)["identidad"].(map[string]any); id["rol"] != "lectura" || fmt.Sprint(id["cuits"]) != "[]" {
		t.Fatalf("carla por defecto: %v", id)
	}
	// Con ES256 también.
	e.lk.Cambiar(func(f *oidctest.Falso) { f.Alg = "ES256" })
	dani := e.nav(t)
	if fin, _ := e.entrarLK(t, dani, persona("dani@cliente.com")); fin != "/" {
		t.Fatalf("dani ES256: %q", fin)
	}
	e.lk.Cambiar(func(f *oidctest.Falso) { f.Alg = "RS256" })

	// Las claves se guardan: con el mismo kid no se vuelve a pedir el JWKS; con un kid nuevo, sí.
	antes := e.lk.PedidosJWKS.Load()
	if fin, _ := e.entrarLK(t, e.nav(t), persona("ana@cliente.com")); fin != "/" || e.lk.PedidosJWKS.Load() != antes {
		t.Fatalf("caché del JWKS: %q %d -> %d", fin, antes, e.lk.PedidosJWKS.Load())
	}
	e.lk.Rotar("RS256")
	if fin, _ := e.entrarLK(t, e.nav(t), persona("ana@cliente.com")); fin != "/" || e.lk.PedidosJWKS.Load() != antes+1 {
		t.Fatalf("rotación de kid: %q %d -> %d", fin, antes, e.lk.PedidosJWKS.Load())
	}

	// La persona en la lista de Usuarios, con origen Lockatus y sin contraseña.
	_, lu := sup.pedir("GET", "/api/usuarios", nil)
	origenes := map[string]string{}
	for _, x := range lu["usuarios"].([]any) {
		u := x.(map[string]any)
		origenes[u["email"].(string)] = fmt.Sprint(u["origen"], " ", u["tiene_clave"], " ", u["cuits"])
	}
	if origenes["ana@cliente.com"] != "lockatus false []" || origenes["beto@cliente.com"] != "lockatus false <nil>" || origenes["diego@ejemplo.com"] != "local true <nil>" {
		t.Fatalf("usuarios: %v", origenes)
	}
}

func TestLockatusRechazos(t *testing.T) {
	e := armarLockatus(t, nil)
	sup := e.crearSuperadmin(t)
	ahora := time.Now()
	casos := []struct {
		nombre  string
		persona oidctest.Persona
		cambiar func(f *oidctest.Falso)
		codigo  string
		motivo  string
	}{
		{"email sin verificar", persona("eva@cliente.com", "email_verified", false), nil, "email", "email verificado"},
		{"email_verified en texto", persona("eva@cliente.com", "email_verified", "false"), nil, "email", "email verificado"},
		{"sin email", oidctest.Persona{"sub": "x"}, nil, "email", "email verificado"},
		{"nonce distinto", persona("eva@cliente.com"), func(f *oidctest.Falso) { f.Retocar = func(c oidctest.Persona) { c["nonce"] = "otro" } }, "token", "nonce"},
		{"firma mala", persona("eva@cliente.com"), func(f *oidctest.Falso) { f.FirmaMala = true }, "token", "firma"},
		{"firma mala ES256", persona("eva@cliente.com"), func(f *oidctest.Falso) { f.FirmaMala, f.Alg = true, "ES256" }, "token", "firma"},
		{"aud de otro", persona("eva@cliente.com"), func(f *oidctest.Falso) { f.Retocar = func(c oidctest.Persona) { c["aud"] = "otra-app" } }, "token", "otro cliente"},
		{"aud con otro y sin azp", persona("eva@cliente.com"), func(f *oidctest.Falso) {
			f.Retocar = func(c oidctest.Persona) { c["aud"] = []string{"factorum", "otra-app"} }
		}, "token", "otro cliente"},
		{"iss de otro", persona("eva@cliente.com"), func(f *oidctest.Falso) {
			f.Retocar = func(c oidctest.Persona) { c["iss"] = "https://otro.ejemplo.com" }
		}, "token", "otro emisor"},
		{"vencido", persona("eva@cliente.com"), func(f *oidctest.Falso) {
			f.Retocar = func(c oidctest.Persona) {
				c["exp"], c["iat"] = ahora.Add(-10*time.Minute).Unix(), ahora.Add(-20*time.Minute).Unix()
			}
		}, "token", "venció"},
		{"nbf en el futuro", persona("eva@cliente.com"), func(f *oidctest.Falso) {
			f.Retocar = func(c oidctest.Persona) { c["nbf"] = ahora.Add(10 * time.Minute).Unix() }
		}, "token", "todavía no vale"},
		{"iat en el futuro", persona("eva@cliente.com"), func(f *oidctest.Falso) {
			f.Retocar = func(c oidctest.Persona) { c["iat"] = ahora.Add(10 * time.Minute).Unix() }
		}, "token", "todavía no vale"},
	}
	for _, c := range casos {
		e.lk.Cambiar(func(f *oidctest.Falso) { f.Retocar, f.FirmaMala, f.Alg = nil, false, "RS256" })
		if c.cambiar != nil {
			e.lk.Cambiar(c.cambiar)
		}
		n := e.nav(t)
		fin, _ := e.entrarLK(t, n, c.persona)
		if fin != "/?lockatus="+c.codigo || !strings.Contains(e.ultimoRechazo(t), c.motivo) {
			t.Errorf("%s: %q, motivo %q", c.nombre, fin, e.ultimoRechazo(t))
		}
		if st, _ := n.pedir("GET", "/api/sesion", nil); st != 401 {
			t.Errorf("%s: quedó con sesión", c.nombre)
		}
	}
	e.lk.Cambiar(func(f *oidctest.Falso) { f.Retocar, f.FirmaMala = nil, false })
	var cuentas int
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM usuarios WHERE email='eva@cliente.com'`).Scan(&cuentas)
	if cuentas != 0 {
		t.Fatal("un rechazo no crea la cuenta")
	}

	// El state cambiado, o el callback en otro navegador (sin la cookie), no sirven.
	n := e.nav(t)
	e.lk.Cambiar(func(f *oidctest.Falso) { f.Auto = persona("eva@cliente.com") })
	_, loc := n.ir("/api/auth/oidc/login")
	_, cb := n.ir(loc)
	cambiado := strings.Replace(cb, "state=", "state=x", 1)
	if _, fin := n.ir(cambiado); fin != "/?lockatus=estado" {
		t.Fatalf("state cambiado: %q", fin)
	}
	_, loc = n.ir("/api/auth/oidc/login")
	_, cb = n.ir(loc)
	if _, fin := e.nav(t).ir(cb); fin != "/?lockatus=estado" {
		t.Fatalf("callback en otro navegador: %q", fin)
	}
	// Quien canceló en Lockatus.
	_, loc = n.ir("/api/auth/oidc/login")
	au, _ := url.Parse(loc)
	if _, fin := n.ir("/api/auth/oidc/callback?error=access_denied&state=" + au.Query().Get("state")); fin != "/?lockatus=cancelado" {
		t.Fatalf("cancelado: %q", fin)
	}

	// Desactivado: no entra.
	if fin, _ := e.entrarLK(t, e.nav(t), persona("fede@cliente.com")); fin != "/" {
		t.Fatalf("fede: %q", fin)
	}
	var fede int64
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT id FROM usuarios WHERE email='fede@cliente.com'`).Scan(&fede)
	if st, m := sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", fede), map[string]any{"activo": false}); st != 200 {
		t.Fatalf("desactivar: %d %v", st, m)
	}
	if fin, _ := e.entrarLK(t, e.nav(t), persona("fede@cliente.com")); fin != "/?lockatus=desactivado" || !strings.Contains(e.ultimoRechazo(t), "desactivada") {
		t.Fatalf("desactivado: %q %q", fin, e.ultimoRechazo(t))
	}

	// Creación apagada (desde la pantalla, en caliente): quien no existe no entra.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"lockatus.crear_usuarios": false}}); st != 200 {
		t.Fatalf("apagar la creación: %d %v", st, m)
	}
	if fin, _ := e.entrarLK(t, e.nav(t), persona("gabi@cliente.com")); fin != "/?lockatus=sin_cuenta" || !strings.Contains(e.ultimoRechazo(t), "creación automática") {
		t.Fatalf("sin creación: %q %q", fin, e.ultimoRechazo(t))
	}
	// El que ya existe, sí.
	if fin, _ := e.entrarLK(t, e.nav(t), persona("diego@ejemplo.com")); fin != "/" {
		t.Fatalf("diego con la creación apagada: %q", fin)
	}

	// Un secreto equivocado (cambiado en caliente) hace fallar el canje; corregido, anda.
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"lockatus.client_secret": "otro-secreto"}}); st != 200 {
		t.Fatal("cambiar el secreto")
	}
	if fin, _ := e.entrarLK(t, e.nav(t), persona("diego@ejemplo.com")); fin != "/?lockatus=token" || !strings.Contains(e.ultimoRechazo(t), "invalid_client") {
		t.Fatalf("secreto malo: %q %q", fin, e.ultimoRechazo(t))
	}
	if st, _ := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"lockatus.client_secret": secretoLK}}); st != 200 {
		t.Fatal("volver al secreto")
	}
	if fin, _ := e.entrarLK(t, e.nav(t), persona("diego@ejemplo.com")); fin != "/" {
		t.Fatalf("secreto corregido: %q", fin)
	}

	// Límite de intentos en el callback: después de cinco fallos, ni uno bueno pasa.
	lim := e.nav(t)
	for i := 0; i < 5; i++ {
		if _, fin := lim.ir("/api/auth/oidc/callback?code=x&state=y"); fin != "/?lockatus=estado" {
			t.Fatalf("intento %d: %q", i, fin)
		}
	}
	if fin, _ := e.entrarLK(t, lim, persona("diego@ejemplo.com")); fin != "/?lockatus=demasiados" {
		t.Fatalf("límite: %q", fin)
	}
}

func TestLockatusCuentas(t *testing.T) {
	e := armarLockatus(t, nil)
	sup := e.crearSuperadmin(t)
	e.sembrar(t, cuitX, cuitY)
	ana := e.nav(t)
	if fin, _ := e.entrarLK(t, ana, persona("ana@cliente.com", "role", "usuario")); fin != "/" {
		t.Fatalf("ana: %q", fin)
	}
	var anaID int64
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT id FROM usuarios WHERE email='ana@cliente.com'`).Scan(&anaID)

	// Sin contraseña: el login local contesta igual que con un email que no existe.
	st1, m1 := e.nav(t).pedir("POST", "/api/sesion", map[string]string{"email": "ana@cliente.com", "clave": "cualquier cosa larga"})
	st2, m2 := e.nav(t).pedir("POST", "/api/sesion", map[string]string{"email": "nadie@cliente.com", "clave": "cualquier cosa larga"})
	if st1 != 401 || st1 != st2 || m1["error"] != m2["error"] {
		t.Fatalf("login local de una cuenta de Lockatus: %d %v / %d %v", st1, m1, st2, m2)
	}
	// Ni «Olvidé mi contraseña» (la respuesta es la de siempre, pero no sale ningún mail).
	if st, m := e.nav(t).pedir("POST", "/api/sesion/olvide", map[string]string{"email": "ana@cliente.com"}); st != 200 || m["mensaje"] == nil {
		t.Fatalf("olvide: %d %v", st, m)
	}
	if r := e.smtp.Esperar(1, 1500*time.Millisecond); len(r) != 0 {
		t.Fatalf("a una cuenta de Lockatus no se le manda el link: %+v", r)
	}
	// Ni «Nueva contraseña», ni link, ni cambiarla ella misma.
	if st, m := sup.pedir("POST", fmt.Sprintf("/api/usuarios/%d/restablecer", anaID), nil); st != 409 || !strings.Contains(fmt.Sprint(m["error"]), "Lockatus") {
		t.Fatalf("restablecer: %d %v", st, m)
	}
	if st, m := sup.pedir("POST", fmt.Sprintf("/api/usuarios/%d/link", anaID), nil); st != 409 || !strings.Contains(fmt.Sprint(m["error"]), "Lockatus") {
		t.Fatalf("link: %d %v", st, m)
	}
	if st, m := ana.pedir("POST", "/api/sesion/clave", map[string]string{"actual": "", "nueva": "una clave nueva larga"}); st != 409 {
		t.Fatalf("cambiar su clave: %d %v", st, m)
	}

	// Un admin le asigna clientes: se cierra su sesión y, al volver a entrar, ve lo de ese cliente.
	if st, m := sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", anaID), map[string]any{"cuits": []string{cuitX}}); st != 200 {
		t.Fatalf("asignar: %d %v", st, m)
	}
	if st, _ := ana.pedir("GET", "/api/sesion", nil); st != 401 {
		t.Fatal("cambiarle los clientes cierra su sesión")
	}
	if fin, _ := e.entrarLK(t, ana, persona("ana@cliente.com", "role", "admin")); fin != "/" {
		t.Fatalf("ana otra vez: %q", fin)
	}
	if id := identidadDe(t, ana)["identidad"].(map[string]any); id["rol"] != "usuario" || fmt.Sprint(id["cuits"]) != "["+cuitX+"]" {
		t.Fatalf("el claim no la escala y ve su cliente: %v", id)
	}
	st, m := ana.pedir("GET", "/api/comprobantes", nil)
	if st != 200 || m["total"].(float64) != 1 {
		t.Fatalf("comprobantes de ana: %d %v", st, m)
	}
	// Y se le pueden volver a sacar todos.
	if st, m := sup.pedir("PATCH", fmt.Sprintf("/api/usuarios/%d", anaID), map[string]any{"sin_clientes": true}); st != 200 || fmt.Sprint(m["usuario"].(map[string]any)["cuits"]) != "[]" {
		t.Fatalf("sin clientes: %d %v", st, m)
	}

	// Alta hecha por un admin para que entre con Lockatus: sin contraseña, con sus clientes.
	st, m = sup.pedir("POST", "/api/usuarios", map[string]any{"email": "beto@cliente.com", "nombre": "Beto", "rol": "lectura", "cuits": []string{cuitY}, "lockatus": true})
	if st != 201 || m["lockatus"] != true || m["clave_temporal"] != nil || m["usuario"].(map[string]any)["origen"] != "lockatus" {
		t.Fatalf("alta para Lockatus: %d %v", st, m)
	}
	beto := e.nav(t)
	if fin, _ := e.entrarLK(t, beto, persona("beto@cliente.com", "role", "superadmin")); fin != "/" {
		t.Fatalf("beto: %q", fin)
	}
	if id := identidadDe(t, beto)["identidad"].(map[string]any); id["rol"] != "lectura" || fmt.Sprint(id["cuits"]) != "["+cuitY+"]" {
		t.Fatalf("beto entra con lo de Factorum: %v", id)
	}

	// Alguien con la contraseña temporal sin cambiar que entra con Lockatus no queda trabado.
	st, m = sup.pedir("POST", "/api/usuarios", map[string]any{"email": "caro@cliente.com", "rol": "usuario", "cuits": []string{cuitX}})
	if st != 201 || m["clave_temporal"] == nil {
		t.Fatalf("alta local: %d %v", st, m)
	}
	caro := e.nav(t)
	if fin, _ := e.entrarLK(t, caro, persona("caro@cliente.com")); fin != "/" {
		t.Fatalf("caro: %q", fin)
	}
	if st, _ := caro.pedir("GET", "/api/comprobantes", nil); st != 200 {
		t.Fatalf("con Lockatus no se pide cambiar la temporal: %d", st)
	}
	// Salir anda igual.
	if st, _ := caro.pedir("DELETE", "/api/sesion", nil); st != 200 {
		t.Fatal("salir")
	}
	if st, _ := caro.pedir("GET", "/api/sesion", nil); st != 401 {
		t.Fatal("la sesión de Lockatus se cierra al salir")
	}
}

func TestLockatusSolo(t *testing.T) {
	e := armarLockatus(t, func(c *config.Config) { c.APIKeySuiteTope = "admin" })
	sup := e.crearSuperadmin(t)

	// Prenderlo desde una sesión con contraseña no se puede: primero hay que entrar con Lockatus.
	if st, m := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"lockatus.solo": true}}); st != 409 || !strings.Contains(fmt.Sprint(m["error"]), "entrá con Lockatus") {
		t.Fatalf("solo desde una sesión local: %d %v", st, m)
	}
	diego := e.nav(t)
	if fin, _ := e.entrarLK(t, diego, persona("diego@ejemplo.com")); fin != "/" {
		t.Fatalf("diego: %q", fin)
	}
	_, conf := diego.pedir("GET", "/api/configuracion", nil)
	if lk := conf["lockatus"].(map[string]any); lk["redirect"] != e.url+"/api/auth/oidc/callback" || lk["sesion_lockatus"] != true {
		t.Fatalf("configuración: %v", lk)
	}
	if st, m := diego.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"lockatus.solo": true}}); st != 200 {
		t.Fatalf("solo desde Lockatus: %d %v", st, m)
	}
	var cambios int
	_ = e.app.Store.Pool.QueryRow(context.Background(), `SELECT count(*) FROM auditoria WHERE accion='config.cambiar' AND objeto LIKE '%lockatus.solo%'`).Scan(&cambios)
	if cambios != 1 {
		t.Fatal("el cambio queda en la auditoría")
	}
	if _, m := e.nav(t).pedir("GET", "/api/arranque", nil); m["solo_lockatus"] != true || m["olvide"] != false {
		t.Fatalf("arranque con solo Lockatus: %v", m)
	}
	// El servidor rechaza el login local, «Olvidé mi contraseña» y los links.
	if st, m := e.nav(t).pedir("POST", "/api/sesion", map[string]string{"email": "diego@ejemplo.com", "clave": "una clave muy larga"}); st != 403 || !strings.Contains(fmt.Sprint(m["error"]), "Lockatus") {
		t.Fatalf("login local con solo Lockatus: %d %v", st, m)
	}
	if st, _ := e.nav(t).pedir("POST", "/api/sesion/olvide", map[string]string{"email": "diego@ejemplo.com"}); st != 403 {
		t.Fatalf("olvide: %d", st)
	}
	if st, _ := e.nav(t).pedir("POST", "/api/sesion/restablecer", map[string]string{"token": "x", "clave": "una clave muy larga"}); st != 403 {
		t.Fatalf("restablecer: %d", st)
	}
	// Las claves de API y el pase de la suite siguen andando.
	if st, _ := e.pedirJSON(t, "GET", "/api/lotes", claveOp, nil); st != 200 {
		t.Fatalf("clave de API: %d", st)
	}
	st, m := e.pedirJSON(t, "POST", "/api/suite/pase", claveOp, map[string]any{"app": "arcanum", "email": "diego@ejemplo.com", "rol": "admin"})
	if st != 200 {
		t.Fatalf("pase: %d %v", st, m)
	}
	pase, _ := url.Parse(m["url"].(string))
	viene := e.nav(t)
	if st, fin := viene.ir(pase.RequestURI()); st != 303 || fin != "/" {
		t.Fatalf("entrar con el pase: %d %q", st, fin)
	}
	if st, _ := viene.pedir("GET", "/api/lotes", nil); st != 200 {
		t.Fatal("la sesión del pase anda")
	}
	// Un alta nueva es para Lockatus (sin contraseña temporal ni invitación).
	st, m = diego.pedir("POST", "/api/usuarios", map[string]any{"email": "nuevo@cliente.com", "rol": "usuario", "cuits": cuitX, "invitar": true})
	if st != 201 || m["lockatus"] != true || m["clave_temporal"] != nil {
		t.Fatalf("alta con solo Lockatus: %d %v", st, m)
	}
	if r := e.smtp.Esperar(1, time.Second); len(r) != 0 {
		t.Fatal("no se manda invitación")
	}
	// La pantalla «Crear el primer superadmin» no cambia (acá ya no hace falta).
	if _, m := e.nav(t).pedir("GET", "/api/arranque", nil); m["necesita"] != false {
		t.Fatalf("arranque: %v", m)
	}
}

func TestLockatusSalidaDeEmergencia(t *testing.T) {
	// «Solo Lockatus» fijado por el entorno: el primer superadmin se crea igual y después no entra con contraseña.
	e := armarLockatus(t, func(c *config.Config) { c.OIDCSolo = true })
	e.crearSuperadmin(t)
	if st, _ := e.nav(t).pedir("POST", "/api/sesion", map[string]string{"email": "diego@ejemplo.com", "clave": "una clave muy larga"}); st != 403 {
		t.Fatalf("solo Lockatus: %d", st)
	}
	// Con FACTORUM_LOGIN_LOCAL=1, sí.
	e2 := armarLockatus(t, func(c *config.Config) { c.OIDCSolo, c.LoginLocal = true, true })
	e2.crearSuperadmin(t)
	if _, m := e2.nav(t).pedir("GET", "/api/arranque", nil); m["solo_lockatus"] != false || m["lockatus"] != true {
		t.Fatalf("arranque con la salida de emergencia: %v", m)
	}
	n := e2.entrar(t, "diego@ejemplo.com", "una clave muy larga")
	_, conf := n.pedir("GET", "/api/configuracion", nil)
	if lk := conf["lockatus"].(map[string]any); lk["login_local"] != true || lk["solo"] != false {
		t.Fatalf("la pantalla avisa la salida de emergencia: %v", lk)
	}
}

func TestLockatusProbarYSinConfigurar(t *testing.T) {
	e := armarLockatus(t, nil)
	sup := e.crearSuperadmin(t)
	st, m := sup.pedir("POST", "/api/configuracion/probar/lockatus", map[string]any{"cambios": map[string]any{}})
	if st != 200 || m["exito"] != true || !strings.Contains(fmt.Sprint(m["mensaje"]), "Lockatus responde") || !strings.Contains(fmt.Sprint(m["mensaje"]), "2 clave(s)") {
		t.Fatalf("probar: %d %v", st, m)
	}
	// Un emisor que dice ser otro.
	otro := oidctest.Nuevo("factorum", "x")
	srv := httptest.NewServer(otro)
	defer srv.Close()
	otro.Cambiar(func(f *oidctest.Falso) { f.Emisor = "https://lockatus.otro.com" })
	st, m = sup.pedir("POST", "/api/configuracion/probar/lockatus", map[string]any{"cambios": map[string]any{"lockatus.emisor": srv.URL}})
	if st != 200 || m["exito"] != false || !strings.Contains(fmt.Sprint(m["mensaje"]), "ice ser") {
		t.Fatalf("probar con otro issuer: %d %v", st, m)
	}
	st, m = sup.pedir("POST", "/api/configuracion/probar/lockatus", map[string]any{"cambios": map[string]any{"lockatus.emisor": "http://127.0.0.1:1"}})
	if st != 200 || m["exito"] != false {
		t.Fatalf("probar sin servidor: %d %v", st, m)
	}
	// Sin Lockatus: el login no lo ofrece y la ruta vuelve al login.
	e2 := armarCorreo(t, nil)
	if _, m := e2.navegador(t).pedir("GET", "/api/arranque", nil); m["lockatus"] != false {
		t.Fatalf("sin configurar: %v", m)
	}
	if st, fin := e2.navegador(t).ir("/api/auth/oidc/login"); st != 303 || fin != "/?lockatus=no_config" {
		t.Fatalf("login sin configurar: %d %q", st, fin)
	}
}
