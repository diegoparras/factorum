// Package oidctest es un Lockatus de mentira para las pruebas: discovery, autorización (que
// devuelve un código), token (que controla PKCE y devuelve un id_token firmado con RS256 o ES256)
// y JWKS, con rotación de claves. Sirve con httptest o como programa aparte (para probar la
// pantalla con un navegador).
package oidctest

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Persona es quién «entra» en el Lockatus de mentira: los claims que van en el id_token.
type Persona map[string]any

type clave struct {
	kid string
	alg string
	rsa *rsa.PrivateKey
	ec  *ecdsa.PrivateKey
}

type pedido struct {
	desafio, nonce, redirect string
	persona                  Persona
	vence                    time.Time
}

// Falso es el proveedor. Los campos se pueden tocar entre pedidos (con el candado de Cambiar).
type Falso struct {
	Emisor        string // lo que publica como issuer (la URL del servidor)
	ClienteID     string
	ClienteSecret string
	// Auto: si no es nil, la autorización entra directo con esta persona; si es nil, muestra un
	// formulario para elegir email, nombre y rol (para probar con un navegador).
	Auto Persona
	// Alg con el que firma: RS256 (por defecto) o ES256.
	Alg string
	// Retocar cambia los claims justo antes de firmar (para probar aud, iss, exp o nonce malos).
	Retocar func(Persona)
	// FirmaMala firma con una clave que no está en el JWKS.
	FirmaMala bool
	// SinPKCE no exige el code_verifier (para ver que Factorum lo manda igual).
	SinPKCE bool

	PedidosJWKS atomic.Int32 // cuántas veces se pidió el JWKS
	Canjes      atomic.Int32 // códigos canjeados bien

	mu      sync.Mutex
	claves  []*clave // las publicadas; la última de cada alg firma
	intrusa *clave
	codigos map[string]pedido
	n       int
}

// Nuevo arma un proveedor con una clave RSA y una EC.
func Nuevo(clienteID, secreto string) *Falso {
	f := &Falso{ClienteID: clienteID, ClienteSecret: secreto, Alg: "RS256", codigos: map[string]pedido{}}
	f.claves = []*clave{nuevaRSA("rsa-1"), nuevaEC("ec-1")}
	f.intrusa = nuevaRSA("rsa-1") // mismo kid, otra clave
	return f
}

func nuevaRSA(kid string) *clave {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return &clave{kid: kid, alg: "RS256", rsa: k}
}

func nuevaEC(kid string) *clave {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return &clave{kid: kid, alg: "ES256", ec: k}
}

// Cambiar toca la configuración con el candado puesto.
func (f *Falso) Cambiar(fn func(*Falso)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// Rotar agrega una clave nueva (con un kid nuevo) para ese algoritmo y deja de publicar la vieja:
// lo que se firme desde ahora lleva el kid nuevo.
func (f *Falso) Rotar(alg string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	var nueva *clave
	if alg == "ES256" {
		nueva = nuevaEC(fmt.Sprintf("ec-%d", f.n+1))
	} else {
		nueva = nuevaRSA(fmt.Sprintf("rsa-%d", f.n+1))
	}
	var quedan []*clave
	for _, c := range f.claves {
		if c.alg != nueva.alg {
			quedan = append(quedan, c)
		}
	}
	f.claves = append(quedan, nueva)
	return nueva.kid
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// ServeHTTP atiende discovery, autorización, token y JWKS.
func (f *Falso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		f.discovery(w)
	case "/authorize":
		f.autorizar(w, r)
	case "/token":
		f.token(w, r)
	case "/jwks":
		f.jwks(w)
	default:
		http.NotFound(w, r)
	}
}

// foto es la configuración del momento, leída con el candado.
type foto struct {
	emisor, id, secreto, alg string
	auto                     Persona
	retocar                  func(Persona)
	mala, sinPKCE            bool
}

func (f *Falso) foto() foto {
	f.mu.Lock()
	defer f.mu.Unlock()
	return foto{emisor: f.Emisor, id: f.ClienteID, secreto: f.ClienteSecret, alg: f.Alg, auto: f.Auto, retocar: f.Retocar, mala: f.FirmaMala, sinPKCE: f.SinPKCE}
}

func escribirJSON(w http.ResponseWriter, st int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(st)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *Falso) discovery(w http.ResponseWriter) {
	e := f.foto().emisor
	escribirJSON(w, 200, map[string]any{
		"issuer": e, "authorization_endpoint": e + "/authorize", "token_endpoint": e + "/token", "jwks_uri": e + "/jwks",
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256", "ES256"}, "code_challenge_methods_supported": []string{"S256"},
		"scopes_supported": []string{"openid", "profile", "email"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
	})
}

func (f *Falso) jwks(w http.ResponseWriter) {
	f.PedidosJWKS.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []map[string]any
	for _, c := range f.claves {
		if c.rsa != nil {
			keys = append(keys, map[string]any{"kty": "RSA", "use": "sig", "alg": "RS256", "kid": c.kid,
				"n": b64(c.rsa.N.Bytes()), "e": b64(big.NewInt(int64(c.rsa.E)).Bytes())})
		} else {
			keys = append(keys, map[string]any{"kty": "EC", "use": "sig", "alg": "ES256", "kid": c.kid, "crv": "P-256",
				"x": b64(c.ec.X.FillBytes(make([]byte, 32))), "y": b64(c.ec.Y.FillBytes(make([]byte, 32)))})
		}
	}
	escribirJSON(w, 200, map[string]any{"keys": keys})
}

func (f *Falso) autorizar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		q = r.PostForm
	}
	fo := f.foto()
	switch {
	case q.Get("client_id") != fo.id:
		http.Error(w, "client_id desconocido", 400)
		return
	case q.Get("response_type") != "code":
		http.Error(w, "response_type tiene que ser code", 400)
		return
	case q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256":
		http.Error(w, "falta PKCE con S256", 400)
		return
	case q.Get("state") == "" || q.Get("nonce") == "":
		http.Error(w, "faltan state y nonce", 400)
		return
	case !strings.Contains(" "+q.Get("scope")+" ", " openid "):
		http.Error(w, "falta el scope openid", 400)
		return
	}
	red, err := url.Parse(q.Get("redirect_uri"))
	if err != nil || red.Host == "" {
		http.Error(w, "redirect_uri inválido", 400)
		return
	}
	persona := fo.auto
	if r.Method == http.MethodPost && q.Get("email") != "" {
		persona = Persona{"sub": "sub-" + q.Get("email"), "email": q.Get("email"), "email_verified": q.Get("verificado") != "no", "name": q.Get("nombre")}
		if rol := q.Get("rol"); rol != "" {
			persona["role"] = rol
		}
	}
	if persona == nil {
		f.formulario(w, q)
		return
	}
	codigo := b64(azar(24))
	f.mu.Lock()
	f.codigos[codigo] = pedido{desafio: q.Get("code_challenge"), nonce: q.Get("nonce"), redirect: q.Get("redirect_uri"), persona: copiar(persona),
		vence: time.Now().Add(time.Minute)}
	f.mu.Unlock()
	v := red.Query()
	v.Set("code", codigo)
	v.Set("state", q.Get("state"))
	red.RawQuery = v.Encode()
	http.Redirect(w, r, red.String(), http.StatusFound)
}

// formulario imita la pantalla de Lockatus: elegir con quién entrar.
func (f *Falso) formulario(w http.ResponseWriter, q url.Values) {
	var ocultos strings.Builder
	for _, k := range []string{"client_id", "response_type", "redirect_uri", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
		fmt.Fprintf(&ocultos, `<input type="hidden" name="%s" value="%s">`, k, html.EscapeString(q.Get(k)))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Lockatus (de prueba)</title>
<style>body{font-family:system-ui,sans-serif;background:#f3f4f6;display:grid;place-items:center;min-height:100vh;margin:0}form{background:#fff;padding:24px;border-radius:12px;display:grid;gap:10px;width:min(340px,90vw);box-shadow:0 2px 12px #0002}input,select,button{font:inherit;padding:8px}button{background:#4338ca;color:#fff;border:0;border-radius:8px}</style></head>
<body><form method="post" action="/authorize"><h2>Lockatus</h2><p>Proveedor de prueba</p>%s
<label>Email <input name="email" type="email" required></label>
<label>Nombre <input name="nombre"></label>
<label>Rol <select name="rol"><option value="">(sin rol)</option><option>lectura</option><option>operador</option><option>usuario</option><option>admin</option><option>superadmin</option></select></label>
<label>Email verificado <select name="verificado"><option value="si">sí</option><option value="no">no</option></select></label>
<button type="submit" id="lkEntrar">Entrar</button></form></body></html>`, ocultos.String())
}

func azar(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func copiar(p Persona) Persona {
	c := Persona{}
	for k, v := range p {
		c[k] = v
	}
	return c
}

func (f *Falso) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		escribirJSON(w, 405, map[string]any{"error": "invalid_request"})
		return
	}
	_ = r.ParseForm()
	fo := f.foto()
	id, sec, ok := r.BasicAuth()
	if ok {
		id, _ = url.QueryUnescape(id)
		sec, _ = url.QueryUnescape(sec)
	} else {
		id, sec = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != fo.id || sec != fo.secreto {
		escribirJSON(w, 401, map[string]any{"error": "invalid_client"})
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		escribirJSON(w, 400, map[string]any{"error": "unsupported_grant_type"})
		return
	}
	f.mu.Lock()
	p, hay := f.codigos[r.PostForm.Get("code")]
	delete(f.codigos, r.PostForm.Get("code")) // un solo uso
	f.mu.Unlock()
	if !hay || time.Now().After(p.vence) || r.PostForm.Get("redirect_uri") != p.redirect {
		escribirJSON(w, 400, map[string]any{"error": "invalid_grant"})
		return
	}
	s := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !fo.sinPKCE && b64(s[:]) != p.desafio {
		escribirJSON(w, 400, map[string]any{"error": "invalid_grant", "error_description": "PKCE"})
		return
	}
	ahora := time.Now()
	claims := Persona{"iss": fo.emisor, "aud": fo.id, "iat": ahora.Unix(), "exp": ahora.Add(5 * time.Minute).Unix(), "nonce": p.nonce}
	for k, v := range p.persona {
		claims[k] = v
	}
	if fo.retocar != nil {
		fo.retocar(claims)
	}
	jwt, err := f.firmar(claims, fo.alg, fo.mala)
	if err != nil {
		escribirJSON(w, 500, map[string]any{"error": "server_error"})
		return
	}
	f.Canjes.Add(1)
	escribirJSON(w, 200, map[string]any{"access_token": b64(azar(16)), "token_type": "Bearer", "expires_in": 300, "id_token": jwt})
}

func (f *Falso) firmar(claims Persona, alg string, mala bool) (string, error) {
	if alg == "" {
		alg = "RS256"
	}
	f.mu.Lock()
	var k *clave
	for _, c := range f.claves {
		if c.alg == alg {
			k = c
		}
	}
	if mala && alg == "RS256" {
		k = f.intrusa
	}
	f.mu.Unlock()
	if k == nil {
		return "", fmt.Errorf("sin clave %s", alg)
	}
	cab, _ := json.Marshal(map[string]string{"alg": alg, "kid": k.kid, "typ": "JWT"})
	cuerpo, _ := json.Marshal(claims)
	firmado := b64(cab) + "." + b64(cuerpo)
	h := sha256.Sum256([]byte(firmado))
	var firma []byte
	if k.rsa != nil {
		var err error
		if firma, err = rsa.SignPKCS1v15(rand.Reader, k.rsa, crypto.SHA256, h[:]); err != nil {
			return "", err
		}
	} else {
		r, s, err := ecdsa.Sign(rand.Reader, k.ec, h[:])
		if err != nil {
			return "", err
		}
		firma = append(r.FillBytes(make([]byte, 32)), s.FillBytes(make([]byte, 32))...)
		if mala {
			firma[0] ^= 0xff
		}
	}
	return firmado + "." + b64(firma), nil
}
