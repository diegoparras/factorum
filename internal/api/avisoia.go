package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/auth"
	"github.com/diegoparras/factorum/internal/avisoia"
	"github.com/diegoparras/factorum/internal/config"
)

// CodigoAvisoIA es el código de error de un pedido que va a usar una IA externa sin el aviso visto
// (la persona) o aceptado (el superadmin, al guardar la configuración).
const CodigoAvisoIA = "aviso_ia"

// vistaAviso es lo que la pantalla necesita para el sello, el aviso y la aceptación.
func (s *Server) vistaAviso(ctx context.Context, c *config.Config, id *auth.Identidad) map[string]any {
	d := avisoia.Detectar(c.LLMBaseURL)
	res := map[string]any{
		"configurada": c.IAConfigurada(), "proveedor": d.Proveedor, "nombre": d.Nombre, "host": d.Host,
		"region": d.Region, "local": d.Local, "externa": d.Externa(), "version": avisoia.Version,
		"aceptado": avisoia.Aceptado(c), "en_uso": avisoia.EnUso(c), "visto": false, "persona": false,
		"puede_aceptar": id != nil && id.Rol >= auth.Superadmin && id.Tipo == auth.TipoSesion,
		"aviso":         avisoia.Aviso(d),
	}
	if r, ok := avisoia.Regiones[d.Region]; ok {
		res["region_nombre"] = r.Nombre
	}
	if d.Externa() && avisoia.Aceptado(c) && id != nil && id.Rol >= auth.Admin {
		res["aceptacion"] = avisoia.AceptacionDe(c)
	}
	if id != nil && id.UsuarioID > 0 {
		res["persona"] = true
		if d.Externa() {
			v, err := s.App.Store.AvisoIAVisto(ctx, id.UsuarioID, d.Host, avisoia.Version)
			if err != nil {
				s.Log.Warn("no se pudo ver si la persona vio el aviso de la IA", "error", err)
			}
			res["visto"] = v
		} else {
			res["visto"] = true // la local no tiene aviso
		}
	}
	return res
}

// GET /api/ia/aviso: la IA configurada y qué pasa con los datos.
func (s *Server) verAvisoIA(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, http.StatusOK, s.vistaAviso(r.Context(), s.App.Config(), IdentidadDe(r.Context())))
}

type avisoPedido struct {
	Host    string `json:"host"`
	Version int    `json:"version"`
}

// POST /api/ia/aviso/visto {host, version}: la persona vio el aviso y va a usar la IA externa.
func (s *Server) avisoIAVisto(w http.ResponseWriter, r *http.Request) {
	var b avisoPedido
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	id := IdentidadDe(r.Context())
	cfg := s.App.Config()
	d := avisoia.Detectar(cfg.LLMBaseURL)
	if !d.Externa() || !avisoia.EnUso(cfg) {
		jsonErr(w, http.StatusConflict, "No hay una IA externa en uso")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(b.Host), d.Host) || b.Version != avisoia.Version {
		jsonCrudo(w, http.StatusConflict, map[string]any{"ok": false, "codigo": CodigoAvisoIA,
			"error": "La IA configurada o su aviso cambiaron: miralo de nuevo", "aviso_ia": s.vistaAviso(r.Context(), cfg, id)})
		return
	}
	if err := s.App.Store.MarcarAvisoIAVisto(r.Context(), id.UsuarioID, d.Host, avisoia.Version); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "ia.aviso_visto", d.Host, map[string]any{"proveedor": d.Proveedor, "host": d.Host, "version": avisoia.Version})
	jsonOK(w, http.StatusOK, s.vistaAviso(r.Context(), cfg, id))
}

// avisoPendiente dice si la persona va a usar una IA externa sin haber visto su aviso. Las claves de
// API, los canales y MCP no son personas: para ellos alcanza con la aceptación del superadmin.
func (s *Server) avisoPendiente(r *http.Request) bool {
	id := IdentidadDe(r.Context())
	if id == nil || id.UsuarioID == 0 {
		return false
	}
	v := s.App.Pipeline.Vision()
	if v == nil {
		return false
	}
	d := avisoia.Detectar(v.BaseURL)
	if !d.Externa() {
		return false
	}
	visto, err := s.App.Store.AvisoIAVisto(r.Context(), id.UsuarioID, d.Host, avisoia.Version)
	if err != nil {
		s.Log.Warn("no se pudo ver si la persona vio el aviso de la IA", "error", err)
		return true // ante la duda, que lo vea
	}
	return !visto
}

func (s *Server) responderAvisoPendiente(w http.ResponseWriter, r *http.Request) {
	jsonCrudo(w, http.StatusConflict, map[string]any{"ok": false, "codigo": CodigoAvisoIA,
		"error":    "Antes de usar la IA externa, mirá qué pasa con los datos (o desmarcá «Usar IA si hace falta»)",
		"aviso_ia": s.vistaAviso(r.Context(), s.App.Config(), IdentidadDe(r.Context()))})
}

// aceptacionIA controla, antes de guardar la configuración, que una IA externa nueva (o de otro host)
// venga con el aviso aceptado. Si hace falta y no viene, responde 409 con el aviso para mostrarlo y
// devuelve false. Si viene, suma a los cambios quién lo aceptó, cuándo y para qué host.
func (s *Server) aceptacionIA(w http.ResponseWriter, r *http.Request, cambios map[string]json.RawMessage, acepta *avisoPedido) (aceptado *avisoia.Deteccion, seguir bool) {
	toca := acepta != nil
	for k := range cambios {
		toca = toca || strings.HasPrefix(k, "ia.")
	}
	if !toca {
		return nil, true
	}
	id := IdentidadDe(r.Context())
	if acepta != nil && (id.Rol < auth.Superadmin || id.Tipo != auth.TipoSesion) {
		jsonErr(w, http.StatusForbidden, "Solo un superadmin con sesión puede aceptar el aviso de la IA")
		return nil, false
	}
	previa, err := s.Ajustes.Previa(cambios)
	if err != nil || !previa.IAConfigurada() {
		return nil, true // Guardar dice qué está mal (o no hay IA: no hay nada que aceptar)
	}
	d := avisoia.Detectar(previa.LLMBaseURL)
	if !d.Externa() || avisoia.Aceptado(previa) {
		return nil, true
	}
	if acepta == nil || !strings.EqualFold(strings.TrimSpace(acepta.Host), d.Host) || acepta.Version != avisoia.Version {
		jsonCrudo(w, http.StatusConflict, map[string]any{"ok": false, "codigo": CodigoAvisoIA,
			"error":    "Es una IA externa: antes de guardarla, mirá qué pasa con los datos y aceptá el aviso",
			"aviso_ia": s.vistaAviso(r.Context(), previa, id)})
		return nil, false
	}
	js := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	cambios["ia.aviso_host"] = js(d.Host)
	cambios["ia.aviso_version"] = js(avisoia.Version)
	cambios["ia.aviso_por"] = js(id.Quien())
	cambios["ia.aviso_cuando"] = js(s.ahora().UTC().Format(time.RFC3339))
	return &d, true
}
