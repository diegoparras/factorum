// Package webhook es el adaptador genérico: deja que otro bot (la secretaria con Baileys,
// Evolution API, n8n) le pase archivos a Factorum por HTTP y reciba las respuestas en una URL suya.
//
// La entrada la atiende la capa HTTP (POST /api/canales/webhook): valida el token con Autorizado,
// arma el mensaje con Decodificar y lo entrega con Recibir. La salida (Enviar) es un POST firmado
// con HMAC-SHA256 a SalidaURL.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/factorum/internal/canal"
)

const (
	// MaxDecodificado es el tope del total de archivos de un mensaje, ya decodificados (25 MB).
	MaxDecodificado = 25 << 20
	// MaxCuerpo es el tope sugerido para el cuerpo HTTP (http.MaxBytesReader): el base64 de
	// MaxDecodificado más margen para el JSON.
	MaxCuerpo = MaxDecodificado/3*4 + 1<<20

	// CabeceraToken es donde viene el token en la entrada.
	CabeceraToken = "X-Factorum-Token"
	// CabeceraFirma es donde va la firma en la salida: "sha256=<hex>".
	CabeceraFirma = "X-Factorum-Firma"

	timeoutEnvio = 10 * time.Second
	reintentos   = 2
)

// ErrNoIniciado lo devuelve Recibir si el canal todavía no arrancó (o ya se detuvo).
var ErrNoIniciado = errors.New("webhook: el canal no está iniciado")

// Config del adaptador.
type Config struct {
	Token     string       // FACTORUM_WEBHOOK_TOKEN: autentica la entrada y firma la salida
	SalidaURL string       // FACTORUM_WEBHOOK_SALIDA_URL: a dónde se mandan las respuestas
	HTTP      *http.Client // opcional; por defecto uno nuevo (el timeout va por contexto)
}

// Webhook implementa canal.Canal. Es seguro para usar desde varias goroutines.
type Webhook struct {
	cfg  Config
	http *http.Client
	log  *slog.Logger

	esperaInicial time.Duration // primer reintento de Enviar; se duplica

	mu         sync.Mutex
	h          canal.Manejador
	ctx        context.Context
	errorEnvio string
}

var _ canal.Canal = (*Webhook)(nil)

// New arma el adaptador.
func New(cfg Config) *Webhook {
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	return &Webhook{
		cfg:           cfg,
		http:          hc,
		log:           slog.Default().With("canal", "webhook"),
		esperaInicial: time.Second,
	}
}

// Nombre devuelve "webhook".
func (w *Webhook) Nombre() string { return "webhook" }

// Iniciar guarda el manejador y espera a que ctx se cancele. Los mensajes los trae Recibir.
func (w *Webhook) Iniciar(ctx context.Context, h canal.Manejador) error {
	if h == nil {
		return errors.New("webhook: falta el manejador de mensajes")
	}
	w.mu.Lock()
	if w.h != nil {
		w.mu.Unlock()
		return errors.New("webhook: el canal ya está iniciado")
	}
	w.h, w.ctx = h, ctx
	w.mu.Unlock()

	<-ctx.Done()

	w.mu.Lock()
	w.h, w.ctx = nil, nil
	w.mu.Unlock()
	return nil
}

// Recibir entrega un mensaje entrante al manejador guardado por Iniciar, en su propia goroutine,
// y vuelve enseguida. El manejador recibe el contexto de Iniciar (no el del pedido HTTP, que
// termina al responder). Devuelve ErrNoIniciado si el canal no está corriendo.
func (w *Webhook) Recibir(ctx context.Context, m canal.Mensaje) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	w.mu.Lock()
	h, hctx := w.h, w.ctx
	w.mu.Unlock()
	if h == nil || hctx.Err() != nil {
		return ErrNoIniciado
	}
	m.Canal = w.Nombre()
	go h(hctx, m)
	return nil
}

// Autorizado compara el token recibido con el configurado en tiempo constante (tampoco filtra
// el largo). Sin token configurado, nada está autorizado.
func (w *Webhook) Autorizado(token string) bool {
	if w.cfg.Token == "" || token == "" {
		return false
	}
	a := sha256.Sum256([]byte(token))
	b := sha256.Sum256([]byte(w.cfg.Token))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// Estado del canal para la UI.
func (w *Webhook) Estado() canal.Estado {
	w.mu.Lock()
	defer w.mu.Unlock()
	e := canal.Estado{Canal: w.Nombre(), Habilitado: w.cfg.Token != "", Conectado: w.h != nil}
	switch {
	case w.cfg.Token == "":
		e.Detalle = "sin token configurado"
	case w.errorEnvio != "":
		e.Detalle = w.errorEnvio
	case w.cfg.SalidaURL == "":
		e.Detalle = "sin URL de salida: solo se contesta en la respuesta del webhook"
	}
	return e
}

// --- entrada ---

type cuerpoEntrada struct {
	Chat            texto          `json:"chat"`
	Remitente       texto          `json:"remitente"`
	NombreRemitente string         `json:"nombre_remitente"`
	Texto           string         `json:"texto"`
	Archivos        []archivoEntra `json:"archivos"`
}

type archivoEntra struct {
	Nombre          string `json:"nombre"`
	Mime            string `json:"mime"`
	ContenidoBase64 string `json:"contenido_base64"`
}

// texto acepta un string o un número en el JSON (algunos bots mandan el chat como número).
type texto string

func (t *texto) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*t = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*t = texto(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return errors.New("tiene que ser un texto o un número")
	}
	*t = texto(n.String())
	return nil
}

// Decodificar arma un canal.Mensaje con el cuerpo JSON del webhook de entrada:
//
//	{ "chat", "remitente", "nombre_remitente", "texto", "archivos": [ { "nombre", "mime", "contenido_base64" } ] }
//
// Los archivos juntos no pueden pasar de MaxDecodificado. Falla si falta el chat, si el mensaje
// no trae ni texto ni archivos, o si algún archivo no es base64 válido.
func Decodificar(body []byte) (canal.Mensaje, error) {
	var c cuerpoEntrada
	if err := json.Unmarshal(body, &c); err != nil {
		return canal.Mensaje{}, fmt.Errorf("webhook: JSON inválido: %w", err)
	}
	m := canal.Mensaje{
		Canal:           "webhook",
		Chat:            strings.TrimSpace(string(c.Chat)),
		Remitente:       strings.TrimSpace(string(c.Remitente)),
		NombreRemitente: strings.TrimSpace(c.NombreRemitente),
		Texto:           c.Texto,
	}
	if m.Chat == "" {
		return canal.Mensaje{}, errors.New("webhook: falta \"chat\"")
	}
	var total int64
	for i, a := range c.Archivos {
		b64 := limpiarBase64(a.ContenidoBase64)
		if b64 == "" {
			return canal.Mensaje{}, fmt.Errorf("webhook: el archivo %d no tiene contenido", i+1)
		}
		// Tope antes de decodificar, para no reservar memoria de más.
		if total+int64(base64.StdEncoding.DecodedLen(len(b64))) > MaxDecodificado+3 {
			return canal.Mensaje{}, errMuyGrande()
		}
		datos, err := decodificarBase64(b64)
		if err != nil {
			return canal.Mensaje{}, fmt.Errorf("webhook: el archivo %d no es base64 válido", i+1)
		}
		total += int64(len(datos))
		if total > MaxDecodificado {
			return canal.Mensaje{}, errMuyGrande()
		}
		nombre := strings.TrimSpace(filepath.Base(strings.ReplaceAll(a.Nombre, "\\", "/")))
		if nombre == "" || nombre == "." || nombre == "/" {
			nombre = fmt.Sprintf("archivo-%d", i+1)
		}
		m.Archivos = append(m.Archivos, canal.Archivo{Nombre: nombre, Mime: adivinarMime(a.Mime, nombre, datos), Datos: datos})
	}
	if strings.TrimSpace(m.Texto) == "" && len(m.Archivos) == 0 {
		return canal.Mensaje{}, errors.New("webhook: el mensaje no trae texto ni archivos")
	}
	return m, nil
}

func errMuyGrande() error {
	return fmt.Errorf("webhook: los archivos superan los %d MB", MaxDecodificado>>20)
}

// limpiarBase64 saca un prefijo data:...;base64, y los espacios o saltos de línea.
func limpiarBase64(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "data:") {
		if i := strings.Index(s, ","); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		}
		return r
	}, s)
}

// decodificarBase64 acepta base64 estándar o URL-safe, con o sin relleno.
func decodificarBase64(s string) ([]byte, error) {
	var err error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		var d []byte
		if d, err = enc.DecodeString(s); err == nil {
			return d, nil
		}
	}
	return nil, err
}

func adivinarMime(declarado, nombre string, datos []byte) string {
	if declarado = strings.TrimSpace(declarado); declarado != "" {
		return declarado
	}
	if t := mime.TypeByExtension(strings.ToLower(filepath.Ext(nombre))); t != "" {
		return t
	}
	return http.DetectContentType(datos)
}

// --- salida ---

// Firmar devuelve el valor de la cabecera X-Factorum-Firma para un cuerpo: "sha256=<hex>".
func Firmar(token string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// Enviar hace POST {"chat","texto"} a SalidaURL, firmado con X-Factorum-Firma. Cada intento
// tiene 10 segundos; ante errores de red, 429 o 5xx reintenta dos veces con espera creciente.
func (w *Webhook) Enviar(ctx context.Context, chat, texto string) error {
	if w.cfg.SalidaURL == "" {
		return errors.New("webhook: no hay URL de salida configurada")
	}
	if w.cfg.Token == "" {
		return errors.New("webhook: falta el token para firmar la salida")
	}
	if strings.TrimSpace(chat) == "" {
		return errors.New("webhook: falta el chat")
	}
	body, err := json.Marshal(map[string]string{"chat": chat, "texto": texto})
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	firma := Firmar(w.cfg.Token, body)

	espera := w.esperaInicial
	for intento := 0; ; intento++ {
		reintentable, err := w.enviarUna(ctx, body, firma)
		if err == nil {
			w.anotar("")
			return nil
		}
		if !reintentable || intento >= reintentos || ctx.Err() != nil {
			w.anotar("falló el último envío a la URL de salida")
			w.log.Warn("webhook: no se pudo mandar la respuesta", "error", err, "intentos", intento+1)
			return err
		}
		t := time.NewTimer(espera)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		espera *= 2
	}
}

func (w *Webhook) enviarUna(ctx context.Context, body []byte, firma string) (reintentable bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeoutEnvio)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.SalidaURL, bytes.NewReader(body))
	if err != nil {
		return false, errors.New("webhook: la URL de salida es inválida")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(CabeceraFirma, firma)
	resp, err := w.http.Do(req)
	if err != nil {
		// Sin la URL: puede llevar credenciales en la query.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return true, fmt.Errorf("webhook: envío: %w", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	err = fmt.Errorf("webhook: la URL de salida respondió HTTP %d", resp.StatusCode)
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500, err
}

func (w *Webhook) anotar(s string) {
	w.mu.Lock()
	w.errorEnvio = s
	w.mu.Unlock()
}
