// Package pipeline arma la cascada: QR de ARCA -> texto del PDF -> OCR -> modelo de visión.
// Cada paso solo corre si el anterior no alcanzó. No depende de la base de datos: lo usan igual la
// CLI, el servidor, MCP y los canales de mensajería.
package pipeline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/pdfx"
	"github.com/diegoparras/factorum/internal/qrscan"
	"github.com/diegoparras/factorum/internal/validar"
)

// Opciones por documento.
type Opciones struct {
	MaxPaginas int  // páginas a leer de cada PDF (por defecto 30)
	SinOCR     bool // no usar OCR aunque esté instalado
	IA         bool // permitir el modelo de visión para este documento
}

// Pipeline es seguro para usar desde varias goroutines a la vez.
type Pipeline struct {
	Motor  pdfx.Motor
	OCR    *extractor.Tesseract // nil = sin OCR
	Vision *extractor.Vision    // nil = sin IA
	Ahora  func() time.Time

	semOCR    chan struct{}
	semVision chan struct{}
}

// Nuevo arma un pipeline. concOCR y concIA limitan cuántos OCR y cuántas llamadas al modelo corren
// en paralelo (el modelo local suele ser el cuello de botella: por defecto 1).
func Nuevo(motor pdfx.Motor, ocr *extractor.Tesseract, vision *extractor.Vision, concOCR, concIA int) *Pipeline {
	if concOCR <= 0 {
		concOCR = 2
	}
	if concIA <= 0 {
		concIA = 1
	}
	return &Pipeline{Motor: motor, OCR: ocr, Vision: vision, Ahora: time.Now,
		semOCR: make(chan struct{}, concOCR), semVision: make(chan struct{}, concIA)}
}

// Procesar lee un archivo (PDF o imagen) y devuelve sus comprobantes con los controles.
func (p *Pipeline) Procesar(ctx context.Context, nombre string, datos []byte, op Opciones) (res modelo.Resultado) {
	t0 := time.Now()
	suma := sha256.Sum256(datos)
	res = modelo.Resultado{Archivo: nombre, SHA256: hex.EncodeToString(suma[:]), Detalle: map[string]float64{}}
	usados := map[string]bool{}
	usar := func(n string) {
		if !usados[n] {
			usados[n] = true
			res.Extractores = append(res.Extractores, n)
		}
	}
	defer func() { res.DuracionMS = time.Since(t0).Milliseconds() }()

	if op.MaxPaginas <= 0 {
		op.MaxPaginas = 30
	}
	var grupos []grupo
	var err error
	tipo := http.DetectContentType(datos[:min(len(datos), 512)])
	switch {
	case bytes.HasPrefix(bytes.TrimLeft(datos[:min(len(datos), 1024)], "\x00\t\r\n "), []byte("%PDF")):
		grupos, err = p.leerPDF(ctx, datos, op, &res, usar)
	case strings.HasPrefix(tipo, "image/"):
		grupos, err = p.leerImagen(ctx, datos, op, &res, usar)
	default:
		err = fmt.Errorf("formato no soportado (%s): mandá un PDF o una imagen", tipo)
	}
	if err != nil {
		res.Error = err.Error()
		return res
	}
	hoy := p.Ahora()
	for _, g := range grupos {
		c := g.cbte
		completarDerivados(&c)
		h := append(append([]modelo.Hallazgo{}, g.hallazgos...), validar.Comprobante(&c, hoy)...)
		res.Comprobantes = append(res.Comprobantes, modelo.ResultadoCbte{
			Comprobante: c, Hallazgos: h, Estado: validar.Estado(&c, h), Confianza: validar.Confianza(&c, h),
		})
	}
	return res
}

// grupo es un comprobante dentro de un documento: una o más páginas con la misma identidad.
type grupo struct {
	cbte      modelo.Comprobante
	qr        *arca.QR
	paginas   []int
	texto     strings.Builder
	hallazgos []modelo.Hallazgo
	imgPag    int // página a usar para OCR/visión
}

func (p *Pipeline) cronometrar(res *modelo.Resultado, etapa string, t time.Time) {
	res.Detalle[etapa] += float64(time.Since(t).Microseconds()) / 1000
}

func (p *Pipeline) leerPDF(ctx context.Context, datos []byte, op Opciones, res *modelo.Resultado, usar func(string)) ([]grupo, error) {
	t := time.Now()
	doc, err := p.Motor.Abrir(ctx, datos)
	if err != nil {
		return nil, err
	}
	defer doc.Cerrar()
	res.Paginas = doc.Paginas()
	n := min(res.Paginas, op.MaxPaginas)

	textos, err := doc.Texto(ctx, n)
	if err != nil {
		return nil, err
	}
	p.cronometrar(res, "texto", t)

	t = time.Now()
	imgs, _ := doc.Imagenes(ctx, n) // sin imágenes no es un error: se rasteriza si hace falta
	p.cronometrar(res, "imagenes", t)

	// QR por página: primero en las imágenes embebidas; si no aparece y la página parece un
	// comprobante (o no tiene texto: escaneada), rasterizamos.
	t = time.Now()
	qrs := make([]*arca.QR, n+1)
	for pag := 1; pag <= n; pag++ {
		for _, img := range imgs[pag] {
			if !casiCuadrada(img) {
				continue
			}
			if s, ok := qrscan.Leer(img); ok {
				if q, err := arca.ParsearQR(s); err == nil {
					qrs[pag] = q
					break
				}
			}
		}
		txt := textos[pag-1]
		if qrs[pag] == nil && (pocoTexto(txt) || pareceComprobante(txt)) {
			if img, _, err := doc.Rasterizar(ctx, pag, 150, true); err == nil {
				if s, ok := qrscan.Leer(img); ok {
					if q, err := arca.ParsearQR(s); err == nil {
						qrs[pag] = q
					}
				}
			}
		}
	}
	p.cronometrar(res, "qr", t)
	for _, q := range qrs {
		if q != nil {
			usar("qr")
			break
		}
	}

	// Agrupar páginas: una página nueva con otra identidad abre otro comprobante. Las páginas sin
	// identidad (continuación de un detalle largo) se suman a la anterior.
	var grupos []*grupo
	for pag := 1; pag <= n; pag++ {
		txt := textos[pag-1]
		id := ""
		if qrs[pag] != nil {
			id = claveQR(qrs[pag])
		} else {
			c := extractor.ParsearTexto(txt, modelo.OrigenTexto)
			id = c.Clave()
		}
		var g *grupo
		if len(grupos) > 0 {
			ult := grupos[len(grupos)-1]
			if id == "" || id == ult.cbte.Clave() || (ult.qr != nil && id == claveQR(ult.qr)) {
				g = ult
			}
		}
		if g == nil {
			g = &grupo{imgPag: pag}
			grupos = append(grupos, g)
		}
		g.paginas = append(g.paginas, pag)
		if g.qr == nil && qrs[pag] != nil {
			g.qr = qrs[pag]
		}
		// De las copias (ORIGINAL, DUPLICADO, TRIPLICADO) alcanza con leer el texto de la primera.
		if len(g.paginas) == 1 || !esCopia(txt) {
			g.texto.WriteString(txt)
			g.texto.WriteString("\n")
		}
		if g.cbte.Clave() == "" {
			g.cbte = extractor.ParsearTexto(g.texto.String(), modelo.OrigenTexto)
		}
	}

	var salida []grupo
	for _, g := range grupos {
		txt := g.texto.String()
		g.cbte = extractor.ParsearTexto(txt, modelo.OrigenTexto)
		if !pocoTexto(txt) {
			usar("texto")
		}
		// Escaneado: OCR de la primera página del grupo.
		if pocoTexto(txt) && p.OCR != nil && !op.SinOCR {
			t := time.Now()
			if _, pngb, err := doc.Rasterizar(ctx, g.imgPag, 300, true); err == nil {
				if o, err := p.ocr(ctx, pngb); err == nil && !pocoTexto(o) {
					txt = o
					g.cbte = extractor.ParsearTexto(o, modelo.OrigenOCR)
					usar("ocr")
				} else if err != nil {
					g.hallazgos = append(g.hallazgos, modelo.Hallazgo{Codigo: "ocr_fallo", Nivel: modelo.NivelInfo, Mensaje: "El OCR no pudo leer la página: " + err.Error()})
				}
			}
			p.cronometrar(res, "ocr", t)
		}
		g.hallazgos = append(g.hallazgos, fusionarQR(&g.cbte, g.qr)...)
		if op.IA && p.Vision != nil && necesitaMas(&g.cbte) {
			t := time.Now()
			var img []byte
			mime := ""
			textoIA := ""
			if pocoTexto(txt) {
				if im, _, err := doc.Rasterizar(ctx, g.imgPag, 200, false); err == nil {
					img, mime = paraModelo(im)
				}
			} else {
				textoIA = txt
			}
			if img != nil || textoIA != "" {
				if v, err := p.vision(ctx, img, mime, textoIA); err == nil {
					g.hallazgos = append(g.hallazgos, completar(&g.cbte, &v)...)
					usar("vision")
				} else {
					g.hallazgos = append(g.hallazgos, modelo.Hallazgo{Codigo: "ia_fallo", Nivel: modelo.NivelAviso, Mensaje: "No se pudo usar el modelo: " + err.Error()})
				}
			}
			p.cronometrar(res, "vision", t)
		}
		g.cbte.Paginas = g.paginas
		salida = append(salida, *g)
	}
	return salida, nil
}

func (p *Pipeline) leerImagen(ctx context.Context, datos []byte, op Opciones, res *modelo.Resultado, usar func(string)) ([]grupo, error) {
	img, _, err := image.Decode(bytes.NewReader(datos))
	if err != nil {
		return nil, fmt.Errorf("imagen ilegible: %w", err)
	}
	res.Paginas = 1
	g := grupo{paginas: []int{1}, imgPag: 1}
	t := time.Now()
	if s, ok := qrscan.Leer(img); ok {
		if q, err := arca.ParsearQR(s); err == nil {
			g.qr = q
			usar("qr")
		}
	}
	p.cronometrar(res, "qr", t)
	if p.OCR != nil && !op.SinOCR {
		t := time.Now()
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			if o, err := p.ocr(ctx, buf.Bytes()); err == nil {
				g.cbte = extractor.ParsearTexto(o, modelo.OrigenOCR)
				usar("ocr")
			}
		}
		p.cronometrar(res, "ocr", t)
	}
	g.hallazgos = append(g.hallazgos, fusionarQR(&g.cbte, g.qr)...)
	if op.IA && p.Vision != nil && necesitaMas(&g.cbte) {
		t := time.Now()
		b, mime := paraModelo(img)
		if v, err := p.vision(ctx, b, mime, ""); err == nil {
			g.hallazgos = append(g.hallazgos, completar(&g.cbte, &v)...)
			usar("vision")
		} else {
			g.hallazgos = append(g.hallazgos, modelo.Hallazgo{Codigo: "ia_fallo", Nivel: modelo.NivelAviso, Mensaje: "No se pudo usar el modelo: " + err.Error()})
		}
		p.cronometrar(res, "vision", t)
	}
	if g.qr == nil && p.OCR == nil && (!op.IA || p.Vision == nil) {
		return nil, errors.New("la imagen no tiene un QR de ARCA legible y no hay OCR ni modelo de visión configurado")
	}
	g.cbte.Paginas = g.paginas
	return []grupo{g}, nil
}

func (p *Pipeline) ocr(ctx context.Context, png []byte) (string, error) {
	select {
	case p.semOCR <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-p.semOCR }()
	return p.OCR.Texto(ctx, png)
}

func (p *Pipeline) vision(ctx context.Context, img []byte, mime, texto string) (modelo.Comprobante, error) {
	select {
	case p.semVision <- struct{}{}:
	case <-ctx.Done():
		return modelo.Comprobante{}, ctx.Err()
	}
	defer func() { <-p.semVision }()
	return p.Vision.Extraer(ctx, img, mime, texto)
}

// fusionarQR pisa los campos que trae el QR (dato oficial) y avisa si el texto decía otra cosa:
// un PDF cuyo texto no coincide con su QR puede estar adulterado.
func fusionarQR(c *modelo.Comprobante, q *arca.QR) []modelo.Hallazgo {
	if q == nil {
		return nil
	}
	var h []modelo.Hallazgo
	dif := func(campo, nombre, texto, qr string) {
		if texto != "" && c.Origen(campo) != "" && texto != qr {
			h = append(h, modelo.Hallazgo{Codigo: "qr_distinto_texto", Nivel: modelo.NivelError, Campo: campo,
				Mensaje: fmt.Sprintf("El %s del texto (%s) no coincide con el del QR (%s). El PDF podría estar adulterado.", nombre, texto, qr)})
		}
	}
	dif("cuit_emisor", "CUIT del emisor", c.CUITEmisor, q.Cuit)
	if c.Tipo != 0 {
		dif("tipo", "tipo de comprobante", fmt.Sprint(c.Tipo), fmt.Sprint(q.TipoCmp))
	}
	if c.PuntoVenta != 0 {
		dif("punto_venta", "punto de venta", fmt.Sprint(c.PuntoVenta), fmt.Sprint(q.PtoVta))
	}
	if c.Numero != 0 {
		dif("numero", "número", fmt.Sprint(c.Numero), fmt.Sprint(q.NroCmp))
	}
	dif("fecha", "fecha", c.Fecha, q.Fecha)
	totalQR := modelo.DesdeFloat(q.Importe)
	if c.Total != 0 && c.Origen("total") != "" && c.Total != totalQR {
		h = append(h, modelo.Hallazgo{Codigo: "qr_distinto_texto", Nivel: modelo.NivelError, Campo: "total",
			Mensaje: fmt.Sprintf("El total del texto (%s) no coincide con el del QR (%s). El PDF podría estar adulterado.", c.Total.AR(), totalQR.AR())})
	}
	if c.CAE != "" && q.CodAut != "" && c.CAE != q.CodAut {
		dif("cae", "CAE", c.CAE, q.CodAut)
	}

	o := modelo.OrigenQR
	c.CUITEmisor, c.Tipo, c.PuntoVenta, c.Numero, c.Fecha = q.Cuit, q.TipoCmp, q.PtoVta, q.NroCmp, q.Fecha
	for _, k := range []string{"cuit_emisor", "tipo", "punto_venta", "numero", "fecha"} {
		c.Marcar(k, o)
	}
	c.Total = totalQR
	c.Marcar("total", o)
	c.Moneda, c.Cotizacion = q.Moneda, q.Ctz
	c.Marcar("moneda", o)
	c.Marcar("cotizacion", o)
	if q.TipoDocRec != 0 {
		c.DocTipoReceptor = q.TipoDocRec
		c.Marcar("doc_tipo_receptor", o)
		if q.NroDocRec != "" && q.NroDocRec != "0" {
			c.DocNroReceptor = q.NroDocRec
			c.Marcar("doc_nro_receptor", o)
		}
	}
	if q.CodAut != "" && q.CodAut != "0" {
		c.CAE, c.TipoCodAut = q.CodAut, q.TipoCodAut
		c.Marcar("cae", o)
	}
	if tp, ok := arca.Tipo(q.TipoCmp); ok && tp.Letra != "" {
		if c.Letra != "" && c.Letra != tp.Letra {
			h = append(h, modelo.Hallazgo{Codigo: "qr_distinto_texto", Nivel: modelo.NivelError, Campo: "letra",
				Mensaje: fmt.Sprintf("La letra impresa (%s) no corresponde al tipo del QR (%s).", c.Letra, tp.Nombre)})
		}
		c.Letra = tp.Letra
		c.Marcar("letra", modelo.OrigenQR)
	}
	return h
}

// completar copia del modelo solo lo que falta. Si el modelo contradice un dato exacto, se avisa.
func completar(c, v *modelo.Comprobante) []modelo.Hallazgo {
	var h []modelo.Hallazgo
	tomarS := func(campo string, dst *string, val string) {
		if val == "" {
			return
		}
		if *dst == "" {
			*dst = val
			c.Marcar(campo, modelo.OrigenVision)
		} else if *dst != val && (c.Origen(campo) == modelo.OrigenQR) {
			h = append(h, modelo.Hallazgo{Codigo: "ia_contradice_qr", Nivel: modelo.NivelInfo, Campo: campo,
				Mensaje: fmt.Sprintf("El modelo leyó %s = %s, pero el QR dice %s (manda el QR).", campo, val, *dst)})
		}
	}
	tomarI := func(campo string, dst *modelo.Importe, val modelo.Importe) {
		if v.Origen(campo) == "" {
			return
		}
		if *dst == 0 && c.Origen(campo) == "" {
			*dst = val
			c.Marcar(campo, modelo.OrigenVision)
		}
	}
	if c.Tipo == 0 && v.Tipo != 0 {
		c.Tipo = v.Tipo
		c.Marcar("tipo", modelo.OrigenVision)
	}
	if c.PuntoVenta == 0 && v.PuntoVenta != 0 {
		c.PuntoVenta = v.PuntoVenta
		c.Marcar("punto_venta", modelo.OrigenVision)
	}
	if c.Numero == 0 && v.Numero != 0 {
		c.Numero = v.Numero
		c.Marcar("numero", modelo.OrigenVision)
	}
	if c.DocTipoReceptor == 0 && v.DocTipoReceptor != 0 {
		c.DocTipoReceptor = v.DocTipoReceptor
		c.Marcar("doc_tipo_receptor", modelo.OrigenVision)
	}
	tomarS("letra", &c.Letra, v.Letra)
	tomarS("fecha", &c.Fecha, v.Fecha)
	tomarS("cuit_emisor", &c.CUITEmisor, v.CUITEmisor)
	tomarS("razon_social_emisor", &c.RazonSocialEmisor, v.RazonSocialEmisor)
	tomarS("condicion_iva_emisor", &c.CondicionIVAEmisor, v.CondicionIVAEmisor)
	tomarS("doc_nro_receptor", &c.DocNroReceptor, v.DocNroReceptor)
	tomarS("razon_social_receptor", &c.RazonSocialReceptor, v.RazonSocialReceptor)
	tomarS("cae", &c.CAE, v.CAE)
	tomarS("vto_cae", &c.VtoCAE, v.VtoCAE)
	tomarS("moneda", &c.Moneda, v.Moneda)
	tomarI("neto_gravado", &c.NetoGravado, v.NetoGravado)
	tomarI("no_gravado", &c.NoGravado, v.NoGravado)
	tomarI("exento", &c.Exento, v.Exento)
	tomarI("iva_27", &c.IVA27, v.IVA27)
	tomarI("iva_21", &c.IVA21, v.IVA21)
	tomarI("iva_10_5", &c.IVA105, v.IVA105)
	tomarI("iva_5", &c.IVA5, v.IVA5)
	tomarI("iva_2_5", &c.IVA25, v.IVA25)
	tomarI("otros_tributos", &c.OtrosTributos, v.OtrosTributos)
	tomarI("subtotal", &c.Subtotal, v.Subtotal)
	tomarI("total", &c.Total, v.Total)
	return h
}

// necesitaMas indica si conviene llamar al modelo: faltan datos clave o las sumas no cierran.
func necesitaMas(c *modelo.Comprobante) bool {
	if c.Tipo == 0 || c.PuntoVenta == 0 || c.Numero == 0 || c.Fecha == "" || c.CUITEmisor == "" || c.Total == 0 {
		return true
	}
	if c.RazonSocialEmisor == "" {
		return true
	}
	switch c.Letra {
	case "B", "C":
		return false
	}
	d := c.SumaComponentes() - c.Total
	return c.SumaComponentes() == 0 || d > 5 || d < -5
}

func completarDerivados(c *modelo.Comprobante) {
	if c.Letra == "" && c.Tipo != 0 {
		if tp, ok := arca.Tipo(c.Tipo); ok && tp.Letra != "" {
			c.Letra = tp.Letra
			c.Marcar("letra", modelo.OrigenCalculo)
		}
	}
	if c.Moneda == "" {
		c.Moneda = "PES"
	}
	if c.Cotizacion == 0 {
		c.Cotizacion = 1
	}
	if c.TipoCodAut == "" && c.CAE != "" {
		c.TipoCodAut = "E"
	}
}

func claveQR(q *arca.QR) string {
	return fmt.Sprintf("%s-%d-%d-%d", q.Cuit, q.TipoCmp, q.PtoVta, q.NroCmp)
}

func casiCuadrada(img image.Image) bool {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w < 21 || h < 21 {
		return false
	}
	r := float64(w) / float64(h)
	return r > 0.8 && r < 1.25
}

func pocoTexto(s string) bool {
	n := 0
	for _, r := range s {
		if r > ' ' {
			n++
		}
	}
	return n < 60
}

func pareceComprobante(s string) bool {
	u := strings.ToUpper(s)
	return strings.Contains(u, "CAE") || strings.Contains(u, "FACTURA") || strings.Contains(u, "NOTA DE")
}

func esCopia(s string) bool {
	u := strings.ToUpper(s)
	return strings.Contains(u, "DUPLICADO") || strings.Contains(u, "TRIPLICADO")
}

// paraModelo reduce la imagen a 1600 px de lado como máximo y la manda en JPEG: alcanza para leer
// un comprobante y baja mucho el tiempo del modelo.
func paraModelo(img image.Image) ([]byte, string) {
	b := img.Bounds()
	const max = 1600
	if b.Dx() > max || b.Dy() > max {
		w, h := b.Dx(), b.Dy()
		if w >= h {
			h, w = h*max/w, max
		} else {
			w, h = w*max/h, max
		}
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Src, nil)
		img = dst
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		return nil, ""
	}
	return buf.Bytes(), "image/jpeg"
}
