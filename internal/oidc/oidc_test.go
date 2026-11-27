package oidc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/oidc"
)

func discoveryCon(t *testing.T, cambiar func(d map[string]any, base string)) string {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		b := srv.URL
		d := map[string]any{"issuer": b, "authorization_endpoint": b + "/authorize", "token_endpoint": b + "/token", "jwks_uri": b + "/jwks",
			"code_challenge_methods_supported": []string{"S256"}, "id_token_signing_alg_values_supported": []string{"RS256"}}
		if cambiar != nil {
			cambiar(d, b)
		}
		_ = json.NewEncoder(w).Encode(d)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDescubrir(t *testing.T) {
	ctx := context.Background()
	if d, err := oidc.Descubrir(ctx, nil, discoveryCon(t, nil)+"/"); err != nil || d.TokenEndpoint == "" {
		t.Fatalf("discovery bueno (con la barra final): %v", err)
	}
	casos := []struct {
		nombre  string
		cambiar func(map[string]any, string)
		error   string
	}{
		{"otro issuer", func(d map[string]any, _ string) { d["issuer"] = "https://otro.com" }, "dice ser"},
		{"sin token", func(d map[string]any, _ string) { delete(d, "token_endpoint") }, "token_endpoint"},
		{"sin jwks", func(d map[string]any, _ string) { d["jwks_uri"] = "/jwks" }, "jwks_uri"},
		{"sin S256", func(d map[string]any, _ string) { d["code_challenge_methods_supported"] = []string{"plain"} }, "S256"},
		{"solo HS256", func(d map[string]any, _ string) { d["id_token_signing_alg_values_supported"] = []string{"HS256"} }, "RS256 o ES256"},
		{"sin code", func(d map[string]any, _ string) { d["response_types_supported"] = []string{"token"} }, "Authorization Code"},
	}
	for _, c := range casos {
		_, err := oidc.Descubrir(ctx, nil, discoveryCon(t, c.cambiar))
		if err == nil || !strings.Contains(err.Error(), c.error) {
			t.Errorf("%s: %v", c.nombre, err)
		}
	}
	if _, err := oidc.Descubrir(ctx, nil, "ftp://lockatus"); err == nil {
		t.Error("un emisor que no es http")
	}
}

func TestClaim(t *testing.T) {
	c := map[string]any{"role": "admin", "realm_access": map[string]any{"roles": []any{"lectura"}}, "a.b": "con punto"}
	if oidc.Claim(c, "role") != "admin" || oidc.Claim(c, "a.b") != "con punto" || oidc.Claim(c, "nada.x") != nil {
		t.Fatal("claims simples")
	}
	if r, ok := oidc.Claim(c, "realm_access.roles").([]any); !ok || r[0] != "lectura" {
		t.Fatalf("claim anidado: %v", oidc.Claim(c, "realm_access.roles"))
	}
}
