// Package ajustes es la configuración que se hace desde la pantalla. Cada campo tiene su variable
// de entorno: si está puesta, gana y el campo queda bloqueado («fijado por el servidor»). Lo que se
// guarda va a la base; lo secreto, cifrado con FACTORUM_MASTER_KEY. Al guardar, los suscriptores
// (Arcanum, el modelo de visión, el almacén, los canales) se reconfiguran sin reiniciar.
package ajustes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/diegoparras/factorum/internal/almacen"
	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/store"
)

// Tipo de un campo.
type Tipo string

const (
	Texto    Tipo = "texto"
	URL      Tipo = "url"
	Bool     Tipo = "bool"
	Lista    Tipo = "lista"    // varias líneas o separadas por coma
	Secreto  Tipo = "secreto"  // nunca sale completo por la API
	CUIT     Tipo = "cuit"     // un CUIT válido o vacío
	Clientes Tipo = "clientes" // remitente -> CUIT de cliente
	Entero   Tipo = "entero"   // un número entre Min y Max
	Opcion   Tipo = "opcion"   // uno de Opciones
	Emails   Tipo = "emails"   // lista de emails (o @dominio, si se permite)
	Hora     Tipo = "hora"     // HH:MM
)

// OpcionCampo es un valor posible de un campo de tipo opción.
type OpcionCampo struct {
	Valor    string `json:"valor"`
	Etiqueta string `json:"etiqueta"`
}

// Campo es un valor configurable.
type Campo struct {
	Clave    string `json:"clave"`
	Seccion  string `json:"seccion"`
	Etiqueta string `json:"etiqueta"`
	Tipo     Tipo   `json:"tipo"`
	Env      string `json:"env"`
	Ayuda    string `json:"ayuda,omitempty"`
	Ejemplo  string `json:"ejemplo,omitempty"`

	Opciones []OpcionCampo `json:"opciones,omitempty"`
	Min      int           `json:"min,omitempty"`
	Max      int           `json:"max,omitempty"`
	Dominios bool          `json:"dominios,omitempty"` // emails: también acepta @dominio
	// Interno: lo guarda Factorum (por ejemplo, el permiso de Google), no la pantalla.
	Interno bool `json:"-"`

	get func(*config.Config) any
	set func(*config.Config, any)
}

func texto(get func(*config.Config) *string) (func(*config.Config) any, func(*config.Config, any)) {
	return func(c *config.Config) any { return *get(c) }, func(c *config.Config, v any) { *get(c) = v.(string) }
}

func booleano(get func(*config.Config) *bool) (func(*config.Config) any, func(*config.Config, any)) {
	return func(c *config.Config) any { return *get(c) }, func(c *config.Config, v any) { *get(c) = v.(bool) }
}

func entero(get func(*config.Config) *int) (func(*config.Config) any, func(*config.Config, any)) {
	return func(c *config.Config) any { return *get(c) }, func(c *config.Config, v any) { *get(c) = v.(int) }
}

func lista(get func(*config.Config) *[]string) (func(*config.Config) any, func(*config.Config, any)) {
	return func(c *config.Config) any { return slices.Clone(*get(c)) }, func(c *config.Config, v any) { *get(c) = slices.Clone(v.([]string)) }
}

func numero(clave, seccion, etiqueta, env, ayuda string, min, max int, g func(*config.Config) any, s func(*config.Config, any)) Campo {
	c := campo(clave, seccion, etiqueta, Entero, env, ayuda, "", g, s)
	c.Min, c.Max = min, max
	return c
}

func opcion(clave, seccion, etiqueta, env, ayuda string, ops []OpcionCampo, g func(*config.Config) any, s func(*config.Config, any)) Campo {
	c := campo(clave, seccion, etiqueta, Opcion, env, ayuda, "", g, s)
	c.Opciones = ops
	return c
}

func campo(clave, seccion, etiqueta string, tipo Tipo, env, ayuda, ejemplo string, g func(*config.Config) any, s func(*config.Config, any)) Campo {
	return Campo{Clave: clave, Seccion: seccion, Etiqueta: etiqueta, Tipo: tipo, Env: env, Ayuda: ayuda, Ejemplo: ejemplo, get: g, set: s}
}

// Secciones, en el orden de la pantalla.
var Secciones = []string{"general", "arcanum", "lockatus", "telegram", "whatsapp", "webhook", "correo_entrada", "correo_salida", "avisos", "ia", "r2", "copias", "papelera", "clientes"}

// Campos es todo lo que se configura desde la pantalla.
var Campos = func() []Campo {
	var cs []Campo
	add := func(c Campo) { cs = append(cs, c) }
	g, s := texto(func(c *config.Config) *string { return &c.Nombre })
	add(campo("general.nombre", "general", "Nombre de la instalación", Texto, "FACTORUM_NOMBRE", "Aparece arriba, al lado del logo.", "Factorum del estudio", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.URLPublica })
	add(campo("general.url_publica", "general", "URL pública", URL, "FACTORUM_URL_PUBLICA", "La dirección con la que se entra desde el navegador. Hace falta para el pase entre apps de la suite y para las cookies seguras.", "https://factorum.estudio.com.ar", g, s))
	g, s = booleano(func(c *config.Config) *bool { return &c.IAPorDefecto })
	add(campo("general.ia_por_defecto", "general", "Usar la IA cuando QR, texto y OCR no alcanzan", Bool, "FACTORUM_IA_POR_DEFECTO", "Lo que viene marcado al cargar. Sin modelo configurado no hace nada.", "", g, s))
	g, s = booleano(func(c *config.Config) *bool { return &c.ConstatarPorDefecto })
	add(campo("general.constatar_por_defecto", "general", "Constatar con ARCA al terminar cada lote", Bool, "FACTORUM_CONSTATAR_POR_DEFECTO", "Lo que viene marcado al cargar y lo que usan los chats. Hace falta Arcanum.", "", g, s))

	g, s = texto(func(c *config.Config) *string { return &c.ArcanumURL })
	add(campo("arcanum.url", "arcanum", "URL interna", URL, "ARCANUM_URL", "Cómo llega Factorum a Arcanum desde el servidor (en Docker, por el nombre del servicio).", "http://arcanum:8094", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.ArcanumURLPublica })
	add(campo("arcanum.url_publica", "arcanum", "URL pública", URL, "ARCANUM_URL_PUBLICA", "La que ve el navegador. Se usa para controlar a dónde lleva el pase de la suite.", "https://arcanum.estudio.com.ar", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.ArcanumAPIKey })
	add(campo("arcanum.api_key", "arcanum", "Clave de Arcanum", Secreto, "ARCANUM_API_KEY", "Una clave de API de Arcanum (ark_...). Para constatar alcanza con rol lectura; para abrir Arcanum desde acá, habilitala para la suite.", "ark_...", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.ArcanumConsultante })
	add(campo("arcanum.cuit_consultante", "arcanum", "CUIT consultante", CUIT, "ARCANUM_CUIT_CONSULTANTE", "El CUIT con certificado y WSCDC habilitado en Arcanum, para cuando el lote no tiene cliente.", "20-12345678-9", g, s))
	g, s = booleano(func(c *config.Config) *bool { return &c.SuiteCrearUsuarios })
	add(campo("arcanum.suite_crear_usuarios", "arcanum", "Crear la cuenta de quien llega desde otra app", Bool, "FACTORUM_SUITE_CREAR_USUARIOS", "Si alguien de Arcanum entra con el pase y no tiene cuenta acá, se la crea con su rol (topeado por la clave). Apagado, solo entran los que ya existen.", "", g, s))

	camposLockatus(add)

	g, s = texto(func(c *config.Config) *string { return &c.TelegramToken })
	add(campo("telegram.token", "telegram", "Token del bot", Secreto, "FACTORUM_TELEGRAM_TOKEN", "El que te da @BotFather.", "123456789:AA...", g, s))
	g, s = lista(func(c *config.Config) *[]string { return &c.TelegramPermitidos })
	add(campo("telegram.permitidos", "telegram", "Usuarios o chats permitidos", Lista, "FACTORUM_TELEGRAM_PERMITIDOS", "Ids numéricos, uno por línea. A los demás el bot les contesta su id y no procesa nada.", "123456789", g, s))

	g, s = booleano(func(c *config.Config) *bool { return &c.WhatsApp })
	add(campo("whatsapp.activo", "whatsapp", "Activar WhatsApp", Bool, "FACTORUM_WHATSAPP", "Como dispositivo vinculado de un número (no es la API oficial: el número se puede bloquear).", "", g, s))
	g, s = lista(func(c *config.Config) *[]string { return &c.WhatsAppPermitidos })
	add(campo("whatsapp.permitidos", "whatsapp", "Números permitidos", Lista, "FACTORUM_WHATSAPP_PERMITIDOS", "Con código de país, sin el 0 ni el 15, uno por línea.", "5491155550000", g, s))

	g, s = texto(func(c *config.Config) *string { return &c.WebhookToken })
	add(campo("webhook.token", "webhook", "Token de entrada", Secreto, "FACTORUM_WEBHOOK_TOKEN", "Lo manda tu bot en X-Factorum-Token. También firma (HMAC) los avisos de salida.", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.WebhookSalidaURL })
	add(campo("webhook.salida_url", "webhook", "URL de salida", URL, "FACTORUM_WEBHOOK_SALIDA_URL", "A dónde se mandan los avisos de fin de lote. Vacía: solo se contesta en la respuesta del webhook.", "https://n8n.estudio.com.ar/webhook/factorum", g, s))

	g, s = texto(func(c *config.Config) *string { return &c.LLMBaseURL })
	add(campo("ia.url", "ia", "Endpoint", URL, "LLM_API_BASE_URL", "Compatible con OpenAI: Ollama, LM Studio, vLLM...", "http://ollama:11434/v1", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.LLMModelo })
	add(campo("ia.modelo", "ia", "Modelo", Texto, "LLM_MODEL", "Un modelo que entienda imágenes.", "qwen2.5vl:7b", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.LLMAPIKey })
	add(campo("ia.api_key", "ia", "Clave", Secreto, "LLM_API_KEY", "Solo si el endpoint la pide (Ollama no).", "", g, s))
	// La aceptación del aviso de una IA externa: la guarda Factorum cuando un superadmin la acepta.
	// No tiene variable de entorno a propósito: aceptar es mirar el aviso en la pantalla.
	for _, a := range []struct {
		clave string
		tipo  Tipo
		get   func(*config.Config) any
		set   func(*config.Config, any)
	}{
		{"ia.aviso_host", Texto, func(c *config.Config) any { return c.IAAvisoHost }, func(c *config.Config, v any) { c.IAAvisoHost = v.(string) }},
		{"ia.aviso_version", Entero, func(c *config.Config) any { return c.IAAvisoVersion }, func(c *config.Config, v any) { c.IAAvisoVersion = v.(int) }},
		{"ia.aviso_por", Texto, func(c *config.Config) any { return c.IAAvisoPor }, func(c *config.Config, v any) { c.IAAvisoPor = v.(string) }},
		{"ia.aviso_cuando", Texto, func(c *config.Config) any { return c.IAAvisoCuando }, func(c *config.Config, v any) { c.IAAvisoCuando = v.(string) }},
	} {
		cp := campo(a.clave, "ia", "Aviso de la IA aceptado", a.tipo, "", "", "", a.get, a.set)
		cp.Interno, cp.Max = true, 1000
		add(cp)
	}

	g, s = texto(func(c *config.Config) *string { return &c.R2Endpoint })
	add(campo("r2.endpoint", "r2", "Endpoint", URL, "FACTORUM_R2_ENDPOINT", "R2 o cualquier S3 compatible.", "https://<cuenta>.r2.cloudflarestorage.com", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.R2Bucket })
	add(campo("r2.bucket", "r2", "Bucket", Texto, "FACTORUM_R2_BUCKET", "", "factorum", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.R2AccessKey })
	add(campo("r2.access_key", "r2", "Access key", Secreto, "FACTORUM_R2_ACCESS_KEY", "", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.R2SecretKey })
	add(campo("r2.secret_key", "r2", "Secret key", Secreto, "FACTORUM_R2_SECRET_KEY", "", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.R2Prefijo })
	add(campo("r2.prefijo", "r2", "Prefijo", Texto, "FACTORUM_R2_PREFIJO", "Carpeta dentro del bucket.", "factorum/", g, s))

	camposCorreo(add)
	g, s = booleano(func(c *config.Config) *bool { return &c.CopiasActivas })
	add(campo("copias.activo", "copias", "Hacer una copia de la base todos los días", Bool, "FACTORUM_COPIAS", "Lógica y portable, comprimida y cifrada con la clave maestra. Va a R2 si está activo; si no, al disco (carpeta copias).", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.CopiasHora })
	add(campo("copias.hora", "copias", "Hora (Buenos Aires)", Hora, "FACTORUM_COPIAS_HORA", "Conviene de madrugada. Si el servidor estaba apagado a esa hora, la copia se hace al volver.", "03:30", g, s))
	g, s = entero(func(c *config.Config) *int { return &c.CopiasDiarias })
	d := campo("copias.diarias", "copias", "Diarias que se guardan", Entero, "FACTORUM_COPIAS_DIARIAS", "Las últimas copias, una por día.", "14", g, s)
	d.Min, d.Max = 1, 366
	add(d)
	g, s = entero(func(c *config.Config) *int { return &c.CopiasMensuales })
	d = campo("copias.mensuales", "copias", "Mensuales que se guardan", Entero, "FACTORUM_COPIAS_MENSUALES", "Además, la primera copia de cada uno de los últimos meses.", "12", g, s)
	d.Max = 120
	add(d)

	g, s = entero(func(c *config.Config) *int { return &c.PapeleraDias })
	d = campo("papelera.dias", "papelera", "Días antes de borrar", Entero, "FACTORUM_PAPELERA_DIAS",
		"Lo que se borra (un lote entero o comprobantes sueltos) queda en la Papelera, donde se puede restaurar como estaba. Pasados estos días, se borra para siempre, con sus archivos.", "30", g, s)
	d.Min, d.Max = 1, 365
	add(d)

	add(campo("clientes.canales", "clientes", "Remitente y CUIT del cliente", Clientes, "FACTORUM_CANAL_CLIENTES",
		"Lo que llega por chat o por mail de ese número, usuario, email o @dominio se carga para ese cliente.", "5491155550000 = 20-22222222-3",
		func(c *config.Config) any { return maps.Clone(c.CanalClienteCUIT) },
		func(c *config.Config, v any) { c.CanalClienteCUIT = maps.Clone(v.(map[string]string)) }))
	return cs
}()

// RolesLockatus son los roles que se pueden elegir para quien entra con Lockatus sin un rol conocido.
var RolesLockatus = []OpcionCampo{{"lectura", "Lectura (solo mira)"}, {"usuario", "Usuario (carga, revisa y aprueba)"},
	{"admin", "Admin (además personas, claves y clientes)"}, {"superadmin", "Superadmin (todo)"}}

// camposLockatus: el inicio de sesión único con Lockatus (OIDC).
func camposLockatus(add func(Campo)) {
	const sec = "lockatus"
	g, s := texto(func(c *config.Config) *string { return &c.OIDCEmisor })
	add(campo("lockatus.emisor", sec, "Emisor (issuer)", URL, "FACTORUM_OIDC_ISSUER",
		"La dirección de Lockatus, tal como la publica en /.well-known/openid-configuration.", "https://lockatus.estudio.com.ar", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.OIDCClienteID })
	add(campo("lockatus.client_id", sec, "Client ID", Texto, "FACTORUM_OIDC_CLIENT_ID", "El del cliente que registraste en Lockatus para Factorum.", "factorum", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.OIDCClienteSecreto })
	add(campo("lockatus.client_secret", sec, "Client secret", Secreto, "FACTORUM_OIDC_CLIENT_SECRET", "El secreto de ese cliente (confidencial).", "", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.OIDCScope })
	add(campo("lockatus.scope", sec, "Scope", Texto, "FACTORUM_OIDC_SCOPE", "Separados por espacios. Hace falta email: la persona se reconoce por su email verificado.", "openid profile email", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.OIDCClaimRol })
	add(campo("lockatus.claim_rol", sec, "Claim del rol", Texto, "FACTORUM_OIDC_ROLE_CLAIM",
		"De qué claim del id_token sale el rol de quien entra por primera vez (superadmin, admin, usuario u operador, lectura).", "role", g, s))
	g, s = texto(func(c *config.Config) *string { return &c.OIDCRolPorDefecto })
	add(opcion("lockatus.rol_por_defecto", sec, "Rol para quien no trae uno conocido", "FACTORUM_OIDC_DEFAULT_ROLE",
		"Solo para cuentas nuevas. Lectura es lo más seguro.", RolesLockatus, g, s))
	g2, s2 := booleano(func(c *config.Config) *bool { return &c.OIDCCrearUsuarios })
	add(campo("lockatus.crear_usuarios", sec, "Crear la cuenta de quien entra por primera vez", Bool, "FACTORUM_OIDC_CREAR_USUARIOS",
		"Con el rol que trae (o el de arriba). Admin y superadmin ven todos los clientes; usuario y lectura, ninguno hasta que un admin se los asigne. Apagado, solo entran los que ya tienen cuenta acá.", "", g2, s2))
	g2, s2 = booleano(func(c *config.Config) *bool { return &c.OIDCSolo })
	add(campo("lockatus.solo", sec, "Solo se entra con Lockatus", Bool, "FACTORUM_OIDC_SOLO",
		"El login con email y contraseña desaparece y el servidor lo rechaza. Las claves de API y el pase de la suite siguen andando. Si Lockatus se cae, la salida de emergencia es FACTORUM_LOGIN_LOCAL=1.", "", g2, s2))
}

// CampoPorClave busca un campo.
func CampoPorClave(k string) (Campo, bool) {
	for _, c := range Campos {
		if c.Clave == k {
			return c, true
		}
	}
	return Campo{}, false
}

// Suscriptor recibe la configuración anterior y la nueva cada vez que cambia.
type Suscriptor func(viejo, nuevo *config.Config)

// Servicio guarda y reparte la configuración.
type Servicio struct {
	st     *store.Store
	sello  *almacen.Almacen // reusa el AES-256-GCM del almacén para los secretos
	base   *config.Config   // la del entorno
	Getenv func(string) string
	log    *slog.Logger

	mu       sync.Mutex // serializa Guardar y los avisos
	guardado map[string]store.FilaConfig
	actual   atomic.Pointer[config.Config]
	subs     []Suscriptor
}

// Nuevo arma el servicio. base es la configuración leída del entorno (con la clave maestra).
func Nuevo(st *store.Store, base *config.Config, log *slog.Logger) (*Servicio, error) {
	if base.ClaveMaestra == nil {
		return nil, errors.New("falta FACTORUM_MASTER_KEY (openssl rand -hex 32): cifra los archivos y la configuración secreta")
	}
	sello, err := almacen.Nuevo(nil, base.ClaveMaestra)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Servicio{st: st, sello: sello, base: base, Getenv: os.Getenv, log: log, guardado: map[string]store.FilaConfig{}}
	s.actual.Store(base)
	return s, nil
}

// Cargar lee lo guardado y arma la configuración vigente (no avisa a los suscriptores: al arrancar
// cada componente se arma con Actual()).
func (s *Servicio) Cargar(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, err := s.st.Configuracion(ctx)
	if err != nil {
		return err
	}
	c, err := s.calcular(g, true)
	if err != nil {
		return err
	}
	s.guardado = g
	s.actual.Store(c)
	return nil
}

// Actual es la configuración vigente.
func (s *Servicio) Actual() *config.Config { return s.actual.Load() }

// Suscribir agrega a quien se avisa en cada cambio.
func (s *Servicio) Suscribir(f Suscriptor) {
	s.mu.Lock()
	s.subs = append(s.subs, f)
	s.mu.Unlock()
}

// Bloqueado dice si el campo lo fija una variable de entorno.
func (s *Servicio) Bloqueado(c Campo) bool {
	return c.Env != "" && strings.TrimSpace(s.Getenv(c.Env)) != ""
}

// valorGuardado decodifica lo guardado de un campo.
func (s *Servicio) valorGuardado(c Campo, f store.FilaConfig) (any, error) {
	if c.Tipo == Secreto {
		if len(f.Secreto) == 0 {
			return "", nil
		}
		b, err := s.sello.Abrir(f.Secreto)
		if err != nil {
			return nil, fmt.Errorf("%s: no se pudo descifrar (¿cambió FACTORUM_MASTER_KEY?)", c.Clave)
		}
		return string(b), nil
	}
	return decodificar(c, f.Valor)
}

// calcular arma la configuración: la del entorno, pisada por lo guardado salvo en los campos
// bloqueados. Con tolerante, un valor ilegible se saltea (con aviso) en lugar de fallar.
func (s *Servicio) calcular(g map[string]store.FilaConfig, tolerante bool) (*config.Config, error) {
	n := s.base.Clonar()
	for _, c := range Campos {
		f, ok := g[c.Clave]
		if !ok || s.Bloqueado(c) {
			continue
		}
		v, err := s.valorGuardado(c, f)
		if err != nil {
			if tolerante {
				s.log.Warn("configuración guardada ilegible: se usa la del entorno", "clave", c.Clave, "error", err)
				continue
			}
			return nil, err
		}
		c.set(n, v)
	}
	if err := n.Validar(); err != nil {
		if tolerante {
			s.log.Warn("la configuración guardada no es válida: se usa la del entorno", "error", err)
			return s.base.Clonar(), nil
		}
		return nil, err
	}
	return n, nil
}

// VistaCampo es un campo como lo ve la pantalla. Un secreto nunca trae su valor: solo si está
// configurado y, si es largo, sus últimos 4 caracteres.
type VistaCampo struct {
	Campo
	Valor          any        `json:"valor"`
	Configurado    bool       `json:"configurado"`
	Ultimos        string     `json:"ultimos,omitempty"`
	Bloqueado      bool       `json:"bloqueado"`
	Origen         string     `json:"origen"` // entorno, pantalla o defecto
	ActualizadoPor string     `json:"actualizado_por,omitempty"`
	Actualizado    *time.Time `json:"actualizado,omitempty"`
}

func ultimos(s string) string {
	if utf8.RuneCountInString(s) < 12 {
		return ""
	}
	r := []rune(s)
	return string(r[len(r)-4:])
}

// Vista arma lo que muestra la pantalla de Configuración.
func (s *Servicio) Vista() []VistaCampo {
	s.mu.Lock()
	g := maps.Clone(s.guardado)
	s.mu.Unlock()
	c := s.Actual()
	res := make([]VistaCampo, 0, len(Campos))
	for _, cp := range Campos {
		if cp.Interno {
			continue
		}
		v := VistaCampo{Campo: cp, Bloqueado: s.Bloqueado(cp), Origen: "defecto"}
		f, guardado := g[cp.Clave]
		switch {
		case v.Bloqueado:
			v.Origen = "entorno"
		case guardado:
			v.Origen = "pantalla"
			v.ActualizadoPor = f.ActualizadoPor
			t := f.Actualizado
			v.Actualizado = &t
		}
		val := cp.get(c)
		if cp.Tipo == Secreto {
			sv, _ := val.(string)
			v.Configurado, v.Ultimos = sv != "", ultimos(sv)
		} else {
			v.Valor = val
			v.Configurado = !vacio(val)
		}
		res = append(res, v)
	}
	return res
}

func vacio(v any) bool {
	switch x := v.(type) {
	case string:
		return x == ""
	case []string:
		return len(x) == 0
	case map[string]string:
		return len(x) == 0
	case int:
		return x == 0
	case bool:
		return false
	}
	return v == nil
}

// ErrBloqueado: se quiso cambiar un campo que fija el entorno.
var ErrBloqueado = errors.New("fijado por el servidor")

// ErrInvalido: un valor que no se entiende.
var ErrInvalido = errors.New("valor inválido")

// Aplicar arma, sin guardar, la configuración que quedaría con esos cambios (sirve para «Probar»
// antes de guardar). Un secreto vacío o que no viene usa el guardado.
func (s *Servicio) Aplicar(cambios map[string]json.RawMessage) (*config.Config, error) {
	n := s.Actual().Clonar()
	for k, raw := range cambios {
		c, ok := CampoPorClave(k)
		if !ok {
			return nil, fmt.Errorf("%w: %q no es un campo de la configuración", ErrInvalido, k)
		}
		if s.Bloqueado(c) || esNulo(raw) {
			continue
		}
		v, err := decodificar(c, raw)
		if err != nil {
			return nil, err
		}
		if c.Tipo == Secreto && v.(string) == "" {
			continue
		}
		c.set(n, v)
	}
	return n, n.Validar()
}

func esNulo(raw json.RawMessage) bool {
	return len(raw) == 0 || strings.TrimSpace(string(raw)) == "null"
}

// Guardar aplica los cambios (clave -> valor JSON; null borra lo guardado y vuelve al valor del
// entorno o al de fábrica), los guarda y avisa a los suscriptores. Devuelve las claves que de
// verdad cambiaron. Un secreto con "" no se toca.
func (s *Servicio) Guardar(ctx context.Context, cambios map[string]json.RawMessage, quien string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nuevo, filas, cambiadas, err := s.preparar(cambios, quien)
	if err != nil {
		return nil, err
	}
	if len(filas) == 0 {
		return []string{}, nil
	}
	cfg, err := s.calcular(nuevo, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalido, err)
	}
	if err := s.st.GuardarConfiguracion(ctx, filas, quien); err != nil {
		return nil, err
	}
	viejo := s.actual.Load()
	s.guardado = nuevo
	s.actual.Store(cfg)
	for _, f := range s.subs {
		f(viejo, cfg)
	}
	return cambiadas, nil
}

// Previa arma, sin guardar, la configuración que quedaría si se guardaran esos cambios, igual que
// Guardar (con null volviendo al valor del entorno o al de fábrica).
func (s *Servicio) Previa(cambios map[string]json.RawMessage) (*config.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	nuevo, _, _, err := s.preparar(cambios, "")
	if err != nil {
		return nil, err
	}
	cfg, err := s.calcular(nuevo, false)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalido, err)
	}
	return cfg, nil
}

// preparar valida los cambios y arma lo que quedaría guardado, las filas a escribir y las claves
// que cambian. Se llama con s.mu tomado.
func (s *Servicio) preparar(cambios map[string]json.RawMessage, quien string) (map[string]store.FilaConfig, []store.FilaConfig, []string, error) {
	nuevo := maps.Clone(s.guardado)
	var filas []store.FilaConfig
	var cambiadas []string
	claves := slices.Sorted(maps.Keys(cambios))
	for _, k := range claves {
		raw := cambios[k]
		c, ok := CampoPorClave(k)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: %q no es un campo de la configuración", ErrInvalido, k)
		}
		if s.Bloqueado(c) {
			return nil, nil, nil, fmt.Errorf("%w: %s (variable %s)", ErrBloqueado, c.Etiqueta, c.Env)
		}
		previo, tenia := s.guardado[k]
		if esNulo(raw) {
			if tenia {
				delete(nuevo, k)
				filas = append(filas, store.FilaConfig{Clave: k})
				cambiadas = append(cambiadas, k)
			}
			continue
		}
		v, err := decodificar(c, raw)
		if err != nil {
			return nil, nil, nil, err
		}
		f := store.FilaConfig{Clave: k, ActualizadoPor: quien, Actualizado: time.Now()}
		if c.Tipo == Secreto {
			sv := v.(string)
			if sv == "" {
				continue
			}
			if tenia {
				if viejo, err := s.valorGuardado(c, previo); err == nil && viejo == sv {
					continue
				}
			}
			if f.Secreto, err = s.sello.Sellar([]byte(sv)); err != nil {
				return nil, nil, nil, err
			}
		} else {
			f.Valor, _ = json.Marshal(v)
			if tenia && string(normalizarJSON(previo.Valor)) == string(f.Valor) {
				continue
			}
		}
		nuevo[k] = f
		filas = append(filas, f)
		cambiadas = append(cambiadas, k)
	}
	return nuevo, filas, cambiadas, nil
}

func normalizarJSON(b []byte) []byte {
	var v any
	if json.Unmarshal(b, &v) != nil {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// decodificar lee y valida el valor de un campo.
func decodificar(c Campo, raw json.RawMessage) (any, error) {
	mal := func(msg string) error { return fmt.Errorf("%w: %s: %s", ErrInvalido, c.Etiqueta, msg) }
	switch c.Tipo {
	case Texto, Secreto, URL, CUIT:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, mal("tiene que ser un texto")
		}
		v = strings.TrimSpace(v)
		if len(v) > 4096 {
			return nil, mal("es demasiado largo")
		}
		switch c.Tipo {
		case URL:
			v = strings.TrimRight(v, "/")
			if v != "" {
				u, err := url.Parse(v)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					return nil, mal("tiene que ser una URL que empiece con http:// o https://")
				}
			}
		case CUIT:
			v = arca.SoloDigitos(v)
			if v != "" && !arca.CUITValido(v) {
				return nil, mal("no es un CUIT válido")
			}
		}
		return v, nil
	case Entero:
		var v int
		if err := json.Unmarshal(raw, &v); err != nil {
			var t string
			if err := json.Unmarshal(raw, &t); err != nil {
				return nil, mal("tiene que ser un número")
			}
			n, err := strconv.Atoi(strings.TrimSpace(t))
			if err != nil {
				return nil, mal("tiene que ser un número")
			}
			v = n
		}
		if v < c.Min || (c.Max > 0 && v > c.Max) {
			return nil, mal(fmt.Sprintf("tiene que ser entre %d y %d", c.Min, c.Max))
		}
		return v, nil
	case Hora:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, mal("tiene que ser una hora")
		}
		h, m, ok := config.HoraMinuto(v)
		if !ok {
			return nil, mal("tiene que ser una hora HH:MM, por ejemplo 03:30")
		}
		return fmt.Sprintf("%02d:%02d", h, m), nil
	case Bool:
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, mal("tiene que ser sí o no")
		}
		return v, nil
	case Lista:
		var l []string
		if err := json.Unmarshal(raw, &l); err != nil {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, mal("tiene que ser una lista")
			}
			l = strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' || r == ';' })
		}
		res := []string{}
		for _, x := range l {
			if x = strings.TrimSpace(x); x != "" && !slices.Contains(res, x) {
				res = append(res, x)
			}
		}
		return res, nil
	case Opcion:
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, mal("tiene que ser un texto")
		}
		v = strings.ToLower(strings.TrimSpace(v))
		for _, o := range c.Opciones {
			if o.Valor == v {
				return v, nil
			}
		}
		var ops []string
		for _, o := range c.Opciones {
			ops = append(ops, o.Valor)
		}
		return nil, mal("tiene que ser " + strings.Join(ops, ", "))
	case Emails:
		l, err := decodificar(Campo{Tipo: Lista, Etiqueta: c.Etiqueta}, raw)
		if err != nil {
			return nil, err
		}
		res := []string{}
		for _, x := range l.([]string) {
			x = strings.ToLower(x)
			if !emailValido(x, c.Dominios) {
				if c.Dominios {
					return nil, mal(fmt.Sprintf("%q no es un email ni un @dominio", x))
				}
				return nil, mal(fmt.Sprintf("%q no es un email", x))
			}
			if !slices.Contains(res, x) {
				res = append(res, x)
			}
		}
		return res, nil
	case Clientes:
		m := map[string]string{}
		var obj map[string]string
		if err := json.Unmarshal(raw, &obj); err != nil {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return nil, mal("tiene que ser una lista de remitente = CUIT")
			}
			obj = map[string]string{}
			for _, par := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '\n' }) {
				if strings.TrimSpace(par) == "" {
					continue
				}
				k, v, ok := strings.Cut(par, "=")
				if !ok {
					return nil, mal(fmt.Sprintf("%q no tiene la forma remitente = CUIT", strings.TrimSpace(par)))
				}
				obj[strings.TrimSpace(k)] = v
			}
		}
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			rem := strings.TrimSpace(k)
			if strings.Contains(rem, "@") {
				rem = strings.ToLower(rem) // los emails no distinguen mayúsculas
			}
			cuit := arca.SoloDigitos(obj[k])
			if rem == "" {
				return nil, mal("falta el remitente")
			}
			if !arca.CUITValido(cuit) {
				return nil, mal(fmt.Sprintf("el CUIT de %s no es válido", rem))
			}
			m[rem] = cuit
		}
		return m, nil
	}
	return nil, mal("tipo desconocido")
}
