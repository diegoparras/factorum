package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/avisoia"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/iaproveedor"
)

// clienteIA habla con los proveedores de IA: sin seguir redirecciones y con el tiempo del contexto
// (Ollama sin GPU tarda más de un minuto por imagen).
func (s *Server) clienteIA() *http.Client { return iaproveedor.Cliente(s.HTTPIA) }

// catalogoIA es lo que Configuración necesita para armar la sección IA.
func catalogoIA(c *config.Config) map[string]any {
	d := avisoia.Detectar(c.LLMBaseURL)
	return map[string]any{
		"proveedores":  iaproveedor.Lista,
		"recomendados": iaproveedor.Recomendados,
		"prueba":       iaproveedor.PruebaTexto,
		"proveedor":    iaproveedor.Efectivo(c.IAProveedor, c.LLMBaseURL),
		"region":       d.Region,
	}
}

// claveIA decide qué clave va a un proveedor: la escrita en pantalla o, si está vacía, la que ya
// tiene la configuración (guardada o del entorno), pero solo si es para el mismo host (o, en OpenAI,
// otra de sus regiones): una clave nunca viaja a otro lado. A Ollama no se le manda.
func claveIA(actual *config.Config, prov, base, escrita string) (clave string, guardada bool) {
	if prov == iaproveedor.Ollama {
		return "", false
	}
	if escrita = strings.TrimSpace(escrita); escrita != "" {
		return escrita, false
	}
	if actual.LLMAPIKey != "" && iaproveedor.MismoDestino(base, actual.LLMBaseURL) {
		return actual.LLMAPIKey, true
	}
	return "", false
}

// urlIA limpia y controla la URL de un proveedor.
func urlIA(p iaproveedor.Proveedor, base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = p.URL
	}
	if base == "" {
		return "", errors.New("Escribí la URL del servidor (por ejemplo http://lmstudio:1234/v1)")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.New("La URL tiene que empezar con http:// o https://")
	}
	if !iaproveedor.Compatible(p.ID, base) {
		return "", fmt.Errorf("Esa URL no es de %s: elegí «Otro compatible con OpenAI» si es un gateway", p.Corto)
	}
	return base, nil
}

// POST /api/ia/modelos {proveedor, url, clave}: la lista de modelos del proveedor para elegir en
// Configuración. No guarda nada; la clave se usa para este pedido y nunca vuelve ni se loguea.
func (s *Server) modelosIA(w http.ResponseWriter, r *http.Request) {
	if s.Ajustes == nil {
		jsonErr(w, http.StatusServiceUnavailable, "La configuración desde la pantalla no está disponible")
		return
	}
	var b struct {
		Proveedor string `json:"proveedor"`
		URL       string `json:"url"`
		Clave     string `json:"clave"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	p, ok := iaproveedor.Por(b.Proveedor)
	if !ok {
		jsonErr(w, http.StatusBadRequest, "Elegí un proveedor de la lista")
		return
	}
	actual := s.App.Config()
	if s.bloqueado("ia.url") { // la URL la fija el servidor
		b.URL = actual.LLMBaseURL
	}
	base, err := urlIA(p, b.URL)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	clave, guardada := claveIA(actual, p.ID, base, b.Clave)
	if s.bloqueado("ia.api_key") && b.Clave != "" { // la clave la fija el servidor: se usa esa
		clave, guardada = claveIA(actual, p.ID, base, "")
	}
	d := avisoia.Detectar(base)
	res := map[string]any{"proveedor": p.ID, "url": base, "clave_guardada": guardada, "local": d.Local,
		"sello": avisoia.Aviso(d).Sello, "recomendados": iaproveedor.RecomendadosDe(p.ID)}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	ms, err := iaproveedor.Listar(ctx, s.clienteIA(), p.ID, base, clave)
	if err != nil {
		motivo := iaproveedor.MotivoRespuesta
		var e *iaproveedor.Error
		if errors.As(err, &e) {
			motivo = e.Motivo
		}
		res["exito"], res["motivo"], res["mensaje"] = false, motivo, iaproveedor.Ocultar(capital(err.Error()), clave)
		jsonOK(w, http.StatusOK, res)
		return
	}
	res["exito"], res["modelos"] = true, ms
	jsonOK(w, http.StatusOK, res)
}

// textoDe lee un valor de texto de los cambios ("" si no viene o no es texto).
func textoDe(cambios map[string]json.RawMessage, k string) string {
	var v string
	if raw, ok := cambios[k]; ok {
		_ = json.Unmarshal(raw, &v)
	}
	return strings.TrimSpace(v)
}

// probarIA prueba la IA con lo que está en pantalla, aunque no esté guardado: le manda el ticket de
// prueba y dice qué leyó y cuánto tardó. Si es externa y nadie aceptó el aviso para ese host, antes
// responde 409 con el aviso (como al guardar); aceptarlo para probar no lo deja aceptado.
func (s *Server) probarIA(w http.ResponseWriter, r *http.Request, cambios map[string]json.RawMessage, acepta *avisoPedido) {
	cfg, err := s.Ajustes.Aplicar(cambios)
	if err != nil {
		jsonOK(w, http.StatusOK, map[string]any{"exito": false, "mensaje": capital(strings.TrimPrefix(err.Error(), "valor inválido: "))})
		return
	}
	prov := iaproveedor.Efectivo(cfg.IAProveedor, cfg.LLMBaseURL)
	falta := ""
	switch {
	case cfg.LLMBaseURL == "":
		falta = "Falta la URL"
	case cfg.IAProveedor != "" && !iaproveedor.Compatible(cfg.IAProveedor, cfg.LLMBaseURL):
		falta = "Esa URL no es de " + iaproveedor.Nombre(cfg.IAProveedor)
	case cfg.LLMModelo == "":
		falta = "Elegí un modelo"
	}
	if falta != "" {
		jsonOK(w, http.StatusOK, map[string]any{"exito": false, "mensaje": falta})
		return
	}
	cfg.LLMAPIKey, _ = claveIA(s.App.Config(), prov, cfg.LLMBaseURL, textoDe(cambios, "ia.api_key"))
	if s.bloqueado("ia.api_key") && prov != iaproveedor.Ollama && iaproveedor.MismoDestino(cfg.LLMBaseURL, s.App.Config().LLMBaseURL) {
		cfg.LLMAPIKey = s.App.Config().LLMAPIKey
	}
	p, _ := iaproveedor.Por(prov)
	if p.Clave == "si" && cfg.LLMAPIKey == "" {
		jsonOK(w, http.StatusOK, map[string]any{"exito": false, "mensaje": "Escribí la clave " + iaproveedor.Del(p.ID)})
		return
	}
	id := IdentidadDe(r.Context())
	d := avisoia.Detectar(cfg.LLMBaseURL)
	if d.Externa() && !avisoia.Aceptado(cfg) {
		if acepta == nil || !strings.EqualFold(strings.TrimSpace(acepta.Host), d.Host) || acepta.Version != avisoia.Version {
			jsonCrudo(w, http.StatusConflict, map[string]any{"ok": false, "codigo": CodigoAvisoIA,
				"error":    "Es una IA externa: antes de mandarle el ticket de prueba, mirá qué pasa con los datos",
				"aviso_ia": s.vistaAviso(r.Context(), cfg, id)})
			return
		}
		s.auditar(r, "ia.aviso_aceptado_prueba", d.Host, map[string]any{"proveedor": d.Proveedor, "host": d.Host, "version": avisoia.Version})
	}
	timeout := cfg.LLMTimeout
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout+5*time.Second)
	defer cancel()
	v := &extractor.Vision{BaseURL: cfg.LLMBaseURL, APIKey: cfg.LLMAPIKey, Modelo: cfg.LLMModelo, Timeout: timeout, HTTP: s.clienteIA()}
	res := iaproveedor.Probar(ctx, v, prov, s.ahora)
	s.auditar(r, "config.probar_ia", d.Host, map[string]any{"proveedor": prov, "host": d.Host, "modelo": cfg.LLMModelo, "exito": res.Exito})
	jsonOK(w, http.StatusOK, map[string]any{"exito": res.Exito, "mensaje": res.Mensaje, "segundos": res.Segundos, "modelo": res.Modelo, "leido": res.Leido})
}

// prepararCambiosIA controla, antes de guardar, que el proveedor y la URL vayan juntos, y saca la
// clave guardada si cambia el host sin una clave nueva (no tiene que viajar a otro lado; una región
// de OpenAI no es otro lado) o si el proveedor es Ollama. Si algo está mal, responde y devuelve false.
func (s *Server) prepararCambiosIA(w http.ResponseWriter, cambios map[string]json.RawMessage) bool {
	toca := false
	for k := range cambios {
		toca = toca || (strings.HasPrefix(k, "ia.") && !strings.HasPrefix(k, "ia.aviso_"))
	}
	if !toca {
		return true
	}
	actual := s.App.Config()
	previa, err := s.Ajustes.Previa(cambios)
	if err != nil {
		return true // Guardar dice qué está mal
	}
	if previa.IAProveedor != "" && previa.LLMBaseURL != "" && !iaproveedor.Compatible(previa.IAProveedor, previa.LLMBaseURL) {
		jsonErr(w, http.StatusBadRequest, fmt.Sprintf("La URL %s no es de %s: elegí «Otro compatible con OpenAI» si es un gateway", previa.LLMBaseURL, iaproveedor.Nombre(previa.IAProveedor)))
		return false
	}
	if s.bloqueado("ia.api_key") || textoDe(cambios, "ia.api_key") != "" || actual.LLMAPIKey == "" {
		return true
	}
	prov := iaproveedor.Efectivo(previa.IAProveedor, previa.LLMBaseURL)
	if prov == iaproveedor.Ollama || (previa.LLMBaseURL != "" && !iaproveedor.MismoDestino(previa.LLMBaseURL, actual.LLMBaseURL)) {
		cambios["ia.api_key"] = json.RawMessage("null")
	}
	return true
}

// recomendadosAviso suma al aviso de la IA los modelos probados (Conectar los muestra).
func recomendadosAviso(res map[string]any) {
	res["recomendados"] = iaproveedor.Recomendados
	res["prueba"] = iaproveedor.PruebaTexto
}
