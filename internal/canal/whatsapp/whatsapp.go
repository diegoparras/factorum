// Package whatsapp es el adaptador de WhatsApp como dispositivo vinculado (igual que WhatsApp Web),
// con go.mau.fi/whatsmeow. La sesión se guarda en la misma base de Factorum (tablas whatsmeow_*).
//
// Si no hay sesión guardada, Estado().QR trae el texto del QR a escanear desde el teléfono
// (Dispositivos vinculados); también se puede vincular con un código (VincularConCodigo).
package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	"github.com/diegoparras/contadorfacturas/internal/canal"
)

const (
	// MaxBytesPorDefecto es el tope por archivo si Config.MaxBytes no se indica.
	MaxBytesPorDefecto int64 = 25 << 20

	esperaInicial   = 2 * time.Second
	esperaMaxima    = 2 * time.Minute
	pausaQR         = 5 * time.Second // entre una tanda de QR vencida y la siguiente
	nombreNavegador = "Chrome (Linux)"
)

// Config del adaptador.
type Config struct {
	// DB es la base donde whatsmeow guarda la sesión (crea sus tablas whatsmeow_*).
	// Con SQLite tiene que tener las claves foráneas activadas (_foreign_keys=on).
	DB *sql.DB
	// Dialecto: "postgres" (también acepta "pgx" y "postgresql") o "sqlite3" (o "sqlite").
	Dialecto string
	// Permitir decide quién puede mandarle archivos a Factorum (recibe el teléfono). Opcional:
	// si es nil, se acepta a todos.
	Permitir func(remitente string) bool
	// MaxBytes es el tope por archivo; por defecto 25 MB.
	MaxBytes int64
}

// WhatsApp implementa canal.Canal. Es seguro para usar desde varias goroutines.
type WhatsApp struct {
	cfg Config
	log *slog.Logger
	wa  waLog.Logger

	mu       sync.Mutex
	cli      *whatsmeow.Client
	reinicio chan error // para cortar la sesión actual y empezar otra (logout, QR vencido)
	qr       string
	numero   string // teléfono propio una vez vinculado
	detalle  string
	iniciado bool
}

var _ canal.Canal = (*WhatsApp)(nil)

// New arma el adaptador. No toca la base ni la red hasta Iniciar.
func New(cfg Config) *WhatsApp {
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = MaxBytesPorDefecto
	}
	l := slog.Default().With("canal", "whatsapp")
	return &WhatsApp{cfg: cfg, log: l, wa: registro{log: l, modulo: "whatsmeow"}}
}

// Nombre devuelve "whatsapp".
func (w *WhatsApp) Nombre() string { return "whatsapp" }

// normalizarDialecto pasa el dialecto de la configuración a uno que whatsmeow (dbutil) entienda.
// El driver de la app es "pgx" (jackc/pgx/v5/stdlib), pero para whatsmeow el dialecto es "postgres".
func normalizarDialecto(d string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "postgres", "postgresql", "pgx":
		return "postgres", nil
	case "sqlite3", "sqlite":
		return "sqlite3", nil
	}
	return "", fmt.Errorf("whatsapp: dialecto %q no soportado (usá \"postgres\" o \"sqlite3\")", d)
}

// Iniciar abre la sesión guardada (o espera la vinculación por QR) y recibe mensajes hasta que
// ctx se cancela. Los cortes de red los resuelve la reconexión automática de whatsmeow; si la
// sesión se pierde (desvinculado desde el teléfono), vuelve a ofrecer un QR.
func (w *WhatsApp) Iniciar(ctx context.Context, h canal.Manejador) error {
	if w.cfg.DB == nil {
		return errors.New("whatsapp: falta la base de datos para guardar la sesión")
	}
	if h == nil {
		return errors.New("whatsapp: falta el manejador de mensajes")
	}
	dialecto, err := normalizarDialecto(w.cfg.Dialecto)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.iniciado {
		w.mu.Unlock()
		return errors.New("whatsapp: el canal ya está iniciado")
	}
	w.iniciado = true
	w.detalle = "iniciando"
	w.mu.Unlock()
	defer func() {
		w.mu.Lock()
		w.iniciado = false
		w.detalle = "detenido"
		w.qr = ""
		w.mu.Unlock()
	}()

	contenedor := sqlstore.NewWithDB(w.cfg.DB, dialecto, w.wa.Sub("Base"))
	if err := contenedor.Upgrade(ctx); err != nil {
		w.anotar("no se pudieron preparar las tablas de la sesión")
		return fmt.Errorf("whatsapp: preparando las tablas de la sesión: %w", err)
	}

	espera := esperaInicial
	for ctx.Err() == nil {
		err := w.sesion(ctx, contenedor, h)
		if ctx.Err() != nil {
			break
		}
		var pausa time.Duration
		switch {
		case errors.Is(err, errQRVencido):
			pausa = pausaQR
		case errors.Is(err, errDesvincular):
			pausa = time.Second
			espera = esperaInicial
		case err != nil:
			w.log.Warn("whatsapp: se cortó la sesión, reintento", "error", err, "espera", espera)
			pausa = espera
			espera = min(espera*2, esperaMaxima)
		default:
			pausa = time.Second
			espera = esperaInicial
		}
		if !dormir(ctx, pausa) {
			break
		}
	}
	return nil
}

var (
	errQRVencido   = errors.New("whatsapp: venció el QR sin escanear")
	errDesvincular = errors.New("whatsapp: sesión desvinculada")
)

// sesion corre un cliente de whatsmeow hasta que ctx se cancela o algo pide reiniciar.
func (w *WhatsApp) sesion(ctx context.Context, contenedor *sqlstore.Container, h canal.Manejador) error {
	dispositivo, err := contenedor.GetFirstDevice(ctx)
	if err != nil {
		w.anotar("no se pudo leer la sesión guardada")
		return fmt.Errorf("whatsapp: leyendo la sesión: %w", err)
	}
	cli := whatsmeow.NewClient(dispositivo, w.wa.Sub("Cliente"))
	cli.EnableAutoReconnect = true
	reinicio := make(chan error, 4)
	cli.AddEventHandler(func(evt any) { w.evento(ctx, cli, h, reinicio, evt) })

	w.mu.Lock()
	w.cli, w.reinicio = cli, reinicio
	w.mu.Unlock()
	defer func() {
		cli.Disconnect()
		w.mu.Lock()
		if w.cli == cli {
			w.cli, w.reinicio = nil, nil
		}
		w.qr = ""
		w.mu.Unlock()
	}()

	if dispositivo.ID == nil {
		qrs, err := cli.GetQRChannel(ctx)
		if err != nil {
			return fmt.Errorf("whatsapp: pidiendo el QR: %w", err)
		}
		w.mu.Lock()
		w.numero = ""
		w.detalle = "conectando para generar el QR"
		w.mu.Unlock()
		if err := cli.ConnectContext(ctx); err != nil {
			w.anotar("sin conexión con WhatsApp")
			return fmt.Errorf("whatsapp: conectando: %w", err)
		}
		go w.seguirQR(qrs, reinicio)
	} else {
		w.mu.Lock()
		w.numero = dispositivo.ID.User
		w.mu.Unlock()
		w.anotar("conectando")
		if err := cli.ConnectContext(ctx); err != nil {
			w.anotar("sin conexión con WhatsApp")
			return fmt.Errorf("whatsapp: conectando: %w", err)
		}
	}

	select {
	case <-ctx.Done():
		return nil
	case err := <-reinicio:
		return err
	}
}

// seguirQR publica cada QR nuevo en el estado y lo limpia al vincular o al vencer.
func (w *WhatsApp) seguirQR(qrs <-chan whatsmeow.QRChannelItem, reinicio chan<- error) {
	for item := range qrs {
		switch item.Event {
		case whatsmeow.QRChannelEventCode:
			w.mu.Lock()
			w.qr = item.Code
			w.detalle = "esperando vinculación: escaneá el QR desde WhatsApp > Dispositivos vinculados"
			w.mu.Unlock()
		case whatsmeow.QRChannelSuccess.Event:
			w.mu.Lock()
			w.qr = ""
			w.detalle = "vinculado, conectando"
			w.mu.Unlock()
		case whatsmeow.QRChannelTimeout.Event:
			w.mu.Lock()
			w.qr = ""
			w.mu.Unlock()
			avisar(reinicio, errQRVencido)
		case whatsmeow.QRChannelEventError:
			w.mu.Lock()
			w.qr = ""
			w.detalle = "falló la vinculación"
			w.mu.Unlock()
			avisar(reinicio, errors.New("whatsapp: falló la vinculación por QR"))
		case whatsmeow.QRChannelClientOutdated.Event:
			w.mu.Lock()
			w.qr = ""
			w.detalle = "WhatsApp rechazó esta versión del cliente: hay que actualizar Factorum"
			w.mu.Unlock()
			avisar(reinicio, errors.New("whatsapp: cliente desactualizado"))
		case whatsmeow.QRChannelEventPasskeyRequest, whatsmeow.QRChannelEventPasskeyResponse:
			// Pasos intermedios de la vinculación: no cambian el QR.
		default:
			w.mu.Lock()
			w.qr = ""
			w.mu.Unlock()
			avisar(reinicio, fmt.Errorf("whatsapp: vinculación interrumpida (%s)", item.Event))
		}
	}
}

func (w *WhatsApp) evento(ctx context.Context, cli *whatsmeow.Client, h canal.Manejador, reinicio chan<- error, evt any) {
	switch e := evt.(type) {
	case *events.Message:
		go w.procesar(ctx, cli, h, e)
	case *events.Connected:
		w.mu.Lock()
		w.qr = ""
		w.detalle = ""
		if id := cli.Store.ID; id != nil {
			w.numero = id.User
		}
		w.mu.Unlock()
		w.log.Info("whatsapp conectado")
	case *events.PairSuccess:
		w.mu.Lock()
		w.qr = ""
		w.numero = e.ID.User
		w.detalle = "vinculado, conectando"
		w.mu.Unlock()
		w.log.Info("whatsapp vinculado")
	case *events.PairError:
		w.anotar("falló la vinculación")
	case *events.Disconnected:
		w.anotar("desconectado, reconectando")
	case *events.KeepAliveTimeout:
		w.anotar("WhatsApp no responde, esperando")
	case *events.KeepAliveRestored:
		w.anotar("")
	case *events.LoggedOut:
		// whatsmeow ya borró la sesión: se arranca de nuevo con un QR.
		w.mu.Lock()
		w.numero = ""
		w.detalle = "la sesión se cerró desde el teléfono: hay que volver a vincular"
		w.mu.Unlock()
		w.log.Warn("whatsapp: la sesión se cerró desde el teléfono")
		avisar(reinicio, nil)
	case *events.StreamReplaced:
		w.anotar("otra instancia abrió la misma sesión de WhatsApp")
		avisar(reinicio, errors.New("whatsapp: otra conexión reemplazó a esta"))
	case *events.TemporaryBan:
		w.anotar("WhatsApp bloqueó temporalmente la cuenta")
		avisar(reinicio, errors.New("whatsapp: bloqueo temporal: "+e.String()))
	case *events.ClientOutdated:
		w.anotar("WhatsApp rechazó esta versión del cliente: hay que actualizar Factorum")
		avisar(reinicio, errors.New("whatsapp: cliente desactualizado"))
	case *events.ConnectFailure:
		w.anotar("WhatsApp rechazó la conexión")
		avisar(reinicio, fmt.Errorf("whatsapp: conexión rechazada (%d)", int(e.Reason)))
	}
}

// procesar convierte, filtra, baja los adjuntos y entrega el mensaje al manejador.
func (w *WhatsApp) procesar(ctx context.Context, cli *whatsmeow.Client, h canal.Manejador, evt *events.Message) {
	buscar := func(lid types.JID) types.JID {
		if cli.Store == nil || cli.Store.LIDs == nil {
			return types.JID{}
		}
		pn, err := cli.Store.LIDs.GetPNForLID(ctx, lid)
		if err != nil {
			return types.JID{}
		}
		return pn
	}
	m, adjuntos, ok := convertir(evt, buscar)
	if !ok {
		return
	}
	if w.cfg.Permitir != nil && !w.cfg.Permitir(m.Remitente) {
		w.log.Debug("whatsapp: mensaje de un remitente no permitido, se ignora")
		return
	}
	if errs := descargar(ctx, cli, &m, adjuntos, w.cfg.MaxBytes); len(errs) > 0 {
		w.log.Warn("whatsapp: no se pudo descargar un adjunto", "error", errs[0], "fallidos", len(errs))
	}
	h(ctx, m)
}

// Enviar manda un mensaje de texto plano. chat es un JID ("549...@s.whatsapp.net") o un teléfono
// en formato internacional.
func (w *WhatsApp) Enviar(ctx context.Context, chat, texto string) error {
	if strings.TrimSpace(texto) == "" {
		return errors.New("whatsapp: no se puede mandar un mensaje vacío")
	}
	jid, err := parsearChat(chat)
	if err != nil {
		return err
	}
	w.mu.Lock()
	cli := w.cli
	w.mu.Unlock()
	if cli == nil || !cli.IsLoggedIn() {
		return errors.New("whatsapp: no hay una sesión conectada")
	}
	if _, err := cli.SendMessage(ctx, jid, &waE2E.Message{Conversation: proto.String(texto)}); err != nil {
		return fmt.Errorf("whatsapp: enviando: %w", err)
	}
	return nil
}

// parsearChat acepta un JID completo o solo el número (con o sin +, espacios o guiones).
func parsearChat(chat string) (types.JID, error) {
	chat = strings.TrimSpace(chat)
	if chat == "" {
		return types.JID{}, errors.New("whatsapp: falta el chat")
	}
	if !strings.Contains(chat, "@") {
		num := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, chat)
		if len(num) < 7 {
			return types.JID{}, fmt.Errorf("whatsapp: %q no es un teléfono válido", chat)
		}
		return types.NewJID(num, types.DefaultUserServer), nil
	}
	jid, err := types.ParseJID(chat)
	if err != nil || jid.User == "" {
		return types.JID{}, fmt.Errorf("whatsapp: chat inválido %q", chat)
	}
	return jid.ToNonAD(), nil
}

// VincularConCodigo pide un código de vinculación para ingresar en el teléfono (Dispositivos
// vinculados > Vincular con el número de teléfono), como alternativa al QR. telefono va en formato
// internacional, sin el 0 ni el 15 (por ejemplo 5491123456789). Devuelve el código "ABCD-EFGH".
// Solo funciona mientras se está mostrando un QR.
func (w *WhatsApp) VincularConCodigo(ctx context.Context, telefono string) (string, error) {
	w.mu.Lock()
	cli, qr, numero := w.cli, w.qr, w.numero
	w.mu.Unlock()
	switch {
	case cli == nil:
		return "", errors.New("whatsapp: el canal no está iniciado")
	case numero != "" || cli.IsLoggedIn():
		return "", errors.New("whatsapp: ya hay un teléfono vinculado; desvinculalo primero")
	case qr == "":
		return "", errors.New("whatsapp: todavía no hay conexión para vincular; probá de nuevo en unos segundos")
	}
	codigo, err := cli.PairPhone(ctx, telefono, true, whatsmeow.PairClientChrome, nombreNavegador)
	switch {
	case errors.Is(err, whatsmeow.ErrPhoneNumberTooShort):
		return "", errors.New("whatsapp: el teléfono es demasiado corto")
	case errors.Is(err, whatsmeow.ErrPhoneNumberIsNotInternational):
		return "", errors.New("whatsapp: el teléfono tiene que ir en formato internacional, sin el 0 adelante (por ejemplo 5491123456789)")
	case err != nil:
		return "", fmt.Errorf("whatsapp: pidiendo el código de vinculación: %w", err)
	}
	w.anotar("esperando vinculación: ingresá el código en el teléfono")
	return codigo, nil
}

// Desvincular cierra la sesión en WhatsApp (el teléfono deja de mostrar este dispositivo) y
// borra la sesión guardada. Después el canal ofrece un QR nuevo.
func (w *WhatsApp) Desvincular(ctx context.Context) error {
	w.mu.Lock()
	cli, reinicio := w.cli, w.reinicio
	w.mu.Unlock()
	if cli == nil {
		return errors.New("whatsapp: el canal no está iniciado")
	}
	if err := cli.Logout(ctx); err != nil {
		if errors.Is(err, whatsmeow.ErrNotLoggedIn) {
			return errors.New("whatsapp: no hay un teléfono vinculado")
		}
		return fmt.Errorf("whatsapp: desvinculando: %w", err)
	}
	w.mu.Lock()
	w.numero = ""
	w.detalle = "desvinculado"
	w.mu.Unlock()
	w.log.Info("whatsapp desvinculado")
	avisar(reinicio, errDesvincular)
	return nil
}

// Estado del canal para la UI. QR trae el texto a codificar mientras se espera la vinculación.
func (w *WhatsApp) Estado() canal.Estado {
	w.mu.Lock()
	cli, qr, numero, detalle, iniciado := w.cli, w.qr, w.numero, w.detalle, w.iniciado
	w.mu.Unlock()
	e := canal.Estado{Canal: w.Nombre(), Habilitado: w.cfg.DB != nil, QR: qr}
	e.Conectado = cli != nil && cli.IsLoggedIn()
	switch {
	case w.cfg.DB == nil:
		e.Detalle = "sin base de datos configurada"
	case !iniciado && detalle == "":
		e.Detalle = "sin iniciar"
	case e.Conectado && detalle == "" && numero != "":
		e.Detalle = "vinculado con +" + numero
	default:
		e.Detalle = detalle
	}
	return e
}

func (w *WhatsApp) anotar(detalle string) {
	w.mu.Lock()
	w.detalle = detalle
	w.mu.Unlock()
}

// avisar manda sin bloquear: si ya hay un pedido de reinicio pendiente, alcanza con ese.
func avisar(ch chan<- error, err error) {
	if ch == nil {
		return
	}
	select {
	case ch <- err:
	default:
	}
}

func dormir(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// registro adapta el logger de whatsmeow a slog. Solo pasa los errores (como advertencias): el
// resto es ruido del protocolo y podría incluir identificadores de contactos.
type registro struct {
	log    *slog.Logger
	modulo string
}

func (r registro) Errorf(msg string, args ...any) {
	r.log.Warn("whatsmeow: "+fmt.Sprintf(msg, args...), "modulo", r.modulo)
}
func (r registro) Warnf(string, ...any)  {}
func (r registro) Infof(string, ...any)  {}
func (r registro) Debugf(string, ...any) {}
func (r registro) Sub(modulo string) waLog.Logger {
	return registro{log: r.log, modulo: r.modulo + "/" + modulo}
}
