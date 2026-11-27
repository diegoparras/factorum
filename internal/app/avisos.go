package app

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

// ---------- Trabajos de la cola ----------

// AvisoCorreoArgs: los mails de un lote terminado (la respuesta a quien lo mandó por mail, el
// aviso de lote terminado y las alertas de apócrifos o adulterados).
type AvisoCorreoArgs struct {
	LoteID int64 `json:"lote_id"`
}

func (AvisoCorreoArgs) Kind() string { return "aviso_correo_lote" }

// SemanalArgs: el resumen de los lunes (el trabajo corre cada 15 minutos y manda una vez por semana).
type SemanalArgs struct{}

func (SemanalArgs) Kind() string { return "resumen_semanal" }

type avisoCorreoWorker struct {
	river.WorkerDefaults[AvisoCorreoArgs]
	a *App
}

func (w *avisoCorreoWorker) Work(ctx context.Context, j *river.Job[AvisoCorreoArgs]) error {
	return w.a.avisarLote(ctx, j.Args.LoteID)
}

type semanalWorker struct {
	river.WorkerDefaults[SemanalArgs]
	a *App
}

func (w *semanalWorker) Work(ctx context.Context, _ *river.Job[SemanalArgs]) error {
	return w.a.ResumenSemanal(ctx, w.a.ahora())
}

func (a *App) calma() time.Duration {
	if a.Calma > 0 {
		return a.Calma
	}
	return 10 * time.Second
}

func (a *App) ahora() time.Time {
	if a.Reloj != nil {
		return a.Reloj()
	}
	return time.Now()
}

// hayAvisosDeLote dice si con esta configuración un lote terminado puede generar algún mail.
func (a *App) hayAvisosDeLote(l *store.Lote) bool {
	c := a.Config()
	if !c.SMTPConfigurado() {
		return false
	}
	return c.AvisoLote || c.AvisoAlertas || (l.Origen == "correo" && c.CorreoContestar)
}

// ---------- Lote terminado ----------

const maxItems = 15

func (a *App) resumenLote(l *store.Lote, cbs []store.Cbte, link string) correo.ResumenLote {
	r := correo.ResumenLote{Nombre: l.Nombre, Total: len(cbs), Ilegibles: l.ConError, Link: link}
	if l.ClienteCUIT != "" {
		r.Cliente = arca.FormatearCUIT(l.ClienteCUIT)
	}
	for _, c := range cbs {
		switch c.Estado {
		case modelo.EstadoOK, modelo.EstadoAprobado:
			r.Bien++
		case modelo.EstadoRevisar:
			r.Revisar++
		case modelo.EstadoError:
			r.Errores++
		}
		if tieneHallazgo(c, "emisor_apocrifo") {
			r.Apocrifos++
		}
		if tieneHallazgo(c, "qr_distinto_texto") {
			r.Adulterados++
		}
	}
	for i, c := range cbs {
		if i == maxItems {
			r.Mas = len(cbs) - maxItems
			break
		}
		r.Items = append(r.Items, itemDe(c, false))
	}
	return r
}

func tieneHallazgo(c store.Cbte, codigo string) bool {
	for _, h := range c.Hallazgos {
		if h.Codigo == codigo {
			return true
		}
	}
	return false
}

func itemDe(c store.Cbte, conCliente bool) correo.ItemCbte {
	d := &c.Datos
	razon := d.RazonSocialEmisor
	if razon == "" {
		razon = arca.FormatearCUIT(d.CUITEmisor)
	}
	it := correo.ItemCbte{Descripcion: nombreCbte(d), Emisor: razon, Total: d.Total.AR(), Estado: etiquetaEstado[c.Estado]}
	for _, h := range c.Hallazgos {
		if h.Nivel == modelo.NivelError || (c.Estado == modelo.EstadoRevisar && h.Nivel == modelo.NivelAviso) {
			it.Nota = h.Mensaje
			break
		}
	}
	if conCliente && c.ClienteCUIT != "" {
		it.Cliente = arca.FormatearCUIT(c.ClienteCUIT)
	}
	return it
}

func (a *App) link(ruta string) string {
	u := strings.TrimRight(a.Config().URLPublica, "/")
	if u == "" {
		return ""
	}
	return u + "/" + ruta
}

// puedeRecibir dice si a ese email le pueden llegar datos de ese cliente: si es una persona de
// Factorum, según su alcance (y si está activa); si no lo es, es una dirección que puso un
// superadmin en la lista de avisos y recibe lo que se le manda.
func (a *App) puedeRecibir(ctx context.Context, email, cliente string) (bool, error) {
	u, err := a.Store.UsuarioPorEmail(ctx, email)
	if err == store.ErrNoEncontrado {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !u.Activo {
		return false, nil
	}
	return u.CUITs == nil || slices.Contains(u.CUITs, cliente), nil
}

// mandarUnaVez manda el mail a cada destinatario que no lo haya recibido ya (clave + email).
func (a *App) mandarUnaVez(ctx context.Context, clave string, para []string, m correo.Mail) error {
	vistos := map[string]bool{}
	for _, p := range para {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || vistos[p] {
			continue
		}
		vistos[p] = true
		k := clave + ":" + p
		if ya, err := a.Store.AvisoEnviado(ctx, k); err != nil || ya {
			if err != nil {
				return err
			}
			continue
		}
		m.Para = []string{p}
		if _, err := a.EnviarCorreo(ctx, m); err != nil {
			a.Log.Warn("no se pudo mandar un aviso por mail", "aviso", strings.SplitN(clave, ":", 2)[0], "error", err)
			return err // la cola reintenta; a quien ya le llegó no se le vuelve a mandar
		}
		if err := a.Store.MarcarAviso(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// avisarLote manda los mails de un lote que terminó.
func (a *App) avisarLote(ctx context.Context, loteID int64) error {
	l, err := a.Store.Lote(ctx, loteID)
	if err != nil || l.Terminado == nil {
		return nil // si se reabrió, el cierre vuelve a encolar el aviso
	}
	if time.Since(l.Actualizado) < a.calma() {
		return river.JobSnooze(a.calma()) // llegaron archivos hace poco: esperamos
	}
	cfg := a.Config()
	if !cfg.SMTPConfigurado() {
		return nil
	}
	cbs, _, err := a.Store.Comprobantes(ctx, store.Filtro{Lote: loteID, Limite: 2000})
	if err != nil {
		return err
	}
	clave := fmt.Sprintf("lote:%d:%d", l.ID, l.Terminado.Unix())
	marca := a.Marca()
	link := a.link("#comprobantes?lote=" + strconv.FormatInt(l.ID, 10))

	// 1. La respuesta a quien lo mandó por mail: solo a un permitido (nunca a un retenido que se
	// aceptó sin sumarlo a la lista), en el mismo hilo.
	if l.Origen == "correo" && cfg.CorreoContestar {
		if id, err := strconv.ParseInt(l.Chat, 10, 64); err == nil {
			msg, err := a.Store.Correo(ctx, id)
			if err != nil && err != store.ErrNoEncontrado {
				return err
			}
			if msg != nil && (msg.Estado == CorreoProcesado || msg.Estado == "aceptado") && correo.Permitido(cfg.CorreoPermitidos, msg.Remitente) {
				m := correo.RespuestaLote(marca, a.resumenLote(l, cbs, link), msg.Asunto)
				m.EnRespuestaA, m.Referencias = msg.MessageID, msg.Referencias
				if err := a.mandarUnaVez(ctx, clave+":respuesta", []string{msg.Remitente}, m); err != nil {
					return err
				}
			}
		}
	}

	// 2. Lote terminado: a quien lo cargó, o a la lista.
	if cfg.AvisoLote {
		var para []string
		if l.CreadoPor != nil {
			if u, err := a.Store.Usuario(ctx, *l.CreadoPor); err == nil && u.Activo && u.Email != "" {
				para = []string{u.Email}
			}
		}
		if para == nil {
			para = cfg.AvisoLoteA
		}
		para, err = a.filtrarPorAlcance(ctx, para, l.ClienteCUIT)
		if err != nil {
			return err
		}
		if err := a.mandarUnaVez(ctx, clave+":lote", para, correo.AvisoLote(marca, a.resumenLote(l, cbs, link))); err != nil {
			return err
		}
	}

	// 3. Apócrifos y posibles adulteraciones: a los admins (de ese cliente) y a la lista.
	if cfg.AvisoAlertas {
		var apoc, adult []correo.ItemCbte
		for _, c := range cbs {
			if tieneHallazgo(c, "emisor_apocrifo") {
				apoc = append(apoc, itemDe(c, true))
			}
			if tieneHallazgo(c, "qr_distinto_texto") {
				adult = append(adult, itemDe(c, true))
			}
		}
		if len(apoc)+len(adult) > 0 {
			var para []string
			admins, err := a.Store.Admins(ctx)
			if err != nil {
				return err
			}
			for _, u := range admins {
				para = append(para, u.Email)
			}
			para, err = a.filtrarPorAlcance(ctx, append(para, cfg.AvisoAlertasA...), l.ClienteCUIT)
			if err != nil {
				return err
			}
			nombre := l.Nombre
			if nombre == "" {
				nombre = "Lote " + strconv.FormatInt(l.ID, 10)
			}
			if err := a.mandarUnaVez(ctx, clave+":alerta", para, correo.Alerta(marca, nombre, recortarItems(apoc), recortarItems(adult), link)); err != nil {
				return err
			}
		}
	}
	return nil
}

func recortarItems(l []correo.ItemCbte) []correo.ItemCbte {
	if len(l) > 30 {
		return l[:30]
	}
	return l
}

func (a *App) filtrarPorAlcance(ctx context.Context, para []string, cliente string) ([]string, error) {
	var res []string
	for _, p := range para {
		ok, err := a.puedeRecibir(ctx, p, cliente)
		if err != nil {
			return nil, err
		}
		if ok {
			res = append(res, p)
		}
	}
	return res, nil
}

// ---------- Resumen semanal ----------

// ResumenSemanal manda el resumen de la semana si es lunes después de las 8 (hora de Buenos
// Aires) y todavía no se mandó. Cada destinatario recibe solo lo de los clientes que puede ver.
func (a *App) ResumenSemanal(ctx context.Context, ahora time.Time) error {
	cfg := a.Config()
	if !cfg.AvisoSemanal || !cfg.SMTPConfigurado() {
		return nil
	}
	t := ahora.In(zonaAR)
	if t.Weekday() != time.Monday || t.Hour() < 8 {
		return nil
	}
	hasta := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, zonaAR)
	desde := hasta.AddDate(0, 0, -7)
	para := cfg.AvisoSemanalA
	if len(para) == 0 {
		admins, err := a.Store.Admins(ctx)
		if err != nil {
			return err
		}
		for _, u := range admins {
			para = append(para, u.Email)
		}
	}
	clave := "semanal:" + hasta.Format("2006-01-02")
	marca := a.Marca()
	for _, p := range para {
		p = strings.ToLower(strings.TrimSpace(p))
		k := clave + ":" + p
		if ya, err := a.Store.AvisoEnviado(ctx, k); err != nil || ya {
			if err != nil {
				return err
			}
			continue
		}
		var alcance []string
		u, err := a.Store.UsuarioPorEmail(ctx, p)
		switch {
		case err == nil && !u.Activo:
			continue
		case err == nil:
			alcance = u.CUITs
		case err != store.ErrNoEncontrado:
			return err
		}
		r, err := a.Store.Periodo(ctx, desde, hasta, alcance)
		if err != nil {
			return err
		}
		s := correo.ResumenSemana{Desde: desde.Format("02/01/2006"), Hasta: hasta.AddDate(0, 0, -1).Format("02/01/2006"), Lotes: r.Lotes,
			Bien: r.PorEstado["ok"] + r.PorEstado["aprobado"], Revisar: r.PorEstado["revisar"], Errores: r.PorEstado["error"],
			Apocrifos: r.Apocrifos, Pendientes: r.Pendientes, Link: a.link("#revisar")}
		s.Total = s.Bien + s.Revisar + s.Errores
		for _, c := range r.PorCliente {
			nombre := "Sin cliente"
			if c.Cliente != "" {
				nombre = arca.FormatearCUIT(c.Cliente)
			}
			s.PorCliente = append(s.PorCliente, correo.FilaCliente{Cliente: nombre, Total: c.Total, Pendientes: c.Pendientes})
		}
		if err := a.mandarUnaVez(ctx, clave, []string{p}, correo.Semanal(marca, s)); err != nil {
			return err
		}
	}
	return nil
}
