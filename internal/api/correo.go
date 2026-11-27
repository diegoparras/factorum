package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/factorum/internal/ajustes"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/store"
)

// EstadoCorreo es cómo viene el correo entrante (lo da el gestor que revisa IMAP o Gmail).
type EstadoCorreo struct {
	Forma      string     `json:"forma"`
	Activo     bool       `json:"activo"`
	Detalle    string     `json:"detalle,omitempty"`
	Ultima     *time.Time `json:"ultima,omitempty"` // última vuelta
	Error      string     `json:"error,omitempty"`
	Recibidos  int        `json:"recibidos"` // desde que arrancó
	Retenidos  int        `json:"retenidos"`
	UltimoMail *time.Time `json:"ultimo_mail,omitempty"`
}

// rutas del correo entrante.
const (
	rutaCloudflare = "/api/correo/entrante/cloudflare"
	rutaProveedor  = "/api/correo/entrante/"
	rutaCallback   = "/api/correo/google/callback"
)

// urlsCorreo son las direcciones que hay que configurar en cada servicio (sin tokens).
func urlsCorreo(c *config.Config) map[string]string {
	if c.URLPublica == "" {
		return nil
	}
	u := strings.TrimRight(c.URLPublica, "/")
	return map[string]string{
		"cloudflare": u + rutaCloudflare, "mailgun": u + rutaProveedor + "mailgun", "postmark": u + rutaProveedor + "postmark",
		"sendgrid": u + rutaProveedor + "sendgrid", "google_redirect": u + rutaCallback,
	}
}

func (s *Server) recibir(w http.ResponseWriter, r *http.Request, m *correo.Mensaje, forma string) {
	res, err := s.App.RecibirCorreo(r.Context(), m, forma)
	if err != nil {
		s.Log.Error("no se pudo recibir un mail", "forma", forma, "error", err)
		jsonErr(w, http.StatusServiceUnavailable, "Factorum no pudo guardar el mail: probá de nuevo en un rato")
		return
	}
	s.correoRecibido(res)
	out := map[string]any{"estado": res.Estado, "motivo": res.Motivo}
	if res.Lote != nil {
		out["lote"] = res.Lote.ID
	}
	jsonOK(w, http.StatusOK, out)
}

// POST /api/correo/entrante/cloudflare: el mail crudo, firmado por el Email Worker.
func (s *Server) entranteCloudflare(w http.ResponseWriter, r *http.Request) {
	cfg := s.App.Config()
	if cfg.FormaCorreo() != "cloudflare" {
		jsonErr(w, http.StatusNotFound, "El correo por Cloudflare no está activo (Configuración, Correo entrante)")
		return
	}
	if cfg.CloudflareSecreto == "" {
		jsonErr(w, http.StatusServiceUnavailable, "Falta el secreto compartido de Cloudflare")
		return
	}
	lim := s.App.LimitesCorreo()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, lim.MaxMail))
	if err != nil {
		s.App.CorreoOmitido(r.Context(), "cloudflare", correo.Sobre{}, "el mail supera el tope")
		jsonErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("El mail supera los %d MB", lim.MaxMail>>20))
		return
	}
	para := r.Header.Get(correo.CabeceraPara)
	firma, err := correo.VerificarCloudflare(cfg.CloudflareSecreto, r.Header.Get(correo.CabeceraFirma), para, raw, s.ahora())
	if err != nil {
		s.Log.Warn("pedido de Cloudflare rechazado", "motivo", err.Error())
		jsonErr(w, http.StatusUnauthorized, capital(err.Error()))
		return
	}
	if s.repetidos.Visto("cf:"+firma, s.ahora()) {
		jsonErr(w, http.StatusConflict, "Ese pedido ya se recibió")
		return
	}
	m, err := correo.Leer(raw, lim)
	if err != nil {
		s.App.CorreoOmitido(r.Context(), "cloudflare", correo.Sobre{Tamano: int64(len(raw))}, err.Error())
		jsonErr(w, http.StatusUnprocessableEntity, capital(err.Error()))
		return
	}
	m.AgregarDestinatario(para)
	s.recibir(w, r, m, "cloudflare")
}

// POST /api/correo/entrante/{proveedor}: Mailgun, Postmark o SendGrid, cada uno con su firma o su
// token y su formato. Un mail que no sirve se contesta 200 (si no, el proveedor reintenta días).
func (s *Server) entranteProveedor(w http.ResponseWriter, r *http.Request) {
	prov := r.PathValue("proveedor")
	cfg := s.App.Config()
	if prov != "mailgun" && prov != "postmark" && prov != "sendgrid" {
		jsonErr(w, http.StatusNotFound, "Proveedor desconocido")
		return
	}
	if cfg.FormaCorreo() != "proveedor" || (cfg.CorreoProveedor != prov && !(cfg.CorreoProveedor == "" && prov == "mailgun")) {
		jsonErr(w, http.StatusNotFound, "Ese proveedor de correo no está activo (Configuración, Correo entrante)")
		return
	}
	lim := s.App.LimitesCorreo()
	r.Body = http.MaxBytesReader(w, r.Body, correo.MaxCuerpo(lim))
	var m *correo.Mensaje
	var err error
	switch prov {
	case "mailgun":
		if cfg.MailgunClave == "" {
			jsonErr(w, http.StatusServiceUnavailable, "Falta la signing key de Mailgun")
			return
		}
		var token string
		m, token, err = correo.Mailgun(r, cfg.MailgunClave, lim, s.ahora())
		if errors.Is(err, correo.ErrFirma) || errors.Is(err, correo.ErrVencido) {
			s.Log.Warn("pedido de Mailgun rechazado", "motivo", err.Error())
			jsonErr(w, http.StatusUnauthorized, capital(err.Error()))
			return
		}
		if token != "" && s.repetidos.Visto("mg:"+token, s.ahora()) {
			jsonErr(w, http.StatusNotAcceptable, "Ese pedido ya se recibió")
			return
		}
	default:
		if cfg.ProveedorToken == "" {
			jsonErr(w, http.StatusServiceUnavailable, "Falta el token de la URL")
			return
		}
		if !correo.TokenEnPedido(r, cfg.ProveedorToken) {
			s.Log.Warn("pedido de correo rechazado: token inválido", "proveedor", prov)
			w.Header().Set("WWW-Authenticate", `Basic realm="factorum"`)
			jsonErr(w, http.StatusUnauthorized, "Token inválido")
			return
		}
		if prov == "postmark" {
			m, err = correo.Postmark(r, lim)
		} else {
			m, err = correo.SendGrid(r, lim)
		}
	}
	if err != nil {
		s.App.CorreoOmitido(r.Context(), prov, correo.Sobre{}, err.Error())
		jsonOK(w, http.StatusOK, map[string]any{"estado": "ignorado", "motivo": err.Error()})
		return
	}
	s.recibir(w, r, m, prov)
}

// ---------- Estado del correo entrante ----------

// correoStats cuenta lo que entra por los webhooks (IMAP y Gmail cuentan en el gestor).
type correoStats struct {
	mu        sync.Mutex
	recibidos int
	retenidos int
	ultimo    time.Time
}

func (s *Server) correoRecibido(res *app.ResultadoCorreo) {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	switch res.Estado {
	case app.CorreoProcesado:
		s.stats.recibidos++
	case app.CorreoRetenido:
		s.stats.retenidos++
	default:
		return
	}
	s.stats.ultimo = time.Now()
}

func (s *Server) estadoCorreo() EstadoCorreo {
	var e EstadoCorreo
	if s.EstadoCorreo != nil {
		e = s.EstadoCorreo()
	}
	cfg := s.App.Config()
	e.Forma = cfg.FormaCorreo()
	switch e.Forma {
	case "cloudflare", "proveedor":
		s.stats.mu.Lock()
		e.Recibidos, e.Retenidos = s.stats.recibidos, s.stats.retenidos
		if !s.stats.ultimo.IsZero() {
			t := s.stats.ultimo
			e.UltimoMail = &t
		}
		s.stats.mu.Unlock()
		secreto := cfg.CloudflareSecreto
		if e.Forma == "proveedor" {
			secreto = cfg.ProveedorToken
			if cfg.CorreoProveedor == "mailgun" || cfg.CorreoProveedor == "" {
				secreto = cfg.MailgunClave
			}
		}
		e.Activo = secreto != "" && cfg.URLPublica != ""
		if !e.Activo {
			e.Detalle = "falta la URL pública o el secreto"
		} else {
			e.Detalle = "esperando mails"
		}
	case "apagado":
		e.Activo, e.Detalle = false, "apagado"
	}
	return e
}

// ---------- Correo retenido ----------

// GET /api/correo/retenido (admin): lo que mandaron remitentes no permitidos.
func (s *Server) retenidos(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lim, _ := strconv.Atoi(q.Get("limite"))
	off, _ := strconv.Atoi(q.Get("offset"))
	l, total, err := s.App.Store.CorreosRetenidos(r.Context(), IdentidadDe(r.Context()).Alcance(), lim, max(off, 0))
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"retenidos": l, "total": total, "permitidos": s.App.Config().CorreoPermitidos,
		"permitidos_fijados": s.Ajustes != nil && s.bloqueado("correo.permitidos")})
}

func (s *Server) bloqueado(clave string) bool {
	c, ok := ajustes.CampoPorClave(clave)
	return ok && s.Ajustes.Bloqueado(c)
}

func (s *Server) retenidoVisible(r *http.Request) (*store.MensajeCorreo, error) {
	id, err := idDe(r)
	if err != nil {
		return nil, store.ErrNoEncontrado
	}
	m, err := s.App.Store.Correo(r.Context(), id)
	if err != nil {
		return nil, err
	}
	if !IdentidadDe(r.Context()).Permite(m.ClienteCUIT) {
		return nil, store.ErrNoEncontrado
	}
	return m, nil
}

// POST /api/correo/retenido/{id}/aceptar {permitir}: lo procesa y, si se pide, suma al remitente a
// los permitidos.
func (s *Server) aceptarRetenido(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Permitir bool `json:"permitir"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b)
	m, err := s.retenidoVisible(r)
	if err != nil {
		s.fallo(w, err)
		return
	}
	id := IdentidadDe(r.Context())
	l, err := s.App.AceptarRetenido(r.Context(), m.ID, id.Quien())
	if errors.Is(err, store.ErrNoRetenido) {
		jsonErr(w, http.StatusConflict, "Ese mail ya no está retenido: otra persona lo resolvió o venció")
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	res := map[string]any{"lote": l}
	det := map[string]any{"mensaje": m.ID, "lote": l.ID}
	if b.Permitir {
		if err := s.permitirRemitente(r.Context(), m.Remitente, id.Quien()); err != nil {
			res["aviso"] = "Se procesó, pero no se pudo sumar el remitente a los permitidos: " + err.Error()
		} else {
			res["permitido"] = true
			det["permitido"] = true
		}
	}
	s.auditar(r, "correo.aceptar", m.Remitente, det)
	jsonOK(w, http.StatusOK, res)
}

func (s *Server) permitirRemitente(ctx context.Context, email, quien string) error {
	if s.Ajustes == nil {
		return errors.New("la configuración desde la pantalla no está disponible")
	}
	if s.bloqueado("correo.permitidos") {
		return errors.New("la lista la fija el servidor (FACTORUM_CORREO_PERMITIDOS)")
	}
	lista := append([]string(nil), s.App.Config().CorreoPermitidos...)
	if correo.Permitido(lista, email) {
		return nil
	}
	lista = append(lista, email)
	raw, _ := json.Marshal(lista)
	_, err := s.Ajustes.Guardar(ctx, map[string]json.RawMessage{"correo.permitidos": raw}, quien)
	return err
}

// POST /api/correo/retenido/{id}/descartar
func (s *Server) descartarRetenido(w http.ResponseWriter, r *http.Request) {
	m, err := s.retenidoVisible(r)
	if err != nil {
		s.fallo(w, err)
		return
	}
	if err := s.App.DescartarRetenido(r.Context(), m.ID, IdentidadDe(r.Context()).Quien()); errors.Is(err, store.ErrNoRetenido) {
		jsonErr(w, http.StatusConflict, "Ese mail ya no está retenido: otra persona lo resolvió o venció")
		return
	} else if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "correo.descartar", m.Remitente, map[string]any{"mensaje": m.ID})
	jsonOK(w, http.StatusOK, map[string]any{})
}

// ---------- Conectar con Google ----------

type estadoOAuth struct {
	verificador string
	usuario     int64
	vence       time.Time
}

// POST /api/correo/google/conectar (superadmin): la URL del consentimiento de Google.
func (s *Server) conectarGoogle(w http.ResponseWriter, r *http.Request) {
	cfg := s.App.Config()
	g := s.App.Google()
	switch {
	case cfg.URLPublica == "":
		jsonErr(w, http.StatusConflict, "Falta la URL pública (Configuración, General): Google vuelve a ella")
		return
	case g.ClienteID == "" || g.ClienteSecreto == "":
		jsonErr(w, http.StatusConflict, "Primero guardá el client ID y el client secret de Google")
		return
	}
	ver, des := correo.PKCE()
	estado := auth.NuevoToken()
	s.oauthMu.Lock()
	if s.oauth == nil {
		s.oauth = map[string]estadoOAuth{}
	}
	for k, v := range s.oauth {
		if time.Now().After(v.vence) {
			delete(s.oauth, k)
		}
	}
	s.oauth[auth.HashToken(estado)] = estadoOAuth{verificador: ver, usuario: IdentidadDe(r.Context()).UsuarioID, vence: time.Now().Add(10 * time.Minute)}
	s.oauthMu.Unlock()
	jsonOK(w, http.StatusOK, map[string]any{"url": g.URLConsentimiento(urlsCorreo(cfg)["google_redirect"], estado, des, "")})
}

// GET /api/correo/google/callback: Google vuelve acá con el código. Se canjea por el refresh token,
// que se guarda cifrado, y se vuelve a Configuración.
func (s *Server) callbackGoogle(w http.ResponseWriter, r *http.Request) {
	volver := func(clave, detalle string) {
		q := url.Values{"google": {clave}}
		if detalle != "" {
			q.Set("detalle", detalle)
		}
		http.Redirect(w, r, "/?"+q.Encode()+"#configuracion", http.StatusSeeOther)
	}
	q := r.URL.Query()
	s.oauthMu.Lock()
	est, ok := s.oauth[auth.HashToken(q.Get("state"))]
	delete(s.oauth, auth.HashToken(q.Get("state")))
	s.oauthMu.Unlock()
	if !ok || time.Now().After(est.vence) {
		volver("error", "El pedido a Google venció o no es de acá: tocá «Conectar con Google» de nuevo")
		return
	}
	id, err := s.Identificar(r)
	if err != nil || id.Tipo != auth.TipoSesion || id.Rol < auth.Superadmin || id.UsuarioID != est.usuario {
		volver("error", "Para conectar con Google tenés que tener la sesión abierta como superadmin")
		return
	}
	r = r.WithContext(ConIdentidad(r.Context(), id))
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			volver("error", "No diste el permiso en Google")
		} else {
			volver("error", "Google contestó: "+recortarTexto(e, 80))
		}
		return
	}
	cfg := s.App.Config()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	g := s.App.Google()
	refresh, err := g.Canjear(ctx, q.Get("code"), est.verificador, urlsCorreo(cfg)["google_redirect"])
	if err != nil {
		volver("error", capital(err.Error()))
		return
	}
	gm := &correo.Gmail{Google: g, RefreshToken: refresh}
	cuenta, err := gm.Perfil(ctx)
	if err != nil {
		volver("error", capital(err.Error()))
		return
	}
	rt, _ := json.Marshal(refresh)
	cu, _ := json.Marshal(cuenta)
	if _, err := s.Ajustes.Guardar(ctx, map[string]json.RawMessage{"google.refresh_token": rt, "google.cuenta": cu}, id.Quien()); err != nil {
		volver("error", "No se pudo guardar el permiso: "+err.Error())
		return
	}
	s.auditar(r, "config.google_conectar", cuenta, nil)
	volver("ok", cuenta)
}

// POST /api/correo/google/desconectar (superadmin): revoca el permiso y lo borra.
func (s *Server) desconectarGoogle(w http.ResponseWriter, r *http.Request) {
	cfg := s.App.Config()
	if s.bloqueado("google.refresh_token") {
		jsonErr(w, http.StatusConflict, "El permiso de Google lo fija el servidor (FACTORUM_GOOGLE_REFRESH_TOKEN)")
		return
	}
	aviso := ""
	if cfg.GoogleRefreshToken != "" {
		if err := s.App.Google().Revocar(r.Context(), cfg.GoogleRefreshToken); err != nil {
			aviso = "Se borró el permiso acá, pero Google no confirmó la revocación (" + err.Error() + "). Sacale el acceso desde tu cuenta de Google."
		}
	}
	if _, err := s.Ajustes.Guardar(r.Context(), map[string]json.RawMessage{"google.refresh_token": nil, "google.cuenta": nil}, IdentidadDe(r.Context()).Quien()); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "config.google_desconectar", cfg.GoogleCuenta, nil)
	jsonOK(w, http.StatusOK, map[string]any{"aviso": aviso})
}

func recortarTexto(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// ---------- Pruebas de la configuración ----------

type ctxDestino struct{}

func (s *Server) probarCorreoEntrada(ctx context.Context, c *config.Config) (string, error) {
	aviso := ""
	if len(c.CorreoPermitidos) == 0 {
		aviso = " Ojo: todavía no hay remitentes permitidos, así que todo lo que llegue va a quedar retenido."
	}
	switch c.FormaCorreo() {
	case "apagado":
		return "", errors.New("el correo entrante está apagado")
	case "imap":
		b := correo.IMAP{Host: c.IMAPHost, Puerto: c.IMAPPuerto, TLS: c.IMAPTLS, Usuario: c.IMAPUsuario, Clave: c.IMAPClave, Carpeta: c.IMAPCarpeta,
			TLSConfig: s.App.TLSCorreo, Timeout: 15 * time.Second}
		total, sin, err := b.Contar(ctx)
		if err != nil {
			return "", err
		}
		carpeta := c.IMAPCarpeta
		if carpeta == "" {
			carpeta = "INBOX"
		}
		return fmt.Sprintf("Entró a %s: la carpeta %s tiene %d mensaje(s), %d sin leer.%s", c.IMAPUsuario, carpeta, total, sin, aviso), nil
	case "gmail":
		g := s.App.GoogleURLs
		g.ClienteID, g.ClienteSecreto = c.GoogleClienteID, c.GoogleClienteSecreto
		if g.ClienteID == "" || g.ClienteSecreto == "" {
			return "", errors.New("faltan el client ID y el client secret de Google")
		}
		if c.GoogleRefreshToken == "" {
			return "", errors.New("falta conectar la cuenta: guardá y tocá «Conectar con Google»")
		}
		gm := &correo.Gmail{Google: g, RefreshToken: c.GoogleRefreshToken, Etiqueta: c.GmailEtiqueta}
		email, total, err := gm.Contar(ctx)
		if err != nil {
			return "", err
		}
		et := c.GmailEtiqueta
		if et == "" {
			et = "Facturas"
		}
		return fmt.Sprintf("Conectado a %s: la etiqueta %s tiene %d mensaje(s).%s", email, et, total, aviso), nil
	}
	// Cloudflare y los proveedores: la URL pública tiene que responder y el secreto tiene que estar.
	if c.URLPublica == "" {
		return "", errors.New("falta la URL pública (Configuración, General): es a donde llegan los mails")
	}
	if _, err := s.probarGeneral(ctx, c); err != nil {
		return "", err
	}
	urls := urlsCorreo(c)
	switch c.FormaCorreo() {
	case "cloudflare":
		if c.CloudflareSecreto == "" {
			return "", errors.New("la URL pública responde, pero falta el secreto compartido")
		}
		return "La URL pública responde y el secreto está. En el Worker, FACTORUM_URL es " + urls["cloudflare"] + "." + aviso, nil
	}
	prov := c.CorreoProveedor
	if prov == "" {
		prov = "mailgun"
	}
	if prov == "mailgun" && c.MailgunClave == "" {
		return "", errors.New("la URL pública responde, pero falta la signing key de Mailgun")
	}
	if prov != "mailgun" && c.ProveedorToken == "" {
		return "", errors.New("la URL pública responde, pero falta el token de la URL")
	}
	return "La URL pública responde y la clave está. La URL para el proveedor es " + urls[prov] + "." + aviso, nil
}

func (s *Server) probarCorreoSalida(ctx context.Context, c *config.Config) (string, error) {
	sm := correo.SMTP{Host: c.SMTPHost, Puerto: c.SMTPPuerto, TLS: c.SMTPTLS, Usuario: c.SMTPUsuario, Clave: c.SMTPClave, Remitente: c.SMTPRemitente,
		ResponderA: c.SMTPResponderA, TLSConfig: s.App.TLSCorreo, Timeout: 15 * time.Second}
	if c.SMTPHost == "" {
		return "", errors.New("falta el servidor SMTP")
	}
	if c.SMTPRemitente == "" {
		return "", errors.New("falta el remitente (por ejemplo: Factorum <facturas@estudio.com.ar>)")
	}
	destino, _ := ctx.Value(ctxDestino{}).(string)
	destino = strings.TrimSpace(destino)
	if destino == "" {
		cx, err := sm.Conectar(ctx)
		if err != nil {
			return "", err
		}
		_ = cx.Quit()
		cx.Cerrar()
		return "El servidor acepta la conexión y el usuario. Escribí una dirección para mandar un mail de prueba.", nil
	}
	if _, err := validarEmail(destino); err != nil {
		return "", errors.New("la dirección para la prueba no es válida")
	}
	m := correo.Prueba(correo.Marca{Nombre: c.Nombre, URL: c.URLPublica})
	m.Para = []string{destino}
	if _, err := sm.Enviar(ctx, m); err != nil {
		return "", err
	}
	return "Se mandó un mail de prueba a " + destino + ". Si no llega en un par de minutos, mirá en spam y revisá que el dominio del remitente esté autorizado (SPF y DKIM).", nil
}
