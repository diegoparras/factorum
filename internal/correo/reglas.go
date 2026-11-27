package correo

import (
	"strings"

	"github.com/diegoparras/factorum/internal/arca"
)

// NormalizarRemitente deja una entrada de la lista de permitidos como se compara: un email en
// minúsculas o "@dominio". Vacío si no es ninguna de las dos.
func NormalizarRemitente(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "":
		return ""
	case strings.HasPrefix(s, "@"):
		if len(s) > 1 && !strings.ContainsAny(s[1:], "@ ") && strings.Contains(s[1:], ".") {
			return s
		}
	case strings.Count(s, "@") == 1 && !strings.ContainsAny(s, " <>"):
		if loc, dom, _ := strings.Cut(s, "@"); loc != "" && strings.Contains(dom, ".") {
			return s
		}
	}
	return ""
}

func dominio(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 {
		return strings.ToLower(email[i+1:])
	}
	return ""
}

// Permitido dice si un remitente está en la lista (emails exactos o "@dominio"). Sin lista, nadie:
// falla cerrada.
func Permitido(lista []string, email string) bool {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || !strings.Contains(email, "@") {
		return false
	}
	dom := "@" + dominio(email)
	for _, x := range lista {
		x = NormalizarRemitente(x)
		if x != "" && (x == email || x == dom) {
			return true
		}
	}
	return false
}

// CUITDeDestinatarios busca una dirección con la forma algo+CUIT@dominio con un CUIT válido (con
// dígito verificador) y devuelve el CUIT. El primero que aparece gana (el sobre viene primero).
func CUITDeDestinatarios(dirs []string) string {
	for _, d := range dirs {
		loc, _, ok := strings.Cut(strings.ToLower(d), "@")
		if !ok {
			continue
		}
		_, etiqueta, ok := strings.Cut(loc, "+")
		if !ok {
			continue
		}
		c := arca.SoloDigitos(etiqueta)
		if len(c) == 11 && len(etiqueta) <= 13 && arca.CUITValido(c) {
			return c
		}
	}
	return ""
}

// ClienteDeRemitente busca el remitente en el mapeo «remitente → CUIT» de Clientes y canales:
// primero el email exacto, después "@dominio".
func ClienteDeRemitente(mapa map[string]string, email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return ""
	}
	dom := "@" + dominio(email)
	var porDominio string
	for k, v := range mapa {
		k = strings.ToLower(strings.TrimSpace(k))
		switch k {
		case email:
			return v
		case dom:
			porDominio = v
		}
	}
	return porDominio
}
