package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/diegoparras/factorum/internal/arca"
	"github.com/diegoparras/factorum/internal/correo"
	"github.com/diegoparras/factorum/internal/store"
)

// Estados de un mail que entró.
const (
	CorreoProcesado = "procesado"
	CorreoRetenido  = "retenido"
	CorreoIgnorado  = "ignorado"
	CorreoRepetido  = "repetido"
)

// ResultadoCorreo es lo que pasó con un mail que entró.
type ResultadoCorreo struct {
	Estado  string      `json:"estado"` // procesado, retenido, ignorado o repetido
	Motivo  string      `json:"motivo,omitempty"`
	Mensaje int64       `json:"mensaje,omitempty"`
	Lote    *store.Lote `json:"lote,omitempty"`
}

var zonaAR = func() *time.Location {
	l, err := time.LoadLocation("America/Argentina/Buenos_Aires")
	if err != nil {
		return time.FixedZone("ART", -3*3600)
	}
	return l
}()

// LimitesCorreo son los topes vigentes para leer un mail.
func (a *App) LimitesCorreo() correo.Limites {
	c := a.Config()
	return correo.Limites{MaxMail: c.MaxMail(), MaxAdjunto: c.MaxArchivo}
}

// auditarCorreo anota en la auditoría lo que pasó con un mail: quién lo mandó y qué se hizo, nunca
// el contenido.
func (a *App) auditarCorreo(ctx context.Context, accion, objeto string, detalle map[string]any) {
	if err := a.Store.Auditar(context.WithoutCancel(ctx), store.Evento{Actor: "correo", ActorTipo: "correo", Accion: accion, Objeto: objeto, Detalle: detalle}); err != nil {
		a.Log.Error("no se pudo anotar en la auditoría", "accion", accion, "error", err)
	}
}

// CorreoOmitido anota un mail que no se bajó (por ejemplo, porque supera el tope).
func (a *App) CorreoOmitido(ctx context.Context, forma string, s correo.Sobre, motivo string) {
	a.auditarCorreo(ctx, "correo.ignorado", s.De, map[string]any{"forma": forma, "motivo": motivo, "tamano": s.Tamano})
}

// ClienteDeCorreo decide para qué cliente es un mail: el +CUIT de la dirección; si no, el mapeo
// remitente → CUIT de Clientes y canales; si no, ninguno.
func (a *App) ClienteDeCorreo(m *correo.Mensaje) string {
	if c := correo.CUITDeDestinatarios(m.Para); c != "" {
		return c
	}
	c := arca.SoloDigitos(correo.ClienteDeRemitente(a.Config().CanalClienteCUIT, m.De))
	if arca.CUITValido(c) {
		return c
	}
	return ""
}

func infoAdjuntos(m *correo.Mensaje) []store.AdjuntoInfo {
	res := make([]store.AdjuntoInfo, 0, len(m.Adjuntos))
	for _, x := range m.Adjuntos {
		res = append(res, store.AdjuntoInfo{Nombre: x.Nombre, Mime: x.Mime, Tamano: x.Tamano()})
	}
	return res
}

// RecibirCorreo es la entrada de todo mail, venga por donde venga. Solo se toman los adjuntos: el
// cuerpo es dato de un tercero y no se interpreta. Un remitente que no está entre los permitidos
// queda retenido; uno permitido genera un lote (origen correo). Un mail repetido (mismo
// Message-ID) no hace nada. Devuelve error solo si es una falla pasajera (la base): quien llama lo
// tiene que reintentar.
func (a *App) RecibirCorreo(ctx context.Context, m *correo.Mensaje, forma string) (*ResultadoCorreo, error) {
	cfg := a.Config()
	cliente := a.ClienteDeCorreo(m)
	dest := ""
	if len(m.Para) > 0 {
		dest = m.Para[0]
	}
	id, err := a.Store.ReservarCorreo(ctx, store.CorreoNuevo{MessageID: m.MessageID, Forma: forma, Remitente: m.De, Nombre: m.NombreDe, Destinatario: dest,
		Asunto: m.Asunto, Referencias: m.Referencias, ClienteCUIT: cliente, Fecha: m.Fecha, Tamano: int64(m.Tamano), Adjuntos: infoAdjuntos(m), Descartados: m.Descartados})
	if errors.Is(err, store.ErrCorreoRepetido) {
		return &ResultadoCorreo{Estado: CorreoRepetido}, nil
	}
	if err != nil {
		return nil, err
	}
	det := map[string]any{"forma": forma, "adjuntos": len(m.Adjuntos), "mensaje": id}
	if cliente != "" {
		det["cliente_cuit"] = cliente
	}
	if len(m.Adjuntos) == 0 {
		motivo := "no trae adjuntos que Factorum pueda leer (PDF, JPG, PNG o ZIP)"
		if err := a.Store.TerminarCorreo(ctx, id, CorreoIgnorado, motivo, 0); err != nil {
			return nil, err
		}
		det["motivo"] = motivo
		a.auditarCorreo(ctx, "correo.ignorado", m.De, det)
		return &ResultadoCorreo{Estado: CorreoIgnorado, Motivo: motivo, Mensaje: id}, nil
	}
	if !correo.Permitido(cfg.CorreoPermitidos, m.De) {
		motivo := "el remitente no está entre los permitidos"
		if len(cfg.CorreoPermitidos) == 0 {
			motivo = "todavía no hay remitentes permitidos"
		}
		var sell []store.AdjuntoSellado
		for _, x := range m.Adjuntos {
			b, err := a.Almacen.Sellar(x.Datos)
			if err != nil {
				_ = a.Store.OlvidarCorreo(ctx, id)
				return nil, err
			}
			sell = append(sell, store.AdjuntoSellado{Nombre: x.Nombre, Mime: x.Mime, Datos: b})
		}
		if err := a.Store.RetenerCorreo(ctx, id, motivo, sell); err != nil {
			_ = a.Store.OlvidarCorreo(context.WithoutCancel(ctx), id)
			return nil, err
		}
		det["motivo"] = motivo
		a.auditarCorreo(ctx, "correo.retenido", m.De, det)
		return &ResultadoCorreo{Estado: CorreoRetenido, Motivo: motivo, Mensaje: id}, nil
	}
	archivos := make([]Archivo, 0, len(m.Adjuntos))
	for _, x := range m.Adjuntos {
		archivos = append(archivos, Archivo{Nombre: x.Nombre, Mime: x.Mime, Datos: x.Datos})
	}
	l, motivo, err := a.loteDeCorreo(ctx, id, m.De, cliente, archivos)
	if err != nil {
		_ = a.Store.OlvidarCorreo(context.WithoutCancel(ctx), id)
		return nil, err
	}
	if l == nil {
		_ = a.Store.TerminarCorreo(ctx, id, CorreoIgnorado, motivo, 0)
		det["motivo"] = motivo
		a.auditarCorreo(ctx, "correo.ignorado", m.De, det)
		return &ResultadoCorreo{Estado: CorreoIgnorado, Motivo: motivo, Mensaje: id}, nil
	}
	if err := a.Store.TerminarCorreo(ctx, id, CorreoProcesado, "", l.ID); err != nil {
		a.Log.Warn("no se pudo cerrar el registro del mail", "mensaje", id, "error", err)
	}
	det["lote"] = l.ID
	a.auditarCorreo(ctx, "correo.recibido", m.De, det)
	return &ResultadoCorreo{Estado: CorreoProcesado, Mensaje: id, Lote: l}, nil
}

// loteDeCorreo crea el lote de un mail. Si ningún adjunto sirve (un ZIP sin facturas), devuelve
// nil y el motivo.
func (a *App) loteDeCorreo(ctx context.Context, id int64, remitente, cliente string, archivos []Archivo) (*store.Lote, string, error) {
	cfg := a.Config()
	if ok, desc := Expandir(archivos, cfg.MaxArchivo); len(ok) == 0 {
		return nil, "ningún adjunto es un PDF o una imagen: " + strings.Join(desc, ", "), nil
	}
	rl, err := a.CrearLote(ctx, store.LoteNuevo{
		Nombre:      fmt.Sprintf("Correo de %s %s", remitente, time.Now().In(zonaAR).Format("02/01 15:04")),
		ClienteCUIT: cliente, Origen: "correo", Canal: "correo", Chat: strconv.FormatInt(id, 10),
		IA: cfg.IAPorDefecto, Constatar: cfg.ConstatarPorDefecto && a.Arcanum() != nil,
	}, archivos, 0)
	if err != nil {
		return nil, "", err
	}
	return rl.Lote, "", nil
}

// AceptarRetenido procesa un mail retenido como si hubiera venido de un permitido.
func (a *App) AceptarRetenido(ctx context.Context, id int64, quien string) (*store.Lote, error) {
	m, err := a.Store.Correo(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := a.Store.TomarRetenido(ctx, id); err != nil {
		return nil, err
	}
	devolver := func(err error) (*store.Lote, error) {
		_ = a.Store.DevolverRetenido(context.WithoutCancel(ctx), id)
		return nil, err
	}
	sell, err := a.Store.AdjuntosRetenidos(ctx, id)
	if err != nil {
		return devolver(err)
	}
	var archivos []Archivo
	for _, s := range sell {
		b, err := a.Almacen.Abrir(s.Datos)
		if err != nil {
			return devolver(fmt.Errorf("no se pudo abrir un adjunto guardado: %w", err))
		}
		archivos = append(archivos, Archivo{Nombre: s.Nombre, Mime: s.Mime, Datos: b})
	}
	l, motivo, err := a.loteDeCorreo(ctx, id, m.Remitente, m.ClienteCUIT, archivos)
	if err != nil {
		return devolver(err)
	}
	if l == nil {
		return devolver(errors.New(motivo))
	}
	if err := a.Store.ResolverRetenido(ctx, id, "aceptado", quien, l.ID); err != nil {
		return nil, err
	}
	return l, nil
}

// DescartarRetenido borra los adjuntos de un retenido y lo deja descartado.
func (a *App) DescartarRetenido(ctx context.Context, id int64, quien string) error {
	return a.Store.ResolverRetenido(ctx, id, "descartado", quien, 0)
}

// SMTP es el correo saliente vigente.
func (a *App) SMTP() correo.SMTP {
	c := a.Config()
	return correo.SMTP{Host: c.SMTPHost, Puerto: c.SMTPPuerto, TLS: c.SMTPTLS, Usuario: c.SMTPUsuario, Clave: c.SMTPClave,
		Remitente: c.SMTPRemitente, ResponderA: c.SMTPResponderA, TLSConfig: a.TLSCorreo}
}

// Marca es cómo se presenta la instalación en los mails.
func (a *App) Marca() correo.Marca {
	c := a.Config()
	return correo.Marca{Nombre: c.Nombre, URL: c.URLPublica}
}

// EnviarCorreo manda un mail con el correo saliente vigente.
func (a *App) EnviarCorreo(ctx context.Context, m correo.Mail) (string, error) {
	s := a.SMTP()
	if !s.Configurado() {
		return "", correo.ErrSinSMTP
	}
	return s.Enviar(ctx, m)
}

// Google es la aplicación de Google Cloud vigente (para «Conectar con Google» y Gmail).
func (a *App) Google() correo.Google {
	c := a.Config()
	g := a.GoogleURLs
	g.ClienteID, g.ClienteSecreto = c.GoogleClienteID, c.GoogleClienteSecreto
	return g
}

// IMAP es la casilla vigente.
func (a *App) IMAP() correo.IMAP {
	c := a.Config()
	return correo.IMAP{Host: c.IMAPHost, Puerto: c.IMAPPuerto, TLS: c.IMAPTLS, Usuario: c.IMAPUsuario, Clave: c.IMAPClave, Carpeta: c.IMAPCarpeta,
		Procesado: c.IMAPProcesado, CarpetaProcesado: c.IMAPCarpetaProcesado, TLSConfig: a.TLSCorreo}
}

// Gmail es la cuenta de Gmail vigente.
func (a *App) Gmail() *correo.Gmail {
	c := a.Config()
	return &correo.Gmail{Google: a.Google(), RefreshToken: c.GoogleRefreshToken, Etiqueta: c.GmailEtiqueta, Procesado: c.GmailProcesado}
}

// Entregar es la entrega de IMAP y Gmail: lee el mail y lo recibe. Un mail que no se puede leer se
// anota y no se reintenta; una falla de la base sí.
func (a *App) Entregar(forma string) correo.Entrega {
	return func(ctx context.Context, raw []byte) error {
		m, err := correo.Leer(raw, a.LimitesCorreo())
		if err != nil {
			a.auditarCorreo(ctx, "correo.ignorado", "", map[string]any{"forma": forma, "motivo": err.Error(), "tamano": len(raw)})
			return nil
		}
		_, err = a.RecibirCorreo(ctx, m, forma)
		return err
	}
}
