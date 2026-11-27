package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/canal"
	"github.com/diegoparras/factorum/internal/export"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

const ayuda = `Factorum lee facturas de ARCA.

- Mandame PDFs o fotos de facturas y te devuelvo los datos con los controles.
- Para saber si una factura es legal, mandala con el texto "verificar".
- "estado": cómo va lo último que mandaste.
- "ayuda": este mensaje.`

// Permitido decide si un remitente puede usar el canal.
func (a *App) Permitido(m canal.Mensaje) bool {
	cfg := a.Config()
	switch m.Canal {
	case "webhook":
		return true // ya pasó por el token
	case "telegram":
		return contiene(cfg.TelegramPermitidos, m.Remitente) || contiene(cfg.TelegramPermitidos, m.Chat)
	case "whatsapp":
		return contiene(cfg.WhatsAppPermitidos, arca.SoloDigitos(m.Remitente))
	}
	return false
}

func contiene(l []string, v string) bool {
	for _, x := range l {
		if x == v || arca.SoloDigitos(x) == v && v != "" {
			return true
		}
	}
	return false
}

// ManejarMensaje es el Manejador de todos los canales. Devuelve la respuesta inmediata (que el
// webhook usa como cuerpo de la respuesta HTTP); en Telegram y WhatsApp además la envía.
func (a *App) ManejarMensaje(ctx context.Context, m canal.Mensaje) string {
	resp := a.responder(ctx, m)
	if resp != "" && m.Canal != "webhook" {
		if c := a.Canal(m.Canal); c != nil {
			if err := c.Enviar(ctx, m.Chat, resp); err != nil {
				a.Log.Warn("no se pudo responder", "canal", m.Canal, "error", err)
			}
		}
	}
	return resp
}

func (a *App) responder(ctx context.Context, m canal.Mensaje) string {
	if !a.Permitido(m) {
		a.Log.Info("mensaje de un remitente no habilitado", "canal", m.Canal, "remitente", m.Remitente)
		return fmt.Sprintf("Este número o usuario no está habilitado para usar Factorum. Tu identificador es %s: pasáselo al estudio para que te habilite.", m.Remitente)
	}
	texto := strings.ToLower(strings.TrimSpace(m.Texto))
	// WhatsApp va vinculado a un número de uso diario: solo se atienden archivos y comandos
	// explícitos ("factorum estado", "/estado"), para no meterse en conversaciones normales.
	if m.Canal == "whatsapp" && len(m.Archivos) == 0 {
		switch {
		case strings.HasPrefix(texto, "factorum"):
			texto = strings.TrimSpace(strings.TrimPrefix(texto, "factorum"))
			if texto == "" {
				texto = "ayuda"
			}
		case strings.HasPrefix(texto, "/"):
		default:
			return ""
		}
	}
	if len(m.Archivos) == 0 {
		switch {
		case strings.HasPrefix(texto, "/start"), strings.Contains(texto, "ayuda"), texto == "/help", texto == "hola":
			return ayuda
		case strings.Contains(texto, "estado"):
			l, err := a.Store.UltimoLoteDeChat(ctx, m.Canal, m.Chat)
			if err != nil {
				return "Todavía no me mandaste facturas."
			}
			return textoLote(l)
		default:
			if texto == "" {
				return ""
			}
			return "Mandame el PDF o la foto de la factura. Escribí \"ayuda\" para ver qué puedo hacer."
		}
	}
	cfg := a.Config()
	cliente := cfg.CanalClienteCUIT[m.Remitente]
	if cliente == "" {
		cliente = cfg.CanalClienteCUIT[arca.SoloDigitos(m.Remitente)]
	}
	if strings.Contains(texto, "verific") || strings.Contains(texto, "legal") || strings.Contains(texto, "trucha") {
		var partes []string
		for _, f := range m.Archivos {
			v, err := a.Verificar(ctx, f.Nombre, f.Datos, cliente, cfg.IAPorDefecto, false)
			if err != nil {
				partes = append(partes, f.Nombre+": "+err.Error())
				continue
			}
			partes = append(partes, TextoVerificacion(f.Nombre, v))
		}
		return strings.Join(partes, "\n\n")
	}
	rl, err := a.CrearLote(ctx, store.LoteNuevo{
		Nombre: fmt.Sprintf("%s %s", m.Canal, time.Now().Format("02/01 15:04")), ClienteCUIT: cliente,
		Origen: m.Canal, Canal: m.Canal, Chat: m.Chat, IA: cfg.IAPorDefecto, Constatar: cfg.ConstatarPorDefecto && a.Arcanum() != nil,
	}, m.Archivos, 5*time.Minute)
	if err != nil {
		return "No pude cargar los archivos: " + err.Error()
	}
	nuevos := rl.Recibidos - rl.Repetidos
	var msg string
	switch {
	case nuevos == 0 && rl.Repetidos == 1:
		msg = "Ese archivo ya lo tenía cargado."
	case nuevos == 0:
		msg = fmt.Sprintf("Esos %d archivos ya los tenía cargados.", rl.Repetidos)
	default:
		msg = fmt.Sprintf("Recibí %d archivo(s). Te aviso cuando termine.", nuevos)
		if rl.Repetidos > 0 {
			msg += fmt.Sprintf(" Otros %d ya los tenía cargados.", rl.Repetidos)
		}
	}
	if len(rl.Descartados) > 0 {
		msg += " No son facturas que pueda leer: " + strings.Join(rl.Descartados, ", ") + "."
	}
	return msg
}

// notificar manda el resumen del lote al chat de origen, una sola vez y cuando hubo calma.
func (a *App) notificar(ctx context.Context, loteID int64) error {
	l, err := a.Store.Lote(ctx, loteID)
	if err != nil || l.Terminado == nil || l.Canal == "" {
		return nil // si se reabrió, el cierre vuelve a encolar el aviso
	}
	ok, err := a.Store.MarcarNotificado(ctx, loteID)
	if err != nil {
		return err
	}
	if !ok {
		var notificado bool
		_ = a.Store.Pool.QueryRow(ctx, `SELECT notificado FROM lotes WHERE id=$1`, loteID).Scan(&notificado)
		if notificado {
			return nil
		}
		return river.JobSnooze(10 * time.Second) // llegaron archivos hace poco: esperamos
	}
	c := a.Canal(l.Canal)
	if c == nil {
		return nil
	}
	cbs, _, err := a.Store.Comprobantes(ctx, store.Filtro{Lote: loteID, Limite: 200})
	if err != nil {
		return err
	}
	return c.Enviar(ctx, l.Chat, TextoResumen(l, cbs))
}

func textoLote(l *store.Lote) string {
	if l.Estado != "terminado" {
		return fmt.Sprintf("Estoy procesando: %d de %d archivos listos.", l.Procesados, l.Documentos)
	}
	return fmt.Sprintf("Lo último que mandaste ya está: %d comprobantes, %d bien, %d para revisar, %d con errores.",
		l.Comprobantes["ok"]+l.Comprobantes["revisar"]+l.Comprobantes["error"]+l.Comprobantes["aprobado"],
		l.Comprobantes["ok"]+l.Comprobantes["aprobado"], l.Comprobantes["revisar"], l.Comprobantes["error"])
}

func nombreCbte(c *modelo.Comprobante) string {
	nombre := "Comprobante"
	if tp, ok := arca.Tipo(c.Tipo); ok {
		nombre = tp.Nombre
	}
	return fmt.Sprintf("%s %05d-%08d", nombre, c.PuntoVenta, c.Numero)
}

var etiquetaEstado = map[modelo.Estado]string{
	modelo.EstadoOK: "bien", modelo.EstadoRevisar: "revisar", modelo.EstadoError: "con errores", modelo.EstadoAprobado: "aprobado",
}

// TextoResumen es el mensaje de fin de lote para un chat (sin emojis, regla de la suite).
func TextoResumen(l *store.Lote, cbs []store.Cbte) string {
	var b strings.Builder
	n := map[modelo.Estado]int{}
	for _, c := range cbs {
		n[c.Estado]++
	}
	fmt.Fprintf(&b, "Listo: %d comprobante(s). %d bien, %d para revisar, %d con errores.", len(cbs), n[modelo.EstadoOK]+n[modelo.EstadoAprobado], n[modelo.EstadoRevisar], n[modelo.EstadoError])
	if l.ConError > 0 {
		fmt.Fprintf(&b, " %d archivo(s) no se pudieron leer.", l.ConError)
	}
	b.WriteString("\n")
	for i, c := range cbs {
		if i == 15 {
			fmt.Fprintf(&b, "\n... y %d más.", len(cbs)-15)
			break
		}
		d := &c.Datos
		razon := d.RazonSocialEmisor
		if razon == "" {
			razon = arca.FormatearCUIT(d.CUITEmisor)
		}
		fmt.Fprintf(&b, "\n- %s, %s, $ %s", nombreCbte(d), razon, d.Total.AR())
		if d.EsTique && d.Rubro != "" {
			fmt.Fprintf(&b, " (%s)", d.Rubro)
		}
		fmt.Fprintf(&b, ": %s", etiquetaEstado[c.Estado])
		for _, h := range c.Hallazgos {
			if h.Nivel == modelo.NivelError || (c.Estado == modelo.EstadoRevisar && h.Nivel == modelo.NivelAviso) {
				fmt.Fprintf(&b, " (%s)", h.Mensaje)
				break
			}
		}
	}
	return b.String()
}

var etiquetaVeredicto = map[string]string{
	"valida": "VÁLIDA", "invalida": "NO VÁLIDA", "apocrifa": "EMISOR APÓCRIFO", "con_errores": "CON ERRORES", "no_verificable": "NO SE PUDO VERIFICAR",
	"tique_controlado": "TICKET CONTROLADO",
}

// TextoVerificacion es la respuesta a "verificar" en un chat.
func TextoVerificacion(nombre string, v *Verificacion) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s. %s", nombre, etiquetaVeredicto[v.Veredicto], v.Mensaje)
	for i, rc := range v.Resultado.Comprobantes {
		c := &rc.Comprobante
		fmt.Fprintf(&b, "\n- %s, CUIT %s, %s", nombreCbte(c), arca.FormatearCUIT(c.CUITEmisor), fechaAR(c.Fecha))
		if c.Hora != "" {
			fmt.Fprintf(&b, " %s", c.Hora)
		}
		fmt.Fprintf(&b, ", $ %s", c.Total.AR())
		if c.EsTique && c.Rubro != "" {
			fmt.Fprintf(&b, ", %s", c.Rubro)
		}
		if c.CAE != "" {
			fmt.Fprintf(&b, ", CAE %s", c.CAE)
		}
		if len(v.Resultado.Comprobantes) > 1 && i < len(v.Comprobantes) {
			fmt.Fprintf(&b, ": %s", etiquetaVeredicto[v.Comprobantes[i].Veredicto])
		}
		for _, h := range rc.Hallazgos {
			if h.Nivel == modelo.NivelError {
				fmt.Fprintf(&b, "\n  %s", h.Mensaje)
			}
		}
	}
	return b.String()
}

// ---------- Exportación ----------

// Filas trae todos los comprobantes del filtro (de a 1000) para exportar o resumir.
func (a *App) Filas(ctx context.Context, f store.Filtro) ([]export.Fila, error) {
	var res []export.Fila
	f.Limite, f.Offset = 1000, 0
	for {
		cbs, total, err := a.Store.Comprobantes(ctx, f)
		if err != nil {
			return nil, err
		}
		for _, c := range cbs {
			res = append(res, export.Fila{ID: c.ID, Archivo: c.Archivo, ClienteCUIT: c.ClienteCUIT, Estado: c.Estado,
				Hallazgos: c.Hallazgos, Constatado: estadoConstatacion(c.Constatacion), C: c.Datos})
		}
		f.Offset += len(cbs)
		if len(cbs) == 0 || f.Offset >= total || f.Offset >= 200000 {
			return res, nil
		}
	}
}

func estadoConstatacion(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var k arcanum.Constatacion
	if json.Unmarshal(raw, &k) != nil {
		return ""
	}
	switch {
	case k.Apoc != nil && k.Apoc.EsApocrifo != nil && *k.Apoc.EsApocrifo:
		return "apocrifo"
	case k.Valido != nil && *k.Valido:
		return "valido"
	case k.Valido != nil:
		return "invalido"
	case k.Apoc != nil && k.Apoc.Consultado && k.Apoc.EsApocrifo != nil:
		return "controlado" // tique: no está en la base de apócrifos (no se constata en ARCA)
	case k.Error != "":
		return "error"
	}
	return ""
}

// fechaAR pasa AAAA-MM-DD a DD/MM/AAAA.
func fechaAR(f string) string {
	if len(f) == 10 && f[4] == '-' {
		return f[8:] + "/" + f[5:7] + "/" + f[:4]
	}
	return f
}
