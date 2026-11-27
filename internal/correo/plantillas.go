package correo

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"
)

// Marca es lo que los mails dicen de la instalación.
type Marca struct {
	Nombre string // el de la instalación (Factorum, si no tiene)
	URL    string // URL pública, para los links
}

func (m Marca) nombre() string {
	if strings.TrimSpace(m.Nombre) == "" {
		return "Factorum"
	}
	return m.Nombre
}

// ItemCbte es una línea del resumen de un lote.
type ItemCbte struct {
	Descripcion string // "Factura A 00002-00001543"
	Emisor      string // razón social o CUIT
	Total       string // "121.000,00"
	Estado      string // bien, revisar, con errores, aprobado
	Nota        string // el hallazgo que importa, si hay
	Cliente     string // CUIT del cliente, formateado (en las alertas y el semanal)
}

// ResumenLote es lo que se cuenta de un lote terminado.
type ResumenLote struct {
	Nombre      string
	Cliente     string // CUIT formateado o vacío
	Total       int
	Bien        int
	Revisar     int
	Errores     int
	Apocrifos   int
	Adulterados int
	Ilegibles   int // archivos que no se pudieron leer
	Items       []ItemCbte
	Mas         int    // cuántos no entran en la lista
	Link        string // a la pantalla del lote
}

// ResumenSemana es el resumen de los lunes.
type ResumenSemana struct {
	Desde, Hasta string // DD/MM/AAAA
	Lotes        int
	Total        int
	Bien         int
	Revisar      int
	Errores      int
	Apocrifos    int
	Pendientes   int // lo que hay para revisar en total (no solo de la semana)
	PorCliente   []FilaCliente
	Link         string
}

// FilaCliente es una línea del resumen semanal.
type FilaCliente struct {
	Cliente    string
	Total      int
	Pendientes int
}

var base = template.Must(template.New("base").Parse(`<!doctype html>
<html lang="es-AR"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>{{.Titulo}}</title></head>
<body style="margin:0;padding:0;background:#f3f6f7;font-family:Inter,Segoe UI,Helvetica,Arial,sans-serif;color:#1d2a2e">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f6f7;padding:24px 12px"><tr><td align="center">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="max-width:600px;background:#ffffff;border:1px solid #dfe7e9;border-radius:12px;overflow:hidden">
<tr><td style="padding:18px 24px;border-bottom:1px solid #e7eef0">
<table role="presentation" cellpadding="0" cellspacing="0"><tr>
<td style="width:30px;height:30px;background:#0b5d6b;border-radius:7px;color:#fff;font-weight:700;font-size:17px;text-align:center;vertical-align:middle">F</td>
<td style="padding-left:10px;font-weight:700;font-size:16px;color:#0b5d6b">{{.Marca}}</td></tr></table></td></tr>
<tr><td style="padding:22px 24px 8px 24px">
<h1 style="margin:0 0 12px 0;font-size:19px;line-height:1.3;color:#1d2a2e">{{.Titulo}}</h1>
{{.Cuerpo}}
</td></tr>
{{if .Boton}}<tr><td style="padding:8px 24px 22px 24px"><a href="{{.BotonURL}}" style="display:inline-block;background:#0b5d6b;color:#ffffff;text-decoration:none;font-weight:600;padding:10px 18px;border-radius:8px">{{.Boton}}</a></td></tr>{{end}}
<tr><td style="padding:14px 24px;border-top:1px solid #e7eef0;color:#6b7c80;font-size:12px;line-height:1.5">{{.Pie}}</td></tr>
</table></td></tr></table></body></html>`))

type datosBase struct {
	Marca, Titulo, Boton, BotonURL string
	Cuerpo                         template.HTML
	Pie                            string
}

func html(m Marca, titulo string, cuerpo template.HTML, boton, url, pie string) string {
	if pie == "" {
		pie = m.nombre() + " lee las facturas de ARCA por su QR y su texto. Este mail lo mandó Factorum automáticamente: no hace falta contestarlo."
	}
	if url == "" {
		boton = ""
	}
	var b bytes.Buffer
	_ = base.Execute(&b, datosBase{Marca: m.nombre(), Titulo: titulo, Cuerpo: cuerpo, Boton: boton, BotonURL: url, Pie: pie})
	return b.String()
}

func parrafo(s string) template.HTML {
	return template.HTML(`<p style="margin:0 0 12px 0;font-size:14px;line-height:1.55">` + template.HTMLEscapeString(s) + `</p>`)
}

func cuenta(r ResumenLote) string {
	s := fmt.Sprintf("%d comprobante(s): %d bien, %d para revisar, %d con errores.", r.Total, r.Bien, r.Revisar, r.Errores)
	if r.Apocrifos > 0 {
		s += fmt.Sprintf(" %d de emisores apócrifos.", r.Apocrifos)
	}
	if r.Adulterados > 0 {
		s += fmt.Sprintf(" %d con el PDF posiblemente adulterado (el texto no coincide con el QR).", r.Adulterados)
	}
	if r.Ilegibles > 0 {
		s += fmt.Sprintf(" %d archivo(s) no se pudieron leer.", r.Ilegibles)
	}
	return s
}

var tablaItems = template.Must(template.New("items").Parse(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;margin:4px 0 14px 0">
{{range .}}<tr><td style="padding:7px 0;border-top:1px solid #eef3f4;vertical-align:top">{{.Descripcion}}<br><span style="color:#6b7c80">{{.Emisor}}{{if .Cliente}} · cliente {{.Cliente}}{{end}}</span>{{if .Nota}}<br><span style="color:#9a5b00">{{.Nota}}</span>{{end}}</td>
<td style="padding:7px 0 7px 10px;border-top:1px solid #eef3f4;text-align:right;white-space:nowrap;vertical-align:top">$ {{.Total}}<br><span style="color:{{if eq .Estado "bien"}}#1f7a4d{{else if eq .Estado "aprobado"}}#1f7a4d{{else if eq .Estado "revisar"}}#9a5b00{{else}}#b42318{{end}};font-weight:600">{{.Estado}}</span></td></tr>
{{end}}</table>`))

func itemsHTML(items []ItemCbte, mas int) template.HTML {
	if len(items) == 0 {
		return ""
	}
	var b bytes.Buffer
	_ = tablaItems.Execute(&b, items)
	if mas > 0 {
		b.WriteString(string(parrafo(fmt.Sprintf("... y %d más.", mas))))
	}
	return template.HTML(b.String())
}

func itemsTexto(items []ItemCbte, mas int) string {
	var b strings.Builder
	for _, it := range items {
		fmt.Fprintf(&b, "\n- %s, %s, $ %s: %s", it.Descripcion, it.Emisor, it.Total, it.Estado)
		if it.Cliente != "" {
			fmt.Fprintf(&b, " (cliente %s)", it.Cliente)
		}
		if it.Nota != "" {
			fmt.Fprintf(&b, " (%s)", it.Nota)
		}
	}
	if mas > 0 {
		fmt.Fprintf(&b, "\n... y %d más.", mas)
	}
	return b.String()
}

func pieTexto(m Marca) string {
	n := m.nombre()
	if n != "Factorum" {
		n += " · Factorum"
	}
	return "\n\n--\n" + n + ", lector de facturas de ARCA. Este mail se mandó automáticamente."
}

// RespuestaLote es la respuesta a quien mandó facturas por mail.
func RespuestaLote(m Marca, r ResumenLote, asuntoOriginal string) Mail {
	asunto := "Re: " + strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(asuntoOriginal, "Re: "), "RE: "))
	if strings.TrimSpace(asuntoOriginal) == "" {
		asunto = "Recibimos tus comprobantes"
	}
	intro := "Recibimos lo que mandaste y ya lo leímos."
	texto := intro + "\n\n" + cuenta(r) + itemsTexto(r.Items, r.Mas)
	if r.Link != "" {
		texto += "\n\nVerlo en " + m.nombre() + ": " + r.Link
	}
	cuerpo := parrafo(intro) + parrafo(cuenta(r)) + itemsHTML(r.Items, r.Mas)
	return Mail{Asunto: asunto, Texto: texto + pieTexto(m), HTML: html(m, "Leímos tus comprobantes", cuerpo, "Verlo en "+m.nombre(), r.Link, "")}
}

// AvisoLote es el aviso de lote terminado.
func AvisoLote(m Marca, r ResumenLote) Mail {
	nombre := r.Nombre
	if nombre == "" {
		nombre = "sin nombre"
	}
	titulo := "Terminó el lote «" + nombre + "»"
	intro := "Factorum terminó de leer el lote."
	if r.Cliente != "" {
		intro = "Factorum terminó de leer el lote del cliente " + r.Cliente + "."
	}
	texto := intro + "\n\n" + cuenta(r) + itemsTexto(r.Items, r.Mas)
	if r.Link != "" {
		texto += "\n\nRevisalo en " + r.Link
	}
	cuerpo := parrafo(intro) + parrafo(cuenta(r)) + itemsHTML(r.Items, r.Mas)
	return Mail{Asunto: titulo, Texto: texto + pieTexto(m), HTML: html(m, titulo, cuerpo, "Revisar el lote", r.Link, "")}
}

// Alerta avisa de emisores apócrifos y PDFs posiblemente adulterados.
func Alerta(m Marca, lote string, apocrifos, adulterados []ItemCbte, link string) Mail {
	titulo := "Alerta: "
	var partes []string
	if len(apocrifos) > 0 {
		partes = append(partes, fmt.Sprintf("%d comprobante(s) de emisores apócrifos", len(apocrifos)))
	}
	if len(adulterados) > 0 {
		partes = append(partes, fmt.Sprintf("%d PDF(s) posiblemente adulterados", len(adulterados)))
	}
	titulo += strings.Join(partes, " y ")
	intro := "En el lote «" + lote + "» Factorum encontró comprobantes que conviene mirar antes de registrarlos."
	texto := intro
	cuerpo := parrafo(intro)
	if len(apocrifos) > 0 {
		t := "Emisor en la base de apócrifos de ARCA:"
		texto += "\n\n" + t + itemsTexto(apocrifos, 0)
		cuerpo += parrafo(t) + itemsHTML(apocrifos, 0)
	}
	if len(adulterados) > 0 {
		t := "El texto del PDF no coincide con su QR (puede estar adulterado):"
		texto += "\n\n" + t + itemsTexto(adulterados, 0)
		cuerpo += parrafo(t) + itemsHTML(adulterados, 0)
	}
	if link != "" {
		texto += "\n\nMiralos en " + link
	}
	return Mail{Asunto: titulo, Texto: texto + pieTexto(m), HTML: html(m, titulo, cuerpo, "Ver en "+m.nombre(), link, "")}
}

// Semanal es el resumen de los lunes.
func Semanal(m Marca, s ResumenSemana) Mail {
	titulo := fmt.Sprintf("Tu semana en %s: %d comprobante(s)", m.nombre(), s.Total)
	l1 := fmt.Sprintf("Del %s al %s entraron %d lote(s) con %d comprobante(s): %d bien, %d para revisar, %d con errores.", s.Desde, s.Hasta, s.Lotes, s.Total, s.Bien, s.Revisar, s.Errores)
	if s.Apocrifos > 0 {
		l1 += fmt.Sprintf(" %d de emisores apócrifos.", s.Apocrifos)
	}
	l2 := fmt.Sprintf("Para revisar quedan %d en total.", s.Pendientes)
	if s.Pendientes == 0 {
		l2 = "No queda nada pendiente de revisar."
	}
	texto := l1 + "\n\n" + l2
	cuerpo := parrafo(l1) + parrafo(l2)
	if len(s.PorCliente) > 0 {
		texto += "\n\nPor cliente:"
		var b strings.Builder
		b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;font-size:13px;margin:4px 0 14px 0"><tr><th align="left" style="padding:6px 0;color:#6b7c80;font-weight:600">Cliente</th><th align="right" style="padding:6px 0;color:#6b7c80;font-weight:600">En la semana</th><th align="right" style="padding:6px 0;color:#6b7c80;font-weight:600">Para revisar</th></tr>`)
		for _, f := range s.PorCliente {
			fmt.Fprintf(&b, `<tr><td style="padding:6px 0;border-top:1px solid #eef3f4">%s</td><td align="right" style="padding:6px 0;border-top:1px solid #eef3f4">%d</td><td align="right" style="padding:6px 0;border-top:1px solid #eef3f4">%d</td></tr>`,
				template.HTMLEscapeString(f.Cliente), f.Total, f.Pendientes)
			texto += fmt.Sprintf("\n- %s: %d en la semana, %d para revisar", f.Cliente, f.Total, f.Pendientes)
		}
		b.WriteString(`</table>`)
		cuerpo += template.HTML(b.String())
	}
	if s.Link != "" {
		texto += "\n\nRevisar: " + s.Link
	}
	return Mail{Asunto: titulo, Texto: texto + pieTexto(m), HTML: html(m, titulo, cuerpo, "Ir a Revisar", s.Link, "")}
}

// Prueba es el mail del botón «Probar» del correo saliente.
func Prueba(m Marca) Mail {
	t := "Si te llegó este mail, el correo saliente de " + m.nombre() + " anda: ya puede contestar a quien manda facturas, mandar los avisos y los links para restablecer contraseñas."
	return Mail{Asunto: "Prueba del correo de " + m.nombre(), Texto: t + pieTexto(m), HTML: html(m, "El correo saliente anda", parrafo(t), "", "", "")}
}

// Restablecer es el mail con el link para elegir una contraseña nueva.
func Restablecer(m Marca, nombre, link string, minutos int) Mail {
	hola := "Hola"
	if nombre != "" {
		hola += " " + nombre
	}
	l1 := fmt.Sprintf("%s. Alguien (ojalá vos) pidió restablecer la contraseña de tu cuenta de %s. Con este link elegís una nueva; vale una sola vez y vence en %d minutos.", hola, m.nombre(), minutos)
	l2 := "Si no fuiste vos, no hagas nada: tu contraseña sigue siendo la de siempre."
	texto := l1 + "\n\n" + link + "\n\n" + l2
	return Mail{Asunto: "Restablecer tu contraseña de " + m.nombre(), Texto: texto + pieTexto(m),
		HTML: html(m, "Restablecer tu contraseña", parrafo(l1)+parrafo(l2), "Elegir una contraseña nueva", link, "Si el botón no anda, copiá este link en el navegador: "+link)}
}

// Invitacion es el mail para que alguien nuevo elija su contraseña.
func Invitacion(m Marca, nombre, quien, link string, horas int) Mail {
	hola := "Hola"
	if nombre != "" {
		hola += " " + nombre
	}
	l1 := fmt.Sprintf("%s. %s te dio acceso a %s, el lector de facturas de ARCA del estudio. Para entrar, elegí tu contraseña con este link (vale una sola vez y vence en %d horas).", hola, quien, m.nombre(), horas)
	texto := l1 + "\n\n" + link
	return Mail{Asunto: "Te invitaron a " + m.nombre(), Texto: texto + pieTexto(m),
		HTML: html(m, "Te invitaron a "+m.nombre(), parrafo(l1), "Elegir mi contraseña", link, "Si el botón no anda, copiá este link en el navegador: "+link)}
}
