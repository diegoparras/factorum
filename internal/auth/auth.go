// Package auth tiene lo que Factorum necesita para saber quién pide algo y qué puede hacer: los
// roles, la identidad de un pedido (sesión, clave de API o clave del entorno), el alcance por CUIT
// de cliente, las contraseñas (argon2id), los tokens (sesiones, claves y pases) y el límite de
// intentos de entrada. No toca la base ni HTTP.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/diegoparras/factorum/internal/arca"
)

// Rol de una persona o una clave, de menor a mayor.
type Rol int

const (
	Ninguno    Rol = iota
	Lectura        // solo mira: GET y las consultas que no modifican
	Usuario        // carga, verifica, revisa, corrige, aprueba y exporta
	Admin          // además: usuarios de rol hasta admin, claves de API, clientes y auditoría
	Superadmin     // todo, incluida la configuración y los superadmin
)

var nombres = map[Rol]string{Lectura: "lectura", Usuario: "usuario", Admin: "admin", Superadmin: "superadmin"}

// Roles válidos, de menor a mayor.
var Roles = []Rol{Lectura, Usuario, Admin, Superadmin}

func (r Rol) String() string {
	if n, ok := nombres[r]; ok {
		return n
	}
	return "ninguno"
}

// MarshalJSON escribe el rol por su nombre.
func (r Rol) MarshalJSON() ([]byte, error) { return json.Marshal(r.String()) }

// UnmarshalJSON lee el rol por su nombre.
func (r *Rol) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, ok := ParsearRol(s)
	if !ok {
		return fmt.Errorf("rol %q desconocido (puede ser lectura, usuario, admin o superadmin)", s)
	}
	*r = v
	return nil
}

// ParsearRol lee un rol de Factorum. Acepta "operador", el nombre viejo de usuario.
func ParsearRol(s string) (Rol, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "operador" {
		return Usuario, true
	}
	for r, n := range nombres {
		if n == s {
			return r, true
		}
	}
	return Ninguno, false
}

// RolDeApp traduce el rol que una persona tiene en otra app de la suite al de Factorum (tabla del
// contrato del pase, docs/suite-pase.md).
func RolDeApp(app, rol string) (Rol, bool) {
	rol = strings.ToLower(strings.TrimSpace(rol))
	switch strings.ToLower(app) {
	case "arcanum":
		switch rol {
		case "superadmin":
			return Superadmin, true
		case "admin":
			return Admin, true
		case "operador":
			return Usuario, true
		case "lectura":
			return Lectura, true
		}
		return Ninguno, false
	case "factorum":
		r, ok := ParsearRol(rol)
		return r, ok && r != Ninguno
	}
	return Ninguno, false
}

// NombreEnApp es el nombre de un rol de Factorum en otra app de la suite.
func NombreEnApp(app string, r Rol) string {
	if strings.ToLower(app) == "arcanum" && r == Usuario {
		return "operador"
	}
	return r.String()
}

// ---------- Identidad ----------

// Tipos de identidad.
const (
	TipoSesion = "sesion" // cookie del navegador
	TipoClave  = "clave"  // clave de API guardada en la base (fct_...)
	TipoEnv    = "env"    // FACTORUM_API_KEY o FACTORUM_API_KEY_LECTURA
)

// Identidad es quién hace un pedido y qué alcance tiene.
type Identidad struct {
	Tipo        string   `json:"tipo"`
	UsuarioID   int64    `json:"usuario_id,omitempty"`
	Email       string   `json:"email,omitempty"`
	Nombre      string   `json:"nombre,omitempty"`
	Rol         Rol      `json:"rol"`
	CUITs       []string `json:"cuits"` // nil = todos los clientes
	DebeCambiar bool     `json:"debe_cambiar,omitempty"`
	Origen      string   `json:"origen,omitempty"` // local, suite:arcanum

	SesionID     string `json:"-"` // hash de la cookie
	ClaveID      int64  `json:"-"`
	ClavePrefijo string `json:"clave,omitempty"`
	Suite        bool   `json:"-"` // la clave puede abrir sesiones de la suite
	SuiteTope    Rol    `json:"-"`
}

// Quien es cómo aparece en la auditoría y en el papel de trabajo.
func (i *Identidad) Quien() string {
	switch {
	case i == nil:
		return ""
	case i.Email != "":
		return i.Email
	case i.ClavePrefijo != "":
		return "clave " + i.ClavePrefijo
	case i.Tipo == TipoEnv:
		return "clave del entorno (" + i.Rol.String() + ")"
	}
	return "desconocido"
}

// Restringido dice si la identidad solo ve algunos clientes.
func (i *Identidad) Restringido() bool { return i != nil && i.CUITs != nil }

// Permite dice si la identidad puede ver o tocar lo de ese CUIT de cliente. Sin restricción, todo;
// con restricción, un comprobante sin cliente ("") no se ve.
func (i *Identidad) Permite(cuit string) bool {
	if i == nil {
		return false
	}
	if i.CUITs == nil {
		return true
	}
	return slices.Contains(i.CUITs, arca.SoloDigitos(cuit))
}

// Alcance es el filtro de CUITs para la base: nil = todos.
func (i *Identidad) Alcance() []string {
	if i == nil {
		return []string{}
	}
	return i.CUITs
}

// ---------- CUITs ----------

// NormalizarCUITs limpia una lista de CUITs: solo dígitos, sin repetidos, ordenada. Vacía = nil
// (todos). Devuelve error si alguno no es un CUIT válido.
func NormalizarCUITs(l []string) ([]string, error) {
	var res []string
	for _, c := range l {
		d := arca.SoloDigitos(c)
		if d == "" {
			continue
		}
		if !arca.CUITValido(d) {
			return nil, fmt.Errorf("%q no es un CUIT válido", c)
		}
		if !slices.Contains(res, d) {
			res = append(res, d)
		}
	}
	slices.Sort(res)
	return res, nil
}

// Intersecar combina dos alcances (nil = todos). El resultado puede ser una lista vacía no nil:
// ningún cliente en común.
func Intersecar(a, b []string) []string {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		return slices.Clone(b)
	case b == nil:
		return slices.Clone(a)
	}
	res := []string{}
	for _, x := range a {
		if slices.Contains(b, x) && !slices.Contains(res, x) {
			res = append(res, x)
		}
	}
	slices.Sort(res)
	return res
}

// Contenido dice si el alcance a cabe en b (nil = todos).
func Contenido(a, b []string) bool {
	if b == nil {
		return true
	}
	if a == nil {
		return false
	}
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return true
}

// ---------- Contraseñas ----------

// MinClave es el largo mínimo de una contraseña.
const MinClave = 10

const maxClave = 256

// ValidarClave controla el largo de una contraseña nueva.
func ValidarClave(p string) error {
	n := utf8.RuneCountInString(p)
	if n < MinClave {
		return fmt.Errorf("la contraseña tiene que tener al menos %d caracteres", MinClave)
	}
	if len(p) > maxClave {
		return errors.New("la contraseña es demasiado larga")
	}
	return nil
}

// Parámetros de argon2id (los que recomienda OWASP: 19 MiB, 2 pasadas, 1 hilo).
const (
	argonMem   = 19 * 1024
	argonTiem  = 2
	argonHilos = 1
	argonLargo = 32
)

// HashClave arma el hash argon2id en formato PHC: $argon2id$v=19$m=...,t=...,p=...$sal$hash.
func HashClave(p string) (string, error) {
	sal := make([]byte, 16)
	if _, err := rand.Read(sal); err != nil {
		return "", err
	}
	h := argon2.IDKey([]byte(p), sal, argonTiem, argonMem, argonHilos, argonLargo)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMem, argonTiem, argonHilos, b64.EncodeToString(sal), b64.EncodeToString(h)), nil
}

// VerificarClave compara una contraseña con su hash en tiempo constante.
func VerificarClave(hash, p string) bool {
	partes := strings.Split(hash, "$")
	if len(partes) != 6 || partes[1] != "argon2id" {
		return false
	}
	var v int
	if _, err := fmt.Sscanf(partes[2], "v=%d", &v); err != nil || v != argon2.Version {
		return false
	}
	var m, t uint32
	var hilos uint8
	if _, err := fmt.Sscanf(partes[3], "m=%d,t=%d,p=%d", &m, &t, &hilos); err != nil || m > 1<<20 || t > 20 || hilos == 0 {
		return false
	}
	b64 := base64.RawStdEncoding
	sal, err1 := b64.DecodeString(partes[4])
	h, err2 := b64.DecodeString(partes[5])
	if err1 != nil || err2 != nil || len(h) == 0 {
		return false
	}
	otro := argon2.IDKey([]byte(p), sal, t, m, hilos, uint32(len(h)))
	return subtle.ConstantTimeCompare(h, otro) == 1
}

// hashFalso sirve para gastar el mismo tiempo cuando el email no existe (no se puede adivinar quién
// tiene cuenta midiendo cuánto tarda la respuesta).
var hashFalso, _ = HashClave("no-es-la-clave-de-nadie")

// GastarTiempo hace una verificación de mentira.
func GastarTiempo(p string) { _ = VerificarClave(hashFalso, p) }

// ---------- Tokens ----------

// NuevoToken devuelve 32 bytes aleatorios en base64url (sesiones, pases).
func NuevoToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // sin azar no hay seguridad posible
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken es lo que se guarda en la base de un token: su SHA-256 en hex.
func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

// PrefijoClave es el comienzo de toda clave de API guardada en la base.
const PrefijoClave = "fct_"

// NuevaClaveAPI devuelve la clave completa (se muestra una sola vez) y su prefijo visible.
func NuevaClaveAPI() (clave, prefijo string) {
	clave = PrefijoClave + NuevoToken()
	return clave, clave[:len(PrefijoClave)+8]
}

// ClaveTemporal genera una contraseña temporal fácil de dictar: 4 grupos de 4 letras y números,
// sin los que se confunden (0/O, 1/l/I).
func ClaveTemporal() string {
	const abc = "abcdefghjkmnpqrstuvwxyz23456789"
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var s strings.Builder
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			s.WriteByte('-')
		}
		s.WriteByte(abc[int(x)%len(abc)])
	}
	return s.String()
}

// CodigoArranque es el código de un solo uso para crear el primer superadmin.
func CodigoArranque() string {
	const abc = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	var s strings.Builder
	for i, x := range b {
		if i > 0 && i%4 == 0 {
			s.WriteByte('-')
		}
		s.WriteByte(abc[int(x)%len(abc)])
	}
	return s.String()
}

// Iguales compara dos secretos en tiempo constante (tampoco filtra el largo).
func Iguales(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}
