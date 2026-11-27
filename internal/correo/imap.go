package correo

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Qué se hace con un mail ya procesado.
const (
	ProcesadoLeido = "leido" // se marca como leído
	ProcesadoMover = "mover" // se mueve a otra carpeta (por defecto «Factorum»)
)

// IMAP es una casilla que Factorum revisa cada tanto.
type IMAP struct {
	Host             string
	Puerto           int
	TLS              string // implicito (por defecto, 993), starttls o ninguno
	Usuario          string
	Clave            string
	Carpeta          string // INBOX
	Procesado        string // leido o mover
	CarpetaProcesado string // Factorum
	MaxPorVuelta     int    // mails por vuelta (50)
	Timeout          time.Duration
	TLSConfig        *tls.Config // para las pruebas
}

// Configurado dice si hay con qué entrar.
func (b IMAP) Configurado() bool { return b.Host != "" && b.Usuario != "" && b.Clave != "" }

func (b IMAP) carpeta() string {
	if strings.TrimSpace(b.Carpeta) == "" {
		return "INBOX"
	}
	return strings.TrimSpace(b.Carpeta)
}

func (b IMAP) carpetaProcesado() string {
	if strings.TrimSpace(b.CarpetaProcesado) == "" {
		return "Factorum"
	}
	return strings.TrimSpace(b.CarpetaProcesado)
}

func (b IMAP) puerto() int {
	if b.Puerto > 0 {
		return b.Puerto
	}
	if b.TLS == TLSImplicito || b.TLS == "" {
		return 993
	}
	return 143
}

func (b IMAP) timeout() time.Duration {
	if b.Timeout > 0 {
		return b.Timeout
	}
	return 45 * time.Second
}

// conectar entra a la casilla. Si ctx se cancela, la conexión se corta.
func (b IMAP) conectar(ctx context.Context) (*imapclient.Client, func(), error) {
	if !b.Configurado() {
		return nil, nil, errors.New("faltan datos de IMAP: servidor, usuario y contraseña")
	}
	addr := net.JoinHostPort(b.Host, strconv.Itoa(b.puerto()))
	tc := b.TLSConfig
	if tc == nil {
		tc = &tls.Config{ServerName: b.Host, MinVersion: tls.VersionTLS12}
	} else {
		tc = tc.Clone()
		if tc.ServerName == "" {
			tc.ServerName = b.Host
		}
	}
	dctx, cancel := context.WithTimeout(ctx, b.timeout())
	defer cancel()
	var conn net.Conn
	var err error
	d := &net.Dialer{}
	if b.TLS == TLSImplicito || b.TLS == "" {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tc}).DialContext(dctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(dctx, "tcp", addr)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("no se pudo conectar a %s: %s", addr, ErrorDeRed(err))
	}
	opts := &imapclient.Options{TLSConfig: tc}
	var c *imapclient.Client
	if b.TLS == TLSStartTLS {
		c, err = imapclient.NewStartTLS(conn, opts)
		if err != nil {
			conn.Close()
			return nil, nil, fmt.Errorf("no se pudo asegurar la conexión con STARTTLS en %s: %s. Si el puerto es 993, elegí TLS implícito", addr, ErrorDeRed(err))
		}
	} else {
		c = imapclient.New(conn, opts)
	}
	parar := context.AfterFunc(ctx, func() { c.Close() })
	cerrar := func() {
		parar()
		_ = c.Logout().Wait()
		c.Close()
	}
	espera := context.AfterFunc(dctx, func() { c.Close() })
	defer espera()
	if err := c.WaitGreeting(); err != nil {
		cerrar()
		return nil, nil, fmt.Errorf("el servidor %s no saludó bien: %s", addr, ErrorDeRed(err))
	}
	if err := c.Login(b.Usuario, b.Clave).Wait(); err != nil {
		cerrar()
		var ie *imap.Error
		if errors.As(err, &ie) {
			return nil, nil, fmt.Errorf("el servidor no aceptó el usuario y la contraseña (%s). En Gmail y Microsoft 365 hace falta una contraseña de aplicación", recortar(ie.Text, 160))
		}
		return nil, nil, fmt.Errorf("no se pudo entrar: %s", ErrorDeRed(err))
	}
	return c, cerrar, nil
}

// Contar entra y cuenta los mensajes de la carpeta (la prueba de la configuración).
func (b IMAP) Contar(ctx context.Context) (total, sinLeer int, err error) {
	c, cerrar, err := b.conectar(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer cerrar()
	st, err := c.Status(b.carpeta(), &imap.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
	if err != nil {
		return 0, 0, fmt.Errorf("entró, pero no encuentra la carpeta %q: %s", b.carpeta(), errorIMAP(err))
	}
	if st.NumMessages != nil {
		total = int(*st.NumMessages)
	}
	if st.NumUnseen != nil {
		sinLeer = int(*st.NumUnseen)
	}
	return total, sinLeer, nil
}

// Sobre es lo que se sabe de un mail sin bajarlo (para avisar de uno que no se bajó).
type Sobre struct {
	De, Asunto string
	Tamano     int64
}

// Entrega recibe un mail crudo. Si devuelve error, el mail queda sin marcar y se reintenta en la
// próxima vuelta (es para fallas pasajeras: la base que no responde). Lo que no sirve se maneja
// adentro y devuelve nil.
type Entrega func(ctx context.Context, raw []byte) error

// Revisar baja los mails sin leer de la carpeta, los entrega de a uno y, a los que se entregaron
// bien, los marca como leídos o los mueve. Los que superan maxMail no se bajan: se avisan con
// omitido y se marcan igual. Devuelve cuántos entregó.
func (b IMAP) Revisar(ctx context.Context, maxMail int64, entregar Entrega, omitido func(Sobre)) (int, error) {
	c, cerrar, err := b.conectar(ctx)
	if err != nil {
		return 0, err
	}
	defer cerrar()
	if _, err := c.Select(b.carpeta(), nil).Wait(); err != nil {
		return 0, fmt.Errorf("no se pudo abrir la carpeta %q: %s", b.carpeta(), errorIMAP(err))
	}
	if b.Procesado == ProcesadoMover {
		// Si la carpeta no existe se crea (si ya existe, el error no importa).
		_ = c.Create(b.carpetaProcesado(), nil).Wait()
	}
	res, err := c.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen, imap.FlagDeleted}}, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("no se pudo buscar en la carpeta: %s", errorIMAP(err))
	}
	uids := res.AllUIDs()
	max := b.MaxPorVuelta
	if max <= 0 {
		max = 50
	}
	if len(uids) > max {
		uids = uids[:max]
	}
	n := 0
	for _, uid := range uids {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		set := imap.UIDSetNum(uid)
		meta, err := c.Fetch(set, &imap.FetchOptions{UID: true, RFC822Size: true, Envelope: true}).Collect()
		if err != nil {
			return n, fmt.Errorf("no se pudo leer un mail: %s", errorIMAP(err))
		}
		if len(meta) == 0 {
			continue
		}
		if maxMail > 0 && meta[0].RFC822Size > maxMail {
			if omitido != nil {
				s := Sobre{Tamano: meta[0].RFC822Size}
				if env := meta[0].Envelope; env != nil {
					s.Asunto = recortar(env.Subject, maxAsunto)
					if len(env.From) > 0 {
						s.De = strings.ToLower(env.From[0].Addr())
					}
				}
				omitido(s)
			}
			if err := b.marcar(c, set); err != nil {
				return n, err
			}
			continue
		}
		sec := &imap.FetchItemBodySection{Peek: true}
		msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
		if err != nil {
			return n, fmt.Errorf("no se pudo bajar un mail: %s", errorIMAP(err))
		}
		if len(msgs) == 0 {
			continue
		}
		raw := msgs[0].FindBodySection(sec)
		if err := entregar(ctx, raw); err != nil {
			return n, err // queda sin marcar: se reintenta en la próxima vuelta
		}
		n++
		if err := b.marcar(c, set); err != nil {
			return n, err
		}
	}
	return n, nil
}

func (b IMAP) marcar(c *imapclient.Client, set imap.UIDSet) error {
	if b.Procesado == ProcesadoMover {
		if _, err := c.Move(set, b.carpetaProcesado()).Wait(); err != nil {
			return fmt.Errorf("no se pudo mover el mail a %q: %s", b.carpetaProcesado(), errorIMAP(err))
		}
		return nil
	}
	err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close()
	if err != nil {
		return fmt.Errorf("no se pudo marcar el mail como leído: %s", errorIMAP(err))
	}
	return nil
}

func errorIMAP(err error) string {
	var ie *imap.Error
	if errors.As(err, &ie) {
		return recortar(ie.Text, 160)
	}
	return ErrorDeRed(err)
}
