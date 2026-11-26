// Package factorum expone lo que vive en la raíz del repo y necesita el binario: por ahora, las
// novedades (CHANGELOG.md), que la UI muestra después de cada actualización.
package factorum

import (
	_ "embed"
	"regexp"
	"strings"
)

//go:embed CHANGELOG.md
var changelog string

// Novedad es una versión del CHANGELOG.
type Novedad struct {
	Version string `json:"version"`
	Fecha   string `json:"fecha"`
	Cuerpo  string `json:"cuerpo"` // markdown simple: ### títulos, - viñetas, **negrita**
}

var reVersion = regexp.MustCompile(`^## \[([0-9][0-9.]*)\](?:\s*-\s*(\S+))?`)

// Novedades devuelve las versiones del CHANGELOG, de la más nueva a la más vieja.
func Novedades() []Novedad {
	var res []Novedad
	var actual *Novedad
	var cuerpo strings.Builder
	cerrar := func() {
		if actual != nil {
			actual.Cuerpo = strings.TrimSpace(cuerpo.String())
			res = append(res, *actual)
		}
		cuerpo.Reset()
	}
	for _, l := range strings.Split(changelog, "\n") {
		if m := reVersion.FindStringSubmatch(l); m != nil {
			cerrar()
			actual = &Novedad{Version: m[1], Fecha: m[2]}
			continue
		}
		if actual != nil {
			cuerpo.WriteString(l)
			cuerpo.WriteString("\n")
		}
	}
	cerrar()
	return res
}
