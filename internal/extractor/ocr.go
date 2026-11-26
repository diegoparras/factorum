// Package extractor tiene las fuentes de datos de la cascada: el parser de texto, el OCR clásico y
// el modelo de visión. El QR vive en arca + qrscan porque no necesita configuración.
package extractor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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
	return t.TextoPSM(ctx, png, 6)
}

// TextoPSM reconoce el texto con un modo de segmentación de página dado: 6 (un bloque uniforme) o
// 4 (una columna de texto de tamaño variable, que suele andar mejor con los tickets).
func (t *Tesseract) TextoPSM(ctx context.Context, png []byte, psm int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "tesseract", "stdin", "stdout", "-l", t.Idiomas, "--psm", strconv.Itoa(psm),
		"-c", "preserve_interword_spaces=1")
	cmd.Stdin = bytes.NewReader(png)
	// Un hilo por proceso: el paralelismo lo maneja el semáforo de OCR del pipeline. Con los hilos de
	// OpenMP, un tesseract solo tarda cuatro veces más y varios a la vez se traban entre sí.
	cmd.Env = append(os.Environ(), "OMP_THREAD_LIMIT=1")
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
