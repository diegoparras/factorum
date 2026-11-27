package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/copias"
	"github.com/diegoparras/factorum/internal/mudanza"
)

// ---------- Pasar lo del disco a R2 ----------

// GET /api/r2/pase
func (s *Server) verPaseR2(w http.ResponseWriter, r *http.Request) {
	if s.Mudanza == nil {
		jsonErr(w, http.StatusServiceUnavailable, "El pase a R2 no está disponible")
		return
	}
	e, err := s.Mudanza.Estado(r.Context())
	if err != nil {
		s.fallo(w, err)
		return
	}
	jsonOK(w, http.StatusOK, map[string]any{"pase": e})
}

// POST /api/r2/pase {accion: iniciar | pausar | seguir | liberar, confirmacion}
func (s *Server) accionPaseR2(w http.ResponseWriter, r *http.Request) {
	if s.Mudanza == nil {
		jsonErr(w, http.StatusServiceUnavailable, "El pase a R2 no está disponible")
		return
	}
	var b struct {
		Accion       string `json:"accion"`
		Confirmacion string `json:"confirmacion"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&b); err != nil {
		jsonErr(w, http.StatusBadRequest, "JSON inválido")
		return
	}
	id := IdentidadDe(r.Context())
	ctx := r.Context()
	var err error
	var accion string
	switch b.Accion {
	case "iniciar":
		err, accion = s.Mudanza.Iniciar(ctx, id.Quien()), "r2.migrar_inicio"
	case "seguir":
		err, accion = s.Mudanza.Seguir(ctx, id.Quien()), "r2.migrar_seguir"
	case "pausar":
		err, accion = s.Mudanza.Pausar(ctx), "r2.migrar_pausa"
	case "liberar":
		err, accion = s.Mudanza.Liberar(ctx, strings.TrimSpace(strings.ToLower(b.Confirmacion))), "r2.liberar_inicio"
	default:
		jsonErr(w, http.StatusBadRequest, "La acción tiene que ser iniciar, pausar, seguir o liberar")
		return
	}
	switch {
	case errors.Is(err, mudanza.ErrSinR2), errors.Is(err, mudanza.ErrNoSePuede):
		msg := strings.TrimPrefix(err.Error(), mudanza.ErrNoSePuede.Error()+": ")
		jsonErr(w, http.StatusConflict, capital(msg))
		return
	case err != nil:
		s.fallo(w, err)
		return
	}
	e, err := s.Mudanza.Estado(ctx)
	if err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, accion, e.Destino, map[string]any{"objetos": e.Total.Objetos, "hechos": e.Hechos.Objetos, "para_liberar": e.ParaLiberar.Objetos})
	jsonOK(w, http.StatusOK, map[string]any{"pase": e})
}

// ---------- Copias de seguridad ----------

// GET /api/copias
func (s *Server) verCopias(w http.ResponseWriter, r *http.Request) {
	if s.Copias == nil {
		jsonErr(w, http.StatusServiceUnavailable, "Las copias de seguridad no están disponibles")
		return
	}
	l, err := s.Copias.Lista(r.Context(), 100)
	if err != nil {
		s.fallo(w, err)
		return
	}
	cfg := s.App.Config()
	h, m := cfg.HoraDeCopias()
	diarias, mensuales := cfg.CopiasAGuardar()
	destino := copias.DestinoDisco
	if s.App.Almacen != nil && s.App.Almacen.R2() != nil {
		destino = copias.DestinoR2
	}
	res := map[string]any{"copias": l, "activas": cfg.CopiasActivas, "hora": fmt.Sprintf("%02d:%02d", h, m),
		"diarias": diarias, "mensuales": mensuales, "destino": destino}
	if cfg.CopiasActivas {
		res["proxima"] = proximaCopia(time.Now(), h, m, l)
	}
	jsonOK(w, http.StatusOK, res)
}

// proximaCopia: hoy a la hora si todavía no se hizo la de hoy; si no, mañana.
func proximaCopia(ahora time.Time, h, m int, l []copias.Copia) time.Time {
	a := ahora.In(copias.Zona)
	hoy := time.Date(a.Year(), a.Month(), a.Day(), h, m, 0, 0, copias.Zona)
	dia := a.Format("2006-01-02")
	for _, c := range l {
		if c.Programada == dia {
			return hoy.AddDate(0, 0, 1)
		}
	}
	if a.Before(hoy) {
		return hoy
	}
	return a.Add(time.Minute).Truncate(time.Minute) // ya pasó la hora: en el próximo minuto
}

// POST /api/copias: hacer una copia ahora (en segundo plano).
func (s *Server) hacerCopia(w http.ResponseWriter, r *http.Request) {
	if s.Copias == nil || s.Copias.Encolar == nil {
		jsonErr(w, http.StatusServiceUnavailable, "Las copias de seguridad no están disponibles")
		return
	}
	id := IdentidadDe(r.Context())
	if err := s.Copias.Encolar(r.Context(), copias.Args{Por: id.Quien()}, nil); err != nil {
		s.fallo(w, err)
		return
	}
	s.auditar(r, "copia.pedir", "", nil)
	jsonOK(w, http.StatusAccepted, map[string]any{"mensaje": "Se está haciendo la copia: aparece en la lista en unos segundos."})
}

// GET /api/copias/{id}/archivo: el archivo cifrado (para guardarlo fuera del servidor).
func (s *Server) descargarCopia(w http.ResponseWriter, r *http.Request) {
	if s.Copias == nil {
		jsonErr(w, http.StatusServiceUnavailable, "Las copias de seguridad no están disponibles")
		return
	}
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "Número de copia inválido")
		return
	}
	rd, tam, c, err := s.Copias.Abrir(r.Context(), n)
	if errors.Is(err, copias.ErrNoEncontrada) {
		msg := "No hay una copia con ese número"
		if c != nil {
			msg = "Esa copia no se puede descargar desde acá (falló o está en un R2 que ya no está activo)"
		}
		jsonErr(w, http.StatusNotFound, msg)
		return
	}
	if err != nil {
		s.fallo(w, err)
		return
	}
	defer rd.Close()
	nombre := path.Base(c.Clave)
	s.auditar(r, "copia.descargar", nombre, map[string]any{"bytes": tam})
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nombre+`"`)
	w.Header().Set("Content-Length", strconv.FormatInt(tam, 10))
	w.Header().Set("Cache-Control", "no-store")
	if c.SHA256 != "" {
		w.Header().Set("X-Factorum-SHA256", c.SHA256)
	}
	if _, err := io.Copy(w, rd); err != nil {
		s.Log.Warn("se cortó la descarga de una copia", "copia", nombre, "error", err)
	}
}
