// Package pdfx lee PDFs con poppler (pdftotext, pdfimages, pdftoppm, pdfinfo): probado en
// millones de PDFs, rápido y sin cgo. Cada llamada corre en su propio proceso con timeout, así un
// PDF roto no tira abajo el servidor.
package pdfx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Motor abstrae el lector de PDF (hoy poppler; mañana pdfium sin cambiar el resto).
type Motor interface {
	// Abrir prepara el PDF para varias lecturas. Hay que llamar a Cerrar.
	Abrir(ctx context.Context, pdf []byte) (Documento, error)
	Disponible() error
}

// Documento es un PDF abierto.
type Documento interface {
	Paginas() int
	// Texto por página (índice 0 = página 1), respetando el diseño en columnas.
	Texto(ctx context.Context, maxPaginas int) ([]string, error)
	// Imagenes embebidas por página (sin rasterizar): el QR de ARCA suele estar ahí, intacto.
	Imagenes(ctx context.Context, maxPaginas int) (map[int][]image.Image, error)
	// Rasterizar una página (desde 1) a la resolución pedida, en escala de grises si gris.
	Rasterizar(ctx context.Context, pagina, dpi int, gris bool) (image.Image, []byte, error)
	Cerrar()
}

// Poppler implementa Motor con los binarios de poppler-utils.
type Poppler struct {
	Timeout time.Duration
	TmpDir  string
}

// NuevoPoppler con un timeout por operación.
func NuevoPoppler(timeout time.Duration) *Poppler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Poppler{Timeout: timeout}
}

func (p *Poppler) Disponible() error {
	for _, b := range []string{"pdftotext", "pdfimages", "pdftoppm", "pdfinfo"} {
		if _, err := exec.LookPath(b); err != nil {
			return fmt.Errorf("falta %s (instalá poppler-utils)", b)
		}
	}
	return nil
}

type docPoppler struct {
	p       *Poppler
	dir     string
	archivo string
	paginas int
}

var ErrNoEsPDF = errors.New("el archivo no es un PDF")

func (p *Poppler) Abrir(ctx context.Context, pdf []byte) (Documento, error) {
	if !bytes.HasPrefix(bytes.TrimLeft(pdf[:min(len(pdf), 1024)], "\x00\t\r\n "), []byte("%PDF")) {
		return nil, ErrNoEsPDF
	}
	dir, err := os.MkdirTemp(p.TmpDir, "contador-pdf-")
	if err != nil {
		return nil, err
	}
	d := &docPoppler{p: p, dir: dir, archivo: filepath.Join(dir, "doc.pdf")}
	if err := os.WriteFile(d.archivo, pdf, 0o600); err != nil {
		d.Cerrar()
		return nil, err
	}
	out, err := d.correr(ctx, "pdfinfo", d.archivo)
	if err != nil {
		d.Cerrar()
		return nil, fmt.Errorf("PDF ilegible: %w", err)
	}
	if m := regexp.MustCompile(`(?m)^Pages:\s+(\d+)`).FindSubmatch(out); m != nil {
		d.paginas, _ = strconv.Atoi(string(m[1]))
	}
	if d.paginas == 0 {
		d.Cerrar()
		return nil, errors.New("PDF sin páginas")
	}
	return d, nil
}

func (d *docPoppler) Paginas() int { return d.paginas }

func (d *docPoppler) Cerrar() { _ = os.RemoveAll(d.dir) }

func (d *docPoppler) correr(ctx context.Context, bin string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, d.p.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("%s: tardó más de %s", bin, d.p.Timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("%s: %v %s", bin, err, msg)
	}
	return out.Bytes(), nil
}

func (d *docPoppler) ultima(max int) int {
	if max <= 0 || max > d.paginas {
		return d.paginas
	}
	return max
}

func (d *docPoppler) Texto(ctx context.Context, maxPaginas int) ([]string, error) {
	l := d.ultima(maxPaginas)
	out, err := d.correr(ctx, "pdftotext", "-layout", "-enc", "UTF-8", "-f", "1", "-l", strconv.Itoa(l), d.archivo, "-")
	if err != nil {
		return nil, err
	}
	pags := strings.Split(string(out), "\f")
	// pdftotext termina con un \f: la última "página" vacía sobra.
	if len(pags) > l {
		pags = pags[:l]
	}
	for len(pags) < l {
		pags = append(pags, "")
	}
	return pags, nil
}

var nombreImagen = regexp.MustCompile(`^img-(\d+)-(\d+)\.(png|jpg|ppm|pbm|tif|jp2|jb2e|ccitt)$`)

func (d *docPoppler) Imagenes(ctx context.Context, maxPaginas int) (map[int][]image.Image, error) {
	l := d.ultima(maxPaginas)
	sub := filepath.Join(d.dir, "img")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		return nil, err
	}
	if _, err := d.correr(ctx, "pdfimages", "-p", "-png", "-f", "1", "-l", strconv.Itoa(l), d.archivo, filepath.Join(sub, "img")); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(sub)
	if err != nil {
		return nil, err
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name() < ents[j].Name() })
	res := map[int][]image.Image{}
	for _, e := range ents {
		m := nombreImagen.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		pag, _ := strconv.Atoi(m[1])
		f, err := os.Open(filepath.Join(sub, e.Name()))
		if err != nil {
			continue
		}
		img, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			continue
		}
		res[pag] = append(res[pag], img)
	}
	return res, nil
}

func (d *docPoppler) Rasterizar(ctx context.Context, pagina, dpi int, gris bool) (image.Image, []byte, error) {
	if pagina < 1 || pagina > d.paginas {
		return nil, nil, fmt.Errorf("página %d fuera de rango", pagina)
	}
	if dpi <= 0 {
		dpi = 150
	}
	pref := filepath.Join(d.dir, fmt.Sprintf("r-%d-%d", pagina, dpi))
	args := []string{"-r", strconv.Itoa(dpi), "-f", strconv.Itoa(pagina), "-l", strconv.Itoa(pagina), "-png", "-singlefile"}
	if gris {
		args = append(args, "-gray")
	}
	args = append(args, d.archivo, pref)
	if _, err := d.correr(ctx, "pdftoppm", args...); err != nil {
		return nil, nil, err
	}
	b, err := os.ReadFile(pref + ".png")
	if err != nil {
		return nil, nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	return img, b, nil
}
