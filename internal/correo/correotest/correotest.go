// Package correotest tiene servidores de correo de prueba en memoria: un SMTP que guarda lo que
// recibe y una casilla IMAP. Los usan las pruebas (no hace falta GreenMail ni Docker).
package correotest

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

// TB es lo que se necesita de *testing.T (también sirve un programa con su propio Cleanup).
type TB interface {
	Helper()
	Fatal(args ...any)
	Cleanup(func())
}

var (
	certUna          sync.Once
	certSrv, certCli *tls.Config
	certErr          error
)

// Certificado es un certificado autofirmado para 127.0.0.1 y localhost, con la configuración del
// servidor y la del cliente (que confía en él). Es el mismo para todos los servidores de prueba:
// así un solo TLSClient sirve para el SMTP y el IMAP.
func Certificado() (servidor, cliente *tls.Config, err error) {
	certUna.Do(func() { certSrv, certCli, certErr = nuevoCertificado() })
	if certErr != nil {
		return nil, nil, certErr
	}
	return certSrv.Clone(), certCli.Clone(), nil
}

func nuevoCertificado() (servidor, cliente *tls.Config, err error) {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "factorum-prueba"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		return nil, nil, err
	}
	cert, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	servidor = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: k}}, MinVersion: tls.VersionTLS12}
	cliente = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return servidor, cliente, nil
}

// ---------- SMTP ----------

// Recibido es un mail que llegó al SMTP de prueba.
type Recibido struct {
	De   string
	Para []string
	Raw  []byte
}

// SMTP es un servidor SMTP de prueba con STARTTLS y AUTH PLAIN.
type SMTP struct {
	Host      string
	Puerto    int
	Usuario   string
	Clave     string
	TLSClient *tls.Config // confía en el certificado del servidor

	mu        sync.Mutex
	recibidos []Recibido
	nuevo     chan struct{}
	srv       *smtp.Server
}

// NuevoSMTP levanta el servidor en un puerto libre de 127.0.0.1. Con usuario vacío no pide AUTH.
func NuevoSMTP(t TB, usuario, clave string) *SMTP {
	t.Helper()
	tlsSrv, tlsCli, err := Certificado()
	if err != nil {
		t.Fatal(err)
	}
	s := &SMTP{Usuario: usuario, Clave: clave, TLSClient: tlsCli, nuevo: make(chan struct{}, 100)}
	srv := smtp.NewServer(s)
	srv.Domain = "localhost"
	srv.AllowInsecureAuth = true
	srv.TLSConfig = tlsSrv
	srv.ReadTimeout, srv.WriteTimeout = 10*time.Second, 10*time.Second
	srv.MaxMessageBytes = 40 << 20
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.srv = srv
	s.Host = "127.0.0.1"
	s.Puerto = ln.Addr().(*net.TCPAddr).Port
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return s
}

// Direccion es host:puerto.
func (s *SMTP) Direccion() string { return net.JoinHostPort(s.Host, strconv.Itoa(s.Puerto)) }

// Recibidos devuelve una copia de lo que llegó.
func (s *SMTP) Recibidos() []Recibido {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Recibido(nil), s.recibidos...)
}

// Esperar espera a que haya al menos n mails.
func (s *SMTP) Esperar(n int, max time.Duration) []Recibido {
	fin := time.After(max)
	for {
		if r := s.Recibidos(); len(r) >= n {
			return r
		}
		select {
		case <-s.nuevo:
		case <-fin:
			return s.Recibidos()
		}
	}
}

// NewSession implementa smtp.Backend.
func (s *SMTP) NewSession(*smtp.Conn) (smtp.Session, error) { return &sesionSMTP{s: s}, nil }

type sesionSMTP struct {
	s    *SMTP
	auth bool
	de   string
	para []string
}

func (x *sesionSMTP) AuthMechanisms() []string { return []string{sasl.Plain} }
func (x *sesionSMTP) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, u, p string) error {
		if u != x.s.Usuario || p != x.s.Clave {
			return errors.New("usuario o contraseña incorrectos")
		}
		x.auth = true
		return nil
	}), nil
}
func (x *sesionSMTP) Mail(from string, _ *smtp.MailOptions) error {
	if x.s.Usuario != "" && !x.auth {
		return smtp.ErrAuthRequired
	}
	x.de = from
	return nil
}
func (x *sesionSMTP) Rcpt(to string, _ *smtp.RcptOptions) error {
	x.para = append(x.para, to)
	return nil
}
func (x *sesionSMTP) Data(r io.Reader) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	x.s.mu.Lock()
	x.s.recibidos = append(x.s.recibidos, Recibido{De: x.de, Para: append([]string(nil), x.para...), Raw: b})
	x.s.mu.Unlock()
	select {
	case x.s.nuevo <- struct{}{}:
	default:
	}
	return nil
}
func (x *sesionSMTP) Reset()        { x.de, x.para = "", nil }
func (x *sesionSMTP) Logout() error { return nil }

// ---------- IMAP ----------

// IMAP es una casilla IMAP de prueba en memoria, con TLS implícito.
type IMAP struct {
	Host      string
	Puerto    int
	Usuario   string
	Clave     string
	TLSClient *tls.Config

	user *imapmemserver.User
}

// NuevoIMAP levanta la casilla (con INBOX) en un puerto libre de 127.0.0.1. Con tlsImplicito, la
// conexión es TLS desde el principio (como el 993); si no, en claro con STARTTLS disponible.
func NuevoIMAP(t TB, usuario, clave string, tlsImplicito bool) *IMAP {
	t.Helper()
	tlsSrv, tlsCli, err := Certificado()
	if err != nil {
		t.Fatal(err)
	}
	mem := imapmemserver.New()
	u := imapmemserver.NewUser(usuario, clave)
	if err := u.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	mem.AddUser(u)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		TLSConfig:    tlsSrv,
		Logger:       nadaLog{},
	})
	var ln net.Listener
	if tlsImplicito {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsSrv)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return &IMAP{Host: "127.0.0.1", Puerto: ln.Addr().(*net.TCPAddr).Port, Usuario: usuario, Clave: clave, TLSClient: tlsCli, user: u}
}

// Agregar pone un mail en una carpeta (sin leer).
func (s *IMAP) Agregar(carpeta string, raw []byte) error {
	_, err := s.user.Append(carpeta, bytes.NewReader(raw), &imap.AppendOptions{Time: time.Now()})
	return err
}

// Contar dice cuántos mails hay en una carpeta y cuántos sin leer.
func (s *IMAP) Contar(carpeta string) (total, sinLeer int) {
	st, err := s.user.Status(carpeta, &imap.StatusOptions{NumMessages: true, NumUnseen: true})
	if err != nil {
		return -1, -1
	}
	if st.NumMessages != nil {
		total = int(*st.NumMessages)
	}
	if st.NumUnseen != nil {
		sinLeer = int(*st.NumUnseen)
	}
	return total, sinLeer
}

type nadaLog struct{}

func (nadaLog) Printf(string, ...any) {}

// ---------- Mails de prueba ----------

// Archivo para adjuntar a un mail de prueba.
type Archivo struct {
	Nombre, Mime string
	Datos        []byte
	Embebido     bool // dentro de multipart/related con Content-ID (como el logo de una firma)
}

// Mail de prueba.
type Mail struct {
	De, Para, Asunto, MessageID, Texto string
	Cabeceras                          map[string]string
	Archivos                           []Archivo
	Adjuntado                          []byte // un .eml adjunto (message/rfc822)
}

// Armar compone el mail crudo (RFC 822).
func Armar(m Mail) []byte {
	var b bytes.Buffer
	w := func(f string, a ...any) { b.WriteString(sprintf(f, a...)) }
	w("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n", m.De, m.Para, m.Asunto, time.Now().Format(time.RFC1123Z))
	if m.MessageID != "" {
		w("Message-ID: %s\r\n", m.MessageID)
	}
	for k, v := range m.Cabeceras {
		w("%s: %s\r\n", k, v)
	}
	const borde = "=_borde_mixto"
	const rel = "=_borde_rel"
	w("Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", borde)
	w("--%s\r\nContent-Type: multipart/related; boundary=\"%s\"\r\n\r\n", borde, rel)
	w("--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s\r\n", rel, m.Texto)
	for _, a := range m.Archivos {
		if a.Embebido {
			w("--%s\r\nContent-Type: %s; name=\"%s\"\r\nContent-ID: <%s>\r\nContent-Disposition: inline; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", rel, a.Mime, a.Nombre, a.Nombre, a.Nombre, b64(a.Datos))
		}
	}
	w("--%s--\r\n", rel)
	for _, a := range m.Archivos {
		if !a.Embebido {
			w("--%s\r\nContent-Type: %s; name=\"%s\"\r\nContent-Disposition: attachment; filename=\"%s\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", borde, a.Mime, a.Nombre, a.Nombre, b64(a.Datos))
		}
	}
	if m.Adjuntado != nil {
		w("--%s\r\nContent-Type: message/rfc822\r\nContent-Disposition: attachment; filename=\"reenviado.eml\"\r\n\r\n", borde)
		b.Write(m.Adjuntado)
		w("\r\n")
	}
	w("--%s--\r\n", borde)
	return b.Bytes()
}

func b64(d []byte) string {
	s := base64.StdEncoding.EncodeToString(d)
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

var sprintf = fmt.Sprintf
