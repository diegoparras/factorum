package correo

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// Modos de TLS (para SMTP e IMAP).
const (
	TLSStartTLS  = "starttls"
	TLSImplicito = "implicito"
	TLSNinguno   = "ninguno"
)

// SMTP es el correo saliente.
type SMTP struct {
	Host       string
	Puerto     int
	TLS        string // starttls (por defecto), implicito o ninguno
	Usuario    string
	Clave      string
	Remitente  string // "Factorum <facturas@estudio.com.ar>"
	ResponderA string
	Timeout    time.Duration
	TLSConfig  *tls.Config // para las pruebas
}

// Configurado dice si hay con qué mandar.
func (s SMTP) Configurado() bool { return s.Host != "" && s.Remitente != "" }

// Mail es lo que se manda.
type Mail struct {
	Para         []string
	Asunto       string
	Texto        string
	HTML         string
	EnRespuestaA string // Message-ID del mail al que se contesta
	Referencias  string // References del mail original
}

// ErrSinSMTP: no está configurado el correo saliente.
var ErrSinSMTP = errors.New("el correo saliente no está configurado (Configuración, Correo saliente)")

func (s SMTP) puerto() int {
	if s.Puerto > 0 {
		return s.Puerto
	}
	if s.TLS == TLSImplicito {
		return 465
	}
	return 587
}

func (s SMTP) tlsConfig() *tls.Config {
	if s.TLSConfig != nil {
		c := s.TLSConfig.Clone()
		if c.ServerName == "" {
			c.ServerName = s.Host
		}
		return c
	}
	return &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}
}

func (s SMTP) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return 30 * time.Second
}

// Conexion es una conexión abierta y autenticada con el servidor SMTP.
type Conexion struct {
	*smtp.Client
	parar func() bool
}

// Cerrar corta la conexión.
func (c *Conexion) Cerrar() {
	c.parar()
	_ = c.Client.Close()
}

// Conectar abre la conexión, la asegura y se autentica (lo usan Enviar y la prueba). Si ctx se
// cancela o vence, la conexión se corta: un servidor que no contesta no deja nada colgado.
func (s SMTP) Conectar(ctx context.Context) (*Conexion, error) {
	if !s.Configurado() {
		return nil, ErrSinSMTP
	}
	if _, err := mail.ParseAddress(s.Remitente); err != nil {
		return nil, errRemitente(s.Remitente)
	}
	to := s.timeout()
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.puerto()))
	dctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	d := &net.Dialer{}
	var conn net.Conn
	var err error
	if s.TLS == TLSImplicito {
		conn, err = (&tls.Dialer{NetDialer: d, Config: s.tlsConfig()}).DialContext(dctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(dctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("no se pudo conectar a %s: %s", addr, ErrorDeRed(err))
	}
	parar := context.AfterFunc(ctx, func() { conn.Close() })
	falla := func(err error) (*Conexion, error) {
		parar()
		conn.Close()
		return nil, err
	}
	var c *smtp.Client
	if s.TLS == TLSStartTLS || s.TLS == "" {
		c, err = smtp.NewClientStartTLS(conn, s.tlsConfig())
		if err != nil {
			return falla(fmt.Errorf("no se pudo asegurar la conexión con STARTTLS en %s: %s. Si el puerto es 465, elegí TLS implícito", addr, resumirRed(err)))
		}
	} else {
		c = smtp.NewClient(conn)
		if err := c.Hello(nombreLocal()); err != nil {
			return falla(fmt.Errorf("el servidor %s no saludó bien: %s", addr, resumirRed(err)))
		}
	}
	c.CommandTimeout, c.SubmissionTimeout = to, 2*to
	if s.Usuario != "" {
		var a sasl.Client
		switch {
		case c.SupportsAuth("PLAIN"):
			a = sasl.NewPlainClient("", s.Usuario, ClaveAplicacion(s.Clave))
		case c.SupportsAuth("LOGIN"):
			a = sasl.NewLoginClient(s.Usuario, ClaveAplicacion(s.Clave))
		default:
			return falla(errors.New("el servidor no ofrece entrar con usuario y contraseña (AUTH PLAIN o LOGIN)"))
		}
		if err := c.Auth(a); err != nil {
			return falla(fmt.Errorf("el servidor no aceptó el usuario y la contraseña: %s", resumir(err)))
		}
	}
	return &Conexion{Client: c, parar: parar}, nil
}

// Enviar manda un mail. Devuelve el Message-ID que le puso.
func (s SMTP) Enviar(ctx context.Context, m Mail) (string, error) {
	if len(m.Para) == 0 {
		return "", errors.New("falta el destinatario")
	}
	de, err := mail.ParseAddress(s.Remitente)
	if err != nil {
		return "", errRemitente(s.Remitente)
	}
	var para []string
	for _, p := range m.Para {
		a, err := mail.ParseAddress(p)
		if err != nil {
			return "", fmt.Errorf("%q no es una dirección válida", p)
		}
		para = append(para, a.Address)
	}
	id, cuerpo, err := s.Componer(m, time.Now())
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*s.timeout())
	defer cancel()
	c, err := s.Conectar(ctx)
	if err != nil {
		return "", err
	}
	defer c.Cerrar()
	if err := c.SendMail(de.Address, para, bytes.NewReader(cuerpo)); err != nil {
		if ctx.Err() != nil {
			return "", errors.New("el servidor no contestó a tiempo")
		}
		return "", fmt.Errorf("el servidor no aceptó el mail: %s", resumir(err))
	}
	_ = c.Quit()
	return id, nil
}

// Componer arma el mail (texto plano y HTML) con sus cabeceras.
func (s SMTP) Componer(m Mail, ahora time.Time) (string, []byte, error) {
	de, err := mail.ParseAddress(s.Remitente)
	if err != nil {
		return "", nil, errRemitente(s.Remitente)
	}
	dom := dominio(de.Address)
	if dom == "" {
		dom = "factorum.local"
	}
	id := "<" + aleatorio() + "@" + dom + ">"
	var b bytes.Buffer
	cab := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	cab("From", direccion(de))
	var para []string
	for _, p := range m.Para {
		a, err := mail.ParseAddress(p)
		if err != nil {
			return "", nil, fmt.Errorf("%q no es una dirección válida", p)
		}
		para = append(para, direccion(a))
	}
	cab("To", strings.Join(para, ", "))
	if s.ResponderA != "" {
		if a, err := mail.ParseAddress(s.ResponderA); err == nil {
			cab("Reply-To", direccion(a))
		}
	}
	cab("Subject", mime.QEncoding.Encode("utf-8", limpiarCabecera(m.Asunto)))
	cab("Date", ahora.Format(time.RFC1123Z))
	cab("Message-ID", id)
	if r := limpiarCabecera(m.EnRespuestaA); r != "" {
		cab("In-Reply-To", r)
		refs := strings.TrimSpace(limpiarCabecera(m.Referencias))
		if !strings.Contains(refs, r) {
			refs = strings.TrimSpace(refs + " " + r)
		}
		cab("References", refs)
	}
	cab("MIME-Version", "1.0")
	cab("Auto-Submitted", "auto-generated") // que los contestadores automáticos no respondan
	cab("X-Auto-Response-Suppress", "All")
	mw := multipart.NewWriter(&b)
	cab("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	b.WriteString("\r\n")
	parte := func(tipo, cuerpo string) error {
		w, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {tipo + "; charset=utf-8"}, "Content-Transfer-Encoding": {"quoted-printable"}})
		if err != nil {
			return err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(cuerpo, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
			return err
		}
		return qp.Close()
	}
	if err := parte("text/plain", m.Texto); err != nil {
		return "", nil, err
	}
	if m.HTML != "" {
		if err := parte("text/html", m.HTML); err != nil {
			return "", nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return "", nil, err
	}
	return id, b.Bytes(), nil
}

func direccion(a *mail.Address) string {
	if a.Name == "" {
		return a.Address
	}
	return (&mail.Address{Name: a.Name, Address: a.Address}).String()
}

func limpiarCabecera(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
}

func aleatorio() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nombreLocal() string {
	return "factorum.local"
}

// ErrorDeRed resume un error de conexión sin datos sensibles.
func ErrorDeRed(err error) string {
	m := err.Error()
	switch {
	case strings.Contains(m, "no such host"):
		return "no se encuentra el host"
	case strings.Contains(m, "connection refused"):
		return "conexión rechazada"
	case strings.Contains(m, "deadline exceeded"), strings.Contains(m, "timeout"), strings.Contains(m, "Timeout"):
		return "no contestó a tiempo"
	case strings.Contains(m, "certificate"), strings.Contains(m, "x509"):
		return "problema con el certificado TLS"
	case strings.Contains(m, "first record does not look like a TLS handshake"), strings.Contains(m, "tls:"):
		return "el servidor no habla TLS en ese puerto (probá con STARTTLS o con otro puerto)"
	}
	return "error de red"
}

// resumirRed: un error de red o de TLS en palabras; uno del servidor, recortado.
func resumirRed(err error) string {
	var se *smtp.SMTPError
	if errors.As(err, &se) {
		return resumir(err)
	}
	return ErrorDeRed(err)
}

// resumir recorta un error del servidor (puede traer texto largo); nunca lleva la contraseña.
func resumir(err error) string {
	m := err.Error()
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}
