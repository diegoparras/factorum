package correo

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ventana es cuánto puede tener de viejo un pedido firmado (contra repeticiones).
const Ventana = 5 * time.Minute

// Errores de los pedidos de entrada.
var (
	ErrFirma    = errors.New("firma inválida")
	ErrVencido  = errors.New("el pedido es viejo o tiene la hora corrida (más de 5 minutos)")
	ErrRepetido = errors.New("el pedido ya se recibió")
)

// CabeceraFirma lleva la firma de Cloudflare: "t=<unix>,v1=<hex>".
const CabeceraFirma = "X-Factorum-Firma"

// CabeceraPara lleva el destinatario del sobre (message.to en el Worker).
const CabeceraPara = "X-Factorum-Para"

// FirmaCloudflare es el HMAC-SHA256 (hex) de "<t>.<para>." seguido del mail crudo.
func FirmaCloudflare(secreto string, t int64, para string, raw []byte) string {
	m := hmac.New(sha256.New, []byte(secreto))
	m.Write([]byte(strconv.FormatInt(t, 10) + "." + para + "."))
	m.Write(raw)
	return hex.EncodeToString(m.Sum(nil))
}

// VerificarCloudflare controla la firma y la hora. Devuelve la firma (para no aceptar el mismo
// pedido dos veces).
func VerificarCloudflare(secreto, cabecera, para string, raw []byte, ahora time.Time) (string, error) {
	if secreto == "" {
		return "", errors.New("falta el secreto compartido")
	}
	var t int64
	var v1 string
	for _, p := range strings.Split(cabecera, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
		switch k {
		case "t":
			t, _ = strconv.ParseInt(v, 10, 64)
		case "v1":
			v1 = strings.ToLower(v)
		}
	}
	if t == 0 || v1 == "" {
		return "", ErrFirma
	}
	esperada := FirmaCloudflare(secreto, t, para, raw)
	if !hmac.Equal([]byte(esperada), []byte(v1)) {
		return "", ErrFirma
	}
	if d := ahora.Sub(time.Unix(t, 0)); d > Ventana || d < -Ventana {
		return "", ErrVencido
	}
	return v1, nil
}

// VerificarMailgun controla la firma de Mailgun: HMAC-SHA256 con la signing key de timestamp+token.
func VerificarMailgun(clave, timestamp, token, firma string, ahora time.Time) error {
	if clave == "" {
		return errors.New("falta la signing key de Mailgun")
	}
	if timestamp == "" || token == "" || firma == "" {
		return ErrFirma
	}
	m := hmac.New(sha256.New, []byte(clave))
	m.Write([]byte(timestamp + token))
	if !hmac.Equal([]byte(hex.EncodeToString(m.Sum(nil))), []byte(strings.ToLower(firma))) {
		return ErrFirma
	}
	t, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return ErrFirma
	}
	if d := ahora.Sub(time.Unix(t, 0)); d > Ventana || d < -Ventana {
		return ErrVencido
	}
	return nil
}

// TokenEnPedido controla el token de Postmark o SendGrid: va como contraseña de la autenticación
// básica de la URL (https://factorum:<token>@...) o en ?token=.
func TokenEnPedido(r *http.Request, token string) bool {
	if token == "" {
		return false
	}
	recibido := r.URL.Query().Get("token")
	if _, p, ok := r.BasicAuth(); ok {
		recibido = p
	}
	if recibido == "" {
		return false
	}
	a, b := sha256.Sum256([]byte(recibido)), sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// Repetidos recuerda las firmas o tokens de los últimos minutos (para no aceptar dos veces el mismo
// pedido firmado). Es en memoria: alcanza porque la ventana es corta y además se deduplica por
// Message-ID en la base.
type Repetidos struct {
	mu sync.Mutex
	m  map[string]time.Time
}

// Visto anota la clave y dice si ya estaba.
func (r *Repetidos) Visto(clave string, ahora time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[string]time.Time{}
	}
	for k, t := range r.m {
		if ahora.Sub(t) > 2*Ventana {
			delete(r.m, k)
		}
	}
	if _, ok := r.m[clave]; ok {
		return true
	}
	r.m[clave] = ahora
	return false
}

// ---------- Formatos de los proveedores ----------

// Partes es un mail que un proveedor ya desarmó.
type Partes struct {
	MessageID   string
	De          string // "Nombre <a@b>" o a@b
	Para        []string
	Sobre       string // destinatario del sobre
	Asunto      string
	Fecha       string
	Referencias string
	Adjuntos    []Adjunto
}

// DesdePartes arma el mensaje con las mismas reglas que Leer (solo adjuntos útiles).
func DesdePartes(p Partes, lim Limites) (*Mensaje, error) {
	if lim.MaxAdjunto <= 0 {
		lim.MaxAdjunto = adjuntoPorDefecto
	}
	if lim.MaxMail <= 0 {
		lim.MaxMail = limitePorDefecto
	}
	m := &Mensaje{Asunto: recortar(decodificarPalabras(p.Asunto), maxAsunto), Referencias: recortar(p.Referencias, 2000)}
	if a, err := mail.ParseAddress(p.De); err == nil {
		m.De, m.NombreDe = strings.ToLower(a.Address), recortar(a.Name, 120)
	} else {
		m.De = strings.ToLower(extraerEmail(p.De))
	}
	for _, d := range p.Para {
		if l, err := mail.ParseAddressList(d); err == nil {
			for _, a := range l {
				m.agregarPara(a.Address)
			}
		} else {
			m.agregarPara(extraerEmail(d))
		}
	}
	if p.Sobre != "" {
		m.AgregarDestinatario(p.Sobre)
	}
	if f, err := mail.ParseDate(p.Fecha); err == nil {
		m.Fecha = f
	}
	total := 0
	for _, a := range p.Adjuntos {
		total += len(a.Datos)
		nombre := limpiarNombre(decodificarPalabras(a.Nombre))
		if int64(len(a.Datos)) > lim.MaxAdjunto {
			m.Descartados = append(m.Descartados, nombreONo(nombre, a.Mime)+" (muy grande)")
			continue
		}
		clase := Clase(a.Datos)
		if clase == "" {
			if nombre != "" {
				m.Descartados = append(m.Descartados, nombre)
			}
			continue
		}
		if strings.HasPrefix(clase, "image/") && len(a.Datos) < minImagen {
			continue
		}
		if nombre == "" {
			nombre = "adjunto" + extension(clase)
		}
		m.Adjuntos = append(m.Adjuntos, Adjunto{Nombre: nombre, Mime: clase, Datos: a.Datos})
	}
	if int64(total) > lim.MaxMail {
		return nil, fmt.Errorf("%w (%d MB, el tope es %d MB)", ErrMuyGrande, total>>20, lim.MaxMail>>20)
	}
	m.Tamano = total
	m.MessageID = strings.TrimSpace(p.MessageID)
	if m.MessageID == "" {
		h := sha256.New()
		fmt.Fprintf(h, "%s|%s|%s|%s", m.De, strings.Join(m.Para, ","), m.Asunto, p.Fecha)
		for _, a := range p.Adjuntos {
			h.Write(a.Datos)
		}
		m.MessageID = "<" + hex.EncodeToString(h.Sum(nil)[:16]) + "@sin-message-id.factorum>"
	} else if !strings.HasPrefix(m.MessageID, "<") {
		m.MessageID = "<" + strings.Trim(m.MessageID, "<>") + ">"
	}
	return m, nil
}

func decodificarPalabras(s string) string {
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// MaxCuerpo es el tope del cuerpo HTTP de un proveedor: el mail en base64 y algo de margen.
func MaxCuerpo(lim Limites) int64 {
	if lim.MaxMail <= 0 {
		lim.MaxMail = limitePorDefecto
	}
	return lim.MaxMail*4/3 + 2<<20
}

func leerArchivosForm(r *http.Request, prefijos ...string) ([]Adjunto, error) {
	var res []Adjunto
	if r.MultipartForm == nil {
		return nil, nil
	}
	for campo, hs := range r.MultipartForm.File {
		ok := false
		for _, p := range prefijos {
			ok = ok || strings.HasPrefix(campo, p)
		}
		if !ok {
			continue
		}
		for _, h := range hs {
			f, err := h.Open()
			if err != nil {
				return nil, err
			}
			b, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				return nil, err
			}
			res = append(res, Adjunto{Nombre: h.Filename, Mime: h.Header.Get("Content-Type"), Datos: b})
		}
	}
	return res, nil
}

// Mailgun lee el pedido de una ruta de Mailgun («forward» a la URL). Si la URL termina en /mime
// o el pedido trae body-mime, toma el mail crudo; si no, los campos ya desarmados. Devuelve el
// mensaje y el token (para no aceptarlo dos veces). La firma se controla acá.
func Mailgun(r *http.Request, clave string, lim Limites, ahora time.Time) (*Mensaje, string, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		return nil, "", fmt.Errorf("formulario inválido: %w", err)
	}
	if r.MultipartForm == nil {
		if err := r.ParseForm(); err != nil {
			return nil, "", fmt.Errorf("formulario inválido: %w", err)
		}
	}
	token := r.FormValue("token")
	if err := VerificarMailgun(clave, r.FormValue("timestamp"), token, r.FormValue("signature"), ahora); err != nil {
		return nil, "", err
	}
	if raw := r.FormValue("body-mime"); raw != "" {
		m, err := Leer([]byte(raw), lim)
		if err != nil {
			return nil, token, err
		}
		if rc := r.FormValue("recipient"); rc != "" {
			m.AgregarDestinatario(rc)
		}
		return m, token, nil
	}
	p := Partes{De: r.FormValue("from"), Asunto: r.FormValue("subject"), Sobre: r.FormValue("recipient")}
	if p.De == "" {
		p.De = r.FormValue("sender")
	}
	var cabs [][]string
	if json.Unmarshal([]byte(r.FormValue("message-headers")), &cabs) == nil {
		for _, c := range cabs {
			if len(c) != 2 {
				continue
			}
			switch strings.ToLower(c[0]) {
			case "message-id":
				p.MessageID = c[1]
			case "date":
				p.Fecha = c[1]
			case "references":
				p.Referencias = c[1]
			case "to", "cc":
				p.Para = append(p.Para, c[1])
			}
		}
	}
	if p.MessageID == "" {
		p.MessageID = r.FormValue("Message-Id")
	}
	if v := r.FormValue("To"); v != "" {
		p.Para = append(p.Para, v)
	}
	var err error
	if p.Adjuntos, err = leerArchivosForm(r, "attachment"); err != nil {
		return nil, token, err
	}
	m, err := DesdePartes(p, lim)
	return m, token, err
}

// postmarkPedido es el JSON del webhook de entrada de Postmark.
type postmarkPedido struct {
	From              string `json:"From"`
	FromFull          struct{ Email, Name string }
	To                string `json:"To"`
	ToFull            []struct{ Email string }
	CcFull            []struct{ Email string }
	OriginalRecipient string `json:"OriginalRecipient"`
	Subject           string `json:"Subject"`
	MessageID         string `json:"MessageID"`
	Date              string `json:"Date"`
	Headers           []struct{ Name, Value string }
	RawEmail          string `json:"RawEmail"`
	Attachments       []struct {
		Name, Content, ContentType string
	}
}

// Postmark lee el webhook de entrada de Postmark (JSON con los adjuntos en base64). El token lo
// controla quien llama (TokenEnPedido).
func Postmark(r *http.Request, lim Limites) (*Mensaje, error) {
	var b postmarkPedido
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		return nil, errors.New("el JSON de Postmark no se entiende")
	}
	if b.RawEmail != "" {
		m, err := Leer([]byte(b.RawEmail), lim)
		if err != nil {
			return nil, err
		}
		m.AgregarDestinatario(b.OriginalRecipient)
		return m, nil
	}
	p := Partes{Asunto: b.Subject, Fecha: b.Date, Sobre: b.OriginalRecipient}
	p.De = b.FromFull.Email
	if b.FromFull.Name != "" {
		p.De = (&mail.Address{Name: b.FromFull.Name, Address: b.FromFull.Email}).String()
	}
	if p.De == "" {
		p.De = b.From
	}
	for _, t := range b.ToFull {
		p.Para = append(p.Para, t.Email)
	}
	for _, t := range b.CcFull {
		p.Para = append(p.Para, t.Email)
	}
	if len(p.Para) == 0 && b.To != "" {
		p.Para = append(p.Para, b.To)
	}
	for _, h := range b.Headers {
		switch strings.ToLower(h.Name) {
		case "message-id":
			p.MessageID = h.Value
		case "references":
			p.Referencias = h.Value
		}
	}
	if p.MessageID == "" && b.MessageID != "" {
		p.MessageID = "<" + b.MessageID + "@postmark>"
	}
	for _, a := range b.Attachments {
		d, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return nil, fmt.Errorf("el adjunto %q no viene bien en base64", a.Name)
		}
		p.Adjuntos = append(p.Adjuntos, Adjunto{Nombre: a.Name, Mime: a.ContentType, Datos: d})
	}
	return DesdePartes(p, lim)
}

// SendGrid lee el pedido de Inbound Parse: con «Send raw» trae el mail entero en el campo email;
// si no, los campos desarmados y los archivos attachment1..N. El token lo controla quien llama.
func SendGrid(r *http.Request, lim Limites) (*Mensaje, error) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		return nil, fmt.Errorf("formulario inválido: %w", err)
	}
	var env struct {
		To   []string `json:"to"`
		From string   `json:"from"`
	}
	_ = json.Unmarshal([]byte(r.FormValue("envelope")), &env)
	if raw := r.FormValue("email"); raw != "" {
		m, err := Leer([]byte(raw), lim)
		if err != nil {
			return nil, err
		}
		for i := len(env.To) - 1; i >= 0; i-- {
			m.AgregarDestinatario(env.To[i])
		}
		return m, nil
	}
	p := Partes{De: r.FormValue("from"), Asunto: r.FormValue("subject")}
	if v := r.FormValue("to"); v != "" {
		p.Para = append(p.Para, v)
	}
	if v := r.FormValue("cc"); v != "" {
		p.Para = append(p.Para, v)
	}
	if len(env.To) > 0 {
		p.Sobre = env.To[0]
	}
	if hs := r.FormValue("headers"); hs != "" {
		if mm, err := mail.ReadMessage(bytes.NewReader([]byte(strings.TrimRight(hs, "\r\n") + "\r\n\r\n"))); err == nil {
			p.MessageID = mm.Header.Get("Message-Id")
			p.Fecha = mm.Header.Get("Date")
			p.Referencias = mm.Header.Get("References")
		}
	}
	var info map[string]struct {
		Filename string `json:"filename"`
		Type     string `json:"type"`
	}
	_ = json.Unmarshal([]byte(r.FormValue("attachment-info")), &info)
	adj, err := leerArchivosForm(r, "attachment")
	if err != nil {
		return nil, err
	}
	for i := range adj {
		for k, v := range info {
			if v.Filename != "" && (k == adj[i].Nombre || v.Filename == adj[i].Nombre) {
				adj[i].Nombre = v.Filename
			}
		}
	}
	p.Adjuntos = adj
	return DesdePartes(p, lim)
}
