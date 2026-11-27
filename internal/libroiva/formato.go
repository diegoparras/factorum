package libroiva

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/diegoparras/factorum/internal/modelo"
)

// Reglas generales del diseño de registro (Especificaciones RG 4597, «Consideraciones generales»):
//   - los importes van en 15 posiciones, 13 enteros y 2 decimales, sin coma ni punto, con ceros a
//     la izquierda; si son negativos, el signo «-» va en la primera posición;
//   - lo numérico se completa con ceros a la izquierda y lo alfanumérico con blancos a la derecha;
//   - el archivo es «ANSI», con los juegos de caracteres ISO 8859-1 o Windows-1252.

// MaxImporte es el mayor importe que entra en 13 enteros y 2 decimales.
const MaxImporte modelo.Importe = 999_999_999_999_999

// FinDeLinea separa los registros. El diseño no lo dice; ver docs/libro-iva-digital.md.
const FinDeLinea = "\r\n"

// numero escribe v con ceros a la izquierda en n posiciones. Error si no entra o es negativo.
func numero(v int64, n int) (string, error) {
	if v < 0 {
		return "", fmt.Errorf("número negativo %d", v)
	}
	s := strconv.FormatInt(v, 10)
	if len(s) > n {
		return "", fmt.Errorf("%d no entra en %d posiciones", v, n)
	}
	return strings.Repeat("0", n-len(s)) + s, nil
}

// importe escribe un monto en centavos en 15 posiciones; negativo, con el «-» en la primera.
func importe(v modelo.Importe) (string, error) {
	if v > MaxImporte || v < -MaxImporte/10 {
		return "", fmt.Errorf("el importe %s no entra en 13 enteros y 2 decimales", v.AR())
	}
	if v < 0 {
		s, err := numero(int64(-v), 14)
		return "-" + s, err
	}
	return numero(int64(v), 15)
}

// tipoCambio escribe la cotización con 4 enteros y 6 decimales (10 posiciones), redondeada al
// millonésimo. Error si es cero, negativa o de 10.000 o más.
func tipoCambio(c float64) (string, error) {
	if !(c > 0) || math.IsInf(c, 0) {
		return "", fmt.Errorf("cotización inválida")
	}
	v := int64(math.Round(c * 1e6))
	if v <= 0 || v > 9_999_999_999 {
		return "", fmt.Errorf("la cotización %v no entra en 4 enteros y 6 decimales", c)
	}
	return numero(v, 10)
}

// alfa escribe un texto en n posiciones: lo pasa a Latin-1 sin acentos (ver Latin1), lo corta si
// es más largo y lo completa con blancos a la derecha.
func alfa(s string, n int) string {
	b := Latin1(s)
	if len(b) > n {
		b = strings.TrimRight(b[:n], " ")
	}
	return b + strings.Repeat(" ", n-len(b))
}

// Latin1 deja un texto en caracteres que el aplicativo lee sin sorpresas: ASCII imprimible más la
// Ñ y la ñ (que existen en ISO 8859-1 y en Windows-1252 con el mismo byte). Las vocales con tilde
// o diéresis pierden el acento, las comillas y guiones tipográficos pasan a los de ASCII, los
// controles y cualquier otro carácter a un blanco, y los blancos repetidos se juntan.
// Devuelve un string de bytes Latin-1 (un byte por carácter), no UTF-8.
func Latin1(s string) string {
	var b strings.Builder
	espacio := false
	for _, r := range s {
		c, ok := latin1Rune(r)
		if !ok || c == ' ' {
			if b.Len() > 0 {
				espacio = true
			}
			continue
		}
		if espacio {
			b.WriteByte(' ')
			espacio = false
		}
		b.WriteByte(c)
	}
	return b.String()
}

var transliterar = map[rune]byte{
	'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'Á': 'A', 'À': 'A', 'Â': 'A', 'Ä': 'A', 'Ã': 'A', 'Å': 'A',
	'É': 'E', 'È': 'E', 'Ê': 'E', 'Ë': 'E',
	'Í': 'I', 'Ì': 'I', 'Î': 'I', 'Ï': 'I',
	'Ó': 'O', 'Ò': 'O', 'Ô': 'O', 'Ö': 'O', 'Õ': 'O',
	'Ú': 'U', 'Ù': 'U', 'Û': 'U', 'Ü': 'U',
	'ç': 'c', 'Ç': 'C', 'ý': 'y', 'Ý': 'Y', 'ÿ': 'y',
	'ñ': 0xF1, 'Ñ': 0xD1,
	'“': '"', '”': '"', '«': '"', '»': '"', '‘': '\'', '’': '\'', '´': '\'', '`': '\'',
	'–': '-', '—': '-', '‐': '-', '°': 'o', 'º': 'o', 'ª': 'a', '·': '.', '…': '.',
}

func latin1Rune(r rune) (byte, bool) {
	if r >= 0x20 && r < 0x7f {
		return byte(r), true
	}
	if c, ok := transliterar[r]; ok {
		return c, true
	}
	return 0, false
}

// LeerLatin1 pasa bytes Latin-1 a UTF-8 (para mostrar o comparar en las pruebas).
func LeerLatin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}
