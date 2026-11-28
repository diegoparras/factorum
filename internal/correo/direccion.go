package correo

import (
	"net/mail"
	"regexp"
	"strings"
)

// entreParentesis: "Factorum (facturas@estudio.com.ar)", un error común al escribir el remitente.
var entreParentesis = regexp.MustCompile(`^\s*(.*?)\s*\(\s*([^()\s]+@[^()\s]+)\s*\)\s*$`)

// ProblemaDireccion dice qué tiene mal una dirección de correo con nombre opcional
// ("Factorum <facturas@estudio.com.ar>" o "facturas@estudio.com.ar"), en palabras para la
// pantalla y con la forma correcta cuando se puede adivinar. "" si está bien o está vacía.
func ProblemaDireccion(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := mail.ParseAddress(s); err == nil {
		return ""
	}
	if m := entreParentesis.FindStringSubmatch(s); m != nil {
		bien := m[2]
		if m[1] != "" {
			bien = m[1] + " <" + m[2] + ">"
		}
		if _, err := mail.ParseAddress(bien); err == nil {
			return "la dirección va entre < > y no entre paréntesis. Escribila así: " + bien
		}
	}
	return "no es una dirección válida. Escribila así: Factorum <facturas@estudio.com.ar> (o solo facturas@estudio.com.ar)"
}

// errRemitente es el error de un remitente mal escrito, con la explicación de ProblemaDireccion.
func errRemitente(s string) error {
	p := ProblemaDireccion(s)
	if p == "" {
		p = "no es una dirección válida"
	}
	return &errTexto{"el remitente «" + strings.TrimSpace(s) + "»: " + p}
}

type errTexto struct{ s string }

func (e *errTexto) Error() string { return e.s }
