// Package iaproveedor es la lista de proveedores de IA (visión) que se eligen en Configuración: la
// URL de cada uno, si pide clave y dónde se saca, cómo se le pide la lista de modelos (filtrando
// lo que no lee imágenes) y los modelos que Factorum probó con sus tickets.
//
// Lo que decide si los datos salen del servidor (y si hace falta el aviso) no es el proveedor
// elegido sino el host de la URL: eso lo mira avisoia.
package iaproveedor

import (
	"net/url"
	"slices"
	"strings"

	"github.com/diegoparras/factorum/internal/avisoia"
)

// Proveedores (el valor de ia.proveedor y de FACTORUM_IA_PROVEEDOR).
const (
	Ollama     = "ollama"
	OpenAI     = "openai"
	Anthropic  = "anthropic"
	Gemini     = "gemini"
	OpenRouter = "openrouter"
	Otro       = "otro"
)

// Proveedor es lo que la pantalla necesita para armar los campos.
type Proveedor struct {
	ID     string `json:"id"`
	Nombre string `json:"nombre"`
	Corto  string `json:"corto"` // para los mensajes: «La clave no es válida para OpenAI»
	URL    string `json:"url"`   // la de siempre; vacía en Otro
	// Editable: la URL se escribe (Ollama puede estar en otra máquina; Otro, en cualquier lado).
	Editable bool `json:"editable"`
	// Clave: "si", "no" (Ollama) u "opcional" (Otro).
	Clave      string   `json:"clave"`
	ClaveURL   string   `json:"clave_url,omitempty"`   // dónde se saca
	ClaveDonde string   `json:"clave_donde,omitempty"` // el texto del link
	Regiones   []Region `json:"regiones,omitempty"`    // OpenAI: residencia de datos
	Ayuda      string   `json:"ayuda"`
}

// Region es un dominio regional de OpenAI (residencia de datos).
type Region struct {
	Codigo string `json:"codigo"` // vacío: global
	Nombre string `json:"nombre"`
	URL    string `json:"url"`
}

// Lista de proveedores, en el orden del select.
var Lista = []Proveedor{
	{ID: Ollama, Nombre: "Ollama (en tu servidor)", Corto: "Ollama", URL: "http://ollama:11434/v1", Editable: true, Clave: "no",
		Ayuda: "Corre en tu servidor: las imágenes no salen. Es más lento (en un servidor sin GPU, cerca de un minuto por imagen)."},
	{ID: OpenAI, Nombre: "OpenAI", Corto: "OpenAI", URL: "https://api.openai.com/v1", Clave: "si",
		ClaveURL: "https://platform.openai.com/api-keys", ClaveDonde: "platform.openai.com, API keys", Regiones: regionesOpenAI(),
		Ayuda: "Rápido y el que mejor leyó los tickets en la prueba de Factorum. Los comprobantes que necesiten IA salen de tu servidor."},
	{ID: Anthropic, Nombre: "Anthropic (Claude)", Corto: "Anthropic", URL: "https://api.anthropic.com/v1", Clave: "si",
		ClaveURL: "https://platform.claude.com/settings/keys", ClaveDonde: "platform.claude.com, API keys",
		Ayuda: "Se usa por su endpoint compatible con OpenAI. Los comprobantes que necesiten IA salen de tu servidor."},
	{ID: Gemini, Nombre: "Google Gemini", Corto: "Google Gemini", URL: "https://generativelanguage.googleapis.com/v1beta/openai", Clave: "si",
		ClaveURL: "https://aistudio.google.com/app/apikey", ClaveDonde: "Google AI Studio, Get API key",
		Ayuda: "Usá una clave de un proyecto con facturación: en el plan gratuito, Google puede usar lo que le mandás."},
	{ID: OpenRouter, Nombre: "OpenRouter", Corto: "OpenRouter", URL: "https://openrouter.ai/api/v1", Clave: "si",
		ClaveURL: "https://openrouter.ai/settings/keys", ClaveDonde: "openrouter.ai, Keys",
		Ayuda: "Una sola clave para modelos de muchos proveedores. Le pasa cada pedido al proveedor del modelo que elijas."},
	{ID: Otro, Nombre: "Otro compatible con OpenAI", Corto: "el proveedor", Editable: true, Clave: "opcional",
		Ayuda: "LM Studio, vLLM, un gateway o cualquier servidor con la API de chat de OpenAI. La URL suele terminar en /v1."},
}

// ordenRegiones: las que procesan en la región primero, como las lista OpenAI.
var ordenRegiones = []string{"us", "eu", "au", "jp", "in", "ae", "ca", "sg", "kr", "gb"}

func regionesOpenAI() []Region {
	rs := []Region{{Codigo: "", Nombre: "Global (sin residencia de datos)", URL: "https://api.openai.com/v1"}}
	for _, c := range ordenRegiones {
		r := avisoia.Regiones[c]
		n := r.Nombre
		if !r.Procesa {
			n += " (solo guarda ahí)"
		}
		rs = append(rs, Region{Codigo: c, Nombre: n, URL: URLRegion(c)})
	}
	return rs
}

// URLRegion es la URL de OpenAI para una región ("" = global).
func URLRegion(codigo string) string {
	if codigo == "" {
		return "https://api.openai.com/v1"
	}
	return "https://" + codigo + ".api.openai.com/v1"
}

// Por busca un proveedor.
func Por(id string) (Proveedor, bool) {
	i := slices.IndexFunc(Lista, func(p Proveedor) bool { return p.ID == id })
	if i < 0 {
		return Proveedor{}, false
	}
	return Lista[i], true
}

// Nombre corto de un proveedor para los mensajes.
func Nombre(id string) string {
	if p, ok := Por(id); ok {
		return p.Corto
	}
	return "el proveedor"
}

// Del: «de OpenAI», «del proveedor».
func Del(id string) string {
	if n, ok := strings.CutPrefix(Nombre(id), "el "); ok {
		return "del " + n
	}
	return "de " + Nombre(id)
}

// Deducir saca el proveedor de la URL, para las configuraciones de antes de elegirlo de una lista
// (o las que vienen solo con LLM_API_BASE_URL). Sin URL, vacío.
func Deducir(base string) string {
	d := avisoia.Detectar(base)
	switch {
	case d.Host == "":
		return ""
	case d.Proveedor == avisoia.OpenAI:
		return OpenAI
	case d.Proveedor == avisoia.Anthropic:
		return Anthropic
	case d.Proveedor == avisoia.Google:
		return Gemini
	case d.Proveedor == avisoia.OpenRouter:
		return OpenRouter
	case esOllama(base):
		return Ollama
	}
	return Otro
}

// esOllama: el puerto de Ollama o «ollama» en el nombre del host (ollama, ollama.internal...).
func esOllama(base string) bool {
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	return u.Port() == "11434" || strings.Contains(strings.ToLower(u.Hostname()), "ollama")
}

// Compatible dice si la URL es de ese proveedor. Los de URL fija (OpenAI, Anthropic, Gemini y
// OpenRouter) solo aceptan su host (OpenAI, también los regionales); Ollama y Otro, cualquiera.
func Compatible(id, base string) bool {
	d := avisoia.Detectar(base)
	switch id {
	case OpenAI:
		return d.Proveedor == avisoia.OpenAI
	case Anthropic:
		return d.Proveedor == avisoia.Anthropic
	case Gemini:
		return d.Proveedor == avisoia.Google
	case OpenRouter:
		return d.Proveedor == avisoia.OpenRouter
	case Ollama, Otro:
		return true
	}
	return false
}

// Efectivo es el proveedor que se muestra: el elegido, si corresponde con la URL; si no (o si no
// se eligió nunca), el que se deduce de la URL.
func Efectivo(elegido, base string) string {
	if _, ok := Por(elegido); ok && (base == "" || Compatible(elegido, base)) {
		return elegido
	}
	return Deducir(base)
}

// MismoHost dice si dos URLs apuntan al mismo host (y puerto). Una clave guardada solo se manda al
// host para el que se guardó.
func MismoHost(a, b string) bool {
	da, db := avisoia.Detectar(a), avisoia.Detectar(b)
	return da.Host != "" && da.Host == db.Host
}
