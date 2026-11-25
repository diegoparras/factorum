// Package telegram es el adaptador de Telegram: la Bot API por HTTP plano, sin librerías de
// terceros. Recibe con long polling (getUpdates) y contesta con sendMessage.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/diegoparras/contadorfacturas/internal/canal"
)

const (
	// BaseURLPorDefecto es la Bot API pública.
	BaseURLPorDefecto = "https://api.telegram.org"
	// MaxBytesPorDefecto es el tope de descarga de la Bot API (20 MB).
	MaxBytesPorDefecto int64 = 20 << 20
	// MaxTexto es el largo máximo de un mensaje de Telegram, en unidades UTF-16.
	MaxTexto = 4096

	timeoutPoll     = 50 // segundos que Telegram retiene getUpdates si no hay novedades
	timeoutLlamada  = 30 * time.Second
	timeoutDescarga = 2 * time.Minute
	esperaMaxima    = time.Minute
	reintentosEnvio = 2
	maxRetryAfter   = 30 * time.Second
	nombreFoto      = "foto.jpg"
	nombreDocumento = "documento"
	mimeFoto        = "image/jpeg"
	mimeDesconocido = "application/octet-stream"
	limiteRespuesta = 4 << 20 // tope para leer respuestas JSON de la API
)

// Config del adaptador.
type Config struct {
	Token    string // token del bot (de @BotFather)
	BaseURL  string // por defecto https://api.telegram.org; se cambia en los tests
	MaxBytes int64  // tope por archivo; por defecto 20 MB, el límite de descarga de la Bot API
}

// Telegram implementa canal.Canal. Es seguro para usar desde varias goroutines.
type Telegram struct {
	cfg  Config
	http *http.Client
	log  *slog.Logger

	esperaInicial time.Duration // primer reintento ante errores; crece al doble hasta esperaMaxima

	mu       sync.Mutex
	usuario  string // @username del bot, de getMe
	pollOK   bool
	detalle  string
	iniciado bool
}

var _ canal.Canal = (*Telegram)(nil)

// New arma el adaptador. No hace ninguna llamada hasta Iniciar.
func New(cfg Config) *Telegram {
	if cfg.BaseURL == "" {
		cfg.BaseURL = BaseURLPorDefecto
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = MaxBytesPorDefecto
	}
	return &Telegram{
		cfg:           cfg,
		http:          &http.Client{}, // los timeouts van por contexto, por llamada
		log:           slog.Default().With("canal", "telegram"),
		esperaInicial: time.Second,
	}
}

// Nombre devuelve "telegram".
func (t *Telegram) Nombre() string { return "telegram" }

// Estado dice si la última consulta a Telegram anduvo y con qué bot.
func (t *Telegram) Estado() canal.Estado {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := canal.Estado{Canal: t.Nombre(), Habilitado: t.cfg.Token != "", Conectado: t.pollOK}
	switch {
	case t.cfg.Token == "":
		e.Detalle = "sin token configurado"
	case t.detalle != "":
		e.Detalle = t.detalle
	case t.usuario != "":
		e.Detalle = "@" + t.usuario
	case !t.iniciado:
		e.Detalle = "sin iniciar"
	}
	return e
}

func (t *Telegram) marcar(ok bool, detalle string) {
	t.mu.Lock()
	t.pollOK = ok
	t.detalle = detalle
	t.mu.Unlock()
}

// Iniciar consulta getMe y después recibe mensajes con long polling hasta que ctx se cancela.
// Ante errores reintenta con espera creciente. Cada mensaje se entrega a h en su propia goroutine.
func (t *Telegram) Iniciar(ctx context.Context, h canal.Manejador) error {
	if t.cfg.Token == "" {
		return errors.New("telegram: falta el token del bot")
	}
	if h == nil {
		return errors.New("telegram: falta el manejador de mensajes")
	}
	t.mu.Lock()
	t.iniciado = true
	t.mu.Unlock()
	defer t.marcar(false, "detenido")

	espera := t.esperaInicial

	// getMe: confirma el token y da el nombre del bot.
	for {
		var yo usuario
		err := t.llamar(ctx, "getMe", nil, &yo, timeoutLlamada)
		if err == nil {
			t.mu.Lock()
			t.usuario = yo.Username
			t.mu.Unlock()
			t.marcar(true, "")
			t.log.Info("telegram conectado", "bot", yo.Username)
			break
		}
		if ctx.Err() != nil {
			return nil
		}
		t.marcar(false, describir(err))
		t.log.Warn("telegram: no se pudo consultar el bot, reintento", "error", err, "espera", espera)
		if !dormir(ctx, espera) {
			return nil
		}
		espera = min(espera*2, esperaMaxima)
	}

	espera = t.esperaInicial
	var offset int64
	for {
		if ctx.Err() != nil {
			return nil
		}
		var novedades []update
		params := map[string]any{
			"timeout":         timeoutPoll,
			"allowed_updates": []string{"message"},
		}
		if offset != 0 {
			params["offset"] = offset
		}
		err := t.llamar(ctx, "getUpdates", params, &novedades, (timeoutPoll+15)*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			t.marcar(false, describir(err))
			if ra := retryAfter(err); ra > 0 {
				espera = max(espera, ra)
			}
			t.log.Warn("telegram: falló getUpdates, reintento", "error", err, "espera", espera)
			if !dormir(ctx, espera) {
				return nil
			}
			espera = min(espera*2, esperaMaxima)
			continue
		}
		espera = t.esperaInicial
		t.marcar(true, "")
		for _, u := range novedades {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message == nil || u.Message.Chat.ID == 0 {
				continue
			}
			msg := *u.Message
			go func() {
				h(ctx, t.armar(ctx, msg))
			}()
		}
	}
}

// armar convierte un mensaje de la Bot API en canal.Mensaje, descargando los adjuntos.
// Si un archivo no se puede bajar (o es muy grande) el mensaje igual se entrega, con una nota.
func (t *Telegram) armar(ctx context.Context, msg message) canal.Mensaje {
	m := canal.Mensaje{
		Canal:           t.Nombre(),
		Chat:            strconv.FormatInt(msg.Chat.ID, 10),
		NombreRemitente: nombreDe(msg.From),
		Texto:           msg.Text,
	}
	if msg.From != nil {
		m.Remitente = strconv.FormatInt(msg.From.ID, 10)
	}
	if m.Texto == "" {
		m.Texto = msg.Caption
	}

	var notas []string
	adjuntar := func(fileID, nombre, mime string, tam int64) {
		a, err := t.bajar(ctx, fileID, nombre, mime, tam)
		switch {
		case errors.Is(err, errMuyGrande):
			notas = append(notas, fmt.Sprintf("(El archivo %q pesa más de %s y no se pudo descargar.)", nombre, megas(t.cfg.MaxBytes)))
		case err != nil:
			t.log.Warn("telegram: no se pudo descargar un adjunto", "error", err)
			notas = append(notas, fmt.Sprintf("(No se pudo descargar el archivo %q.)", nombre))
		default:
			m.Archivos = append(m.Archivos, a)
		}
	}
	if d := msg.Document; d != nil && d.FileID != "" {
		nombre := d.FileName
		if nombre == "" {
			nombre = nombreDocumento
		}
		adjuntar(d.FileID, nombre, d.MimeType, d.FileSize)
	}
	if p, ok := fotoMayor(msg.Photo); ok {
		adjuntar(p.FileID, nombreFoto, mimeFoto, p.FileSize)
	}
	if len(notas) > 0 {
		if m.Texto != "" {
			notas = append([]string{m.Texto}, notas...)
		}
		m.Texto = strings.Join(notas, "\n")
	}
	return m
}

var errMuyGrande = errors.New("telegram: el archivo supera el tamaño máximo")

// bajar resuelve el file_path con getFile y descarga el archivo, con tope MaxBytes.
func (t *Telegram) bajar(ctx context.Context, fileID, nombre, mime string, tam int64) (canal.Archivo, error) {
	if tam > t.cfg.MaxBytes {
		return canal.Archivo{}, errMuyGrande
	}
	var f archivo
	if err := t.llamar(ctx, "getFile", map[string]any{"file_id": fileID}, &f, timeoutLlamada); err != nil {
		if strings.Contains(strings.ToLower(describir(err)), "too big") {
			return canal.Archivo{}, errMuyGrande
		}
		return canal.Archivo{}, err
	}
	if f.FileSize > t.cfg.MaxBytes {
		return canal.Archivo{}, errMuyGrande
	}
	if f.FilePath == "" {
		return canal.Archivo{}, errors.New("telegram: getFile no devolvió file_path")
	}

	ctx, cancel := context.WithTimeout(ctx, timeoutDescarga)
	defer cancel()
	dir := t.cfg.BaseURL + "/file/bot" + t.cfg.Token + "/" + escaparRuta(f.FilePath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dir, nil)
	if err != nil {
		return canal.Archivo{}, errors.New("telegram: no se pudo armar la descarga")
	}
	resp, err := t.http.Do(req)
	if err != nil {
		return canal.Archivo{}, fmt.Errorf("telegram: descarga: %w", sinURL(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return canal.Archivo{}, fmt.Errorf("telegram: descarga: HTTP %d", resp.StatusCode)
	}
	datos, err := io.ReadAll(io.LimitReader(resp.Body, t.cfg.MaxBytes+1))
	if err != nil {
		return canal.Archivo{}, fmt.Errorf("telegram: descarga: %w", sinURL(err))
	}
	if int64(len(datos)) > t.cfg.MaxBytes {
		return canal.Archivo{}, errMuyGrande
	}
	if mime == "" {
		mime = http.DetectContentType(datos)
		if mime == "" {
			mime = mimeDesconocido
		}
	}
	return canal.Archivo{Nombre: nombre, Mime: mime, Datos: datos}, nil
}

// Enviar manda texto plano a un chat, partido en mensajes de hasta 4096 caracteres.
func (t *Telegram) Enviar(ctx context.Context, chat, texto string) error {
	if t.cfg.Token == "" {
		return errors.New("telegram: falta el token del bot")
	}
	if strings.TrimSpace(chat) == "" {
		return errors.New("telegram: falta el chat")
	}
	if strings.TrimSpace(texto) == "" {
		return errors.New("telegram: no se puede mandar un mensaje vacío")
	}
	for _, parte := range partir(texto, MaxTexto) {
		params := map[string]any{"chat_id": chat, "text": parte}
		var err error
		for intento := 0; ; intento++ {
			err = t.llamar(ctx, "sendMessage", params, nil, timeoutLlamada)
			ra := retryAfter(err)
			if err == nil || ra <= 0 || intento >= reintentosEnvio {
				break
			}
			if !dormir(ctx, min(ra, maxRetryAfter)) {
				return ctx.Err()
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// partir corta el texto en trozos de hasta max unidades UTF-16 (como cuenta Telegram), cortando
// de preferencia en un salto de línea o un espacio.
func partir(texto string, max int) []string {
	var partes []string
	for texto != "" {
		if largoUTF16(texto) <= max {
			partes = append(partes, texto)
			break
		}
		// Hasta dónde entra (en bytes) sin pasarse de max unidades UTF-16.
		corte, unidades := 0, 0
		for i, r := range texto {
			u := utf16.RuneLen(r)
			if u < 0 {
				u = 1
			}
			if unidades+u > max {
				break
			}
			unidades += u
			corte = i + utf8.RuneLen(r)
		}
		trozo := texto[:corte]
		if i := strings.LastIndex(trozo, "\n"); i > corte/2 {
			corte = i + 1
		} else if i := strings.LastIndex(trozo, " "); i > corte/2 {
			corte = i + 1
		}
		if corte == 0 { // no debería pasar, pero evita un bucle infinito
			_, n := utf8.DecodeRuneInString(texto)
			corte = n
		}
		partes = append(partes, texto[:corte])
		texto = texto[corte:]
	}
	return partes
}

func largoUTF16(s string) int {
	n := 0
	for _, r := range s {
		if u := utf16.RuneLen(r); u > 0 {
			n += u
		} else {
			n++
		}
	}
	return n
}

// --- Bot API ---

type respuesta struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  *struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// errorAPI es un error devuelto por Telegram (ok=false).
type errorAPI struct {
	Metodo      string
	Codigo      int
	Descripcion string
	RetryAfter  time.Duration
}

func (e *errorAPI) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Metodo, e.Codigo, e.Descripcion)
}

type usuario struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type documento struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MimeType string `json:"mime_type"`
	FileSize int64  `json:"file_size"`
}

type foto struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int64  `json:"file_size"`
}

type message struct {
	MessageID int64      `json:"message_id"`
	From      *usuario   `json:"from"`
	Chat      chat       `json:"chat"`
	Text      string     `json:"text"`
	Caption   string     `json:"caption"`
	Document  *documento `json:"document"`
	Photo     []foto     `json:"photo"`
}

type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
}

type archivo struct {
	FileID   string `json:"file_id"`
	FileSize int64  `json:"file_size"`
	FilePath string `json:"file_path"`
}

// llamar hace POST {BaseURL}/bot<token>/<metodo> con params en JSON y decodifica result en dst.
// Los errores nunca incluyen la URL (que lleva el token).
func (t *Telegram) llamar(ctx context.Context, metodo string, params map[string]any, dst any, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if params == nil {
		params = map[string]any{}
	}
	cuerpo, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", metodo, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.cfg.BaseURL+"/bot"+t.cfg.Token+"/"+metodo, bytes.NewReader(cuerpo))
	if err != nil {
		return fmt.Errorf("telegram %s: no se pudo armar el pedido", metodo)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return fmt.Errorf("telegram %s: %w", metodo, sinURL(err))
	}
	defer resp.Body.Close()
	crudo, err := io.ReadAll(io.LimitReader(resp.Body, limiteRespuesta))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", metodo, sinURL(err))
	}
	var r respuesta
	if err := json.Unmarshal(crudo, &r); err != nil {
		return fmt.Errorf("telegram %s: respuesta inválida (HTTP %d)", metodo, resp.StatusCode)
	}
	if !r.OK {
		e := &errorAPI{Metodo: metodo, Codigo: r.ErrorCode, Descripcion: r.Description}
		if e.Codigo == 0 {
			e.Codigo = resp.StatusCode
		}
		if r.Parameters != nil && r.Parameters.RetryAfter > 0 {
			e.RetryAfter = time.Duration(r.Parameters.RetryAfter) * time.Second
		}
		return e
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, dst); err != nil {
		return fmt.Errorf("telegram %s: resultado inválido: %w", metodo, err)
	}
	return nil
}

// --- utilidades ---

// sinURL saca la URL (que lleva el token) de los errores de net/http.
func sinURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

func retryAfter(err error) time.Duration {
	var e *errorAPI
	if errors.As(err, &e) {
		return e.RetryAfter
	}
	return 0
}

// describir da un texto corto para la UI.
func describir(err error) string {
	var e *errorAPI
	if errors.As(err, &e) {
		if e.Codigo == http.StatusUnauthorized {
			return "token inválido"
		}
		return e.Descripcion
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "Telegram no respondió a tiempo"
	}
	return "sin conexión con Telegram"
}

func dormir(ctx context.Context, d time.Duration) bool {
	tm := time.NewTimer(d)
	defer tm.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-tm.C:
		return true
	}
}

func nombreDe(u *usuario) string {
	if u == nil {
		return ""
	}
	n := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if n == "" {
		n = u.Username
	}
	return n
}

// fotoMayor elige la versión más grande de una foto (Telegram manda varias resoluciones).
func fotoMayor(fotos []foto) (foto, bool) {
	var mejor foto
	ok := false
	for _, f := range fotos {
		if f.FileID == "" {
			continue
		}
		if !ok || f.Width*f.Height > mejor.Width*mejor.Height ||
			(f.Width*f.Height == mejor.Width*mejor.Height && f.FileSize > mejor.FileSize) {
			mejor, ok = f, true
		}
	}
	return mejor, ok
}

func escaparRuta(p string) string {
	partes := strings.Split(p, "/")
	for i, s := range partes {
		partes[i] = url.PathEscape(s)
	}
	return strings.Join(partes, "/")
}

func megas(n int64) string {
	if n%(1<<20) == 0 {
		return fmt.Sprintf("%d MB", n>>20)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}
