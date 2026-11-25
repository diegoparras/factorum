// Package web embebe la interfaz (HTML, CSS y JS sin build).
package web

import "embed"

//go:embed index.html app.js estilo.css
var Archivos embed.FS
