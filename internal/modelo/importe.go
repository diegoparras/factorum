// Package modelo tiene los tipos que comparten todas las capas: el comprobante leído, de dónde
// salió cada campo y los hallazgos de las validaciones.
package modelo

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Importe es un monto en centavos. Los importes nunca se guardan en float: una suma de IVA
// que no cierra por un redondeo binario es justo el tipo de error que esta herramienta busca.
type Importe int64

// DesdeFloat redondea al centavo.
func DesdeFloat(f float64) Importe { return Importe(math.Round(f * 100)) }

func (i Importe) Float() float64 { return float64(i) / 100 }

// String devuelve el importe con punto decimal y dos decimales ("1234.50").
func (i Importe) String() string {
	signo := ""
	v := int64(i)
	if v < 0 {
		signo, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", signo, v/100, v%100)
}

// AR devuelve el importe como se escribe en Argentina: 1.234.567,89.
func (i Importe) AR() string {
	s := i.String()
	signo := ""
	if strings.HasPrefix(s, "-") {
		signo, s = "-", s[1:]
	}
	ent, dec, _ := strings.Cut(s, ".")
	var b strings.Builder
	for n, r := range ent {
		if n > 0 && (len(ent)-n)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return signo + b.String() + "," + dec
}

func (i Importe) MarshalJSON() ([]byte, error) { return []byte(i.String()), nil }

func (i *Importe) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*i = 0
		return nil
	}
	v, err := ParsearImporte(s)
	if err != nil {
		return err
	}
	*i = v
	return nil
}

// ParsearImporte entiende "1234.5", "1234,50", "1.234,50", "1,234.50", "$ 3570000,00" y "-12,3".
// La regla para el separador decimal: el último punto o coma seguido de 1 o 2 dígitos al final.
func ParsearImporte(s string) (Importe, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "$")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, " ", "")
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	} else if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg, s = true, s[1:len(s)-1]
	}
	if s == "" {
		return 0, fmt.Errorf("importe vacío")
	}
	ent, dec := s, ""
	if k := strings.LastIndexAny(s, ".,"); k >= 0 {
		resto := s[k+1:]
		otros := strings.Count(s, ".") + strings.Count(s, ",")
		ambos := strings.Contains(s, ".") && strings.Contains(s, ",")
		// Con los dos separadores, el último es el decimal. Con uno solo, "1.234" (tres dígitos) es miles.
		if ambos || len(resto) >= 1 && len(resto) <= 2 || (otros == 1 && len(resto) != 3) {
			ent, dec = s[:k], resto
		}
	}
	ent = strings.NewReplacer(".", "", ",", "").Replace(ent)
	if ent == "" {
		ent = "0"
	}
	for len(dec) < 2 {
		dec += "0"
	}
	if len(dec) > 2 {
		// Más de dos decimales: redondeamos.
		f, err := strconv.ParseFloat(ent+"."+dec, 64)
		if err != nil {
			return 0, fmt.Errorf("importe inválido %q", s)
		}
		v := DesdeFloat(f)
		if neg {
			v = -v
		}
		return v, nil
	}
	e, err := strconv.ParseInt(ent, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("importe inválido %q", s)
	}
	d, err := strconv.ParseInt(dec, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("importe inválido %q", s)
	}
	v := Importe(e*100 + d)
	if neg {
		v = -v
	}
	return v, nil
}
