// Package qrscan encuentra y lee códigos QR en imágenes.
//
// gozxing (puro Go) falla con QR cuyo módulo mide una cantidad no entera de píxeles, que es
// justo lo que pasa con el QR embebido en un PDF (220 px para 73 módulos). Por eso, antes de
// decodificar, "normalizamos" las imágenes puras a una grilla exacta de módulos. Para fotos se
// prueban varias escalas y, si está instalado, zbarimg como último recurso.
package qrscan

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os/exec"
	"strings"
	"time"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	xdraw "golang.org/x/image/draw"
)

// Leer intenta decodificar un QR en la imagen.
func Leer(img image.Image) (string, bool) {
	if img == nil {
		return "", false
	}
	b := img.Bounds()
	if b.Dx() < 21 || b.Dy() < 21 {
		return "", false
	}
	gris := aGris(img)
	// 1) Imagen pura (QR embebido o recorte limpio): normalizar a la grilla de módulos.
	if n := normalizarPuro(gris); n != nil {
		if s, ok := intentar(n); ok {
			return s, true
		}
	}
	// 2) Detector normal en varias escalas.
	for _, f := range escalas(b) {
		im := image.Image(gris)
		if f != 1 {
			im = escalar(gris, f)
		}
		if s, ok := intentar(im); ok {
			return s, true
		}
	}
	// 3) Página entera: buscar en los cuadrantes (el QR de ARCA va abajo a la izquierda).
	if b.Dx() >= 600 || b.Dy() >= 600 {
		w, h := b.Dx(), b.Dy()
		zonas := []image.Rectangle{
			image.Rect(0, h/2, w/2+w/8, h),
			image.Rect(0, 0, w/2+w/8, h/2+h/8),
			image.Rect(w/2-w/8, h/2, w, h),
			image.Rect(w/2-w/8, 0, w, h/2+h/8),
		}
		for _, z := range zonas {
			sub := gris.SubImage(z.Add(gris.Bounds().Min)).(*image.Gray)
			if n := recortarQR(sub); n != nil {
				if nn := normalizarPuro(n); nn != nil {
					if s, ok := intentar(nn); ok {
						return s, true
					}
				}
			}
			for _, f := range []float64{1, 1.5} {
				im := image.Image(sub)
				if f != 1 {
					im = escalar(sub, f)
				}
				if s, ok := intentar(im); ok {
					return s, true
				}
			}
		}
	}
	// 4) zbar, si está.
	return zbar(gris)
}

func escalas(b image.Rectangle) []float64 {
	lado := max(b.Dx(), b.Dy())
	switch {
	case lado < 400:
		return []float64{1, 1.5, 2.5, 3.5}
	case lado > 2500:
		return []float64{0.5, 0.75, 1}
	default:
		return []float64{1, 1.5, 0.75}
	}
}

var hints = map[gozxing.DecodeHintType]interface{}{
	gozxing.DecodeHintType_TRY_HARDER:       true,
	gozxing.DecodeHintType_POSSIBLE_FORMATS: []gozxing.BarcodeFormat{gozxing.BarcodeFormat_QR_CODE},
}

func intentar(img image.Image) (s string, ok bool) {
	defer func() {
		// gozxing puede entrar en pánico con imágenes degeneradas: lo tomamos como "no hay QR".
		if r := recover(); r != nil {
			s, ok = "", false
		}
	}()
	src := gozxing.NewLuminanceSourceFromImage(img)
	lector := qrcode.NewQRCodeReader()
	for _, bin := range []gozxing.Binarizer{gozxing.NewHybridBinarizer(src), gozxing.NewGlobalHistgramBinarizer(src)} {
		bmp, err := gozxing.NewBinaryBitmap(bin)
		if err != nil {
			continue
		}
		if r, err := lector.Decode(bmp, hints); err == nil {
			return r.GetText(), true
		}
	}
	return "", false
}

func aGris(img image.Image) *image.Gray {
	if g, ok := img.(*image.Gray); ok {
		return g
	}
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	// Fondo blanco por si la imagen tiene transparencia.
	draw.Draw(g, g.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(g, g.Bounds(), img, b.Min, draw.Over)
	return g
}

func escalar(img *image.Gray, f float64) image.Image {
	b := img.Bounds()
	w, h := int(float64(b.Dx())*f), int(float64(b.Dy())*f)
	m := 16
	dst := image.NewGray(image.Rect(0, 0, w+2*m, h+2*m))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	xdraw.CatmullRom.Scale(dst, image.Rect(m, m, m+w, m+h), img, b, draw.Over, nil)
	return dst
}

// umbral de Otsu sobre el histograma.
func otsu(g *image.Gray) uint8 {
	var hist [256]int
	b := g.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		off := g.PixOffset(b.Min.X, y)
		for x := 0; x < b.Dx(); x++ {
			hist[g.Pix[off+x]]++
		}
	}
	total := b.Dx() * b.Dy()
	sum := 0.0
	for i := 0; i < 256; i++ {
		sum += float64(i * hist[i])
	}
	sumB, wB, mejor, umbral := 0.0, 0, 0.0, 128
	for t := 0; t < 256; t++ {
		wB += hist[t]
		if wB == 0 {
			continue
		}
		wF := total - wB
		if wF == 0 {
			break
		}
		sumB += float64(t * hist[t])
		mB, mF := sumB/float64(wB), (sum-sumB)/float64(wF)
		v := float64(wB) * float64(wF) * (mB - mF) * (mB - mF)
		if v > mejor {
			mejor, umbral = v, t
		}
	}
	return uint8(umbral)
}

// recortarQR busca la caja de píxeles oscuros: sirve cuando la zona tiene solo el QR y blanco.
func recortarQR(g *image.Gray) *image.Gray {
	b := g.Bounds()
	u := otsu(g)
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if g.GrayAt(x, y).Y <= u {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return nil
	}
	w, h := maxX-minX+1, maxY-minY+1
	if w < 21 || h < 21 || math.Abs(float64(w)/float64(h)-1) > 0.15 {
		return nil
	}
	return g.SubImage(image.Rect(minX, minY, maxX+1, maxY+1)).(*image.Gray)
}

// normalizarPuro toma un QR "de frente" (sin perspectiva) y lo vuelve a dibujar con módulos de
// 8 px exactos y zona quieta de 4 módulos. Devuelve nil si la imagen no parece un QR puro.
func normalizarPuro(g *image.Gray) image.Image {
	b := g.Bounds()
	u := otsu(g)
	osc := func(x, y int) bool { return g.GrayAt(x, y).Y <= u }

	// Caja de los píxeles oscuros.
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if osc(x, y) {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < minX {
		return nil
	}
	w, h := maxX-minX+1, maxY-minY+1
	if w < 21 || math.Abs(float64(w)/float64(h)-1) > 0.05 {
		return nil
	}
	// El patrón de esquina arriba a la izquierda arranca con 7 módulos oscuros seguidos.
	y0 := minY + 1
	run := 0
	for x := minX; x <= maxX && osc(x, y0); x++ {
		run++
	}
	if run < 7 {
		return nil
	}
	modulo := float64(run) / 7
	dim := int(math.Round(float64(w) / modulo))
	// Una dimensión válida es 4k+1, entre 21 y 177.
	dim = ((dim-21+2)/4)*4 + 21
	if dim < 21 || dim > 177 {
		return nil
	}
	mx := float64(w) / float64(dim)
	my := float64(h) / float64(dim)
	const px, quieta = 8, 4
	lado := (dim + 2*quieta) * px
	dst := image.NewGray(image.Rect(0, 0, lado, lado))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	negro := image.NewUniform(color.Gray{0})
	for j := 0; j < dim; j++ {
		for i := 0; i < dim; i++ {
			cx := minX + int((float64(i)+0.5)*mx)
			cy := minY + int((float64(j)+0.5)*my)
			if osc(cx, cy) {
				r := image.Rect((i+quieta)*px, (j+quieta)*px, (i+quieta+1)*px, (j+quieta+1)*px)
				draw.Draw(dst, r, negro, image.Point{}, draw.Src)
			}
		}
	}
	return dst
}

// zbar usa zbarimg si está instalado (paquete zbar-tools). Es más tolerante con fotos.
func zbar(g *image.Gray) (string, bool) {
	if _, err := exec.LookPath("zbarimg"); err != nil {
		return "", false
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, g); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "zbarimg", "--quiet", "--raw", "-Sdisable", "-Sqrcode.enable", "-")
	cmd.Stdin = &buf
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	s := strings.TrimSpace(string(out))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s, s != ""
}
