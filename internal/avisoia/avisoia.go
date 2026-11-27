// Package avisoia dice a quién le manda Factorum los comprobantes cuando usa la IA (visión) y qué
// pasa con esos datos según las páginas oficiales de cada proveedor. Una IA externa no se usa hasta
// que un superadmin acepta el aviso para ese host (y esa versión del texto); cada persona lo ve la
// primera vez que la va a usar.
package avisoia

import (
	"net"
	"net/url"
	"strings"

	"github.com/diegoparras/factorum/internal/config"
)

// Version del texto del aviso. Si cambia lo que dice, se sube: hay que aceptarlo y verlo de nuevo.
const Version = 1

// Fecha en que se tomaron los datos de las páginas oficiales.
const Fecha = "2026-11-27"

// Proveedor de la IA, según el host del endpoint.
type Proveedor string

const (
	Local       Proveedor = "local"
	OpenAI      Proveedor = "openai"
	Anthropic   Proveedor = "anthropic"
	Google      Proveedor = "google"
	OpenRouter  Proveedor = "openrouter"  // pasa el pedido al proveedor del modelo elegido
	Desconocido Proveedor = "desconocido" // externo, sin datos de sus políticas
)

// Deteccion es lo que se sabe del endpoint configurado.
type Deteccion struct {
	Proveedor Proveedor `json:"proveedor"`
	Nombre    string    `json:"nombre"`           // OpenAI, Anthropic, Google Gemini, el host
	Host      string    `json:"host"`             // en minúsculas, con el puerto si lo tiene
	Region    string    `json:"region,omitempty"` // código de la región de OpenAI (eu, us...)
	Local     bool      `json:"local"`
}

// Externa dice si los datos salen del servidor.
func (d Deteccion) Externa() bool { return d.Host != "" && !d.Local }

// Regiones de OpenAI con dominio propio (<código>.api.openai.com). Procesa: además de guardar, la
// región procesa los pedidos.
var Regiones = map[string]struct {
	Nombre  string
	Procesa bool
}{
	"us": {"Estados Unidos", true},
	"eu": {"Europa (EEE y Suiza)", true},
	"au": {"Australia", true},
	"jp": {"Japón", true},
	"in": {"India", true},
	"ae": {"Emiratos Árabes Unidos", true},
	"ca": {"Canadá", false},
	"sg": {"Singapur", false},
	"kr": {"Corea del Sur", false},
	"gb": {"Reino Unido", false},
}

// Detectar mira el host del endpoint (http://ollama:11434/v1, https://api.openai.com/v1...). Sin
// URL, devuelve una detección vacía.
func Detectar(base string) Deteccion {
	base = strings.TrimSpace(base)
	if base == "" {
		return Deteccion{}
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return Deteccion{Proveedor: Desconocido, Nombre: base, Host: strings.ToLower(base)}
	}
	host := strings.ToLower(u.Host)
	nombre := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	d := Deteccion{Host: host}
	switch {
	case esLocal(nombre):
		d.Proveedor, d.Nombre, d.Local = Local, "Local", true
	case nombre == "api.openai.com":
		d.Proveedor, d.Nombre = OpenAI, "OpenAI"
	case strings.HasSuffix(nombre, ".api.openai.com"):
		d.Proveedor, d.Nombre = OpenAI, "OpenAI"
		if r := strings.TrimSuffix(nombre, ".api.openai.com"); Regiones[r].Nombre != "" {
			d.Region = r
		}
	case nombre == "api.anthropic.com":
		d.Proveedor, d.Nombre = Anthropic, "Anthropic"
	case nombre == "generativelanguage.googleapis.com":
		d.Proveedor, d.Nombre = Google, "Google Gemini"
	case nombre == "openrouter.ai":
		d.Proveedor, d.Nombre = OpenRouter, "OpenRouter"
	default:
		d.Proveedor, d.Nombre = Desconocido, host
	}
	return d
}

// esLocal: el modelo corre en la misma máquina o en la red privada del servidor (Ollama en el mismo
// compose, LM Studio en la red de la oficina).
func esLocal(h string) bool {
	h = strings.Trim(h, "[]")
	if h == "" {
		return false
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
	}
	// Un nombre sin punto es un servicio de Docker o de la red local (ollama, lmstudio, vllm).
	return h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") || !strings.Contains(h, ".")
}

// Aceptacion es lo que se guarda cuando un superadmin acepta el aviso de una IA externa.
type Aceptacion struct {
	Host    string `json:"host"`
	Version int    `json:"version"`
	Por     string `json:"por,omitempty"`
	Cuando  string `json:"cuando,omitempty"`
}

// AceptacionDe lee la aceptación guardada en la configuración.
func AceptacionDe(c *config.Config) Aceptacion {
	return Aceptacion{Host: c.IAAvisoHost, Version: c.IAAvisoVersion, Por: c.IAAvisoPor, Cuando: c.IAAvisoCuando}
}

// Aceptado dice si hay una aceptación vigente para el endpoint de la configuración: la del mismo
// host y la versión actual del aviso. Una IA local no necesita aceptación.
func Aceptado(c *config.Config) bool {
	d := Detectar(c.LLMBaseURL)
	if d.Local {
		return true
	}
	return d.Host != "" && c.IAAvisoHost == d.Host && c.IAAvisoVersion == Version
}

// EnUso dice si Factorum puede usar la IA de la configuración: está configurada y es local o tiene
// el aviso aceptado. Si es externa y nadie lo aceptó, no se usa (ni en la pantalla, ni en los
// canales, ni por MCP).
func EnUso(c *config.Config) bool { return c.IAConfigurada() && Aceptado(c) }
