// Package oidc habla con Lockatus (u otro proveedor OpenID Connect) para el inicio de sesión
// único: lee el discovery del emisor, arma la dirección de autorización (Authorization Code con
// PKCE S256, state y nonce) y canjea el código por el id_token, que verifica entero antes de
// devolver quién es la persona.
//
// La firma la verifica github.com/coreos/go-oidc (RS256 y ES256, con las claves del JWKS del emisor
// en caché y refrescadas cuando aparece un kid nuevo); el canje lo hace golang.org/x/oauth2. El
// emisor, la audiencia, el vencimiento, nbf, iat y el nonce los controla este paquete, con una
// tolerancia chica para la hora, y cada rechazo tiene su error para la auditoría. Nada de lo que
// pasa por acá (códigos, tokens, state, nonce, el client secret) va a un log ni a un error.
package oidc

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Tolerancia es cuánto se perdona de diferencia de hora con el emisor (exp, nbf, iat).
const Tolerancia = time.Minute

// Algoritmos de firma que se aceptan para el id_token.
var Algoritmos = []string{gooidc.RS256, gooidc.ES256}

// Errores de la verificación. Ninguno lleva el token ni el código.
var (
	ErrDiscovery  = errors.New("el discovery del emisor no sirve")
	ErrCanje      = errors.New("Lockatus no aceptó el código")
	ErrSinIDToken = errors.New("Lockatus no devolvió un id_token")
	ErrFirma      = errors.New("la firma del id_token no es válida")
	ErrClaves     = errors.New("no se pudieron leer las claves (JWKS) del emisor")
	ErrEmisor     = errors.New("el id_token es de otro emisor")
	ErrAudiencia  = errors.New("el id_token es para otro cliente")
	ErrVencido    = errors.New("el id_token venció")
	ErrTodavia    = errors.New("el id_token todavía no vale")
	ErrNonce      = errors.New("el nonce del id_token no coincide")
)

// Discovery es lo que publica el emisor en /.well-known/openid-configuration.
type Discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	Algoritmos            []string `json:"id_token_signing_alg_values_supported"`
	PKCE                  []string `json:"code_challenge_methods_supported"`
	Scopes                []string `json:"scopes_supported"`
	MetodosToken          []string `json:"token_endpoint_auth_methods_supported"`
	TiposRespuesta        []string `json:"response_types_supported"`
}

func mismoEmisor(a, b string) bool { return strings.TrimRight(a, "/") == strings.TrimRight(b, "/") }

func urlAbsoluta(s string, soloHTTPS bool) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "https" || (!soloHTTPS && u.Scheme == "http")
}

// Descubrir lee y controla el discovery del emisor: que diga ser ese emisor y que tenga los
// endpoints de autorización, de token y de claves (https si el emisor es https), PKCE con S256 y
// alguno de los algoritmos que se aceptan.
func Descubrir(ctx context.Context, cli *http.Client, emisor string) (*Discovery, error) {
	emisor = strings.TrimRight(strings.TrimSpace(emisor), "/")
	if !urlAbsoluta(emisor, false) {
		return nil, fmt.Errorf("%w: el emisor tiene que ser una URL http:// o https://", ErrDiscovery)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, emisor+"/.well-known/openid-configuration", nil)
	if err != nil {
		return nil, fmt.Errorf("%w: el emisor no es una URL válida", ErrDiscovery)
	}
	req.Header.Set("Accept", "application/json")
	if cli == nil {
		cli = http.DefaultClient
	}
	res, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: no responde (%s)", ErrDiscovery, errorDeRed(err))
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s/.well-known/openid-configuration respondió %d", ErrDiscovery, emisor, res.StatusCode)
	}
	var d Discovery
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&d); err != nil {
		return nil, fmt.Errorf("%w: respondió algo que no es el JSON del discovery", ErrDiscovery)
	}
	soloHTTPS := strings.HasPrefix(emisor, "https://")
	switch {
	case d.Issuer == "":
		return nil, fmt.Errorf("%w: no dice su issuer", ErrDiscovery)
	case !mismoEmisor(d.Issuer, emisor):
		return nil, fmt.Errorf("%w: dice ser %q y no %q (copiá el emisor tal como lo publica)", ErrDiscovery, d.Issuer, emisor)
	case !urlAbsoluta(d.AuthorizationEndpoint, soloHTTPS):
		return nil, fmt.Errorf("%w: falta authorization_endpoint (o no es https)", ErrDiscovery)
	case !urlAbsoluta(d.TokenEndpoint, soloHTTPS):
		return nil, fmt.Errorf("%w: falta token_endpoint (o no es https)", ErrDiscovery)
	case !urlAbsoluta(d.JWKSURI, soloHTTPS):
		return nil, fmt.Errorf("%w: falta jwks_uri (o no es https)", ErrDiscovery)
	case len(d.TiposRespuesta) > 0 && !slices.Contains(d.TiposRespuesta, "code"):
		return nil, fmt.Errorf("%w: no ofrece Authorization Code (response_type code)", ErrDiscovery)
	case len(d.PKCE) > 0 && !slices.Contains(d.PKCE, "S256"):
		return nil, fmt.Errorf("%w: no ofrece PKCE con S256", ErrDiscovery)
	case len(d.Algoritmos) > 0 && !slices.ContainsFunc(d.Algoritmos, func(a string) bool { return slices.Contains(Algoritmos, a) }):
		return nil, fmt.Errorf("%w: firma los id_token con %s y Factorum acepta RS256 o ES256", ErrDiscovery, strings.Join(d.Algoritmos, ", "))
	}
	return &d, nil
}

// errorDeRed resume un error de conexión sin la URL.
func errorDeRed(err error) string {
	m := err.Error()
	switch {
	case strings.Contains(m, "no such host"):
		return "no se encuentra el host"
	case strings.Contains(m, "connection refused"):
		return "conexión rechazada"
	case strings.Contains(m, "deadline exceeded"), strings.Contains(m, "Timeout"):
		return "no contestó a tiempo"
	case strings.Contains(m, "certificate"):
		return "problema con el certificado TLS"
	}
	return "error de red"
}

// Config es lo que hace falta para hablar con el emisor.
type Config struct {
	Emisor, ClienteID, ClienteSecreto string
	Redirect                          string // <URL pública>/api/auth/oidc/callback
	Scopes                            []string
	HTTP                              *http.Client     // nil = http.DefaultClient
	Ahora                             func() time.Time // nil = time.Now
}

// Proveedor es un emisor ya descubierto, con sus claves (que se cargan y se refrescan solas).
type Proveedor struct {
	Discovery *Discovery
	cfg       Config
	oauth     oauth2.Config
	verif     *gooidc.IDTokenVerifier
}

// Nuevo lee el discovery y arma el proveedor. Las claves del JWKS se piden con el primer id_token.
func Nuevo(ctx context.Context, c Config) (*Proveedor, error) {
	if c.HTTP == nil {
		c.HTTP = http.DefaultClient
	}
	if c.Ahora == nil {
		c.Ahora = time.Now
	}
	d, err := Descubrir(ctx, c.HTTP, c.Emisor)
	if err != nil {
		return nil, err
	}
	estilo := oauth2.AuthStyleAutoDetect
	switch {
	case slices.Contains(d.MetodosToken, "client_secret_basic"):
		estilo = oauth2.AuthStyleInHeader
	case slices.Contains(d.MetodosToken, "client_secret_post"):
		estilo = oauth2.AuthStyleInParams
	}
	// Las claves se piden con el cliente HTTP de Factorum y sin cancelarse con el pedido que las
	// pidió primero (go-oidc guarda el contexto).
	claves := gooidc.NewRemoteKeySet(gooidc.ClientContext(context.Background(), c.HTTP), d.JWKSURI)
	// La firma (con RS256 o ES256) la controla go-oidc; emisor, audiencia y horas, Verificar.
	verif := gooidc.NewVerifier(d.Issuer, claves, &gooidc.Config{ClientID: c.ClienteID, SupportedSigningAlgs: Algoritmos,
		SkipClientIDCheck: true, SkipExpiryCheck: true, SkipIssuerCheck: true})
	return &Proveedor{Discovery: d, cfg: c, verif: verif, oauth: oauth2.Config{
		ClientID: c.ClienteID, ClientSecret: c.ClienteSecreto, RedirectURL: c.Redirect, Scopes: c.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: d.AuthorizationEndpoint, TokenURL: d.TokenEndpoint, AuthStyle: estilo},
	}}, nil
}

// NuevoVerificador es un PKCE verifier nuevo (32 bytes al azar).
func NuevoVerificador() string { return oauth2.GenerateVerifier() }

// URLAutorizacion es a dónde se manda al navegador: Authorization Code con PKCE S256, state y nonce.
func (p *Proveedor) URLAutorizacion(state, nonce, verificador string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verificador), gooidc.Nonce(nonce))
}

// Persona es quién entró, según el id_token ya verificado.
type Persona struct {
	Sub             string
	Email           string
	EmailVerificado bool // email_verified no vino o vino distinto de false
	Nombre          string
	Claims          map[string]any
}

// Canjear cambia el código por los tokens y verifica el id_token: firma, emisor, audiencia,
// vencimiento, nbf, iat y nonce.
func (p *Proveedor) Canjear(ctx context.Context, codigo, verificador, nonce string) (*Persona, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, p.cfg.HTTP)
	tok, err := p.oauth.Exchange(ctx, codigo, oauth2.VerifierOption(verificador))
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			det := re.ErrorCode
			if det == "" && re.Response != nil {
				det = fmt.Sprintf("respondió %d", re.Response.StatusCode)
			}
			return nil, fmt.Errorf("%w (%s)", ErrCanje, recortar(det, 60))
		}
		return nil, fmt.Errorf("%w (%s)", ErrCanje, errorDeRed(err))
	}
	crudo, _ := tok.Extra("id_token").(string)
	if crudo == "" {
		return nil, ErrSinIDToken
	}
	return p.Verificar(ctx, crudo, nonce)
}

// Verificar controla un id_token entero y devuelve la persona.
func (p *Proveedor) Verificar(ctx context.Context, crudo, nonce string) (*Persona, error) {
	idt, err := p.verif.Verify(ctx, crudo)
	if err != nil {
		if strings.Contains(err.Error(), "fetching keys") {
			return nil, ErrClaves
		}
		return nil, ErrFirma
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return nil, ErrFirma
	}
	ahora := p.cfg.Ahora()
	switch {
	case idt.Issuer != p.Discovery.Issuer:
		return nil, ErrEmisor
	case !slices.Contains(idt.Audience, p.cfg.ClienteID):
		return nil, ErrAudiencia
	}
	if azp, _ := claims["azp"].(string); (len(idt.Audience) > 1 || azp != "") && azp != p.cfg.ClienteID {
		return nil, ErrAudiencia
	}
	if idt.Expiry.IsZero() {
		return nil, fmt.Errorf("%w: no dice cuándo vence", ErrVencido)
	}
	if ahora.After(idt.Expiry.Add(Tolerancia)) {
		return nil, ErrVencido
	}
	if nbf, ok := numero(claims["nbf"]); ok && time.Unix(nbf, 0).After(ahora.Add(Tolerancia)) {
		return nil, ErrTodavia
	}
	if !idt.IssuedAt.IsZero() && idt.IssuedAt.After(ahora.Add(Tolerancia)) {
		return nil, ErrTodavia
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		return nil, ErrNonce
	}
	per := &Persona{Sub: idt.Subject, Claims: claims}
	per.Email, _ = claims["email"].(string)
	per.Email = strings.TrimSpace(per.Email)
	per.EmailVerificado = per.Email != "" && !esFalso(claims["email_verified"])
	per.Nombre = nombreDe(claims)
	return per, nil
}

// esFalso: email_verified en false (o "false"). Si no viene, se acepta: Lockatus solo pone emails
// que verificó (igual que Arcanum).
func esFalso(v any) bool {
	switch x := v.(type) {
	case bool:
		return !x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "false")
	}
	return false
}

func numero(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	}
	return 0, false
}

func nombreDe(c map[string]any) string {
	txt := func(k string) string { s, _ := c[k].(string); return strings.TrimSpace(s) }
	if n := txt("name"); n != "" {
		return n
	}
	if n := strings.TrimSpace(txt("given_name") + " " + txt("family_name")); n != "" {
		return n
	}
	return txt("preferred_username")
}

// Claim busca un claim por su nombre; con puntos, adentro de objetos (realm_access.roles).
func Claim(c map[string]any, nombre string) any {
	if v, ok := c[nombre]; ok {
		return v
	}
	var actual any = c
	for _, p := range strings.Split(nombre, ".") {
		m, ok := actual.(map[string]any)
		if !ok {
			return nil
		}
		actual = m[p]
	}
	return actual
}

func recortar(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
