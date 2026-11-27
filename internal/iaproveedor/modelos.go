package iaproveedor

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/factorum/internal/avisoia"
)

// Modelo es una opción del select de modelos.
type Modelo struct {
	ID      string `json:"id"`
	Nombre  string `json:"nombre,omitempty"`  // el que muestra el proveedor (Anthropic, OpenRouter)
	Grupo   string `json:"grupo,omitempty"`   // OpenRouter: de quién es el modelo
	Detalle string `json:"detalle,omitempty"` // Ollama: tamaño
	// Imagenes: lee imágenes. Los que no (solo en Ollama, que lo dice) se muestran deshabilitados.
	Imagenes    bool         `json:"imagenes"`
	Nota        string       `json:"nota,omitempty"` // «no lee imágenes»
	Recomendado *Recomendado `json:"recomendado,omitempty"`

	creado int64 // para ordenar: los más nuevos arriba
}

// Motivos de error, para que la pantalla sepa qué hacer.
const (
	MotivoSinClave  = "sin_clave"
	MotivoClave     = "clave"
	MotivoRed       = "red"
	MotivoVacia     = "vacia"
	MotivoRespuesta = "respuesta"
)

// Error es un error con un mensaje para la persona. Nunca trae la clave.
type Error struct {
	Motivo  string
	Mensaje string
}

func (e *Error) Error() string { return e.Mensaje }

// Cliente arma el cliente para hablar con los proveedores: no sigue redirecciones (una clave no
// tiene que terminar en otro host) y el tiempo lo pone el contexto.
func Cliente(base *http.Client) *http.Client {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if base != nil {
		c.Transport = base.Transport
	}
	return c
}

// Listar pide la lista de modelos al proveedor, sin guardar nada. base es la URL de la API (la de
// la configuración, con /v1); clave puede venir vacía (Ollama y OpenRouter no la piden para listar).
func Listar(ctx context.Context, cli *http.Client, prov, base, clave string) ([]Modelo, error) {
	p, ok := Por(prov)
	if !ok {
		return nil, &Error{MotivoRespuesta, "No se conoce el proveedor " + prov}
	}
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = p.URL
	}
	if base == "" {
		return nil, &Error{MotivoRespuesta, "Escribí la URL del servidor (por ejemplo http://lmstudio:1234/v1)"}
	}
	if p.Clave == "si" && clave == "" && prov != OpenRouter {
		return nil, &Error{MotivoSinClave, "Escribí la clave " + Del(p.ID) + " para ver sus modelos"}
	}
	l := lector{cli: cli, prov: p, clave: clave}
	var ms []Modelo
	var err error
	switch prov {
	case Ollama:
		ms, err = l.ollama(ctx, base)
	case OpenAI:
		ms, err = l.openai(ctx, base)
	case Anthropic:
		ms, err = l.anthropic(ctx, base)
	case Gemini:
		ms, err = l.gemini(ctx, base)
	case OpenRouter:
		ms, err = l.openrouter(ctx, base)
	default:
		ms, err = l.otro(ctx, base)
	}
	if err != nil {
		return nil, err
	}
	if len(ms) == 0 {
		return nil, &Error{MotivoVacia, vacia(p)}
	}
	for i := range ms {
		ms[i].Recomendado = RecomendadoDe(prov, ms[i].ID)
	}
	return ms, nil
}

func vacia(p Proveedor) string {
	switch p.ID {
	case Ollama:
		return "Ollama no tiene modelos. Bajá uno que lea imágenes, por ejemplo: ollama pull qwen3-vl:4b-instruct"
	case Otro:
		return "El servidor no devolvió modelos"
	}
	return p.Corto + " no devolvió modelos que lean imágenes"
}

type lector struct {
	cli   *http.Client
	prov  Proveedor
	clave string
}

// pedir hace un pedido y devuelve el cuerpo si salió bien. Los errores dicen qué pasó sin la URL
// completa ni lo que mandó el proveedor si puede traer la clave.
func (l lector) pedir(ctx context.Context, metodo, u string, cab map[string]string, cuerpo any) ([]byte, error) {
	var rd io.Reader
	if cuerpo != nil {
		b, _ := json.Marshal(cuerpo)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, metodo, u, rd)
	if err != nil {
		return nil, &Error{MotivoRespuesta, "La URL no es válida"}
	}
	req.Header.Set("Accept", "application/json")
	if cuerpo != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range cab {
		req.Header.Set(k, v)
	}
	cli := l.cli
	if cli == nil {
		cli = Cliente(nil)
	}
	res, err := cli.Do(req)
	if err != nil {
		return nil, l.errorDeRed(err, u)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if res.StatusCode == 401 || res.StatusCode == 403 || (l.prov.ID == Gemini && res.StatusCode == 400 && strings.Contains(strings.ToLower(string(b)), "api key")) {
		if l.prov.ID == Ollama {
			return nil, &Error{MotivoClave, fmt.Sprintf("Ollama respondió %d: ¿está detrás de un proxy que pide clave?", res.StatusCode)}
		}
		if l.clave == "" {
			if l.prov.ID == Otro {
				return nil, &Error{MotivoSinClave, "El servidor pide una clave: escribila para ver sus modelos"}
			}
			return nil, &Error{MotivoSinClave, "Escribí la clave " + Del(l.prov.ID) + " para ver sus modelos"}
		}
		if l.prov.ID == Otro {
			return nil, &Error{MotivoClave, fmt.Sprintf("El servidor no acepta la clave (%d)", res.StatusCode)}
		}
		if d := avisoia.Detectar(u); l.prov.ID == OpenAI && d.Region != "" {
			return nil, &Error{MotivoClave, fmt.Sprintf("OpenAI no acepta la clave en %s: revisá la clave y que el proyecto tenga habilitada la residencia de datos en esa región", d.Host)}
		}
		return nil, &Error{MotivoClave, "La clave no es válida para " + l.prov.Corto}
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, &Error{MotivoRespuesta, fmt.Sprintf("%s respondió con una redirección (%d): revisá la URL", l.prov.Corto, res.StatusCode)}
	}
	if res.StatusCode == 404 {
		if l.prov.ID == Ollama {
			return nil, &Error{MotivoRespuesta, "En esa dirección responde algo que no es Ollama: revisá la URL (termina en /v1, por ejemplo http://ollama:11434/v1)"}
		}
		return nil, &Error{MotivoRespuesta, l.prov.Corto + " no tiene la lista de modelos en esa URL: revisá que termine en /v1"}
	}
	if res.StatusCode >= 300 {
		return nil, &Error{MotivoRespuesta, fmt.Sprintf("%s respondió %d%s", l.prov.Corto, res.StatusCode, mensajeDe(b, l.clave))}
	}
	return b, nil
}

// mensajeDe saca el mensaje de error que manda el proveedor, corto y sin la clave.
func mensajeDe(b []byte, clave string) string {
	var e struct {
		Error any `json:"error"`
	}
	m := ""
	if json.Unmarshal(b, &e) == nil {
		switch x := e.Error.(type) {
		case string:
			m = x
		case map[string]any:
			m, _ = x["message"].(string)
		}
	}
	m = Ocultar(strings.TrimSpace(m), clave)
	if m == "" {
		return ""
	}
	if r := []rune(m); len(r) > 200 {
		m = string(r[:200]) + "…"
	}
	return ": " + m
}

// Ocultar saca la clave de un texto (por si un proveedor la repite en un error).
func Ocultar(s, clave string) string {
	if len(clave) < 4 {
		return s
	}
	return strings.ReplaceAll(s, clave, "[clave]")
}

func (l lector) errorDeRed(err error, u string) error {
	m := err.Error()
	por := "error de red"
	switch {
	case strings.Contains(m, "no such host"), strings.Contains(m, "server misbehaving"):
		por = "no se encuentra el host"
	case strings.Contains(m, "connection refused"):
		por = "conexión rechazada"
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(m, "deadline exceeded"), strings.Contains(m, "Timeout"):
		por = "no contestó a tiempo"
	case strings.Contains(m, "certificate"):
		por = "problema con el certificado TLS"
	}
	if l.prov.ID == Ollama {
		return &Error{MotivoRed, fmt.Sprintf("Ollama no responde en %s (%s): ¿está prendido? Con el compose de Factorum: docker compose --profile ia up -d", raiz(u), por)}
	}
	if l.prov.Editable {
		return &Error{MotivoRed, fmt.Sprintf("El servidor no responde en %s (%s)", raiz(u), por)}
	}
	return &Error{MotivoRed, fmt.Sprintf("No se pudo conectar con %s (%s). Revisá que el servidor de Factorum tenga salida a internet", l.prov.Corto, por)}
}

// raiz es el esquema y el host de una URL (para los mensajes).
func raiz(u string) string {
	if i := strings.Index(u, "://"); i >= 0 {
		if j := strings.IndexByte(u[i+3:], '/'); j >= 0 {
			return u[:i+3+j]
		}
	}
	return u
}

func (l lector) bearer() map[string]string {
	if l.clave == "" {
		return nil
	}
	return map[string]string{"Authorization": "Bearer " + l.clave}
}

func respuestaRara(p Proveedor) error {
	return &Error{MotivoRespuesta, p.Corto + " respondió algo que no es una lista de modelos"}
}

// ---------- Ollama ----------

// ollamaBase saca el /v1 de la URL compatible con OpenAI: la API propia de Ollama está en la raíz.
func ollamaBase(base string) string {
	return strings.TrimSuffix(strings.TrimRight(base, "/"), "/v1")
}

type ollamaModelo struct {
	Name    string `json:"name"`
	Model   string `json:"model"`
	Size    int64  `json:"size"`
	Details struct {
		ParameterSize string `json:"parameter_size"`
	} `json:"details"`
	Capabilities []string `json:"capabilities"`
	ModifiedAt   string   `json:"modified_at"`
}

// ollama: GET /api/tags y, para los que no traen capabilities (versiones viejas de Ollama),
// POST /api/show de cada uno. Primero los que leen imágenes.
func (l lector) ollama(ctx context.Context, base string) ([]Modelo, error) {
	raizO := ollamaBase(base)
	b, err := l.pedir(ctx, http.MethodGet, raizO+"/api/tags", nil, nil)
	if err != nil {
		return nil, err
	}
	var t struct {
		Models *[]ollamaModelo `json:"models"`
	}
	if json.Unmarshal(b, &t) != nil || t.Models == nil {
		return nil, &Error{MotivoRespuesta, "En esa dirección responde algo que no es Ollama: revisá la URL (por ejemplo http://ollama:11434/v1)"}
	}
	ms := *t.Models
	if len(ms) > 200 {
		ms = ms[:200]
	}
	// Las capacidades que falten se piden con /api/show, de a cuatro.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range ms {
		if ms[i].Capabilities != nil {
			continue
		}
		wg.Add(1)
		go func(m *ollamaModelo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sb, err := l.pedir(ctx, http.MethodPost, raizO+"/api/show", nil, map[string]string{"model": nombreOllama(*m)})
			if err != nil {
				return
			}
			var s struct {
				Capabilities []string `json:"capabilities"`
			}
			if json.Unmarshal(sb, &s) == nil && s.Capabilities != nil {
				m.Capabilities = s.Capabilities
			}
		}(&ms[i])
	}
	wg.Wait()
	var res []Modelo
	for _, m := range ms {
		x := Modelo{ID: nombreOllama(m), Detalle: detalleOllama(m)}
		switch {
		case m.Capabilities == nil:
			x.Imagenes, x.Nota = true, "Ollama no dice si lee imágenes"
		case slices.Contains(m.Capabilities, "vision"):
			x.Imagenes = true
		default:
			x.Nota = "no lee imágenes"
		}
		if t, err := time.Parse(time.RFC3339Nano, m.ModifiedAt); err == nil {
			x.creado = t.Unix()
		}
		res = append(res, x)
	}
	slices.SortStableFunc(res, func(a, b Modelo) int {
		if a.Imagenes != b.Imagenes {
			if a.Imagenes {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return res, nil
}

func nombreOllama(m ollamaModelo) string {
	if m.Name != "" {
		return m.Name
	}
	return m.Model
}

func detalleOllama(m ollamaModelo) string {
	var p []string
	if m.Details.ParameterSize != "" {
		p = append(p, m.Details.ParameterSize)
	}
	if m.Size > 0 {
		p = append(p, strings.Replace(fmt.Sprintf("%.1f GB", float64(m.Size)/1e9), ".", ",", 1))
	}
	return strings.Join(p, " · ")
}

// ---------- OpenAI ----------

// Excluido es una regla de la lista de exclusión de OpenAI: lo que no sirve para leer una imagen
// con /chat/completions.
type Excluido struct {
	Patron string // expresión regular sobre el id
	Motivo string
}

// ExcluidosOpenAI: modelos de audio, de voz, de imágenes, de embeddings, de moderación y de
// búsqueda, los viejos de texto y los que solo andan con la API Responses (pro, codex).
var ExcluidosOpenAI = []Excluido{
	{`embedding`, "embeddings"},
	{`deep-research`, "solo con la API Responses"},
	{`(^|-)tts`, "voz"},
	{`whisper`, "audio"},
	{`transcribe`, "audio"},
	{`audio`, "audio"},
	{`realtime`, "voz en tiempo real"},
	{`(^|-)live($|-)`, "voz en vivo"},
	{`dall-e`, "genera imágenes"},
	{`image`, "genera imágenes"}, // gpt-image, chatgpt-image
	{`moderation`, "moderación"},
	{`search`, "búsqueda"},
	{`codex`, "solo con la API Responses"},
	{`computer-use`, "maneja una computadora"},
	{`(^|-)pro($|-)`, "solo con la API Responses"},
	{`davinci|babbage`, "modelo viejo de texto"},
	{`^gpt-3\.5`, "no lee imágenes"},
	{`instruct`, "no es de chat"},
}

var excluidosOpenAI = func() []*regexp.Regexp {
	rs := make([]*regexp.Regexp, len(ExcluidosOpenAI))
	for i, e := range ExcluidosOpenAI {
		rs[i] = regexp.MustCompile(e.Patron)
	}
	return rs
}()

// ExcluidoOpenAI dice si un modelo de OpenAI no sirve para Factorum, y por qué.
func ExcluidoOpenAI(id string) (string, bool) {
	id = strings.ToLower(id)
	for i, r := range excluidosOpenAI {
		if r.MatchString(id) {
			return ExcluidosOpenAI[i].Motivo, true
		}
	}
	return "", false
}

// conFecha: una versión fija (gpt-5.4-mini-2026-03-17). Se esconde si está el alias.
var conFecha = regexp.MustCompile(`-\d{4}-\d{2}-\d{2}$`)

type listaOpenAI struct {
	Data *[]struct {
		ID      string `json:"id"`
		Created int64  `json:"created"`
	} `json:"data"`
}

func (l lector) openai(ctx context.Context, base string) ([]Modelo, error) {
	b, err := l.pedir(ctx, http.MethodGet, base+"/models", l.bearer(), nil)
	if err != nil {
		return nil, err
	}
	var lo listaOpenAI
	if json.Unmarshal(b, &lo) != nil || lo.Data == nil {
		return nil, respuestaRara(l.prov)
	}
	ids := map[string]bool{}
	for _, m := range *lo.Data {
		ids[m.ID] = true
	}
	var res []Modelo
	for _, m := range *lo.Data {
		if _, fuera := ExcluidoOpenAI(m.ID); fuera {
			continue
		}
		if conFecha.MatchString(m.ID) && ids[conFecha.ReplaceAllString(m.ID, "")] {
			continue
		}
		res = append(res, Modelo{ID: m.ID, Imagenes: true, creado: m.Created})
	}
	ordenarNuevos(res)
	return res, nil
}

// ordenarNuevos: los más nuevos arriba y, entre los del mismo día, por nombre.
func ordenarNuevos(ms []Modelo) {
	slices.SortStableFunc(ms, func(a, b Modelo) int {
		if c := cmp.Compare(b.creado, a.creado); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
}

// ---------- Anthropic ----------

func (l lector) anthropic(ctx context.Context, base string) ([]Modelo, error) {
	cab := map[string]string{"x-api-key": l.clave, "anthropic-version": "2023-06-01"}
	var res []Modelo
	despues := ""
	for range 10 { // páginas de hasta 1000: alcanza y sobra
		u := base + "/models?limit=1000"
		if despues != "" {
			u += "&after_id=" + despues
		}
		b, err := l.pedir(ctx, http.MethodGet, u, cab, nil)
		if err != nil {
			return nil, err
		}
		var p struct {
			Data *[]struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				CreatedAt   string `json:"created_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(b, &p) != nil || p.Data == nil {
			return nil, respuestaRara(l.prov)
		}
		for _, m := range *p.Data {
			x := Modelo{ID: m.ID, Nombre: m.DisplayName, Imagenes: true} // todos los Claude actuales leen imágenes
			if t, err := time.Parse(time.RFC3339Nano, m.CreatedAt); err == nil {
				x.creado = t.Unix()
			}
			res = append(res, x)
		}
		if !p.HasMore || p.LastID == "" || p.LastID == despues {
			break
		}
		despues = p.LastID
	}
	ordenarNuevos(res)
	return res, nil
}

// ---------- Gemini ----------

// excluidosGemini: lo que no es para leer una imagen y contestar texto.
var excluidosGemini = regexp.MustCompile(`embedding|tts|(^|-)live($|-)|audio|-image|imagen|veo|computer-use|robotics|aqa`)

func (l lector) gemini(ctx context.Context, base string) ([]Modelo, error) {
	b, err := l.pedir(ctx, http.MethodGet, base+"/models", l.bearer(), nil)
	if err != nil {
		return nil, err
	}
	var lo listaOpenAI
	if json.Unmarshal(b, &lo) != nil || lo.Data == nil {
		return nil, respuestaRara(l.prov)
	}
	var res []Modelo
	for _, m := range *lo.Data {
		id := strings.TrimPrefix(m.ID, "models/")
		if !strings.HasPrefix(id, "gemini-") || excluidosGemini.MatchString(id) {
			continue
		}
		res = append(res, Modelo{ID: id, Imagenes: true})
	}
	// Sin fecha: por nombre al revés, que deja las versiones nuevas arriba (gemini-3 antes que gemini-2.5).
	slices.SortFunc(res, func(a, b Modelo) int { return cmp.Compare(b.ID, a.ID) })
	return res, nil
}

// ---------- OpenRouter ----------

func (l lector) openrouter(ctx context.Context, base string) ([]Modelo, error) {
	// Listar no pide clave: no se manda.
	b, err := l.pedir(ctx, http.MethodGet, base+"/models", nil, nil)
	if err != nil {
		return nil, err
	}
	var lo struct {
		Data *[]struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			Created      int64  `json:"created"`
			Architecture struct {
				Input  []string `json:"input_modalities"`
				Output []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &lo) != nil || lo.Data == nil {
		return nil, respuestaRara(l.prov)
	}
	var res []Modelo
	for _, m := range *lo.Data {
		if !slices.Contains(m.Architecture.Input, "image") || (len(m.Architecture.Output) > 0 && !slices.Contains(m.Architecture.Output, "text")) {
			continue
		}
		if strings.HasSuffix(m.ID, ":batch") { // los de procesamiento por lotes no contestan en el momento
			continue
		}
		grupo, nombre, ok := strings.Cut(m.Name, ": ")
		if !ok {
			grupo, _, _ = strings.Cut(m.ID, "/")
			nombre = m.Name
		}
		res = append(res, Modelo{ID: m.ID, Nombre: nombre, Grupo: grupo, Imagenes: true, creado: m.Created})
	}
	ordenarNuevos(res)
	slices.SortStableFunc(res, func(a, b Modelo) int { return cmp.Compare(strings.ToLower(a.Grupo), strings.ToLower(b.Grupo)) })
	return res, nil
}

// ---------- Otro ----------

func (l lector) otro(ctx context.Context, base string) ([]Modelo, error) {
	b, err := l.pedir(ctx, http.MethodGet, base+"/models", l.bearer(), nil)
	if err != nil {
		return nil, err
	}
	var lo listaOpenAI
	if json.Unmarshal(b, &lo) != nil || lo.Data == nil {
		return nil, &Error{MotivoRespuesta, "El servidor respondió algo que no es una lista de modelos compatible con OpenAI: revisá la URL (suele terminar en /v1)"}
	}
	var res []Modelo
	for _, m := range *lo.Data {
		res = append(res, Modelo{ID: m.ID, Imagenes: true})
	}
	slices.SortFunc(res, func(a, b Modelo) int { return cmp.Compare(a.ID, b.ID) })
	return res, nil
}
