package whatsapp

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"

	"github.com/diegoparras/contadorfacturas/internal/canal"
)

// Funciones puras que pasan un evento de whatsmeow a canal.Mensaje. No necesitan un cliente
// conectado, así que se pueden probar sin teléfono.

const (
	nombreFoto      = "foto.jpg"
	nombreDocumento = "documento"
	mimeFoto        = "image/jpeg"
)

// adjunto es un archivo que todavía hay que bajar de los servidores de WhatsApp.
type adjunto struct {
	nombre string
	mime   string
	tam    uint64
	media  whatsmeow.DownloadableMessage
}

// descargador es la parte del cliente que baja los adjuntos (whatsmeow.Client.Download).
type descargador interface {
	Download(ctx context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error)
}

// buscadorPN traduce un LID (identificador oculto) al JID con el número de teléfono.
// Devuelve un JID vacío si no lo conoce.
type buscadorPN func(lid types.JID) types.JID

// aceptable dice si un mensaje es para Factorum: solo chats individuales, nunca grupos,
// estados, listas de difusión, canales ni mensajes propios.
func aceptable(info types.MessageInfo) bool {
	if info.IsFromMe || info.IsGroup {
		return false
	}
	switch info.Chat.Server {
	case types.DefaultUserServer, types.HiddenUserServer:
	default: // g.us, broadcast (incluye status@broadcast), newsletter, bot...
		return false
	}
	if info.Chat.IsBot() || info.Sender.IsBot() {
		return false
	}
	return true
}

// extraerTexto devuelve el texto o el epígrafe de un mensaje.
func extraerTexto(msg *waE2E.Message) string {
	if msg == nil {
		return ""
	}
	if t := msg.GetConversation(); t != "" {
		return t
	}
	if t := msg.GetExtendedTextMessage().GetText(); t != "" {
		return t
	}
	if t := msg.GetDocumentMessage().GetCaption(); t != "" {
		return t
	}
	return msg.GetImageMessage().GetCaption()
}

// remitente devuelve el número de teléfono de quien mandó el mensaje. Con direccionamiento por
// LID usa SenderAlt o, si no viene, el buscador; si nada lo resuelve, devuelve el LID tal cual.
func remitente(src types.MessageSource, buscar buscadorPN) string {
	s := src.Sender
	if s.IsEmpty() {
		s = src.Chat
	}
	if s.Server == types.HiddenUserServer {
		if alt := src.SenderAlt; alt.Server == types.DefaultUserServer && alt.User != "" {
			return alt.User
		}
		if buscar != nil {
			if pn := buscar(s.ToNonAD()); pn.Server == types.DefaultUserServer && pn.User != "" {
				return pn.User
			}
		}
	}
	return s.User
}

// convertir arma el mensaje (sin los datos de los archivos) y la lista de adjuntos a bajar.
// ok es false si el mensaje hay que ignorarlo.
func convertir(evt *events.Message, buscar buscadorPN) (m canal.Mensaje, adjuntos []adjunto, ok bool) {
	if evt == nil || evt.Message == nil || !aceptable(evt.Info) {
		return canal.Mensaje{}, nil, false
	}
	// Ediciones, reacciones, borrados y otros mensajes de protocolo no son facturas nuevas.
	if evt.IsEdit || evt.Message.GetProtocolMessage() != nil {
		return canal.Mensaje{}, nil, false
	}
	msg := evt.Message
	if d := msg.GetDocumentMessage(); d != nil {
		nombre := strings.TrimSpace(d.GetFileName())
		if nombre == "" {
			nombre = strings.TrimSpace(d.GetTitle())
		}
		if nombre == "" {
			nombre = nombreDocumento + extension(d.GetMimetype())
		}
		adjuntos = append(adjuntos, adjunto{nombre: nombre, mime: d.GetMimetype(), tam: d.GetFileLength(), media: d})
	}
	if img := msg.GetImageMessage(); img != nil {
		tipo := img.GetMimetype()
		if tipo == "" {
			tipo = mimeFoto
		}
		adjuntos = append(adjuntos, adjunto{nombre: nombreFoto, mime: tipo, tam: img.GetFileLength(), media: img})
	}
	texto := extraerTexto(msg)
	if strings.TrimSpace(texto) == "" && len(adjuntos) == 0 {
		return canal.Mensaje{}, nil, false // stickers, audios, ubicaciones, etc.
	}
	m = canal.Mensaje{
		Canal:           "whatsapp",
		Chat:            evt.Info.Chat.ToNonAD().String(),
		Remitente:       remitente(evt.Info.MessageSource, buscar),
		NombreRemitente: evt.Info.PushName,
		Texto:           texto,
	}
	return m, adjuntos, true
}

// descargar baja los adjuntos y los agrega al mensaje. Los que pasan de max o fallan no se
// agregan: el texto del mensaje lo avisa, para que el manejador pueda contestar.
func descargar(ctx context.Context, d descargador, m *canal.Mensaje, adjuntos []adjunto, max int64) []error {
	var notas []string
	var errs []error
	for _, a := range adjuntos {
		if max > 0 && a.tam > uint64(max) {
			notas = append(notas, notaGrande(a.nombre, max))
			continue
		}
		datos, err := d.Download(ctx, a.media)
		if err != nil {
			errs = append(errs, err)
			notas = append(notas, fmt.Sprintf("(No se pudo descargar el archivo %q.)", a.nombre))
			continue
		}
		if max > 0 && int64(len(datos)) > max {
			notas = append(notas, notaGrande(a.nombre, max))
			continue
		}
		tipo := a.mime
		if tipo == "" {
			tipo = "application/octet-stream"
		}
		m.Archivos = append(m.Archivos, canal.Archivo{Nombre: a.nombre, Mime: tipo, Datos: datos})
	}
	if len(notas) > 0 {
		if m.Texto != "" {
			notas = append([]string{m.Texto}, notas...)
		}
		m.Texto = strings.Join(notas, "\n")
	}
	return errs
}

func notaGrande(nombre string, max int64) string {
	return fmt.Sprintf("(El archivo %q pesa más de %d MB y no se pudo descargar.)", nombre, max>>20)
}

func extension(tipo string) string {
	switch strings.ToLower(strings.TrimSpace(strings.Split(tipo, ";")[0])) {
	case "":
		return ""
	case "application/pdf":
		return ".pdf"
	case "image/jpeg":
		return ".jpg"
	}
	if exts, _ := mime.ExtensionsByType(tipo); len(exts) > 0 {
		return exts[0]
	}
	return ""
}
