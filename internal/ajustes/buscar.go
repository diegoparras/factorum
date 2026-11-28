package ajustes

import (
	"errors"
	"slices"
	"strings"
)

// TituloSeccion es el nombre de cada sección en la pantalla (el mismo que muestra web/app.js).
var TituloSeccion = map[string]string{
	"general":        "General",
	"arcanum":        "Arcanum",
	"lockatus":       "Lockatus",
	"telegram":       "Telegram",
	"whatsapp":       "WhatsApp",
	"webhook":        "Webhook",
	"correo_entrada": "Correo entrante",
	"correo_salida":  "Correo saliente",
	"avisos":         "Avisos",
	"ia":             "IA (visión)",
	"r2":             "Almacenamiento R2",
	"copias":         "Copias de seguridad",
	"papelera":       "Papelera",
	"clientes":       "Clientes y canales",
}

// palabrasSeccion: otras formas de nombrar una sección, para el buscador.
var palabrasSeccion = map[string]string{
	"general":        "instalación nombre dirección url pública dominio",
	"arcanum":        "arca constatación constatar wscdc apócrifos padrón suite pase",
	"lockatus":       "inicio de sesión único sso oidc openid login entrar",
	"telegram":       "bot chat botfather",
	"whatsapp":       "chat dispositivo vinculado qr teléfono",
	"webhook":        "bot n8n evolution secretaria token hmac",
	"correo_entrada": "mail email imap gmail cloudflare mailgun postmark sendgrid remitentes permitidos",
	"correo_salida":  "mail email smtp remitente invitaciones contraseña",
	"avisos":         "notificaciones mails alertas resumen semanal lote terminado",
	"ia":             "inteligencia artificial visión modelo ollama openai anthropic gemini openrouter",
	"r2":             "cloudflare s3 bucket archivos disco almacén",
	"copias":         "backup respaldo base de datos restaurar",
	"papelera":       "borrar borrado días restaurar",
	"clientes":       "remitentes cuit canales chat mail",
}

// HallazgoAjuste es una sección o un campo de la configuración que coincide con lo buscado.
// Clave vacía: la sección entera.
type HallazgoAjuste struct {
	Seccion       string `json:"seccion"`
	SeccionTitulo string `json:"seccion_titulo"`
	Clave         string `json:"clave,omitempty"`
	Titulo        string `json:"titulo"`
	Detalle       string `json:"detalle,omitempty"`
}

var sinTildes = strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n")

func normal(s string) string { return sinTildes.Replace(strings.ToLower(s)) }

// BuscarAjustes busca en los títulos de las secciones y de los campos (y en la variable de entorno
// de cada uno) de las secciones que se pueden ver. Primero las secciones, después los campos; hasta
// lim en total.
func BuscarAjustes(q string, secciones []string, lim int) []HallazgoAjuste {
	res := []HallazgoAjuste{}
	q = normal(strings.TrimSpace(q))
	if len([]rune(q)) < 2 || lim <= 0 {
		return res
	}
	for _, sec := range Secciones {
		if !slices.Contains(secciones, sec) {
			continue
		}
		t := TituloSeccion[sec]
		if strings.Contains(normal(t), q) || strings.Contains(normal(palabrasSeccion[sec]), q) {
			res = append(res, HallazgoAjuste{Seccion: sec, SeccionTitulo: t, Titulo: t})
		}
	}
	for _, c := range Campos {
		if c.Interno || !slices.Contains(secciones, c.Seccion) {
			continue
		}
		if strings.Contains(normal(c.Etiqueta), q) || (len(q) >= 4 && strings.Contains(normal(c.Env), q)) {
			res = append(res, HallazgoAjuste{Seccion: c.Seccion, SeccionTitulo: TituloSeccion[c.Seccion], Clave: c.Clave, Titulo: c.Etiqueta, Detalle: c.Env})
		}
	}
	if len(res) > lim {
		res = res[:lim]
	}
	return res
}

// ErrorCampo es un valor inválido de un campo en particular: la pantalla lo marca en ese campo.
type ErrorCampo struct {
	Clave string
	Err   error
}

func (e *ErrorCampo) Error() string { return e.Err.Error() }
func (e *ErrorCampo) Unwrap() error { return e.Err }

// CampoDelError dice de qué campo es un error de validación ("" si no es de uno solo).
func CampoDelError(err error) string {
	var ec *ErrorCampo
	if errors.As(err, &ec) {
		return ec.Clave
	}
	return ""
}
