package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/canal/telegram"
	"github.com/diegoparras/factorum/internal/canal/webhook"
	"github.com/diegoparras/factorum/internal/config"
)

// Prueba hace una prueba real de una sección con la configuración que se le pasa y dice qué pasó.
type Prueba func(ctx context.Context, c *config.Config) (string, error)

// seccionesDe: lo que cada rol ve y cambia. Un admin solo los clientes; un superadmin, todo.
func seccionesDe(id *auth.Identidad) []string {
	if id.Rol >= auth.Superadmin {
		return ajustes.Secciones
	}
	return []string{"clientes"}
}

// GET /api/configuracion
func (s *Server) verConfiguracion(w http.ResponseWriter, r *http.Request) {
	if s.Ajustes == nil {
		jsonErr(w, http.StatusServiceUnavailable, "La configuración desde la pantalla no está disponible")
		return
	}
	s.responderConfiguracion(w, r, nil)
}

func (s *Server) responderConfiguracion(w http.ResponseWriter, r *http.Request, cambiadas []string) {
	id := IdentidadDe(r.Context())
	secs := seccionesDe(id)
	var campos []ajustes.VistaCampo
	for _, v := range s.Ajustes.Vista() {
		if slices.Contains(secs, v.Seccion) {
			campos = append(campos, v)
		}
	}
	cfg := s.App.Config()
	res := map[string]any{"campos": campos, "secciones": secs, "canales": s.App.Canales(), "url_publica": cfg.URLPublica}
	if cfg.URLPublica != "" {
		res["webhook_entrada"] = cfg.URLPublica + "/api/canales/webhook"
	}
	if cambiadas != nil {
		res["cambiadas"] = cambiadas
	}
	jsonOK(w, http.StatusOK, res)
}

// PUT /api/configuracion {cambios: {clave: valor|null}}
func (s *Server) guardarConfiguracion(w http.ResponseWriter, r *http.Request) {
	if s.Ajustes == nil {
		jsonErr(w, http.StatusServiceUnavailable, "La configuración desde la pantalla no está disponible")
		return
	}
	var b struct {
		Cambios map[string]json.RawMessage `json:"cambios"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	id := IdentidadDe(r.Context())
	secs := seccionesDe(id)
	for k := range b.Cambios {
		c, ok := ajustes.CampoPorClave(k)
		if !ok {
			jsonErr(w, http.StatusBadRequest, fmt.Sprintf("%q no es un campo de la configuración", k))
			return
		}
		if !slices.Contains(secs, c.Seccion) {
			jsonErr(w, http.StatusForbidden, "Solo un superadmin puede cambiar esta parte de la configuración")
			return
		}
	}
	cambiadas, err := s.Ajustes.Guardar(r.Context(), b.Cambios, id.Quien())
	switch {
	case errors.Is(err, ajustes.ErrBloqueado):
		jsonErr(w, http.StatusConflict, capital(err.Error()))
		return
	case errors.Is(err, ajustes.ErrInvalido):
		jsonErr(w, http.StatusBadRequest, capital(strings.TrimPrefix(err.Error(), ajustes.ErrInvalido.Error()+": ")))
		return
	case err != nil:
		s.fallo(w, err)
		return
	}
	if len(cambiadas) > 0 {
		// Qué claves cambiaron, nunca los valores (los secretos, menos que menos).
		s.auditar(r, "config.cambiar", strings.Join(cambiadas, ", "), map[string]any{"claves": cambiadas})
	}
	s.responderConfiguracion(w, r, cambiadas)
}

// POST /api/configuracion/probar/{seccion} {cambios}: prueba con lo que está en pantalla (sin guardar).
func (s *Server) probarConfiguracion(w http.ResponseWriter, r *http.Request) {
	if s.Ajustes == nil {
		jsonErr(w, http.StatusServiceUnavailable, "La configuración desde la pantalla no está disponible")
		return
	}
	sec := r.PathValue("seccion")
	p, ok := s.Probar[sec]
	if !ok {
		jsonErr(w, http.StatusNotFound, "Esa sección no tiene prueba")
		return
	}
	var b struct {
		Cambios map[string]json.RawMessage `json:"cambios"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&b)
	for k := range b.Cambios {
		if c, ok := ajustes.CampoPorClave(k); !ok || c.Seccion != sec {
			delete(b.Cambios, k)
		}
	}
	cfg, err := s.Ajustes.Aplicar(b.Cambios)
	if err != nil {
		jsonOK(w, http.StatusOK, map[string]any{"exito": false, "mensaje": capital(strings.TrimPrefix(err.Error(), ajustes.ErrInvalido.Error()+": "))})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	msg, err := p(ctx, cfg)
	if err != nil {
		jsonOK(w, http.StatusOK, map[string]any{"exito": false, "mensaje": capital(err.Error())})
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"exito": true, "mensaje": msg})
}

// ---------- Pruebas de cada sección ----------

func (s *Server) pruebasPorDefecto() map[string]Prueba {
	return map[string]Prueba{
		"general":  s.probarGeneral,
		"arcanum":  s.probarArcanum,
		"telegram": s.probarTelegram,
		"webhook":  s.probarWebhook,
		"ia":       s.probarIA,
		"r2":       s.probarR2,
	}
}

// pedirJSON hace un pedido y decodifica la respuesta. Nunca pone el pedido entero en el error (puede
// llevar un token en la URL).
func (s *Server) pedirJSON(ctx context.Context, metodo, url string, cab map[string]string, cuerpo []byte, dst any) (int, error) {
	var rd io.Reader
	if cuerpo != nil {
		rd = bytes.NewReader(cuerpo)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, url, rd)
	if err != nil {
		return 0, errors.New("la URL no es válida")
	}
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cab {
		req.Header.Set(k, v)
	}
	res, err := s.cliente().Do(req)
	if err != nil {
		return 0, fmt.Errorf("no responde (%s)", errorDeRed(err))
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if dst != nil && len(b) > 0 {
		if err := json.Unmarshal(b, dst); err != nil && res.StatusCode < 300 {
			return res.StatusCode, errors.New("respondió algo que no es JSON")
		}
	}
	return res.StatusCode, nil
}

// errorDeRed resume un error de conexión sin la URL (que puede llevar un token).
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

func (s *Server) probarGeneral(ctx context.Context, c *config.Config) (string, error) {
	if c.URLPublica == "" {
		return "", errors.New("falta la URL pública")
	}
	var b struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, c.URLPublica+"/healthz", nil, nil, &b)
	if err != nil {
		return "", fmt.Errorf("la URL pública, vista desde el servidor, %v. Si desde tu navegador anda, puede ser la red del servidor; si no, revisá el DNS y el proxy", err)
	}
	if st != 200 || !b.OK {
		return "", fmt.Errorf("la URL pública respondió %d: no parece ser Factorum", st)
	}
	return fmt.Sprintf("La URL pública responde: es Factorum %s.", b.Version), nil
}

func (s *Server) probarArcanum(ctx context.Context, c *config.Config) (string, error) {
	if c.ArcanumURL == "" {
		return "", errors.New("falta la URL interna de Arcanum")
	}
	var h struct {
		OK      bool   `json:"ok"`
		Service string `json:"service"`
		Version string `json:"version"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, c.ArcanumURL+"/api/health", nil, nil, &h)
	if err != nil {
		return "", fmt.Errorf("Arcanum %v en %s", err, c.ArcanumURL)
	}
	if st != 200 || !h.OK {
		return "", fmt.Errorf("%s/api/health respondió %d: ¿es la URL de Arcanum?", c.ArcanumURL, st)
	}
	if c.ArcanumAPIKey == "" {
		return "", fmt.Errorf("Arcanum %s responde, pero falta la clave", h.Version)
	}
	var t struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Tenants []struct {
			CUIT string `json:"cuit"`
		} `json:"tenants"`
	}
	st, err = s.pedirJSON(ctx, http.MethodGet, c.ArcanumURL+"/api/tenants", map[string]string{"X-API-Key": c.ArcanumAPIKey}, nil, &t)
	if err != nil {
		return "", fmt.Errorf("Arcanum %v", err)
	}
	if st == 401 || st == 403 {
		return "", fmt.Errorf("Arcanum responde, pero no acepta la clave (%d): revisá que esté bien copiada y no esté revocada", st)
	}
	if st != 200 {
		return "", fmt.Errorf("Arcanum respondió %d a una consulta de solo lectura: %s", st, t.Error)
	}
	msg := fmt.Sprintf("Arcanum %s responde y la clave anda. CUITs con certificado: %d.", h.Version, len(t.Tenants))
	if c.ArcanumConsultante != "" {
		hay := false
		for _, x := range t.Tenants {
			hay = hay || x.CUIT == c.ArcanumConsultante
		}
		if !hay {
			msg += " Ojo: el CUIT consultante no figura entre los que esta clave puede usar."
		}
	}
	return msg, nil
}

// telegramBase permite cambiar la Bot API en las pruebas.
var telegramBase = telegram.BaseURLPorDefecto

func (s *Server) probarTelegram(ctx context.Context, c *config.Config) (string, error) {
	if c.TelegramToken == "" {
		return "", errors.New("falta el token del bot")
	}
	var b struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			Username string `json:"username"`
		} `json:"result"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, telegramBase+"/bot"+c.TelegramToken+"/getMe", nil, nil, &b)
	if err != nil {
		return "", fmt.Errorf("Telegram %v", err)
	}
	if st == 401 || st == 404 {
		return "", errors.New("Telegram no reconoce el token: copialo de nuevo de @BotFather")
	}
	if !b.OK {
		return "", fmt.Errorf("Telegram respondió %d: %s", st, b.Description)
	}
	msg := fmt.Sprintf("El token anda: el bot es @%s.", b.Result.Username)
	if len(c.TelegramPermitidos) == 0 {
		msg += " Todavía no hay nadie permitido: el bot le va a contestar a cada uno su id."
	}
	return msg, nil
}

func (s *Server) probarWebhook(ctx context.Context, c *config.Config) (string, error) {
	if c.WebhookToken == "" {
		return "", errors.New("falta el token de entrada")
	}
	if c.WebhookSalidaURL == "" {
		return "El token está. No hay URL de salida para probar: Factorum va a contestar solo en la respuesta del webhook.", nil
	}
	cuerpo, _ := json.Marshal(map[string]string{"chat": "factorum-prueba", "texto": "Prueba de Factorum: si ves esto, la salida del webhook anda."})
	st, err := s.pedirJSON(ctx, http.MethodPost, c.WebhookSalidaURL, map[string]string{webhook.CabeceraFirma: webhook.Firmar(c.WebhookToken, cuerpo)}, cuerpo, nil)
	if err != nil {
		return "", fmt.Errorf("la URL de salida %v", err)
	}
	if st >= 300 {
		return "", fmt.Errorf("la URL de salida respondió %d", st)
	}
	return fmt.Sprintf("Se mandó un aviso de prueba firmado a la URL de salida y respondió %d.", st), nil
}

func (s *Server) probarIA(ctx context.Context, c *config.Config) (string, error) {
	if c.LLMBaseURL == "" {
		return "", errors.New("falta el endpoint")
	}
	cab := map[string]string{}
	if c.LLMAPIKey != "" {
		cab["Authorization"] = "Bearer " + c.LLMAPIKey
	}
	var b struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	st, err := s.pedirJSON(ctx, http.MethodGet, strings.TrimRight(c.LLMBaseURL, "/")+"/models", cab, nil, &b)
	if err != nil {
		return "", fmt.Errorf("el endpoint %v", err)
	}
	if st == 401 || st == 403 {
		return "", fmt.Errorf("el endpoint no acepta la clave (%d)", st)
	}
	if st != 200 {
		return "", fmt.Errorf("%s/models respondió %d: ¿es compatible con OpenAI? (Ollama termina en /v1)", c.LLMBaseURL, st)
	}
	var ids []string
	for _, m := range b.Data {
		ids = append(ids, m.ID)
	}
	if c.LLMModelo != "" && !slices.Contains(ids, c.LLMModelo) {
		return "", fmt.Errorf("el endpoint responde, pero no tiene el modelo %q (tiene: %s). En Ollama: ollama pull %s", c.LLMModelo, strings.Join(primeros(ids, 8), ", "), c.LLMModelo)
	}
	return fmt.Sprintf("El endpoint responde con %d modelo(s) y tiene %s.", len(ids), c.LLMModelo), nil
}

func primeros(l []string, n int) []string {
	if len(l) > n {
		return append(l[:n:n], "...")
	}
	return l
}

func (s *Server) probarR2(ctx context.Context, c *config.Config) (string, error) {
	if c.R2Endpoint == "" || c.R2Bucket == "" || c.R2AccessKey == "" || c.R2SecretKey == "" {
		return "", errors.New("faltan datos: endpoint, bucket, access key y secret key")
	}
	s3, err := almacen.NuevoS3(c.R2Endpoint, c.R2Bucket, c.R2AccessKey, c.R2SecretKey, c.R2Region, c.R2Prefijo)
	if err != nil {
		return "", fmt.Errorf("no se pudo armar el cliente: %v", err)
	}
	clave := "prueba-factorum/" + auth.NuevoToken()[:12]
	if err := s3.Poner(ctx, clave, []byte("prueba de Factorum: se puede borrar")); err != nil {
		return "", fmt.Errorf("no se pudo guardar un objeto de prueba: %v", resumirS3(err))
	}
	if err := s3.Borrar(ctx, clave); err != nil {
		return "", fmt.Errorf("se pudo guardar, pero no borrar el objeto de prueba %s: %v", c.R2Prefijo+clave, resumirS3(err))
	}
	return fmt.Sprintf("Se guardó y se borró un objeto de prueba en %s/%s.", c.R2Bucket, strings.TrimSuffix(c.R2Prefijo, "/")), nil
}

func resumirS3(err error) string {
	m := err.Error()
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}
