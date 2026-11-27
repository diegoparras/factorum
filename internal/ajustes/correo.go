package ajustes

import (
	"net/mail"
	"strings"

	"github.com/diegoparras/factorum/internal/config"
)

// emailValido: una dirección suelta (sin nombre) o, si se permite, "@dominio".
func emailValido(s string, dominios bool) bool {
	if dominios && strings.HasPrefix(s, "@") {
		d := s[1:]
		return d != "" && strings.Contains(d, ".") && !strings.ContainsAny(d, "@ <>")
	}
	a, err := mail.ParseAddress(s)
	return err == nil && strings.EqualFold(a.Address, s) && strings.Contains(s[strings.Index(s, "@"):], ".")
}

func emails(clave, seccion, etiqueta, env, ayuda, ejemplo string, dominios bool, get func(*config.Config) *[]string) Campo {
	g, s := lista(get)
	c := campo(clave, seccion, etiqueta, Emails, env, ayuda, ejemplo, g, s)
	c.Dominios = dominios
	return c
}

var opsTLS = []OpcionCampo{{"implicito", "TLS implícito (993 / 465)"}, {"starttls", "STARTTLS (143 / 587)"}, {"ninguno", "Sin cifrar (solo en una red de confianza)"}}

// camposCorreo: correo entrante, saliente y avisos.
func camposCorreo(add func(Campo)) {
	const ce, cs, av = "correo_entrada", "correo_salida", "avisos"
	g, s := texto(func(c *config.Config) *string { return &c.CorreoForma })
	add(opcion("correo.forma", ce, "Cómo entra el correo", "FACTORUM_CORREO_FORMA", "Una sola forma a la vez. Al cambiarla se aplica en el momento.",
		[]OpcionCampo{{"imap", "Una casilla por IMAP"}, {"cloudflare", "Cloudflare Email Routing"}, {"proveedor", "Un proveedor (Mailgun, Postmark, SendGrid)"},
			{"gmail", "Gmail con permiso de Google"}, {"apagado", "Apagado"}}, g, s))
	g, s = lista(func(c *config.Config) *[]string { return &c.CorreoPermitidos })
	perm := campo("correo.permitidos", ce, "Remitentes permitidos", Emails, "FACTORUM_CORREO_PERMITIDOS",
		"Uno por línea: un email o @dominio. Solo estos generan lotes; lo demás queda en Correo retenido. Sin lista, todo queda retenido.", "facturacion@proveedor.com.ar", g, s)
	perm.Dominios = true
	add(perm)
	g, s = entero(func(c *config.Config) *int { return &c.CorreoMaxMB })
	add(numero("correo.max_mb", ce, "Tope por mail (MB)", "FACTORUM_CORREO_MAX_MB", "Un mail más grande no se baja (queda una línea en la auditoría). Cada adjunto, además, respeta el tope por archivo.", 1, 100, g, s))
	g, s = booleano(func(c *config.Config) *bool { return &c.CorreoContestar })
	add(campo("correo.contestar", ce, "Contestar al remitente", Bool, "FACTORUM_CORREO_CONTESTAR",
		"Cuando termina el lote, un mail en el mismo hilo con el resumen. Solo a permitidos, nunca a retenidos. Hace falta el correo saliente.", "", g, s))

	// IMAP.
	g, s = texto(func(c *config.Config) *string { return &c.IMAPHost })
	add(campo("imap.host", ce, "Servidor IMAP", Texto, "FACTORUM_IMAP_HOST", "", "imap.gmail.com", g, s))
	g, s = entero(func(c *config.Config) *int { return &c.IMAPPuerto })
	add(numero("imap.puerto", ce, "Puerto", "FACTORUM_IMAP_PUERTO", "993 con TLS implícito; 143 con STARTTLS.", 1, 65535, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPTLS })
	add(opcion("imap.tls", ce, "Seguridad", "FACTORUM_IMAP_TLS", "", opsTLS, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPUsuario })
	add(campo("imap.usuario", ce, "Usuario", Texto, "FACTORUM_IMAP_USUARIO", "Casi siempre, el email entero.", "facturas@estudio.com.ar", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPClave })
	add(campo("imap.clave", ce, "Contraseña", Secreto, "FACTORUM_IMAP_CLAVE", "En Gmail y Microsoft 365, una contraseña de aplicación (no la de siempre).", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPCarpeta })
	add(campo("imap.carpeta", ce, "Carpeta", Texto, "FACTORUM_IMAP_CARPETA", "Factorum mira los mails sin leer de esta carpeta.", "INBOX", g, s))
	g, s = entero(func(c *config.Config) *int { return &c.IMAPIntervalo })
	add(numero("imap.intervalo", ce, "Cada cuánto mira (segundos)", "FACTORUM_IMAP_INTERVALO", "", 15, 3600, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPProcesado })
	add(opcion("imap.procesado", ce, "Qué hace con lo procesado", "FACTORUM_IMAP_PROCESADO", "",
		[]OpcionCampo{{"leido", "Lo marca como leído"}, {"mover", "Lo mueve a otra carpeta"}}, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.IMAPCarpetaProcesado })
	add(campo("imap.carpeta_procesado", ce, "Carpeta de lo procesado", Texto, "FACTORUM_IMAP_CARPETA_PROCESADO", "Si no existe, Factorum la crea.", "Factorum", g, s))

	// Cloudflare.
	g, s = texto(func(c *config.Config) *string { return &c.CloudflareSecreto })
	add(campo("cloudflare.secreto", ce, "Secreto compartido", Secreto, "FACTORUM_CLOUDFLARE_SECRETO",
		"El mismo que ponés en el Worker como variable secreta FACTORUM_SECRETO. Firma cada mail (HMAC-SHA256 con la hora).", "", g, s))

	// Proveedor.
	g, s = texto(func(c *config.Config) *string { return &c.CorreoProveedor })
	add(opcion("correo.proveedor", ce, "Proveedor", "FACTORUM_CORREO_PROVEEDOR", "",
		[]OpcionCampo{{"mailgun", "Mailgun"}, {"postmark", "Postmark"}, {"sendgrid", "SendGrid (Inbound Parse)"}}, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.MailgunClave })
	add(campo("mailgun.signing_key", ce, "HTTP webhook signing key de Mailgun", Secreto, "FACTORUM_MAILGUN_SIGNING_KEY",
		"En Mailgun: Sending, Webhooks, HTTP webhook signing key. Factorum controla la firma de cada pedido.", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.ProveedorToken })
	add(campo("correo.token", ce, "Token de la URL", Secreto, "FACTORUM_CORREO_TOKEN",
		"Para Postmark y SendGrid: va en la URL que les configurás. Tocá Generar.", "", g, s))

	// Gmail.
	g, s = texto(func(c *config.Config) *string { return &c.GoogleClienteID })
	add(campo("google.client_id", ce, "Client ID de Google", Secreto, "FACTORUM_GOOGLE_CLIENT_ID", "De las credenciales «Aplicación web» del proyecto de Google Cloud.", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.GoogleClienteSecreto })
	add(campo("google.client_secret", ce, "Client secret de Google", Secreto, "FACTORUM_GOOGLE_CLIENT_SECRET", "", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.GmailEtiqueta })
	add(campo("gmail.etiqueta", ce, "Etiqueta", Texto, "FACTORUM_GMAIL_ETIQUETA", "Factorum lee los mails con esta etiqueta. Creala en Gmail (y un filtro que se la ponga, si querés).", "Facturas", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.GmailProcesado })
	add(opcion("gmail.procesado", ce, "Qué hace con lo procesado", "FACTORUM_GMAIL_PROCESADO", "",
		[]OpcionCampo{{"quitar", "Le saca la etiqueta"}, {"leido", "Lo marca como leído"}}, g, s))
	g, s = entero(func(c *config.Config) *int { return &c.GmailIntervalo })
	add(numero("gmail.intervalo", ce, "Cada cuánto mira (segundos)", "FACTORUM_GMAIL_INTERVALO", "", 15, 3600, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.GoogleRefreshToken })
	rt := campo("google.refresh_token", ce, "Permiso de Google", Secreto, "FACTORUM_GOOGLE_REFRESH_TOKEN", "", "", g, s)
	rt.Interno = true
	add(rt)
	g, s = texto(func(c *config.Config) *string { return &c.GoogleCuenta })
	cu := campo("google.cuenta", ce, "Cuenta de Google conectada", Texto, "FACTORUM_GOOGLE_CUENTA", "", "", g, s)
	cu.Interno = true
	add(cu)

	// Correo saliente.
	g, s = texto(func(c *config.Config) *string { return &c.SMTPHost })
	add(campo("smtp.host", cs, "Servidor SMTP", Texto, "FACTORUM_SMTP_HOST", "", "smtp.gmail.com", g, s))
	g, s = entero(func(c *config.Config) *int { return &c.SMTPPuerto })
	add(numero("smtp.puerto", cs, "Puerto", "FACTORUM_SMTP_PUERTO", "587 con STARTTLS; 465 con TLS implícito.", 1, 65535, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.SMTPTLS })
	add(opcion("smtp.tls", cs, "Seguridad", "FACTORUM_SMTP_TLS", "", []OpcionCampo{opsTLS[1], opsTLS[0], opsTLS[2]}, g, s))
	g, s = texto(func(c *config.Config) *string { return &c.SMTPUsuario })
	add(campo("smtp.usuario", cs, "Usuario", Texto, "FACTORUM_SMTP_USUARIO", "Vacío si el servidor no pide usuario.", "facturas@estudio.com.ar", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.SMTPClave })
	add(campo("smtp.clave", cs, "Contraseña", Secreto, "FACTORUM_SMTP_CLAVE", "En Gmail y Microsoft 365, una contraseña de aplicación; en un servicio transaccional, su clave de API o de SMTP.", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.SMTPRemitente })
	add(campo("smtp.remitente", cs, "Remitente", Texto, "FACTORUM_SMTP_REMITENTE", "Nombre y dirección. El dominio tiene que estar autorizado en el servidor (SPF y DKIM).", "Factorum <facturas@estudio.com.ar>", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.SMTPResponderA })
	add(campo("smtp.responder_a", cs, "Responder a", Texto, "FACTORUM_SMTP_RESPONDER_A", "Opcional: a dónde van las respuestas (por ejemplo, el mail del estudio).", "estudio@estudio.com.ar", g, s))

	// Avisos.
	g, s = booleano(func(c *config.Config) *bool { return &c.AvisoLote })
	add(campo("avisos.lote", av, "Lote terminado", Bool, "FACTORUM_AVISO_LOTE", "A quien lo cargó desde la pantalla; si vino por chat, por la API o por mail, a la lista.", "", g, s))
	add(emails("avisos.lote_a", av, "Destinatarios del lote terminado", "FACTORUM_AVISO_LOTE_A", "Uno por línea.", "estudio@estudio.com.ar", false,
		func(c *config.Config) *[]string { return &c.AvisoLoteA }))
	g, s = booleano(func(c *config.Config) *bool { return &c.AvisoAlertas })
	add(campo("avisos.alertas", av, "Emisor apócrifo o PDF posiblemente adulterado", Bool, "FACTORUM_AVISO_ALERTAS", "A los admins y superadmins (cada uno, de sus clientes) y a la lista.", "", g, s))
	add(emails("avisos.alertas_a", av, "Además, a", "FACTORUM_AVISO_ALERTAS_A", "Uno por línea.", "", false,
		func(c *config.Config) *[]string { return &c.AvisoAlertasA }))
	g, s = booleano(func(c *config.Config) *bool { return &c.AvisoSemanal })
	add(campo("avisos.semanal", av, "Resumen semanal", Bool, "FACTORUM_AVISO_SEMANAL", "Los lunes a las 8, hora de Buenos Aires: lo de la semana y lo pendiente de revisar.", "", g, s))
	add(emails("avisos.semanal_a", av, "Destinatarios del resumen", "FACTORUM_AVISO_SEMANAL_A", "Uno por línea. Vacío: a los admins y superadmins.", "", false,
		func(c *config.Config) *[]string { return &c.AvisoSemanalA }))
}
