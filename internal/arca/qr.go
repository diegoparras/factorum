package arca

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// QR es el contenido del código de la RG 4892: un JSON en base64 en el parámetro "p" de
// https://www.afip.gob.ar/fe/qr/?p=... (o arca.gob.ar). Es el dato oficial del comprobante.
type QR struct {
	Ver        int     `json:"ver"`
	Fecha      string  `json:"fecha"` // AAAA-MM-DD
	Cuit       string  `json:"cuit"`
	PtoVta     int     `json:"ptoVta"`
	TipoCmp    int     `json:"tipoCmp"`
	NroCmp     int64   `json:"nroCmp"`
	Importe    float64 `json:"importe"`
	Moneda     string  `json:"moneda"`
	Ctz        float64 `json:"ctz"`
	TipoDocRec int     `json:"tipoDocRec"`
	NroDocRec  string  `json:"nroDocRec"`
	TipoCodAut string  `json:"tipoCodAut"` // E = CAE, A = CAEA
	CodAut     string  `json:"codAut"`
}

// ErrNoEsQRARCA indica que el texto del QR no es un QR de comprobante de ARCA.
var ErrNoEsQRARCA = errors.New("el QR no es de un comprobante de ARCA")

// ParsearQR recibe el texto crudo que devuelve el lector de QR.
// Es tolerante con lo que hacen los sistemas de facturación en la práctica: base64 con o sin
// relleno, en variante URL, números como texto, espacios y saltos de línea en el medio.
func ParsearQR(texto string) (*QR, error) {
	texto = strings.TrimSpace(texto)
	u, err := url.Parse(texto)
	if err != nil || !esHostARCA(u.Host) {
		return nil, ErrNoEsQRARCA
	}
	p := u.Query().Get("p")
	if p == "" {
		// Algunos sistemas no codifican el "+" del base64 y url.Query lo vuelve espacio.
		if _, resto, ok := strings.Cut(u.RawQuery, "p="); ok {
			p = resto
		}
	}
	if p == "" {
		return nil, fmt.Errorf("%w: sin parámetro p", ErrNoEsQRARCA)
	}
	crudo, err := decodificarBase64(p)
	if err != nil {
		return nil, fmt.Errorf("QR de ARCA con base64 inválido: %w", err)
	}
	return parsearJSONQR(crudo)
}

func esHostARCA(h string) bool {
	h = strings.ToLower(h)
	return h == "www.afip.gob.ar" || h == "afip.gob.ar" || h == "www.arca.gob.ar" || h == "arca.gob.ar" ||
		strings.HasSuffix(h, ".afip.gob.ar") || strings.HasSuffix(h, ".arca.gob.ar")
}

func decodificarBase64(p string) ([]byte, error) {
	p = strings.Map(func(r rune) rune {
		switch r {
		case ' ':
			return '+' // "+" convertido en espacio por un decodificador de URL
		case '\n', '\r', '\t':
			return -1
		}
		return r
	}, p)
	if s, err := url.QueryUnescape(p); err == nil && strings.ContainsAny(p, "%") {
		p = s
	}
	sinRelleno := strings.TrimRight(p, "=")
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(sinRelleno); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("no se pudo decodificar")
}

func parsearJSONQR(crudo []byte) (*QR, error) {
	var m map[string]any
	d := json.NewDecoder(strings.NewReader(string(crudo)))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return nil, fmt.Errorf("QR de ARCA con JSON inválido: %w", err)
	}
	q := &QR{
		Ver:        int(num(m["ver"])),
		Fecha:      normalizarFecha(str(m["fecha"])),
		Cuit:       SoloDigitos(str(m["cuit"])),
		PtoVta:     int(num(m["ptoVta"])),
		TipoCmp:    int(num(m["tipoCmp"])),
		NroCmp:     int64(num(m["nroCmp"])),
		Importe:    num(m["importe"]),
		Moneda:     strings.ToUpper(strings.TrimSpace(str(m["moneda"]))),
		Ctz:        num(m["ctz"]),
		TipoDocRec: int(num(m["tipoDocRec"])),
		NroDocRec:  SoloDigitos(str(m["nroDocRec"])),
		TipoCodAut: strings.ToUpper(strings.TrimSpace(str(m["tipoCodAut"]))),
		CodAut:     SoloDigitos(str(m["codAut"])),
	}
	if q.Cuit == "" || q.TipoCmp == 0 || q.NroCmp == 0 {
		return nil, fmt.Errorf("%w: faltan CUIT, tipo o número", ErrNoEsQRARCA)
	}
	if q.Moneda == "" {
		q.Moneda = "PES"
	}
	if q.Ctz == 0 {
		q.Ctz = 1
	}
	if q.TipoCodAut == "" {
		q.TipoCodAut = "E"
	}
	return q, nil
}

// num acepta json.Number, string ("123", "1.234,5") o float.
func num(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	case string:
		s := strings.TrimSpace(x)
		if strings.Contains(s, ",") {
			s = strings.ReplaceAll(s, ".", "")
			s = strings.ReplaceAll(s, ",", ".")
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	return 0
}

func str(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// normalizarFecha lleva AAAA-MM-DD, AAAAMMDD y DD/MM/AAAA a AAAA-MM-DD.
func normalizarFecha(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	if len(s) == 8 && SoloDigitos(s) == s {
		return s[:4] + "-" + s[4:6] + "-" + s[6:]
	}
	if len(s) == 10 && (s[2] == '/' || s[2] == '-') {
		return s[6:] + "-" + s[3:5] + "-" + s[:2]
	}
	return s
}

// NormalizarFecha es la versión exportada para el resto de los paquetes.
func NormalizarFecha(s string) string { return normalizarFecha(s) }

// URL arma la URL del QR para un comprobante (útil en tests y para regenerar el código).
func (q *QR) URL() string {
	b, _ := json.Marshal(map[string]any{
		"ver": q.Ver, "fecha": q.Fecha, "cuit": json.Number(q.Cuit), "ptoVta": q.PtoVta, "tipoCmp": q.TipoCmp,
		"nroCmp": q.NroCmp, "importe": q.Importe, "moneda": q.Moneda, "ctz": q.Ctz, "tipoDocRec": q.TipoDocRec,
		"nroDocRec": json.Number(nz(q.NroDocRec)), "tipoCodAut": q.TipoCodAut, "codAut": json.Number(nz(q.CodAut)),
	})
	return "https://www.afip.gob.ar/fe/qr/?p=" + base64.StdEncoding.EncodeToString(b)
}

func nz(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
