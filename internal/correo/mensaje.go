// Package correo es el correo de Factorum: leer un mail (solo sus adjuntos: el cuerpo es dato de un
// tercero y nunca se interpreta), las reglas de quién puede mandar y para qué cliente, las cuatro
// formas de recibirlo (IMAP, Cloudflare Email Routing, un proveedor y Gmail con OAuth) y el envío
// por SMTP con sus plantillas.
//
// Nada de este paquete loguea cuerpos de mail, contraseñas ni tokens.
package correo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

	_ "github.com/emersion/go-message/charset" // asuntos y nombres en ISO-8859-1, Windows-1252...
	"github.com/emersion/go-message/mail"

	gomessage "github.com/emersion/go-message"
)

// Adjunto de un mail que Factorum puede leer (PDF, JPG, PNG o ZIP).
type Adjunto struct {
	Nombre string `json:"nombre"`
	Mime   string `json:"mime"`
	Datos  []byte `json:"-"`
}

// Tamano en bytes.
func (a Adjunto) Tamano() int { return len(a.Datos) }

// Mensaje es lo que Factorum toma de un mail: quién, a quién, el asunto, la fecha y los adjuntos.
// El cuerpo no se guarda ni se lee.
type Mensaje struct {
	MessageID   string    // con los < >; si el mail no trae, uno armado con el hash del mail
	De          string    // email del remitente (From), en minúsculas
	NombreDe    string    // nombre del remitente, si viene
	Para        []string  // destinatarios en minúsculas: el sobre (si se conoce) y To, Cc, Delivered-To, X-Original-To
	Asunto      string    // decodificado
	Fecha       time.Time // la del mail (Date); cero si no trae
	Referencias string    // References del mail, para contestar en el mismo hilo
	Adjuntos    []Adjunto
	Descartados []string // nombres de adjuntos que no son facturas que se puedan leer
	Tamano      int      // bytes del mail entero
}

// Limites al leer un mail.
type Limites struct {
	MaxMail    int64 // bytes del mail entero (25 MB por defecto)
	MaxAdjunto int64 // bytes por adjunto (FACTORUM_MAX_MB)
}

// ErrMuyGrande: el mail supera el tope.
var ErrMuyGrande = errors.New("el mail es demasiado grande")

const (
	maxPartes     = 300 // partes MIME por mail
	maxProfundo   = 12  // anidamiento de multipart
	maxEMLAnidado = 3   // mails adjuntos dentro de mails adjuntos
	// Las imágenes chiquitas o embebidas en el HTML son logos y firmas, no tickets.
	minImagen          = 8 << 10
	maxImagenEmbebida  = 200 << 10
	limitePorDefecto   = 25 << 20
	adjuntoPorDefecto  = 25 << 20
	maxNombreAdjunto   = 200
	maxAsunto          = 300
	maxDestinatariosOK = 50
)

// Leer interpreta un mail crudo (RFC 822). Solo toma las cabeceras de arriba y los adjuntos; las
// partes de texto se saltean sin leerlas.
func Leer(raw []byte, lim Limites) (*Mensaje, error) {
	if lim.MaxMail <= 0 {
		lim.MaxMail = limitePorDefecto
	}
	if lim.MaxAdjunto <= 0 {
		lim.MaxAdjunto = adjuntoPorDefecto
	}
	if int64(len(raw)) > lim.MaxMail {
		return nil, fmt.Errorf("%w (%d MB, el tope es %d MB)", ErrMuyGrande, len(raw)>>20, lim.MaxMail>>20)
	}
	e, err := gomessage.Read(bytes.NewReader(raw))
	if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
		return nil, fmt.Errorf("el mail no se puede leer: %w", err)
	}
	h := mail.Header{Header: e.Header}
	m := &Mensaje{Tamano: len(raw)}
	m.MessageID = idMensaje(h, raw)
	if l, err := h.AddressList("From"); err == nil && len(l) > 0 {
		m.De, m.NombreDe = strings.ToLower(strings.TrimSpace(l[0].Address)), recortar(l[0].Name, 120)
	} else if f := strings.TrimSpace(h.Get("From")); f != "" {
		m.De = strings.ToLower(extraerEmail(f))
	}
	for _, k := range []string{"Delivered-To", "X-Original-To", "To", "Cc"} {
		if l, err := h.AddressList(k); err == nil {
			for _, a := range l {
				m.agregarPara(a.Address)
			}
			continue
		}
		for _, v := range h.Values(k) {
			m.agregarPara(extraerEmail(v))
		}
	}
	if s, err := h.Subject(); err == nil {
		m.Asunto = recortar(s, maxAsunto)
	} else {
		m.Asunto = recortar(h.Get("Subject"), maxAsunto)
	}
	if d, err := h.Date(); err == nil {
		m.Fecha = d
	}
	m.Referencias = recortar(strings.TrimSpace(h.Get("References")), 2000)
	l := &lector{lim: lim, m: m}
	l.recorrer(e, 0, 0, false)
	return m, nil
}

// AgregarDestinatario suma una dirección del sobre (la que dio Cloudflare o el proveedor) al
// principio de la lista.
func (m *Mensaje) AgregarDestinatario(dir string) {
	dir = strings.ToLower(strings.TrimSpace(extraerEmail(dir)))
	if dir == "" {
		return
	}
	for i, x := range m.Para {
		if x == dir {
			m.Para = append(m.Para[:i], m.Para[i+1:]...)
			break
		}
	}
	m.Para = append([]string{dir}, m.Para...)
}

func (m *Mensaje) agregarPara(dir string) {
	dir = strings.ToLower(strings.TrimSpace(dir))
	if dir == "" || !strings.Contains(dir, "@") || len(m.Para) >= maxDestinatariosOK {
		return
	}
	for _, x := range m.Para {
		if x == dir {
			return
		}
	}
	m.Para = append(m.Para, dir)
}

// idMensaje es el Message-ID del mail o, si no trae, uno armado con su hash (sirve igual para no
// procesar dos veces el mismo mail).
func idMensaje(h mail.Header, raw []byte) string {
	id := strings.TrimSpace(h.Get("Message-Id"))
	if id != "" && len(id) <= 500 {
		if !strings.HasPrefix(id, "<") {
			id = "<" + strings.Trim(id, "<>") + ">"
		}
		return id
	}
	s := sha256.Sum256(raw)
	return "<" + hex.EncodeToString(s[:16]) + "@sin-message-id.factorum>"
}

type lector struct {
	lim    Limites
	m      *Mensaje
	partes int
}

// recorrer baja por el árbol MIME. relacionado dice si la parte está dentro de un multipart/related
// (las imágenes de ahí son las del HTML: logos, firmas).
func (l *lector) recorrer(e *gomessage.Entity, profundo, eml int, relacionado bool) {
	l.partes++
	if l.partes > maxPartes || profundo > maxProfundo {
		return
	}
	tipo, params, _ := e.Header.ContentType()
	tipo = strings.ToLower(tipo)
	if mr := e.MultipartReader(); mr != nil {
		rel := relacionado || tipo == "multipart/related"
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) || p == nil {
				return
			}
			if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
				return // un multipart roto: se queda con lo que ya leyó
			}
			l.recorrer(p, profundo+1, eml, rel)
		}
	}
	ah := mail.AttachmentHeader{Header: e.Header}
	nombre, _ := ah.Filename()
	if nombre == "" {
		nombre = params["name"]
		if d, err := new(mime.WordDecoder).DecodeHeader(nombre); err == nil {
			nombre = d
		}
	}
	nombre = limpiarNombre(nombre)
	disp, _, _ := e.Header.ContentDisposition()
	// Un mail adjunto (.eml): se buscan sus adjuntos, sin leer su texto.
	if tipo == "message/rfc822" || strings.HasSuffix(strings.ToLower(nombre), ".eml") {
		if eml >= maxEMLAnidado {
			return
		}
		b, err := io.ReadAll(io.LimitReader(e.Body, l.lim.MaxMail+1))
		if err != nil || int64(len(b)) > l.lim.MaxMail {
			return
		}
		sub, err := gomessage.Read(bytes.NewReader(b))
		if err != nil && !gomessage.IsUnknownCharset(err) && !gomessage.IsUnknownEncoding(err) {
			return
		}
		l.recorrer(sub, profundo+1, eml+1, false)
		return
	}
	// El texto del mail (y el HTML) es dato de un tercero: ni se lee.
	if strings.HasPrefix(tipo, "text/") && nombre == "" {
		return
	}
	if strings.HasPrefix(tipo, "multipart/") {
		return
	}
	if nombre == "" && tipo != "application/pdf" && !strings.HasPrefix(tipo, "image/") && tipo != "application/zip" {
		return
	}
	b, err := io.ReadAll(io.LimitReader(e.Body, l.lim.MaxAdjunto+1))
	if err != nil {
		l.m.Descartados = append(l.m.Descartados, nombreONo(nombre, tipo)+" (no se pudo leer)")
		return
	}
	if int64(len(b)) > l.lim.MaxAdjunto {
		l.m.Descartados = append(l.m.Descartados, nombreONo(nombre, tipo)+" (muy grande)")
		return
	}
	clase := Clase(b)
	if clase == "" {
		if nombre != "" {
			l.m.Descartados = append(l.m.Descartados, nombre)
		}
		return
	}
	if strings.HasPrefix(clase, "image/") {
		embebida := relacionado && e.Header.Get("Content-Id") != "" && strings.ToLower(disp) != "attachment"
		if len(b) < minImagen || (embebida && len(b) < maxImagenEmbebida) {
			return // un logo o una firma
		}
	}
	if nombre == "" {
		nombre = "adjunto" + extension(clase)
	}
	l.m.Adjuntos = append(l.m.Adjuntos, Adjunto{Nombre: nombre, Mime: clase, Datos: b})
}

// Clase dice qué tipo de archivo útil es (por su contenido, no por lo que dice el mail): PDF, JPEG,
// PNG, WEBP o ZIP. Vacío = no sirve.
func Clase(b []byte) string {
	cab := b[:min(len(b), 1024)]
	switch {
	case bytes.HasPrefix(bytes.TrimLeft(cab, "\x00\t\r\n "), []byte("%PDF")):
		return "application/pdf"
	case bytes.HasPrefix(b, []byte("PK\x03\x04")):
		return "application/zip"
	}
	switch t := http.DetectContentType(b[:min(len(b), 512)]); t {
	case "image/jpeg", "image/png", "image/webp":
		return t
	}
	return ""
}

func extension(mime string) string {
	switch mime {
	case "application/pdf":
		return ".pdf"
	case "application/zip":
		return ".zip"
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ""
}

func nombreONo(nombre, tipo string) string {
	if nombre != "" {
		return nombre
	}
	return "adjunto (" + tipo + ")"
}

// limpiarNombre deja solo el nombre del archivo (sin rutas) y sin caracteres de control.
func limpiarNombre(n string) string {
	n = strings.ReplaceAll(n, "\\", "/")
	n = path.Base(strings.TrimSpace(n))
	if n == "." || n == "/" {
		return ""
	}
	n = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, n)
	return recortar(n, maxNombreAdjunto)
}

func recortar(s string, n int) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' {
			return ' '
		}
		return r
	}, s))
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// extraerEmail saca la dirección de "Nombre <a@b>" o la deja como está.
func extraerEmail(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	return strings.Trim(s, "<> \"")
}
