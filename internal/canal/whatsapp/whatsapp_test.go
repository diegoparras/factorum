package whatsapp

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"

	"github.com/diegoparras/factorum/internal/canal"
)

var (
	yo      = types.NewJID("5491100000001", types.DefaultUserServer)
	cliente = types.NewJID("5491122223333", types.DefaultUserServer)
	lid     = types.NewJID("123456789012345", types.HiddenUserServer)
)

func info(chat, sender types.JID) types.MessageInfo {
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: sender},
		ID:            "ABC",
		PushName:      "Diego",
	}
}

func evento(inf types.MessageInfo, msg *waE2E.Message) *events.Message {
	return &events.Message{Info: inf, Message: msg}
}

func TestAceptable(t *testing.T) {
	grupo := types.NewJID("120363000000000000", types.GroupServer)
	casos := []struct {
		nombre string
		info   types.MessageInfo
		quiero bool
	}{
		{"chat individual", info(cliente, cliente), true},
		{"chat individual por LID", info(lid, lid), true},
		{"chat con dispositivo", info(cliente, types.JID{User: cliente.User, Server: types.DefaultUserServer, Device: 3}), true},
		{"grupo", func() types.MessageInfo { i := info(grupo, cliente); i.IsGroup = true; return i }(), false},
		{"grupo sin bandera", info(grupo, cliente), false},
		{"estados", info(types.StatusBroadcastJID, cliente), false},
		{"lista de difusión", info(types.NewJID("1234", types.BroadcastServer), cliente), false},
		{"canal", info(types.NewJID("1234", types.NewsletterServer), cliente), false},
		{"propio", func() types.MessageInfo { i := info(cliente, yo); i.IsFromMe = true; return i }(), false},
		{"bot", info(types.NewJID("1234", types.BotServer), types.NewJID("1234", types.BotServer)), false},
	}
	for _, c := range casos {
		if got := aceptable(c.info); got != c.quiero {
			t.Errorf("%s: aceptable = %v, quería %v", c.nombre, got, c.quiero)
		}
	}
}

func TestExtraerTexto(t *testing.T) {
	casos := []struct {
		nombre string
		msg    *waE2E.Message
		quiero string
	}{
		{"nil", nil, ""},
		{"conversación", &waE2E.Message{Conversation: proto.String("hola")}, "hola"},
		{"texto extendido", &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("mirá https://x")}}, "mirá https://x"},
		{"documento con epígrafe", &waE2E.Message{DocumentMessage: &waE2E.DocumentMessage{Caption: proto.String("la de octubre")}}, "la de octubre"},
		{"imagen con epígrafe", &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String("ticket")}}, "ticket"},
		{"sticker", &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}, ""},
	}
	for _, c := range casos {
		if got := extraerTexto(c.msg); got != c.quiero {
			t.Errorf("%s: %q, quería %q", c.nombre, got, c.quiero)
		}
	}
}

func TestRemitente(t *testing.T) {
	if got := remitente(types.MessageSource{Chat: cliente, Sender: cliente.ToNonAD()}, nil); got != "5491122223333" {
		t.Errorf("por teléfono: %q", got)
	}
	conDispositivo := types.JID{User: "5491122223333", Server: types.DefaultUserServer, Device: 7}
	if got := remitente(types.MessageSource{Chat: cliente, Sender: conDispositivo}, nil); got != "5491122223333" {
		t.Errorf("con dispositivo: %q", got)
	}
	if got := remitente(types.MessageSource{Chat: lid, Sender: lid, SenderAlt: cliente}, nil); got != "5491122223333" {
		t.Errorf("LID con SenderAlt: %q", got)
	}
	buscar := func(l types.JID) types.JID {
		if l == lid {
			return cliente
		}
		return types.JID{}
	}
	if got := remitente(types.MessageSource{Chat: lid, Sender: lid}, buscar); got != "5491122223333" {
		t.Errorf("LID con buscador: %q", got)
	}
	if got := remitente(types.MessageSource{Chat: lid, Sender: lid}, nil); got != lid.User {
		t.Errorf("LID sin resolver: %q", got)
	}
}

func TestConvertirTexto(t *testing.T) {
	m, adj, ok := convertir(evento(info(cliente, cliente), &waE2E.Message{Conversation: proto.String("¿llegó la factura?")}), nil)
	if !ok {
		t.Fatal("tendría que aceptarlo")
	}
	if len(adj) != 0 {
		t.Errorf("adjuntos = %v", adj)
	}
	want := canal.Mensaje{Canal: "whatsapp", Chat: "5491122223333@s.whatsapp.net", Remitente: "5491122223333", NombreRemitente: "Diego", Texto: "¿llegó la factura?"}
	if m.Canal != want.Canal || m.Chat != want.Chat || m.Remitente != want.Remitente || m.NombreRemitente != want.NombreRemitente || m.Texto != want.Texto {
		t.Errorf("mensaje = %+v\nquería    %+v", m, want)
	}
}

func TestConvertirDocumentoEImagen(t *testing.T) {
	doc := &waE2E.DocumentMessage{FileName: proto.String("FA-0001-00001234.pdf"), Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(1234), Caption: proto.String("va")}
	_, adj, ok := convertir(evento(info(cliente, cliente), &waE2E.Message{DocumentMessage: doc}), nil)
	if !ok || len(adj) != 1 {
		t.Fatalf("documento: ok=%v adj=%v", ok, adj)
	}
	if a := adj[0]; a.nombre != "FA-0001-00001234.pdf" || a.mime != "application/pdf" || a.tam != 1234 || a.media != doc {
		t.Errorf("documento: %+v", a)
	}

	sinNombre := &waE2E.DocumentMessage{Mimetype: proto.String("application/pdf")}
	_, adj, _ = convertir(evento(info(cliente, cliente), &waE2E.Message{DocumentMessage: sinNombre}), nil)
	if len(adj) != 1 || adj[0].nombre != "documento.pdf" {
		t.Errorf("documento sin nombre: %+v", adj)
	}

	img := &waE2E.ImageMessage{Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(50)}
	m, adj, ok := convertir(evento(info(cliente, cliente), &waE2E.Message{ImageMessage: img}), nil)
	if !ok || len(adj) != 1 || adj[0].nombre != "foto.jpg" || adj[0].mime != "image/jpeg" || adj[0].media != img {
		t.Fatalf("imagen: ok=%v adj=%+v", ok, adj)
	}
	if m.Texto != "" {
		t.Errorf("imagen sin epígrafe: texto %q", m.Texto)
	}
}

func TestConvertirIgnora(t *testing.T) {
	grupo := info(types.NewJID("120363000000000000", types.GroupServer), cliente)
	grupo.IsGroup = true
	propio := info(cliente, yo)
	propio.IsFromMe = true
	texto := &waE2E.Message{Conversation: proto.String("hola")}
	casos := map[string]*events.Message{
		"nil":        nil,
		"sin cuerpo": {Info: info(cliente, cliente)},
		"grupo":      evento(grupo, texto),
		"estados":    evento(info(types.StatusBroadcastJID, cliente), texto),
		"propio":     evento(propio, texto),
		"sticker":    evento(info(cliente, cliente), &waE2E.Message{StickerMessage: &waE2E.StickerMessage{}}),
		"protocolo":  evento(info(cliente, cliente), &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{}}),
		"edición":    {Info: info(cliente, cliente), Message: texto, IsEdit: true},
		"vacío":      evento(info(cliente, cliente), &waE2E.Message{Conversation: proto.String("   ")}),
	}
	for nombre, evt := range casos {
		if _, _, ok := convertir(evt, nil); ok {
			t.Errorf("%s: tendría que ignorarlo", nombre)
		}
	}
}

type descargadorFalso struct {
	datos   map[whatsmeow.DownloadableMessage][]byte
	falla   map[whatsmeow.DownloadableMessage]bool
	pedidos int
}

func (d *descargadorFalso) Download(_ context.Context, msg whatsmeow.DownloadableMessage) ([]byte, error) {
	d.pedidos++
	if d.falla[msg] {
		return nil, errors.New("media vencida")
	}
	return d.datos[msg], nil
}

func TestDescargar(t *testing.T) {
	pdf := &waE2E.DocumentMessage{FileName: proto.String("f.pdf"), Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(4)}
	enorme := &waE2E.DocumentMessage{FileName: proto.String("enorme.pdf"), FileLength: proto.Uint64(10 << 20)}
	mentiroso := &waE2E.DocumentMessage{FileName: proto.String("mentiroso.pdf"), FileLength: proto.Uint64(1)}
	roto := &waE2E.ImageMessage{FileLength: proto.Uint64(3)}
	d := &descargadorFalso{
		datos: map[whatsmeow.DownloadableMessage][]byte{pdf: []byte("%PDF"), mentiroso: make([]byte, 2<<20)},
		falla: map[whatsmeow.DownloadableMessage]bool{roto: true},
	}
	m := canal.Mensaje{Texto: "van tres"}
	adj := []adjunto{
		{nombre: "f.pdf", mime: "application/pdf", tam: 4, media: pdf},
		{nombre: "enorme.pdf", tam: 10 << 20, media: enorme},
		{nombre: "mentiroso.pdf", tam: 1, media: mentiroso},
		{nombre: "foto.jpg", mime: "image/jpeg", tam: 3, media: roto},
	}
	errs := descargar(context.Background(), d, &m, adj, 1<<20)
	if len(errs) != 1 {
		t.Errorf("errores = %v", errs)
	}
	if d.pedidos != 3 {
		t.Errorf("no tendría que bajar lo que declara pesar de más: pedidos = %d", d.pedidos)
	}
	if len(m.Archivos) != 1 || m.Archivos[0].Nombre != "f.pdf" || string(m.Archivos[0].Datos) != "%PDF" || m.Archivos[0].Mime != "application/pdf" {
		t.Errorf("archivos = %+v", m.Archivos)
	}
	for _, s := range []string{"van tres\n", `"enorme.pdf" pesa más de 1 MB`, `"mentiroso.pdf" pesa más de 1 MB`, `No se pudo descargar el archivo "foto.jpg"`} {
		if !strings.Contains(m.Texto, s) {
			t.Errorf("el texto no avisa %q:\n%s", s, m.Texto)
		}
	}
}

func TestParsearChat(t *testing.T) {
	casos := map[string]string{
		"5491122223333@s.whatsapp.net":   "5491122223333@s.whatsapp.net",
		"5491122223333:4@s.whatsapp.net": "5491122223333@s.whatsapp.net",
		"+54 9 11 2222-3333":             "5491122223333@s.whatsapp.net",
		"123456789012345@lid":            "123456789012345@lid",
	}
	for entrada, quiero := range casos {
		jid, err := parsearChat(entrada)
		if err != nil {
			t.Errorf("%q: %v", entrada, err)
			continue
		}
		if jid.String() != quiero {
			t.Errorf("%q: %s, quería %s", entrada, jid, quiero)
		}
	}
	for _, malo := range []string{"", "  ", "123", "@s.whatsapp.net"} {
		if _, err := parsearChat(malo); err == nil {
			t.Errorf("%q tendría que fallar", malo)
		}
	}
}

func TestNormalizarDialecto(t *testing.T) {
	for entrada, quiero := range map[string]string{"pgx": "postgres", "postgres": "postgres", "PostgreSQL": "postgres", "sqlite3": "sqlite3", "sqlite": "sqlite3"} {
		got, err := normalizarDialecto(entrada)
		if err != nil || got != quiero {
			t.Errorf("%q: %q, %v", entrada, got, err)
		}
	}
	if _, err := normalizarDialecto("mysql"); err == nil {
		t.Error("mysql no está soportado")
	}
}

func TestSinIniciar(t *testing.T) {
	w := New(Config{})
	if err := w.Iniciar(context.Background(), func(context.Context, canal.Mensaje) {}); err == nil {
		t.Error("sin base tendría que fallar")
	}
	if e := w.Estado(); e.Habilitado || e.Conectado || e.Canal != "whatsapp" {
		t.Errorf("estado = %+v", e)
	}
	w = New(Config{DB: &sql.DB{}, Dialecto: "oracle"})
	if err := w.Iniciar(context.Background(), func(context.Context, canal.Mensaje) {}); err == nil {
		t.Error("con un dialecto no soportado tendría que fallar")
	}
	if err := w.Enviar(context.Background(), "5491122223333", "hola"); err == nil {
		t.Error("sin sesión Enviar tendría que fallar")
	}
	if _, err := w.VincularConCodigo(context.Background(), "5491122223333"); err == nil {
		t.Error("sin iniciar VincularConCodigo tendría que fallar")
	}
	if err := w.Desvincular(context.Background()); err == nil {
		t.Error("sin iniciar Desvincular tendría que fallar")
	}
	if e := w.Estado(); !e.Habilitado || e.Conectado || e.Detalle != "sin iniciar" {
		t.Errorf("estado = %+v", e)
	}
}
