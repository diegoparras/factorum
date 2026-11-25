// Package extractor tiene las fuentes de datos de la cascada: el parser de texto, el OCR clásico y
// el modelo de visión. El QR vive en arca + qrscan porque no necesita configuración.
package extractor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Tesseract corre el binario tesseract (OCR clásico, sin IA generativa, determinístico).
type Tesseract struct {
	Idiomas string // "spa" por defecto
	Timeout time.Duration
}

// NuevoTesseract devuelve nil si tesseract no está instalado.
func NuevoTesseract(idiomas string, timeout time.Duration) *Tesseract {
	if _, err := exec.LookPath("tesseract"); err != nil {
		return nil
	}
	if idiomas == "" {
		idiomas = "spa"
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &Tesseract{Idiomas: idiomas, Timeout: timeout}
}

// Texto reconoce el texto de una imagen PNG. Usa el modo 6 (bloque uniforme), que conserva mejor
// las filas "etiqueta: valor" de un comprobante.
func (t *Tesseract) Texto(ctx context.Context, png []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "-l", t.Idiomas, "--psm", "6",
		"-c", "preserve_interword_spaces=1")
	cmd.Stdin = bytes.NewReader(png)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("tesseract tardó más de %s", t.Timeout)
		}
		return "", fmt.Errorf("tesseract: %v %s", err, strings.TrimSpace(errb.String()))
	}
	// Con preserve_interword_spaces las columnas quedan separadas por varios espacios, como en
	// pdftotext -layout: el mismo parser sirve para los dos.
	return out.String(), nil
}
