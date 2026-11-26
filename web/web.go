// Package web embebe la interfaz (HTML, CSS y JS sin build).
package web

import "embed"

//go:embed index.html app.js estilo.css tema.js escriba-ui.css logo.svg favicon.svg apple-touch-icon.png vendor/fonts
var Archivos embed.FS
