package iaproveedor

import (
	"fmt"
	"math"
	"strings"
)

// La prueba de Factorum de donde salen los recomendados. Para actualizarlos, cambiá esta tabla (y,
// si cambia la prueba, estos textos).
const (
	PruebaFecha = "2026-11-27"
	PruebaTexto = "Prueba chica de Factorum del 27/11/2026: 8 tickets de compra (4 limpios y 4 fotos movidas), con el mismo pedido que hace Factorum. Los tiempos de Ollama son en un servidor de 4 núcleos sin GPU; con GPU, mucho menos."
)

// Recomendado es un modelo que Factorum probó con sus tickets.
type Recomendado struct {
	Proveedor string `json:"proveedor"`
	Modelo    string `json:"modelo"`
	Campos    int    `json:"campos"`   // campos bien leídos
	Total     int    `json:"total"`    // campos probados
	Segundos  int    `json:"segundos"` // por imagen, aproximado
	Mitad     bool   `json:"mitad"`    // se probó con la mitad de los tickets
	CPU       bool   `json:"cpu"`      // tiempo en el servidor de 4 núcleos sin GPU

	Porcentaje int    `json:"porcentaje"`
	Corto      string `json:"corto"` // para la opción: «100 % · ~7 s»
	Texto      string `json:"texto"` // «Probado por Factorum: 100 % · ~7 s por imagen»
	Nota       string `json:"nota,omitempty"`
}

// tabla: lo medido el 27/11/2026. El orden es el de la pantalla (el mejor primero).
var tabla = []Recomendado{
	{Proveedor: OpenAI, Modelo: "gpt-6.1-terra", Campos: 56, Total: 56, Segundos: 7},
	{Proveedor: OpenAI, Modelo: "gpt-6-luna", Campos: 53, Total: 56, Segundos: 4},
	{Proveedor: Ollama, Modelo: "qwen3-vl:4b-instruct", Campos: 44, Total: 56, Segundos: 73, CPU: true},
	{Proveedor: Ollama, Modelo: "gemma4:e4b", Campos: 17, Total: 28, Segundos: 225, CPU: true, Mitad: true},
}

// Recomendados es la tabla con los textos armados.
var Recomendados = func() []Recomendado {
	rs := make([]Recomendado, len(tabla))
	for i, r := range tabla {
		rs[i] = armar(r)
	}
	return rs
}()

func armar(r Recomendado) Recomendado {
	r.Porcentaje = int(math.Round(float64(r.Campos) * 100 / float64(r.Total)))
	t := "~" + duracion(r.Segundos)
	r.Corto = fmt.Sprintf("%d %% · %s", r.Porcentaje, t)
	pct := fmt.Sprintf("%d %%", r.Porcentaje)
	if r.Mitad {
		pct += " (en la mitad de la prueba)"
	}
	por := t + " por imagen"
	if r.CPU {
		por += " en 4 núcleos sin GPU"
	}
	r.Texto = "Probado por Factorum: " + pct + " · " + por
	r.Nota = fmt.Sprintf("Leyó bien %d de %d campos.", r.Campos, r.Total)
	if r.Mitad {
		r.Nota = fmt.Sprintf("Leyó bien %d de %d campos: se probó con la mitad de los tickets, porque tarda mucho en un servidor sin GPU.", r.Campos, r.Total)
	}
	return r
}

// duracion: «7 s», «73 s», «3 min 45 s».
func duracion(s int) string {
	if s < 120 {
		return fmt.Sprintf("%d s", s)
	}
	if s%60 == 0 {
		return fmt.Sprintf("%d min", s/60)
	}
	return fmt.Sprintf("%d min %d s", s/60, s%60)
}

// RecomendadosDe son los recomendados de un proveedor. En OpenRouter, los de OpenAI con su nombre
// de OpenRouter (openai/gpt-6.1-terra), aclarando que se midieron con OpenAI directo.
func RecomendadosDe(prov string) []Recomendado {
	var res []Recomendado
	for _, r := range Recomendados {
		switch {
		case r.Proveedor == prov:
			res = append(res, r)
		case prov == OpenRouter && r.Proveedor == OpenAI:
			r.Modelo = "openai/" + r.Modelo
			r.Texto += " (medido con OpenAI directo)"
			res = append(res, r)
		}
	}
	return res
}

// RecomendadoDe busca si un modelo de ese proveedor está entre los probados.
func RecomendadoDe(prov, modelo string) *Recomendado {
	for _, r := range RecomendadosDe(prov) {
		if strings.EqualFold(r.Modelo, strings.TrimSpace(modelo)) {
			return &r
		}
	}
	return nil
}
