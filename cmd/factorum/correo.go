package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/factorum/internal/api"
	"github.com/diegoparras/factorum/internal/app"
	"github.com/diegoparras/factorum/internal/config"
	"github.com/diegoparras/factorum/internal/correo"
)

// gestorCorreo revisa la casilla (IMAP) o la cuenta de Gmail cada tanto, según la forma elegida en
// Configuración. Cloudflare y los proveedores llegan solos por HTTP: no hay nada que revisar. Al
// cambiar la configuración, la vuelta que corre se corta y arranca la nueva.
type gestorCorreo struct {
	ctx context.Context
	a   *app.App
	log *slog.Logger

	mu     sync.Mutex
	firma  string
	cancel context.CancelFunc
	listo  chan struct{}

	emu    sync.Mutex
	estado api.EstadoCorreo
}

func nuevoGestorCorreo(ctx context.Context, a *app.App, log *slog.Logger) *gestorCorreo {
	return &gestorCorreo{ctx: ctx, a: a, log: log.With("componente", "correo")}
}

// Estado es cómo viene la revisión (para la pantalla).
func (g *gestorCorreo) Estado() api.EstadoCorreo {
	g.emu.Lock()
	defer g.emu.Unlock()
	return g.estado
}

func firmaCorreo(c *config.Config) string {
	switch c.FormaCorreo() {
	case "imap":
		if c.IMAPHost == "" || c.IMAPUsuario == "" || c.IMAPClave == "" {
			return ""
		}
		return strings.Join([]string{"imap", c.IMAPHost, strconv.Itoa(c.IMAPPuerto), c.IMAPTLS, c.IMAPUsuario, c.IMAPClave, c.IMAPCarpeta,
			strconv.Itoa(c.IMAPIntervalo), c.IMAPProcesado, c.IMAPCarpetaProcesado}, "|")
	case "gmail":
		if c.GoogleClienteID == "" || c.GoogleClienteSecreto == "" || c.GoogleRefreshToken == "" {
			return ""
		}
		return strings.Join([]string{"gmail", c.GoogleClienteID, c.GoogleClienteSecreto, c.GoogleRefreshToken, c.GmailEtiqueta, c.GmailProcesado,
			strconv.Itoa(c.GmailIntervalo)}, "|")
	}
	return ""
}

// Aplicar deja la revisión como dice la configuración.
func (g *gestorCorreo) Aplicar(c *config.Config) {
	g.mu.Lock()
	defer g.mu.Unlock()
	firma := firmaCorreo(c)
	if firma == g.firma {
		return
	}
	if g.cancel != nil {
		g.cancel()
		select {
		case <-g.listo:
		case <-time.After(20 * time.Second):
			g.log.Warn("la revisión del correo tardó en detenerse")
		}
		g.cancel = nil
		g.log.Info("revisión del correo detenida")
	}
	g.firma = firma
	g.emu.Lock()
	g.estado = api.EstadoCorreo{Forma: c.FormaCorreo()}
	switch {
	case firma != "":
		g.estado.Activo, g.estado.Detalle = true, "revisando"
	case c.FormaCorreo() == "imap":
		g.estado.Detalle = "faltan el servidor, el usuario o la contraseña"
	case c.FormaCorreo() == "gmail":
		g.estado.Detalle = "falta conectar con Google"
	}
	g.emu.Unlock()
	if firma == "" {
		return
	}
	ctx, cancel := context.WithCancel(g.ctx)
	g.cancel, g.listo = cancel, make(chan struct{})
	forma := c.FormaCorreo()
	intervalo := c.IMAPIntervalo
	var revisar func(ctx context.Context, max int64, e correo.Entrega, o func(correo.Sobre)) (int, error)
	if forma == "imap" {
		b := g.a.IMAP()
		revisar = b.Revisar
	} else {
		intervalo = c.GmailIntervalo
		revisar = g.a.Gmail().Revisar
	}
	if intervalo <= 0 {
		intervalo = 60
	}
	go g.vueltas(ctx, g.listo, forma, time.Duration(intervalo)*time.Second, revisar)
	g.log.Info("revisión del correo iniciada", "forma", forma, "cada_s", intervalo)
}

func (g *gestorCorreo) vueltas(ctx context.Context, listo chan struct{}, forma string, cada time.Duration,
	revisar func(context.Context, int64, correo.Entrega, func(correo.Sobre)) (int, error)) {
	defer close(listo)
	entregar := func(ctx context.Context, raw []byte) error {
		m, err := correo.Leer(raw, g.a.LimitesCorreo())
		if err != nil {
			g.a.CorreoOmitido(ctx, forma, correo.Sobre{Tamano: int64(len(raw))}, err.Error())
			return nil
		}
		res, err := g.a.RecibirCorreo(ctx, m, forma)
		if err != nil {
			return err
		}
		g.emu.Lock()
		switch res.Estado {
		case app.CorreoProcesado:
			g.estado.Recibidos++
		case app.CorreoRetenido:
			g.estado.Retenidos++
		}
		ahora := time.Now()
		g.estado.UltimoMail = &ahora
		g.emu.Unlock()
		return nil
	}
	omitido := func(s correo.Sobre) { g.a.CorreoOmitido(ctx, forma, s, "el mail supera el tope") }
	for {
		vctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		n, err := revisar(vctx, g.a.Config().MaxMail(), entregar, omitido)
		cancel()
		if ctx.Err() != nil {
			return
		}
		ahora := time.Now()
		g.emu.Lock()
		g.estado.Ultima = &ahora
		g.estado.Error = ""
		g.estado.Detalle = "revisando"
		if err != nil {
			g.estado.Error = err.Error()
			g.estado.Detalle = "con problemas"
		}
		g.emu.Unlock()
		if err != nil {
			g.log.Warn("no se pudo revisar el correo", "forma", forma, "error", err)
		} else if n > 0 {
			g.log.Info("correo revisado", "forma", forma, "mails", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(cada):
		}
	}
}
