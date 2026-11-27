// Package config lee la configuración del entorno. Todo tiene un valor por defecto razonable:
// sin nada configurado, Factorum arranca, lee QR y texto, y guarda en disco.
package config

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Version la completa el build (-ldflags "-X .../config.Version=0.1.0").
var Version = "0.1.0-dev"

// Config es la configuración de Factorum. La de arranque sale del entorno (Leer); lo que se puede
// configurar desde la pantalla lo pisa el servicio de ajustes, salvo que la variable de entorno
// esté puesta: el entorno gana.
type Config struct {
	Nombre     string // nombre de la instalación, para la pantalla
	URLPublica string // la que ve el navegador (https://factorum.estudio.com); hace falta para la suite

	Puerto      int
	DatabaseURL string
	APIKey      string
	APIKeyLect  string
	// APIKeySuiteTope habilita FACTORUM_API_KEY para abrir sesiones de la suite (la que usa Arcanum
	// como FACTORUM_TOKEN), con ese rol tope: lectura, usuario, admin o superadmin. Vacía = no.
	APIKeySuiteTope string
	DirDatos        string // archivos en disco cuando no hay R2
	ClaveMaestra    []byte // 32 bytes para cifrar lo que se guarda (AES-256-GCM)

	Workers      int // documentos procesados en paralelo
	MaxPaginas   int
	MaxArchivo   int64 // bytes por archivo
	ConcOCR      int
	ConcIA       int
	IAPorDefecto bool // usar el modelo cuando hace falta, si no se dice otra cosa

	ConstatarPorDefecto bool // constatar con Arcanum al terminar un lote, si no se dice otra cosa

	// Modelo de visión (API compatible con OpenAI: Ollama, LM Studio, vLLM, OpenAI...).
	LLMBaseURL string
	LLMAPIKey  string
	LLMModelo  string
	LLMTimeout time.Duration

	OCRIdiomas string

	// R2 (o cualquier S3).
	R2Endpoint  string
	R2Bucket    string
	R2AccessKey string
	R2SecretKey string
	R2Region    string
	R2Prefijo   string

	// Copias de seguridad de la base (diarias, a una hora de Buenos Aires).
	CopiasActivas   bool
	CopiasHora      string // HH:MM
	CopiasDiarias   int    // cuántas diarias se guardan
	CopiasMensuales int    // y cuántas mensuales (la primera de cada mes)

	// Arcanum.
	ArcanumURL         string // interna (http://arcanum:8094)
	ArcanumURLPublica  string // la que ve el navegador, para el pase entre apps
	ArcanumAPIKey      string
	ArcanumConsultante string
	ArcanumTimeout     time.Duration

	// Suite: si una persona que no existe llega con un pase desde otra app, se la crea.
	SuiteCrearUsuarios bool

	// Canales.
	TelegramToken      string
	TelegramPermitidos []string // ids de usuario o chat; vacío = nadie
	WhatsApp           bool
	WhatsAppPermitidos []string // teléfonos (solo dígitos); vacío = nadie
	WebhookToken       string
	WebhookSalidaURL   string
	CanalClienteCUIT   map[string]string // remitente -> CUIT del cliente (FACTORUM_CANAL_CLIENTES="5491155550000=20222222223,..."); también emails y @dominio

	// Correo entrante: una sola forma a la vez.
	CorreoForma      string   // imap (por defecto), cloudflare, proveedor, gmail o apagado
	CorreoProveedor  string   // mailgun, postmark o sendgrid (con la forma proveedor)
	CorreoPermitidos []string // emails o @dominio; vacío = todo queda retenido
	CorreoMaxMB      int      // tope por mail
	CorreoContestar  bool     // contestarle al remitente cuando termina el lote

	IMAPHost             string
	IMAPPuerto           int
	IMAPTLS              string // implicito, starttls o ninguno
	IMAPUsuario          string
	IMAPClave            string
	IMAPCarpeta          string
	IMAPIntervalo        int    // segundos entre vueltas
	IMAPProcesado        string // leido o mover
	IMAPCarpetaProcesado string

	CloudflareSecreto string // el secreto compartido con el Email Worker
	MailgunClave      string // la HTTP webhook signing key de Mailgun
	ProveedorToken    string // el de la URL de Postmark y SendGrid

	GoogleClienteID      string
	GoogleClienteSecreto string
	GoogleRefreshToken   string // lo guarda «Conectar con Google», cifrado
	GoogleCuenta         string // el email de la cuenta conectada
	GmailEtiqueta        string
	GmailProcesado       string // quitar o leido
	GmailIntervalo       int

	// Correo saliente.
	SMTPHost       string
	SMTPPuerto     int
	SMTPTLS        string // starttls, implicito o ninguno
	SMTPUsuario    string
	SMTPClave      string
	SMTPRemitente  string // Factorum <facturas@estudio.com.ar>
	SMTPResponderA string

	// Avisos por mail.
	AvisoLote     bool
	AvisoLoteA    []string // si quien cargó no tiene email (chats, correo, API)
	AvisoAlertas  bool     // apócrifos y posibles adulteraciones, a los admins
	AvisoAlertasA []string
	AvisoSemanal  bool // lunes 8:00, hora de Buenos Aires
	AvisoSemanalA []string
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(env(k, "")); err == nil {
		return v
	}
	return def
}

func envBool(k string, def bool) bool {
	switch strings.ToLower(env(k, "")) {
	case "1", "true", "si", "sí", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return def
}

func lista(k string) []string {
	var r []string
	for _, p := range strings.Split(env(k, ""), ",") {
		if p = strings.TrimSpace(p); p != "" {
			r = append(r, p)
		}
	}
	return r
}

// Leer arma la configuración desde el entorno.
func Leer() (*Config, error) {
	c := &Config{
		Nombre:      env("FACTORUM_NOMBRE", "Factorum"),
		URLPublica:  strings.TrimRight(env("FACTORUM_URL_PUBLICA", ""), "/"),
		Puerto:      envInt("FACTORUM_PUERTO", 8095),
		DatabaseURL: env("FACTORUM_DATABASE_URL", env("DATABASE_URL", "")),
		APIKey:      env("FACTORUM_API_KEY", ""),
		APIKeyLect:  env("FACTORUM_API_KEY_LECTURA", ""),

		APIKeySuiteTope: strings.ToLower(env("FACTORUM_API_KEY_SUITE_TOPE", "")),
		DirDatos:        env("FACTORUM_DATOS", "/data"),
		Workers:         envInt("FACTORUM_WORKERS", runtime.NumCPU()),
		MaxPaginas:      envInt("FACTORUM_MAX_PAGINAS", 30),
		MaxArchivo:      int64(envInt("FACTORUM_MAX_MB", 25)) << 20,
		ConcOCR:         envInt("FACTORUM_OCR_CONCURRENCIA", max(1, runtime.NumCPU()/2)),
		ConcIA:          envInt("FACTORUM_IA_CONCURRENCIA", 1),
		IAPorDefecto:    envBool("FACTORUM_IA_POR_DEFECTO", true),

		ConstatarPorDefecto: envBool("FACTORUM_CONSTATAR_POR_DEFECTO", true),
		SuiteCrearUsuarios:  envBool("FACTORUM_SUITE_CREAR_USUARIOS", true),
		LLMBaseURL:          env("LLM_API_BASE_URL", ""),
		LLMAPIKey:           env("LLM_API_KEY", ""),
		LLMModelo:           env("LLM_MODEL", ""),
		LLMTimeout:          time.Duration(envInt("LLM_TIMEOUT_S", 180)) * time.Second,
		OCRIdiomas:          env("FACTORUM_OCR_IDIOMAS", "spa"),

		R2Endpoint:  env("FACTORUM_R2_ENDPOINT", ""),
		R2Bucket:    env("FACTORUM_R2_BUCKET", ""),
		R2AccessKey: env("FACTORUM_R2_ACCESS_KEY", ""),
		R2SecretKey: env("FACTORUM_R2_SECRET_KEY", ""),
		R2Region:    env("FACTORUM_R2_REGION", "auto"),
		R2Prefijo:   env("FACTORUM_R2_PREFIJO", "factorum/"),

		CopiasActivas:   envBool("FACTORUM_COPIAS", true),
		CopiasHora:      env("FACTORUM_COPIAS_HORA", "03:30"),
		CopiasDiarias:   envInt("FACTORUM_COPIAS_DIARIAS", 14),
		CopiasMensuales: envInt("FACTORUM_COPIAS_MENSUALES", 12),

		ArcanumURL:         strings.TrimRight(env("ARCANUM_URL", ""), "/"),
		ArcanumURLPublica:  strings.TrimRight(env("ARCANUM_URL_PUBLICA", ""), "/"),
		ArcanumAPIKey:      env("ARCANUM_API_KEY", ""),
		ArcanumConsultante: env("ARCANUM_CUIT_CONSULTANTE", ""),
		ArcanumTimeout:     time.Duration(envInt("ARCANUM_TIMEOUT_S", 60)) * time.Second,

		TelegramToken:      env("FACTORUM_TELEGRAM_TOKEN", ""),
		TelegramPermitidos: lista("FACTORUM_TELEGRAM_PERMITIDOS"),
		WhatsApp:           envBool("FACTORUM_WHATSAPP", false),
		WhatsAppPermitidos: lista("FACTORUM_WHATSAPP_PERMITIDOS"),
		WebhookToken:       env("FACTORUM_WEBHOOK_TOKEN", ""),
		WebhookSalidaURL:   env("FACTORUM_WEBHOOK_SALIDA_URL", ""),
		CanalClienteCUIT:   map[string]string{},

		CorreoForma:      strings.ToLower(env("FACTORUM_CORREO_FORMA", "imap")),
		CorreoProveedor:  strings.ToLower(env("FACTORUM_CORREO_PROVEEDOR", "mailgun")),
		CorreoPermitidos: lista("FACTORUM_CORREO_PERMITIDOS"),
		CorreoMaxMB:      envInt("FACTORUM_CORREO_MAX_MB", 25),
		CorreoContestar:  envBool("FACTORUM_CORREO_CONTESTAR", true),

		IMAPHost:             env("FACTORUM_IMAP_HOST", ""),
		IMAPPuerto:           envInt("FACTORUM_IMAP_PUERTO", 993),
		IMAPTLS:              strings.ToLower(env("FACTORUM_IMAP_TLS", "implicito")),
		IMAPUsuario:          env("FACTORUM_IMAP_USUARIO", ""),
		IMAPClave:            env("FACTORUM_IMAP_CLAVE", ""),
		IMAPCarpeta:          env("FACTORUM_IMAP_CARPETA", "INBOX"),
		IMAPIntervalo:        envInt("FACTORUM_IMAP_INTERVALO", 60),
		IMAPProcesado:        strings.ToLower(env("FACTORUM_IMAP_PROCESADO", "leido")),
		IMAPCarpetaProcesado: env("FACTORUM_IMAP_CARPETA_PROCESADO", "Factorum"),

		CloudflareSecreto: env("FACTORUM_CLOUDFLARE_SECRETO", ""),
		MailgunClave:      env("FACTORUM_MAILGUN_SIGNING_KEY", ""),
		ProveedorToken:    env("FACTORUM_CORREO_TOKEN", ""),

		GoogleClienteID:      env("FACTORUM_GOOGLE_CLIENT_ID", ""),
		GoogleClienteSecreto: env("FACTORUM_GOOGLE_CLIENT_SECRET", ""),
		GoogleRefreshToken:   env("FACTORUM_GOOGLE_REFRESH_TOKEN", ""),
		GoogleCuenta:         env("FACTORUM_GOOGLE_CUENTA", ""),
		GmailEtiqueta:        env("FACTORUM_GMAIL_ETIQUETA", "Facturas"),
		GmailProcesado:       strings.ToLower(env("FACTORUM_GMAIL_PROCESADO", "quitar")),
		GmailIntervalo:       envInt("FACTORUM_GMAIL_INTERVALO", 60),

		SMTPHost:       env("FACTORUM_SMTP_HOST", ""),
		SMTPPuerto:     envInt("FACTORUM_SMTP_PUERTO", 587),
		SMTPTLS:        strings.ToLower(env("FACTORUM_SMTP_TLS", "starttls")),
		SMTPUsuario:    env("FACTORUM_SMTP_USUARIO", ""),
		SMTPClave:      env("FACTORUM_SMTP_CLAVE", ""),
		SMTPRemitente:  env("FACTORUM_SMTP_REMITENTE", ""),
		SMTPResponderA: env("FACTORUM_SMTP_RESPONDER_A", ""),

		AvisoLote:     envBool("FACTORUM_AVISO_LOTE", false),
		AvisoLoteA:    lista("FACTORUM_AVISO_LOTE_A"),
		AvisoAlertas:  envBool("FACTORUM_AVISO_ALERTAS", true),
		AvisoAlertasA: lista("FACTORUM_AVISO_ALERTAS_A"),
		AvisoSemanal:  envBool("FACTORUM_AVISO_SEMANAL", false),
		AvisoSemanalA: lista("FACTORUM_AVISO_SEMANAL_A"),
	}
	for _, par := range lista("FACTORUM_CANAL_CLIENTES") {
		if k, v, ok := strings.Cut(par, "="); ok {
			c.CanalClienteCUIT[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if mk := env("FACTORUM_MASTER_KEY", ""); mk != "" {
		b, err := decodificarClave(mk)
		if err != nil {
			return nil, err
		}
		c.ClaveMaestra = b
	}
	switch c.APIKeySuiteTope {
	case "", "lectura", "usuario", "admin", "superadmin":
	default:
		return nil, fmt.Errorf("FACTORUM_API_KEY_SUITE_TOPE tiene que ser lectura, usuario, admin o superadmin (o vacía)")
	}
	if err := c.Validar(); err != nil {
		return nil, err
	}
	if c.Workers < 1 {
		c.Workers = 1
	}
	return c, nil
}

// Validar controla que la configuración tenga sentido (sirve para el arranque y para lo que se
// guarda desde la pantalla).
func (c *Config) Validar() error {
	if c.R2Endpoint != "" && (c.R2Bucket == "" || c.R2AccessKey == "" || c.R2SecretKey == "") {
		return fmt.Errorf("R2 a medias: con el endpoint hacen falta el bucket, la access key y la secret key")
	}
	if c.R2Endpoint != "" && c.ClaveMaestra == nil {
		return fmt.Errorf("para guardar en R2 hace falta FACTORUM_MASTER_KEY (openssl rand -hex 32): los comprobantes se cifran antes de salir")
	}
	if _, _, ok := HoraMinuto(c.CopiasHora); !ok && c.CopiasHora != "" {
		return fmt.Errorf("la hora de las copias tiene que ser HH:MM (por ejemplo 03:30)")
	}
	if c.CopiasDiarias < 0 || c.CopiasDiarias > 366 {
		return fmt.Errorf("las copias diarias que se guardan tienen que ser entre 1 y 366")
	}
	if c.CopiasMensuales < 0 || c.CopiasMensuales > 120 {
		return fmt.Errorf("las copias mensuales que se guardan tienen que ser entre 0 y 120")
	}
	if c.LLMBaseURL != "" && c.LLMModelo == "" {
		return fmt.Errorf("con la URL del modelo de visión hace falta el modelo (por ejemplo qwen2.5vl:7b)")
	}
	return c.validarCorreo()
}

// uno: el valor vacío (el de fábrica) o uno de los de la lista.
func uno(v string, ok ...string) bool {
	if v == "" {
		return true
	}
	for _, x := range ok {
		if v == x {
			return true
		}
	}
	return false
}

func entre(v, desde, hasta int) bool { return v == 0 || (v >= desde && v <= hasta) }

// validarCorreo controla el correo. Un cero o un vacío es «el de fábrica».
func (c *Config) validarCorreo() error {
	switch {
	case !uno(c.CorreoForma, "imap", "cloudflare", "proveedor", "gmail", "apagado"):
		return fmt.Errorf("la forma del correo entrante tiene que ser imap, cloudflare, proveedor, gmail o apagado")
	case !uno(c.CorreoProveedor, "mailgun", "postmark", "sendgrid"):
		return fmt.Errorf("el proveedor de correo tiene que ser mailgun, postmark o sendgrid")
	case !uno(c.IMAPTLS, "implicito", "starttls", "ninguno"), !uno(c.SMTPTLS, "implicito", "starttls", "ninguno"):
		return fmt.Errorf("el TLS del correo tiene que ser implicito, starttls o ninguno")
	case !uno(c.IMAPProcesado, "leido", "mover"):
		return fmt.Errorf("lo procesado en IMAP se marca como leído (leido) o se mueve (mover)")
	case !uno(c.GmailProcesado, "quitar", "leido"):
		return fmt.Errorf("lo procesado en Gmail se queda sin la etiqueta (quitar) o se marca como leído (leido)")
	case !entre(c.IMAPPuerto, 1, 65535), !entre(c.SMTPPuerto, 1, 65535):
		return fmt.Errorf("el puerto tiene que estar entre 1 y 65535")
	case !entre(c.IMAPIntervalo, 15, 3600), !entre(c.GmailIntervalo, 15, 3600):
		return fmt.Errorf("cada cuánto se mira el correo: entre 15 y 3600 segundos")
	case !entre(c.CorreoMaxMB, 1, 100):
		return fmt.Errorf("el tope por mail tiene que estar entre 1 y 100 MB")
	}
	return nil
}

// FormaCorreo es la forma del correo entrante (imap si no se eligió).
func (c *Config) FormaCorreo() string {
	if c.CorreoForma == "" {
		return "imap"
	}
	return c.CorreoForma
}

// MaxMail es el tope de un mail entero, en bytes.
func (c *Config) MaxMail() int64 {
	if c.CorreoMaxMB <= 0 {
		return 25 << 20
	}
	return int64(c.CorreoMaxMB) << 20
}

// SMTPConfigurado dice si se pueden mandar mails.
func (c *Config) SMTPConfigurado() bool { return c.SMTPHost != "" && c.SMTPRemitente != "" }

// Clonar copia la configuración, con sus listas y mapas.
func (c *Config) Clonar() *Config {
	n := *c
	n.TelegramPermitidos = append([]string(nil), c.TelegramPermitidos...)
	n.WhatsAppPermitidos = append([]string(nil), c.WhatsAppPermitidos...)
	n.CorreoPermitidos = append([]string(nil), c.CorreoPermitidos...)
	n.AvisoLoteA = append([]string(nil), c.AvisoLoteA...)
	n.AvisoAlertasA = append([]string(nil), c.AvisoAlertasA...)
	n.AvisoSemanalA = append([]string(nil), c.AvisoSemanalA...)
	n.CanalClienteCUIT = make(map[string]string, len(c.CanalClienteCUIT))
	for k, v := range c.CanalClienteCUIT {
		n.CanalClienteCUIT[k] = v
	}
	return &n
}

// DecodificarClave lee FACTORUM_MASTER_KEY (64 caracteres hexadecimales).
func DecodificarClave(s string) ([]byte, error) { return decodificarClave(s) }

func decodificarClave(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) != 64 {
		return nil, fmt.Errorf("FACTORUM_MASTER_KEY tiene que tener 64 caracteres hexadecimales (openssl rand -hex 32)")
	}
	b := make([]byte, 32)
	for i := 0; i < 32; i++ {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, fmt.Errorf("FACTORUM_MASTER_KEY no es hexadecimal")
		}
		b[i] = byte(v)
	}
	return b, nil
}

// HoraMinuto lee "HH:MM" (de 00:00 a 23:59).
func HoraMinuto(s string) (int, int, bool) {
	s = strings.TrimSpace(s)
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) < 1 || len(h) > 2 || len(m) != 2 {
		return 0, 0, false
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, 0, false
	}
	return hh, mm, true
}

// HoraDeCopias es la hora de la copia diaria (03:30 si no está o no se entiende).
func (c *Config) HoraDeCopias() (int, int) {
	if h, m, ok := HoraMinuto(c.CopiasHora); ok {
		return h, m
	}
	return 3, 30
}

// CopiasAGuardar son las diarias (al menos 1; 14 si no se dijo) y las mensuales que se guardan.
func (c *Config) CopiasAGuardar() (diarias, mensuales int) {
	diarias, mensuales = c.CopiasDiarias, max(0, c.CopiasMensuales)
	if diarias < 1 {
		diarias = 14
	}
	return diarias, mensuales
}

// IAConfigurada indica si hay un modelo de visión.
func (c *Config) IAConfigurada() bool { return c.LLMBaseURL != "" && c.LLMModelo != "" }

// EsHTTPS dice si la URL pública es https (las cookies van con Secure).
func (c *Config) EsHTTPS() bool { return strings.HasPrefix(strings.ToLower(c.URLPublica), "https://") }

// ArcanumConfigurado indica si se puede constatar.
func (c *Config) ArcanumConfigurado() bool { return c.ArcanumURL != "" && c.ArcanumAPIKey != "" }
