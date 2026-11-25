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

type Config struct {
	Puerto       int
	DatabaseURL  string
	APIKey       string
	APIKeyLect   string
	DirDatos     string // archivos en disco cuando no hay R2
	ClaveMaestra []byte // 32 bytes para cifrar lo que se guarda (AES-256-GCM)

	Workers      int // documentos procesados en paralelo
	MaxPaginas   int
	MaxArchivo   int64 // bytes por archivo
	ConcOCR      int
	ConcIA       int
	IAPorDefecto bool // usar el modelo cuando hace falta, si no se dice otra cosa

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

	// Arcanum.
	ArcanumURL         string
	ArcanumAPIKey      string
	ArcanumConsultante string
	ArcanumTimeout     time.Duration

	// Canales.
	TelegramToken      string
	TelegramPermitidos []string // ids de usuario o chat; vacío = nadie
	WhatsApp           bool
	WhatsAppPermitidos []string // teléfonos (solo dígitos); vacío = nadie
	WebhookToken       string
	WebhookSalidaURL   string
	CanalClienteCUIT   map[string]string // remitente -> CUIT del cliente (FACTORUM_CANAL_CLIENTES="5491155550000=20222222223,...")
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
		Puerto:       envInt("FACTORUM_PUERTO", 8095),
		DatabaseURL:  env("FACTORUM_DATABASE_URL", env("DATABASE_URL", "")),
		APIKey:       env("FACTORUM_API_KEY", ""),
		APIKeyLect:   env("FACTORUM_API_KEY_LECTURA", ""),
		DirDatos:     env("FACTORUM_DATOS", "/data"),
		Workers:      envInt("FACTORUM_WORKERS", runtime.NumCPU()),
		MaxPaginas:   envInt("FACTORUM_MAX_PAGINAS", 30),
		MaxArchivo:   int64(envInt("FACTORUM_MAX_MB", 25)) << 20,
		ConcOCR:      envInt("FACTORUM_OCR_CONCURRENCIA", max(1, runtime.NumCPU()/2)),
		ConcIA:       envInt("FACTORUM_IA_CONCURRENCIA", 1),
		IAPorDefecto: envBool("FACTORUM_IA_POR_DEFECTO", true),
		LLMBaseURL:   env("LLM_API_BASE_URL", ""),
		LLMAPIKey:    env("LLM_API_KEY", ""),
		LLMModelo:    env("LLM_MODEL", ""),
		LLMTimeout:   time.Duration(envInt("LLM_TIMEOUT_S", 180)) * time.Second,
		OCRIdiomas:   env("FACTORUM_OCR_IDIOMAS", "spa"),

		R2Endpoint:  env("FACTORUM_R2_ENDPOINT", ""),
		R2Bucket:    env("FACTORUM_R2_BUCKET", ""),
		R2AccessKey: env("FACTORUM_R2_ACCESS_KEY", ""),
		R2SecretKey: env("FACTORUM_R2_SECRET_KEY", ""),
		R2Region:    env("FACTORUM_R2_REGION", "auto"),
		R2Prefijo:   env("FACTORUM_R2_PREFIJO", "factorum/"),

		ArcanumURL:         strings.TrimRight(env("ARCANUM_URL", ""), "/"),
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
	if c.R2Endpoint != "" && (c.R2Bucket == "" || c.R2AccessKey == "" || c.R2SecretKey == "") {
		return nil, fmt.Errorf("R2 a medias: con FACTORUM_R2_ENDPOINT hacen falta FACTORUM_R2_BUCKET, FACTORUM_R2_ACCESS_KEY y FACTORUM_R2_SECRET_KEY")
	}
	if c.R2Endpoint != "" && c.ClaveMaestra == nil {
		return nil, fmt.Errorf("para guardar en R2 hace falta FACTORUM_MASTER_KEY (openssl rand -hex 32): los comprobantes se cifran antes de salir")
	}
	if c.LLMBaseURL != "" && c.LLMModelo == "" {
		return nil, fmt.Errorf("con LLM_API_BASE_URL hace falta LLM_MODEL (por ejemplo qwen2.5vl:7b)")
	}
	if c.Workers < 1 {
		c.Workers = 1
	}
	return c, nil
}

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

// IAConfigurada indica si hay un modelo de visión.
func (c *Config) IAConfigurada() bool { return c.LLMBaseURL != "" && c.LLMModelo != "" }

// ArcanumConfigurado indica si se puede constatar.
func (c *Config) ArcanumConfigurado() bool { return c.ArcanumURL != "" && c.ArcanumAPIKey != "" }
