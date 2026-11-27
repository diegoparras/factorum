package extractor

import (
	"bytes"
	"image"
	"image/color"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Preprocesado de fotos para el OCR, en Go puro. Un ticket térmico fotografiado con el celular
// llega chico (el papel ocupa una parte de la foto), gris, con luz despareja y la tinta gastada.
// Tesseract lee mejor una imagen pareja, con letras de buen tamaño:
//
//  1. Escala de grises.
//  2. Agrandar las imágenes angostas o chicas (ancho mínimo AnchoMinimoOCR) y achicar las enormes.
//  3. Aplanar el fondo: cada píxel se divide por el promedio de su vecindario, así la sombra y el
//     degradé de la foto dejan de pesar.
//  4. Estirar el contraste (percentiles 1 y 99).
//  5. Binarizar con el umbral de Otsu.
//
// La versión gris (hasta el paso 4) y la binarizada se devuelven por separado: con las fotos de
// prueba, un ticket desenfocado se lee mejor en gris (Tesseract binariza por su cuenta y no pierde
// los trazos finos), y la binarizada rescata las fotos con muy poco contraste. El pipeline prueba
// primero la gris y usa la binarizada si falta algún dato clave.

const (
	// AnchoMinimoOCR: una foto más angosta se agranda hasta este ancho (con interpolación bilineal,
	// que no agrega halos al ruido del JPEG como la bicúbica).
	AnchoMinimoOCR = 1100
	anchoMaximoOCR = 3600 // una foto más ancha se achica (Tesseract tarda mucho y no gana nada)
	escalaMaxima   = 4.0
)

// PrepararParaOCR devuelve la imagen lista para Tesseract: gris (escalada, con el fondo aplanado y
// el contraste estirado) y binarizada con Otsu.
func PrepararParaOCR(img image.Image) (gris, binaria *image.Gray) {
	gris = escalar(grises(img))
	aplanarFondo(gris)
	estirarContraste(gris)
	binaria = image.NewGray(gris.Bounds())
	copy(binaria.Pix, gris.Pix)
	binarizarOtsu(binaria)
	return gris, binaria
}

// PNGsParaOCR prepara la imagen y codifica en PNG las dos versiones para mandarlas a Tesseract.
func PNGsParaOCR(img image.Image) (gris, binaria []byte, err error) {
	g, b := PrepararParaOCR(img)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	var bg, bb bytes.Buffer
	if err := enc.Encode(&bg, g); err != nil {
		return nil, nil, err
	}
	if err := enc.Encode(&bb, b); err != nil {
		return nil, nil, err
	}
	return bg.Bytes(), bb.Bytes(), nil
}

func grises(img image.Image) *image.Gray {
	b := img.Bounds()
	g := image.NewGray(image.Rect(0, 0, b.Dx(), b.Dy()))
	if yc, ok := img.(*image.YCbCr); ok { // JPEG: el canal Y ya es la escala de grises
		for y := 0; y < b.Dy(); y++ {
			o := yc.YOffset(b.Min.X, b.Min.Y+y)
			copy(g.Pix[y*g.Stride:y*g.Stride+b.Dx()], yc.Y[o:o+b.Dx()])
		}
		return g
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g.SetGray(x-b.Min.X, y-b.Min.Y, color.GrayModel.Convert(img.At(x, y)).(color.Gray))
		}
	}
	return g
}

func escalar(g *image.Gray) *image.Gray {
	w, h := g.Bounds().Dx(), g.Bounds().Dy()
	if w == 0 || h == 0 {
		return g
	}
	f := 1.0
	switch {
	case w < AnchoMinimoOCR:
		f = min(float64(AnchoMinimoOCR)/float64(w), escalaMaxima)
	case w > anchoMaximoOCR:
		f = float64(anchoMaximoOCR) / float64(w)
	}
	if f == 1 {
		return g
	}
	nw, nh := int(float64(w)*f+0.5), int(float64(h)*f+0.5)
	dst := image.NewGray(image.Rect(0, 0, nw, nh))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), g, g.Bounds(), xdraw.Src, nil)
	return dst
}

// aplanarFondo divide cada píxel por el brillo promedio de un vecindario grande (con una imagen
// integral, en tiempo lineal). El papel queda blanco aunque la foto tenga sombra o degradé.
func aplanarFondo(g *image.Gray) {
	w, h := g.Bounds().Dx(), g.Bounds().Dy()
	r := max(w, h) / 24
	if r < 8 || w < 2 || h < 2 {
		return
	}
	integ := make([]int64, (w+1)*(h+1))
	for y := 0; y < h; y++ {
		var fila int64
		for x := 0; x < w; x++ {
			fila += int64(g.Pix[y*g.Stride+x])
			integ[(y+1)*(w+1)+x+1] = integ[y*(w+1)+x+1] + fila
		}
	}
	for y := 0; y < h; y++ {
		y0, y1 := max(y-r, 0), min(y+r+1, h)
		for x := 0; x < w; x++ {
			x0, x1 := max(x-r, 0), min(x+r+1, w)
			suma := integ[y1*(w+1)+x1] - integ[y0*(w+1)+x1] - integ[y1*(w+1)+x0] + integ[y0*(w+1)+x0]
			media := float64(suma) / float64((y1-y0)*(x1-x0))
			if media < 1 {
				media = 1
			}
			// El vecindario incluye letras, así que su promedio queda un poco por debajo del papel.
			v := float64(g.Pix[y*g.Stride+x]) * 235 / media
			if v > 255 {
				v = 255
			}
			g.Pix[y*g.Stride+x] = uint8(v)
		}
	}
}

func histograma(g *image.Gray) (h [256]int, n int) {
	w, alto := g.Bounds().Dx(), g.Bounds().Dy()
	for y := 0; y < alto; y++ {
		for _, p := range g.Pix[y*g.Stride : y*g.Stride+w] {
			h[p]++
		}
	}
	return h, w * alto
}

// estirarContraste lleva el percentil 1 a negro y el 99 a blanco.
func estirarContraste(g *image.Gray) {
	h, n := histograma(g)
	if n == 0 {
		return
	}
	lo, hi, acc := 0, 255, 0
	for i := 0; i < 256; i++ {
		acc += h[i]
		if acc*100 >= n {
			lo = i
			break
		}
	}
	acc = 0
	for i := 255; i >= 0; i-- {
		acc += h[i]
		if acc*100 >= n {
			hi = i
			break
		}
	}
	if hi-lo < 8 {
		return
	}
	var lut [256]uint8
	for i := range lut {
		v := (i - lo) * 255 / (hi - lo)
		lut[i] = uint8(min(max(v, 0), 255))
	}
	w, alto := g.Bounds().Dx(), g.Bounds().Dy()
	for y := 0; y < alto; y++ {
		fila := g.Pix[y*g.Stride : y*g.Stride+w]
		for i, p := range fila {
			fila[i] = lut[p]
		}
	}
}

// UmbralOtsu calcula el umbral que mejor separa tinta y papel (máxima varianza entre clases).
func UmbralOtsu(g *image.Gray) uint8 {
	h, n := histograma(g)
	if n == 0 {
		return 128
	}
	var sumaTotal float64
	for i, c := range h {
		sumaTotal += float64(i * c)
	}
	var sumaFondo, mejor float64
	pesoFondo, umbral := 0, 0
	for t := 0; t < 256; t++ {
		pesoFondo += h[t]
		if pesoFondo == 0 {
			continue
		}
		pesoFrente := n - pesoFondo
		if pesoFrente == 0 {
			break
		}
		sumaFondo += float64(t * h[t])
		mf := sumaFondo / float64(pesoFondo)
		mF := (sumaTotal - sumaFondo) / float64(pesoFrente)
		v := float64(pesoFondo) * float64(pesoFrente) * (mf - mF) * (mf - mF)
		if v > mejor {
			mejor, umbral = v, t
		}
	}
	return uint8(umbral)
}

func binarizarOtsu(g *image.Gray) {
	u := UmbralOtsu(g)
	w, alto := g.Bounds().Dx(), g.Bounds().Dy()
	for y := 0; y < alto; y++ {
		fila := g.Pix[y*g.Stride : y*g.Stride+w]
		for i, p := range fila {
			if p > u {
				fila[i] = 255
			} else {
				fila[i] = 0
			}
		}
	}
}
