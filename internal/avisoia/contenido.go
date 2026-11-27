package avisoia

import "fmt"

// Contenido es el aviso armado para que la pantalla lo pinte. Los textos pueden traer **negrita**
// para los números que importan.
type Contenido struct {
	Version    int       `json:"version"`
	Fecha      string    `json:"fecha"`
	Titulo     string    `json:"titulo"`
	Intro      string    `json:"intro"`
	Alerta     *Alerta   `json:"alerta,omitempty"`
	Tarjetas   []Tarjeta `json:"tarjetas"`
	Fuentes    []Fuente  `json:"fuentes"`
	Pie        string    `json:"pie"`
	Sello      string    `json:"sello"`       // lo que va al lado de «Usar IA si hace falta»
	StoreFalse bool      `json:"store_false"` // Factorum le pide que no guarde la respuesta (store=false)
}

// Tarjeta es una de las cuatro preguntas: a dónde, cuánto tiempo, si entrenan y qué queda acá.
type Tarjeta struct {
	ID        string   `json:"id"` // donde, tiempo, entrenan, factorum (también es el ícono)
	Titulo    string   `json:"titulo"`
	Destacado string   `json:"destacado"`       // la respuesta corta, grande
	Tono      string   `json:"tono,omitempty"`  // ok, aviso o mal (el color del destacado)
	Puntos    []string `json:"puntos"`          // 1 a 3 líneas
	Notas     []string `json:"notas,omitempty"` // letra chica, también verificada
}

// Alerta es el recuadro que va arriba de las tarjetas cuando hay algo que mirar con cuidado.
type Alerta struct {
	Nivel  string `json:"nivel"` // aviso o mal
	Titulo string `json:"titulo"`
	Texto  string `json:"texto"`
}

// Fuente es una página oficial de donde salen los datos.
type Fuente struct {
	Titulo string `json:"titulo"`
	URL    string `json:"url"`
	Fecha  string `json:"fecha"` // cuándo se leyó
	Nota   string `json:"nota,omitempty"`
}

const fechaLarga = "27 de noviembre de 2026"

// Aviso arma el contenido para una detección. Cada afirmación sobre un proveedor sale de su página
// oficial (las fuentes, leídas en la Fecha); lo común lo hace Factorum y se puede ver en el código.
func Aviso(d Deteccion) Contenido {
	c := Contenido{Version: Version, Fecha: Fecha}
	if d.Host == "" {
		return c
	}
	if d.Local {
		c.Titulo = "IA local"
		c.Sello = "Local: las imágenes no salen de tu servidor"
		c.Intro = "El modelo corre en tu servidor o en su red privada (" + d.Host + "): las imágenes no salen de tu servidor."
		return c
	}
	nombre := d.Nombre
	c.Titulo = nombre
	c.Sello = "IA externa: " + nombre
	c.Intro = fmt.Sprintf("Cuando el QR, el texto del PDF y el OCR no alcanzan para leer un comprobante, Factorum le manda a %s la imagen de esa página (o su texto), junto con sus instrucciones. Solo esas páginas, no todo el lote, pero con todo lo que tengan: CUIT, razones sociales, importes y CAE.", nombreCorto(d))

	donde := Tarjeta{ID: "donde", Titulo: "A dónde viajan", Destacado: "Fuera de tu servidor", Tono: "aviso",
		Puntos: []string{fmt.Sprintf("A los servidores de %s (**%s**), fuera de tu servidor y fuera de Argentina.", nombreCorto(d), d.Host)}}
	factorum := Tarjeta{ID: "factorum", Titulo: "Qué queda en Factorum", Destacado: "Marcado «IA»", Tono: "ok",
		Puntos: []string{"Lo que la IA leyó queda marcado como **«IA»** para que una persona lo revise.",
			fmt.Sprintf("Factorum no le manda nada más a %s.", nombreCorto(d))}}
	pie := "Datos tomados de las páginas oficiales de cada proveedor el " + fechaLarga + ". Pueden cambiar: revisalos antes de usarlo con datos sensibles."

	switch d.Proveedor {
	case OpenAI:
		c.StoreFalse = true
		if r, ok := Regiones[d.Region]; ok {
			que := "procesa y guarda los datos en la región"
			if !r.Procesa {
				que = "solo guarda los datos en la región"
			}
			donde.Destacado = "Región: " + r.Nombre
			donde.Puntos = []string{
				fmt.Sprintf("A los servidores de OpenAI (**%s**), fuera de tu servidor.", d.Host),
				fmt.Sprintf("Con residencia de datos en **%s**, OpenAI %s.", r.Nombre, que),
			}
		} else {
			donde.Puntos = append(donde.Puntos, "Sin configurar residencia de datos, OpenAI no publica en esa página dónde los procesa.")
			donde.Notas = []string{"Con residencia de datos (para organizaciones elegibles, desde la configuración del proyecto) se puede elegir región. Estados Unidos, Europa (EEE y Suiza), Australia, Japón, India y Emiratos procesan y guardan en la región; Canadá, Singapur, Corea del Sur y Reino Unido solo guardan en la región. Se usa con el dominio regional, por ejemplo eu.api.openai.com."}
		}
		c.Tarjetas = []Tarjeta{donde,
			{ID: "tiempo", Titulo: "Cuánto tiempo se guardan", Destacado: "Hasta 30 días", Tono: "aviso",
				Puntos: []string{
					"Guarda registros de monitoreo de abuso **hasta 30 días**, salvo que la ley exija más o que sea razonablemente necesario para proteger sus servicios o a terceros.",
					"Factorum le pide a OpenAI que no guarde la respuesta (**store=false**). El endpoint que usa (/v1/chat/completions) no guarda estado de la aplicación.",
				},
				Notas: []string{
					"OpenAI analiza las imágenes que recibe para detectar material de abuso sexual infantil; si lo detecta, retiene la imagen para revisión manual.",
					"Retención cero (Zero Data Retention): existe, pero requiere aprobación previa de OpenAI (se pide a su equipo de ventas).",
				}},
			{ID: "entrenan", Titulo: "¿Entrenan con tus datos?", Destacado: "No", Tono: "ok",
				Puntos: []string{"Desde el **1 de marzo de 2023**, lo que se manda a la API no se usa para entrenar ni mejorar sus modelos, salvo que la organización lo habilite explícitamente."}},
			factorum}
		c.Fuentes = []Fuente{
			{Titulo: "OpenAI: Your data", URL: "https://developers.openai.com/api/docs/guides/your-data", Fecha: Fecha},
			{Titulo: "OpenAI: Data residency", URL: "https://developers.openai.com/api/docs/guides/data-residency", Fecha: Fecha},
		}
	case Anthropic:
		c.Tarjetas = []Tarjeta{donde,
			{ID: "tiempo", Titulo: "Cuánto tiempo se guardan", Destacado: "Hasta 30 días", Tono: "aviso",
				Puntos: []string{
					"Borra automáticamente lo que recibe y lo que genera **dentro de los 30 días**.",
					"Salvo servicios con retención que controla el cliente, un acuerdo distinto (por ejemplo, retención cero) o lo necesario para hacer cumplir su política de uso o cumplir la ley.",
				},
				Notas: []string{"Si un contenido se marca por violar la política de uso, puede guardarlo hasta **2 años** (y las puntuaciones de seguridad, hasta **7 años**)."}},
			{ID: "entrenan", Titulo: "¿Entrenan con tus datos?", Destacado: "No, por defecto", Tono: "ok",
				Puntos: []string{"Por defecto no usa lo que se manda por sus productos comerciales (incluida la API) para entrenar, salvo que se le envíe feedback."}},
			factorum}
		c.Fuentes = []Fuente{
			{Titulo: "Anthropic: How long do you store my organization's data?", URL: "https://privacy.claude.com/en/articles/7996866-how-long-do-you-store-my-organization-s-data", Fecha: Fecha},
			{Titulo: "Anthropic: Is my data used for model training?", URL: "https://privacy.claude.com/en/articles/7996868-is-my-data-used-for-model-training", Fecha: Fecha},
		}
	case Google:
		c.Alerta = &Alerta{Nivel: "mal", Titulo: "Si la clave es de un proyecto sin facturación, los comprobantes de tus clientes pueden usarse para mejorar productos de Google",
			Texto: "En el plan gratuito, Google usa lo que se manda y las respuestas para proveer, mejorar y desarrollar sus productos. Con el plan pago (un proyecto de Cloud con facturación activa), no los usa para mejorar sus productos."}
		donde.Notas = []string{"Puede guardarse de forma transitoria o en caché en cualquier país donde Google tenga instalaciones."}
		c.Tarjetas = []Tarjeta{donde,
			{ID: "tiempo", Titulo: "Cuánto tiempo se guardan", Destacado: "Un tiempo limitado", Tono: "aviso",
				Puntos: []string{"Guarda lo que se manda y las respuestas «por un tiempo limitado», solo para detectar abusos.", "**No publica cuántos días.**"}},
			{ID: "entrenan", Titulo: "¿Entrenan con tus datos?", Destacado: "Depende del plan", Tono: "mal",
				Puntos: []string{
					"**Plan pago** (proyecto de Cloud con facturación activa): no usa lo que se manda ni las respuestas para mejorar sus productos.",
					"**Plan gratuito**: sí lo usa para proveer, mejorar y desarrollar sus productos.",
				}},
			factorum}
		c.Fuentes = []Fuente{{Titulo: "Términos adicionales de la API de Gemini", URL: "https://ai.google.dev/gemini-api/terms", Fecha: Fecha,
			Nota: "Hoy redirige a aistudio.google.com/docs/terms. Vigentes desde el 23/3/2026."}}
	default:
		c.Alerta = &Alerta{Nivel: "aviso", Titulo: "Factorum no conoce las políticas de " + d.Host,
			Texto: "Antes de usarlo, revisá sus términos: si guarda los datos, cuánto tiempo, si entrena con ellos y en qué país los procesa."}
		donde.Destacado = "Fuera de tu servidor"
		donde.Puntos = []string{fmt.Sprintf("A los servidores de **%s**, fuera de tu servidor.", d.Host), "En qué país los procesa: revisalo en sus términos."}
		c.Tarjetas = []Tarjeta{donde,
			{ID: "tiempo", Titulo: "Cuánto tiempo se guardan", Destacado: "Sin datos", Tono: "aviso",
				Puntos: []string{"Revisá en sus términos si guarda los datos y cuánto tiempo."}},
			{ID: "entrenan", Titulo: "¿Entrenan con tus datos?", Destacado: "Sin datos", Tono: "aviso",
				Puntos: []string{"Revisá en sus términos si entrena con ellos."}},
			factorum}
		pie = "Factorum no tiene datos verificados de este proveedor."
	}
	c.Pie = pie
	if c.Fuentes == nil {
		c.Fuentes = []Fuente{}
	}
	return c
}

func nombreCorto(d Deteccion) string {
	if d.Proveedor == Google {
		return "Google"
	}
	return d.Nombre
}
