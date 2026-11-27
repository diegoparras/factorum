package correo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Direcciones de Google (se cambian en las pruebas).
const (
	GoogleAuthURL   = "https://accounts.google.com/o/oauth2/v2/auth"
	GoogleTokenURL  = "https://oauth2.googleapis.com/token"
	GoogleRevokeURL = "https://oauth2.googleapis.com/revoke"
	GmailAPI        = "https://gmail.googleapis.com/gmail/v1"
	// AlcanceGmail deja leer, etiquetar y marcar (no borrar para siempre ni mandar).
	AlcanceGmail = "https://www.googleapis.com/auth/gmail.modify"
)

// Qué se hace en Gmail con un mail ya procesado.
const (
	GmailQuitar = "quitar" // se le saca la etiqueta
	GmailLeido  = "leido"  // se marca como leído (y se buscan solo los no leídos)
)

// Google es la aplicación de Google Cloud (OAuth) y sus direcciones.
type Google struct {
	ClienteID      string
	ClienteSecreto string
	AuthURL        string
	TokenURL       string
	RevokeURL      string
	APIBase        string
	HTTP           *http.Client
}

func (g Google) url(def, v string) string {
	if v != "" {
		return strings.TrimRight(v, "/")
	}
	return def
}

func (g Google) cliente() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// PKCE genera el verificador y su desafío (S256).
func PKCE() (verificador, desafio string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	verificador = base64.RawURLEncoding.EncodeToString(b)
	s := sha256.Sum256([]byte(verificador))
	return verificador, base64.RawURLEncoding.EncodeToString(s[:])
}

// URLConsentimiento es a dónde se manda al navegador para «Conectar con Google».
func (g Google) URLConsentimiento(redirect, estado, desafio, email string) string {
	q := url.Values{
		"client_id":             {g.ClienteID},
		"redirect_uri":          {redirect},
		"response_type":         {"code"},
		"scope":                 {AlcanceGmail},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
		"state":                 {estado},
		"code_challenge":        {desafio},
		"code_challenge_method": {"S256"},
	}
	if email != "" {
		q.Set("login_hint", email)
	}
	return g.url(GoogleAuthURL, g.AuthURL) + "?" + q.Encode()
}

type respToken struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	Descripcion  string `json:"error_description"`
}

func (g Google) token(ctx context.Context, form url.Values) (*respToken, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url(GoogleTokenURL, g.TokenURL), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := g.cliente().Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google no responde (%s)", ErrorDeRed(err))
	}
	defer res.Body.Close()
	var t respToken
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&t)
	if res.StatusCode != 200 || t.AccessToken == "" {
		switch t.Error {
		case "invalid_grant":
			return nil, errors.New("Google ya no acepta el permiso (se revocó, venció o cambió la contraseña): tocá «Conectar con Google» de nuevo")
		case "invalid_client", "unauthorized_client":
			return nil, errors.New("Google no reconoce el client ID o el client secret: revisalos en la consola de Google Cloud")
		case "redirect_uri_mismatch":
			return nil, errors.New("el redirect no coincide con el configurado en Google Cloud")
		}
		return nil, fmt.Errorf("Google respondió %d: %s", res.StatusCode, recortar(t.Error+" "+t.Descripcion, 160))
	}
	return &t, nil
}

// Canjear cambia el código de la vuelta del consentimiento por el refresh token.
func (g Google) Canjear(ctx context.Context, codigo, verificador, redirect string) (refresh string, err error) {
	t, err := g.token(ctx, url.Values{"code": {codigo}, "client_id": {g.ClienteID}, "client_secret": {g.ClienteSecreto},
		"redirect_uri": {redirect}, "grant_type": {"authorization_code"}, "code_verifier": {verificador}})
	if err != nil {
		return "", err
	}
	if t.RefreshToken == "" {
		return "", errors.New("Google no mandó el permiso para seguir leyendo (refresh token): sacale el acceso a la app en tu cuenta de Google y conectá de nuevo")
	}
	if t.Scope != "" && !strings.Contains(t.Scope, AlcanceGmail) {
		return "", errors.New("hace falta el permiso para leer y etiquetar el correo (gmail.modify): conectá de nuevo y aceptalo")
	}
	return t.RefreshToken, nil
}

// Revocar le saca el permiso a Factorum en la cuenta de Google.
func (g Google) Revocar(ctx context.Context, refresh string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.url(GoogleRevokeURL, g.RevokeURL), strings.NewReader(url.Values{"token": {refresh}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := g.cliente().Do(req)
	if err != nil {
		return fmt.Errorf("Google no responde (%s)", ErrorDeRed(err))
	}
	res.Body.Close()
	if res.StatusCode != 200 && res.StatusCode != 400 { // 400: ya estaba revocado o vencido
		return fmt.Errorf("Google respondió %d al revocar", res.StatusCode)
	}
	return nil
}

// Gmail lee una etiqueta de una cuenta de Gmail con la API (por HTTP, sin el SDK).
type Gmail struct {
	Google
	RefreshToken string
	Etiqueta     string // Facturas
	Procesado    string // quitar o leido
	MaxPorVuelta int

	mu     sync.Mutex
	acceso string
	vence  time.Time
}

func (g *Gmail) etiqueta() string {
	if strings.TrimSpace(g.Etiqueta) == "" {
		return "Facturas"
	}
	return strings.TrimSpace(g.Etiqueta)
}

func (g *Gmail) accessToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.acceso != "" && time.Now().Before(g.vence) {
		return g.acceso, nil
	}
	if g.RefreshToken == "" {
		return "", errors.New("Gmail no está conectado: tocá «Conectar con Google»")
	}
	t, err := g.token(ctx, url.Values{"refresh_token": {g.RefreshToken}, "client_id": {g.ClienteID}, "client_secret": {g.ClienteSecreto}, "grant_type": {"refresh_token"}})
	if err != nil {
		return "", err
	}
	g.acceso = t.AccessToken
	g.vence = time.Now().Add(time.Duration(max(t.ExpiresIn-60, 30)) * time.Second)
	return g.acceso, nil
}

// api hace un pedido a la API de Gmail. Nunca pone el token en un error.
func (g *Gmail) api(ctx context.Context, metodo, ruta string, cuerpo any, dst any) error {
	tok, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	var rd io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, g.url(GmailAPI, g.APIBase)+"/users/me"+ruta, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := g.cliente().Do(req)
	if err != nil {
		return fmt.Errorf("Gmail no responde (%s)", ErrorDeRed(err))
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	if res.StatusCode == 401 {
		g.mu.Lock()
		g.acceso = ""
		g.mu.Unlock()
		return errors.New("Gmail no aceptó el permiso: conectá de nuevo con Google")
	}
	if res.StatusCode == 403 {
		return errors.New("Gmail rechazó el pedido (403): ¿está habilitada la Gmail API en el proyecto de Google Cloud?")
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		return fmt.Errorf("Gmail respondió %d: %s", res.StatusCode, recortar(e.Error.Message, 160))
	}
	if dst != nil {
		if err := json.Unmarshal(b, dst); err != nil {
			return errors.New("Gmail respondió algo que no se entiende")
		}
	}
	return nil
}

// Perfil devuelve el email de la cuenta conectada.
func (g *Gmail) Perfil(ctx context.Context) (string, error) {
	var p struct {
		EmailAddress string `json:"emailAddress"`
	}
	if err := g.api(ctx, http.MethodGet, "/profile", nil, &p); err != nil {
		return "", err
	}
	return p.EmailAddress, nil
}

// idEtiqueta busca el id de la etiqueta por su nombre (sin distinguir mayúsculas).
func (g *Gmail) idEtiqueta(ctx context.Context) (string, error) {
	var l struct {
		Labels []struct{ ID, Name string } `json:"labels"`
	}
	if err := g.api(ctx, http.MethodGet, "/labels", nil, &l); err != nil {
		return "", err
	}
	for _, x := range l.Labels {
		if strings.EqualFold(x.Name, g.etiqueta()) {
			return x.ID, nil
		}
	}
	return "", fmt.Errorf("la cuenta no tiene la etiqueta %q: creala en Gmail (y, si querés, un filtro que se la ponga a los mails con facturas)", g.etiqueta())
}

// Contar dice cuántos mails tienen la etiqueta (para «Probar»).
func (g *Gmail) Contar(ctx context.Context) (email string, total int, err error) {
	if email, err = g.Perfil(ctx); err != nil {
		return "", 0, err
	}
	id, err := g.idEtiqueta(ctx)
	if err != nil {
		return email, 0, err
	}
	var l struct {
		MessagesTotal int `json:"messagesTotal"`
	}
	if err := g.api(ctx, http.MethodGet, "/labels/"+url.PathEscape(id), nil, &l); err != nil {
		return email, 0, err
	}
	return email, l.MessagesTotal, nil
}

// Revisar baja los mails con la etiqueta (sin leer, si se marcan como leídos), los entrega y, a
// los que se entregaron bien, les saca la etiqueta o los marca como leídos.
func (g *Gmail) Revisar(ctx context.Context, maxMail int64, entregar Entrega, omitido func(Sobre)) (int, error) {
	id, err := g.idEtiqueta(ctx)
	if err != nil {
		return 0, err
	}
	q := url.Values{"labelIds": {id}, "maxResults": {"50"}}
	if g.Procesado == GmailLeido {
		q.Add("labelIds", "UNREAD")
	}
	if g.MaxPorVuelta > 0 {
		q.Set("maxResults", fmt.Sprint(g.MaxPorVuelta))
	}
	var l struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := g.api(ctx, http.MethodGet, "/messages?"+q.Encode(), nil, &l); err != nil {
		return 0, err
	}
	n := 0
	for _, m := range l.Messages {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		ruta := "/messages/" + url.PathEscape(m.ID)
		var meta struct {
			SizeEstimate int64 `json:"sizeEstimate"`
		}
		if err := g.api(ctx, http.MethodGet, ruta+"?format=minimal", nil, &meta); err != nil {
			return n, err
		}
		if maxMail > 0 && meta.SizeEstimate > maxMail*4/3 {
			if omitido != nil {
				omitido(Sobre{Tamano: meta.SizeEstimate})
			}
			if err := g.marcar(ctx, ruta, id); err != nil {
				return n, err
			}
			continue
		}
		var raw struct {
			Raw string `json:"raw"`
		}
		if err := g.api(ctx, http.MethodGet, ruta+"?format=raw", nil, &raw); err != nil {
			return n, err
		}
		b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(raw.Raw, "="))
		if err != nil {
			return n, errors.New("Gmail mandó un mail que no se puede decodificar")
		}
		if err := entregar(ctx, b); err != nil {
			return n, err
		}
		n++
		if err := g.marcar(ctx, ruta, id); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (g *Gmail) marcar(ctx context.Context, ruta, etiqueta string) error {
	cuerpo := map[string][]string{"removeLabelIds": {etiqueta}}
	if g.Procesado == GmailLeido {
		cuerpo = map[string][]string{"removeLabelIds": {"UNREAD"}}
	}
	return g.api(ctx, http.MethodPost, ruta+"/modify", cuerpo, nil)
}
