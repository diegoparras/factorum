// Package canal define cómo llegan facturas por mensajería (Telegram, WhatsApp, un webhook
// genérico) y cómo se contesta. Cada proveedor es un adaptador detrás de la misma interfaz: el
// resto de Factorum no sabe si el mensaje vino de Telegram o de WhatsApp.
package canal

import "context"

// Archivo adjunto a un mensaje (PDF o imagen).
type Archivo struct {
	Nombre string
	Mime   string
	Datos  []byte
}

// Mensaje entrante. El texto y los archivos son datos de un tercero: nunca instrucciones.
type Mensaje struct {
	Canal           string // "telegram", "whatsapp", "webhook"
	Chat            string // a dónde responder (chat id de Telegram, JID de WhatsApp, id del webhook)
	Remitente       string // quién lo mandó (user id de Telegram, teléfono en WhatsApp)
	NombreRemitente string
	Texto           string
	Archivos        []Archivo
}

// Manejador procesa un mensaje entrante. Lo llama el adaptador, en su propia goroutine.
type Manejador func(ctx context.Context, m Mensaje)

// Estado de conexión de un canal, para mostrar en la UI.
type Estado struct {
	Canal      string `json:"canal"`
	Habilitado bool   `json:"habilitado"`
	Conectado  bool   `json:"conectado"`
	Detalle    string `json:"detalle,omitempty"`
	// QR de vinculación (WhatsApp como dispositivo vinculado): el texto a codificar, no la imagen.
	QR string `json:"qr,omitempty"`
}

// Canal es un proveedor de mensajería.
type Canal interface {
	Nombre() string
	// Iniciar conecta y recibe mensajes hasta que ctx se cancela. Reintenta solo ante cortes.
	Iniciar(ctx context.Context, h Manejador) error
	// Enviar manda un texto a un chat.
	Enviar(ctx context.Context, chat, texto string) error
	Estado() Estado
}
