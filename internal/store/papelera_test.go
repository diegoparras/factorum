package store_test

// La papelera contra Postgres real (una base nueva por prueba, a partir de FACTORUM_TEST_DB): todo
// lo que lista, cuenta, resume o deduplica ignora lo borrado; restaurar deja todo como estaba; el
// borrado definitivo borra las filas y anota los archivos que ya no usa nadie.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/pruebas"
	"github.com/diegoparras/factorum/internal/store"
)

const (
	cliX = "20222222223"
	cliY = "30711111111"
)

func abrir(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Abrir(context.Background(), pruebas.BaseNueva(t), 6)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Cerrar)
	return st
}

// cbteDe arma un comprobante leído: emisor, número y si es apócrifo.
func cbteDe(emisor string, numero int64, apocrifo bool) modelo.ResultadoCbte {
	c := modelo.Comprobante{Tipo: 1, Letra: "A", PuntoVenta: 2, Numero: numero, Fecha: "2026-11-02", CUITEmisor: emisor,
		RazonSocialEmisor: "EMISOR " + emisor, NetoGravado: 10000, IVA21: 2100, Total: 12100}
	rc := modelo.ResultadoCbte{Comprobante: c, Estado: modelo.EstadoOK, Confianza: 1}
	if apocrifo {
		rc.Estado = modelo.EstadoError
		rc.Hallazgos = []modelo.Hallazgo{{Codigo: "emisor_apocrifo", Nivel: modelo.NivelError, Mensaje: "apócrifo"}}
	}
	return rc
}

type carga struct {
	lote int64
	docs []int64
	cbts [][]int64 // por documento
}

// cargar crea un lote del cliente con un documento por sha (cada uno con esos comprobantes),
// procesados como lo hace el trabajo de la cola.
func cargar(t *testing.T, st *store.Store, cliente, canal, chat string, docs map[string][]modelo.ResultadoCbte, orden ...string) carga {
	t.Helper()
	ctx := context.Background()
	var c carga
	err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		var err error
		c.lote, err = st.CrearLote(ctx, tx, store.LoteNuevo{Nombre: "Lote de " + cliente, ClienteCUIT: cliente, Origen: "web", Canal: canal, Chat: chat})
		if err != nil {
			return err
		}
		for _, sha := range orden {
			id, rep, err := st.CrearDocumento(ctx, tx, c.lote, sha+".pdf", "application/pdf", 100, sha)
			if err != nil {
				return err
			}
			if rep {
				return fmt.Errorf("%s salió repetido", sha)
			}
			c.docs = append(c.docs, id)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, sha := range orden {
		doc, err := st.Documento(ctx, c.docs[i])
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := st.TomarDocumento(ctx, doc.ID); err != nil || !ok {
			t.Fatalf("tomar: %v %v", ok, err)
		}
		ids, err := st.GuardarResultado(ctx, doc, &modelo.Resultado{Archivo: sha + ".pdf", Paginas: 1, Comprobantes: docs[sha], DuracionMS: 15})
		if err != nil {
			t.Fatal(err)
		}
		c.cbts = append(c.cbts, ids)
	}
	if _, err := st.CerrarLoteSiTermino(ctx, c.lote); err != nil {
		t.Fatal(err)
	}
	return c
}

// cuenta es lo que se ve de una instalación por todos los caminos que listan o cuentan.
type cuenta struct {
	lotes, cbtes, cbtesRevisar, panelTotal, panelHoy, panelApoc, periodoLotes, periodoTotal, periodoPend, rubros int
	metrica                                                                                                      int64
	porCliente                                                                                                   string
}

func contar(t *testing.T, st *store.Store, alcance []string) cuenta {
	t.Helper()
	ctx := context.Background()
	var c cuenta
	var err error
	if _, c.lotes, err = st.Lotes(ctx, 100, 0, alcance); err != nil {
		t.Fatal(err)
	}
	if _, c.cbtes, err = st.Comprobantes(ctx, store.Filtro{Alcance: alcance}); err != nil {
		t.Fatal(err)
	}
	if _, c.cbtesRevisar, err = st.Comprobantes(ctx, store.Filtro{Alcance: alcance, Estado: "error"}); err != nil {
		t.Fatal(err)
	}
	p, err := st.Panel(ctx, time.Now(), alcance)
	if err != nil {
		t.Fatal(err)
	}
	c.panelTotal, c.panelHoy, c.panelApoc = p.Total, p.Hoy.Comprobantes, p.Apocrifos
	dia := 0
	for _, d := range p.PorDia {
		dia += d.OK + d.Revisar + d.Error
	}
	if dia != p.Total {
		t.Fatalf("el gráfico de 14 días suma %d y el total es %d", dia, p.Total)
	}
	r, err := st.Periodo(ctx, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), alcance)
	if err != nil {
		t.Fatal(err)
	}
	c.periodoLotes, c.periodoPend = r.Lotes, r.Pendientes
	for _, n := range r.PorEstado {
		c.periodoTotal += n
	}
	var pc []string
	for _, x := range r.PorCliente {
		pc = append(pc, fmt.Sprintf("%s=%d", x.Cliente, x.Total))
	}
	c.porCliente = strings.Join(pc, ",")
	rs, err := st.Rubros(ctx, alcance)
	if err != nil {
		t.Fatal(err)
	}
	c.rubros = len(rs)
	m, err := st.Estadisticas(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.metrica = m["comprobantes_ok"] + m["comprobantes_error"]
	return c
}

func TestLoBorradoNoSeVeEnNingunLado(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	// Cliente X: un documento con dos facturas (una de un apócrifo) y otro con una. Cliente Y: una.
	x := cargar(t, st, cliX, "telegram", "chat-x", map[string][]modelo.ResultadoCbte{
		"aaa": {cbteDe("30708888881", 1, false), cbteDe("20123456786", 2, true)},
		"bbb": {cbteDe("30708888881", 3, false)},
	}, "aaa", "bbb")
	y := cargar(t, st, cliY, "", "", map[string][]modelo.ResultadoCbte{"ccc": {cbteDe("30708888881", 9, false)}}, "ccc")
	if _, err := st.GuardarRubroCUIT(ctx, "30708888881", "combustible", "diego"); err != nil {
		t.Fatal(err)
	}

	antes := contar(t, st, nil)
	if antes.lotes != 2 || antes.cbtes != 4 || antes.cbtesRevisar != 1 || antes.panelTotal != 4 || antes.panelHoy != 4 || antes.panelApoc != 1 ||
		antes.periodoLotes != 2 || antes.periodoTotal != 4 || antes.periodoPend != 1 || antes.rubros != 1 || antes.metrica != 4 {
		t.Fatalf("antes de borrar: %+v", antes)
	}
	soloY := contar(t, st, []string{cliY})

	// --- Borrar sueltos: el apócrifo y la del documento bbb (que se queda sin ninguno y se va). ---
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][1], y.cbts[0][0]}, "ana@ejemplo.com", []string{cliX}); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("con alcance X no se puede borrar uno de Y (y no se borra ninguno): %v", err)
	}
	b, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][1], x.cbts[1][0], x.cbts[1][0]}, "ana@ejemplo.com", []string{cliX})
	if err != nil || b.Comprobantes != 2 || b.Documentos != 1 {
		t.Fatalf("borrar sueltos: %+v %v", b, err)
	}
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[1][0]}, "ana", nil); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("lo que ya está en la papelera no se vuelve a borrar: %v", err)
	}
	d := contar(t, st, nil)
	if d.lotes != 2 || d.cbtes != 2 || d.cbtesRevisar != 0 || d.panelTotal != 2 || d.panelHoy != 2 || d.panelApoc != 0 ||
		d.periodoTotal != 2 || d.periodoPend != 0 || d.metrica != 2 || d.porCliente != cliX+"=1,"+cliY+"=1" {
		t.Fatalf("después de borrar sueltos: %+v", d)
	}
	if _, err := st.Comprobante(ctx, x.cbts[0][1]); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("un comprobante en la papelera no se trae: %v", err)
	}
	if l, _ := st.ComprobantesDeDocumento(ctx, x.docs[0]); len(l) != 1 {
		t.Fatalf("comprobantes del documento: %d", len(l))
	}
	if _, err := st.Documento(ctx, x.docs[1]); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("el documento que se quedó sin comprobantes se va a la papelera: %v", err)
	}
	if _, err := st.Documento(ctx, x.docs[0]); err != nil {
		t.Fatalf("el que todavía tiene uno vivo, no: %v", err)
	}
	l, _ := st.Lote(ctx, x.lote)
	if l.Documentos != 1 || l.Comprobantes["error"] != 0 || l.Comprobantes["ok"] != 1 {
		t.Fatalf("los números del lote: %+v", l)
	}
	if sc, _ := st.SinConstatar(ctx, x.lote, nil, 100); len(sc) != 1 {
		t.Fatalf("sin constatar: %d", len(sc))
	}
	// Corregir algo que está en la papelera no se puede.
	cb := &store.Cbte{ID: x.cbts[1][0], Estado: modelo.EstadoAprobado}
	if err := st.ActualizarCbte(ctx, cb, []store.Correccion{{Campo: "total", Antes: "1", Despues: "2"}}, "diego"); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("actualizar lo borrado: %v", err)
	}
	// El rubro del emisor no les cambia a los de la papelera.
	if n, _ := st.GuardarRubroCUIT(ctx, "30708888881", "servicios", "diego"); n != 2 {
		t.Fatalf("el rubro pasó a %d comprobantes (los vivos son 2)", n)
	}
	if rs, _ := st.Rubros(ctx, []string{cliX}); len(rs) != 1 {
		t.Fatalf("rubros de X: %v", rs)
	}

	// --- Duplicados y repetidos: lo de la papelera no cuenta. ---
	err = pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		_, rep, err := st.CrearDocumento(ctx, tx, x.lote, "bbb-otra-vez.pdf", "application/pdf", 100, "bbb")
		if rep {
			return errors.New("el documento en la papelera no hace repetido al que se vuelve a subir")
		}
		if err != nil {
			return err
		}
		_, rep, err = st.CrearDocumento(ctx, tx, x.lote, "aaa-otra-vez.pdf", "application/pdf", 100, "aaa")
		if !rep {
			return errors.New("el que sigue vivo sí")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Una tercera carga de la factura 3 (la borrada) no sale duplicada; la 1 (viva) sí.
	z := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{"ddd": {cbteDe("30708888881", 3, false), cbteDe("30708888881", 1, false)}}, "ddd")
	c3, _ := st.Comprobante(ctx, z.cbts[0][0])
	c1, _ := st.Comprobante(ctx, z.cbts[0][1])
	if c3.DuplicadoDe != nil || c1.DuplicadoDe == nil || *c1.DuplicadoDe != x.cbts[0][0] {
		t.Fatalf("duplicados: la 3 %v, la 1 %v", c3.DuplicadoDe, c1.DuplicadoDe)
	}

	// --- Borrar el lote entero: se lleva lo vivo; lo borrado suelto sigue suelto. ---
	var bl *store.Borrado
	err = pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		if _, err := st.BorrarLote(ctx, tx, y.lote, "ana", []string{cliX}); !errors.Is(err, store.ErrNoEncontrado) {
			return fmt.Errorf("con alcance X no se borra un lote de Y: %v", err)
		}
		var err error
		bl, err = st.BorrarLote(ctx, tx, x.lote, "admin@ejemplo.com", nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// El lote x tenía: aaa (vivo, 1 comprobante), bbb (en la papelera) y los dos documentos que se
	// cargaron recién (bbb otra vez, pendiente, y aaa repetido).
	if bl.Documentos != 3 || bl.Comprobantes != 1 || len(bl.Pendientes) != 1 {
		t.Fatalf("borrar el lote: %+v", bl)
	}
	if _, err := st.Lote(ctx, x.lote); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("el lote en la papelera no se trae: %v", err)
	}
	if ok, _ := st.TomarDocumento(ctx, bl.Pendientes[0]); ok {
		t.Fatal("un documento en la papelera no se procesa")
	}
	if cerrado, _ := st.CerrarLoteSiTermino(ctx, x.lote); cerrado {
		t.Fatal("un lote en la papelera no se cierra")
	}
	if _, err := st.UltimoLoteDeChat(ctx, "telegram", "chat-x"); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("el «estado» de un chat no ve el lote borrado: %v", err)
	}
	err = pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		id, err := st.LoteAbiertoDeChat(ctx, tx, "telegram", "chat-x", time.Hour)
		if id != 0 {
			return fmt.Errorf("lo que llega por el chat no va a un lote de la papelera (%d)", id)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// Quedan y (1 comprobante) y z (2, uno de ellos duplicado de la factura 1, que se fue con x).
	d = contar(t, st, nil)
	if d.lotes != 2 || d.cbtes != 3 || d.panelTotal != 3 || d.periodoLotes != 2 || d.porCliente != cliX+"=2,"+cliY+"=1" {
		t.Fatalf("después de borrar el lote: %+v", d)
	}
	got := contar(t, st, []string{cliY})
	got.metrica, soloY.metrica = 0, 0 // las métricas son de toda la instalación
	if got != soloY {
		t.Fatalf("lo de Y no cambió: %+v y %+v", got, soloY)
	}
	// Si el documento pendiente termina de leerse ahora, no se guarda nada.
	doc := &store.Documento{ID: bl.Pendientes[0], LoteID: x.lote}
	if _, err := st.GuardarResultado(ctx, doc, &modelo.Resultado{Comprobantes: []modelo.ResultadoCbte{cbteDe("30708888881", 77, false)}}); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("guardar el resultado de un documento en la papelera: %v", err)
	}

	// --- La papelera ---
	items, total, err := st.Papelera(ctx, nil, time.Now(), 30, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(items) != 3 || items[0].Tipo != store.PapeleraLote || items[0].ID != x.lote || items[0].Documentos != 3 || items[0].Comprobantes != 1 ||
		items[0].BorradoPor != "admin@ejemplo.com" || items[0].Dias != 30 {
		t.Fatalf("papelera: %d %+v", total, items)
	}
	for _, it := range items[1:] {
		if it.Tipo != store.PapeleraComprobante || !it.LoteBorrado || it.BorradoPor != "ana@ejemplo.com" || it.Emisor == "" || it.Total != "121.00" {
			t.Fatalf("comprobante suelto en la papelera: %+v", it)
		}
	}
	if _, n, _ := st.Papelera(ctx, []string{cliY}, time.Now(), 30, 100, 0); n != 0 {
		t.Fatalf("Y no ve lo de X: %d", n)
	}
}

func TestVencimiento(t *testing.T) {
	b := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		ahora time.Time
		dias  int
	}{{b, 30}, {b.Add(29*24*time.Hour + time.Hour), 1}, {b.Add(30 * 24 * time.Hour), 0}, {b.Add(40 * 24 * time.Hour), 0}, {b.Add(time.Minute), 30}} {
		if _, d := store.Vencimiento(b, c.ahora, 30); d != c.dias {
			t.Errorf("a las %v: %d días, esperaba %d", c.ahora, d, c.dias)
		}
	}
}

func TestRestaurar(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	x := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{
		"aaa": {cbteDe("30708888881", 1, false), cbteDe("30708888881", 2, false)},
		"bbb": {cbteDe("30708888881", 3, false)},
	}, "aaa", "bbb")
	// Una corrección y una aprobación: vuelven tal cual.
	c, _ := st.Comprobante(ctx, x.cbts[1][0])
	c.Estado = modelo.EstadoAprobado
	c.Datos.Rubro = "servicios"
	if err := st.ActualizarCbte(ctx, c, []store.Correccion{{Campo: "rubro", Antes: "", Despues: "servicios", Usuario: "diego"}}, "diego"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[1][0]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][0]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error { _, err := st.BorrarLote(ctx, tx, x.lote, "diego", nil); return err })
	if err != nil {
		t.Fatal(err)
	}
	// Un comprobante suelto cuyo lote también está en la papelera: primero el lote.
	if _, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraComprobante, ID: x.cbts[1][0]}, nil); !errors.Is(err, store.ErrConflicto) || !strings.Contains(err.Error(), "restaurá primero el lote") {
		t.Fatalf("restaurar sin su lote: %v", err)
	}
	if _, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraLote, ID: x.lote}, []string{cliY}); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("fuera del alcance: %v", err)
	}
	r, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraLote, ID: x.lote}, nil)
	if err != nil || r.Documentos != 1 || r.Comprobantes != 1 {
		t.Fatalf("restaurar el lote: %+v %v", r, err)
	}
	// Vuelve el lote con lo que se fue con él: aaa con su comprobante 2. Lo borrado suelto sigue.
	l, err := st.Lote(ctx, x.lote)
	if err != nil || l.Documentos != 1 || l.Comprobantes["ok"] != 1 {
		t.Fatalf("lote restaurado: %+v %v", l, err)
	}
	if _, n, _ := st.Papelera(ctx, nil, time.Now(), 30, 10, 0); n != 2 {
		t.Fatalf("en la papelera quedan los dos sueltos: %d", n)
	}
	// El suelto del documento bbb (que se fue con él): vuelve con su documento, aprobado y corregido.
	if _, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraComprobante, ID: x.cbts[1][0]}, nil); err != nil {
		t.Fatal(err)
	}
	c, err = st.Comprobante(ctx, x.cbts[1][0])
	if err != nil || c.Estado != modelo.EstadoAprobado || c.Datos.Rubro != "servicios" || c.RevisadoPor == nil {
		t.Fatalf("restaurado como estaba: %+v %v", c, err)
	}
	if k, _ := st.Correcciones(ctx, c.ID); len(k) != 1 {
		t.Fatalf("las correcciones: %v", k)
	}
	if _, err := st.Documento(ctx, x.docs[1]); err != nil {
		t.Fatalf("su documento volvió: %v", err)
	}
	if _, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraComprobante, ID: x.cbts[1][0]}, nil); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("restaurar dos veces: %v", err)
	}

	// Conflicto de archivo: aaa se borra entero, se vuelve a subir (se procesa como nuevo) y
	// restaurar el viejo dejaría dos iguales.
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][1]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	nuevo := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{"aaa": {cbteDe("30708888881", 1, false)}}, "aaa")
	_, err = st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraComprobante, ID: x.cbts[0][1]}, nil)
	if !errors.Is(err, store.ErrConflicto) || !strings.Contains(err.Error(), "ya hay uno igual cargado después") || !strings.Contains(err.Error(), fmt.Sprintf("lote %d", nuevo.lote)) {
		t.Fatalf("restaurar con el mismo archivo cargado después: %v", err)
	}
	// Borrado el nuevo, el viejo vuelve.
	if _, err := st.BorrarComprobantes(ctx, nuevo.cbts[0], "diego", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Restaurar(ctx, store.RefPapelera{Tipo: store.PapeleraComprobante, ID: x.cbts[0][1]}, nil); err != nil {
		t.Fatalf("sin el nuevo, el viejo vuelve: %v", err)
	}
}

func TestPurgar(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	// El mismo archivo (compartido) en dos lotes: el segundo es repetido.
	x := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{
		"comp": {cbteDe("30708888881", 1, false)}, "solo": {cbteDe("30708888881", 2, false), cbteDe("30708888881", 3, false)},
	}, "comp", "solo")
	var rep int64
	err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error {
		l, err := st.CrearLote(ctx, tx, store.LoteNuevo{Nombre: "Otro", ClienteCUIT: cliY})
		if err != nil {
			return err
		}
		rep, _, err = st.CrearDocumento(ctx, tx, l, "comp.pdf", "", 100, "comp")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	k, _ := st.Comprobante(ctx, x.cbts[1][0])
	if err := st.ActualizarCbte(ctx, k, []store.Correccion{{Campo: "total", Antes: "1", Despues: "2"}}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[1][0]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	// Todavía no venció: no se borra nada.
	p, err := st.Purgar(ctx, store.FiltroPurga{Antes: time.Now().Add(-30 * 24 * time.Hour)})
	if err != nil || !p.Vacia() {
		t.Fatalf("sin vencer: %+v %v", p, err)
	}
	// Vencido: el suelto (con su corrección); el documento sigue (tiene otro vivo).
	p, err = st.Purgar(ctx, store.FiltroPurga{Antes: time.Now().Add(time.Minute)})
	if err != nil || p.Comprobantes != 1 || p.Documentos != 0 || p.Lotes != 0 {
		t.Fatalf("purgar el suelto: %+v %v", p, err)
	}
	var n int
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM correcciones`).Scan(&n)
	if n != 0 {
		t.Fatalf("las correcciones se van con el comprobante: %d", n)
	}
	// El lote entero: se va con todo, y los dos archivos quedan anotados para el almacén.
	err = pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error { _, err := st.BorrarLote(ctx, tx, x.lote, "diego", nil); return err })
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := st.Purgar(ctx, store.FiltroPurga{Todo: true, Alcance: []string{cliY}}); !p.Vacia() {
		t.Fatalf("vaciar con alcance Y no toca lo de X: %+v", p)
	}
	p, err = st.Purgar(ctx, store.FiltroPurga{Antes: time.Now().Add(time.Minute)})
	if err != nil || p.Lotes != 1 || p.Documentos != 2 || p.Comprobantes != 2 {
		t.Fatalf("purgar el lote: %+v %v", p, err)
	}
	shas, _ := st.PorBorrarDelAlmacen(ctx, 10)
	if strings.Join(shas, ",") != "comp,solo" && strings.Join(shas, ",") != "solo,comp" {
		t.Fatalf("archivos por borrar: %v", shas)
	}
	// comp lo sigue usando el repetido del otro lote (que ya no apunta a nada): no se borra.
	var repDe *int64
	_ = st.Pool.QueryRow(ctx, `SELECT repetido_de FROM documentos WHERE id=$1`, rep).Scan(&repDe)
	if repDe != nil {
		t.Fatalf("el repetido apuntaba al documento borrado: %v", *repDe)
	}
	borrados := map[string]bool{}
	for _, sha := range shas {
		ok, err := st.BorrarArchivoSiNadieLoUsa(ctx, sha, func(context.Context) error { borrados[sha] = true; return nil })
		if err != nil {
			t.Fatal(err)
		}
		if ok != borrados[sha] {
			t.Fatalf("%s: %v", sha, ok)
		}
	}
	if borrados["comp"] || !borrados["solo"] {
		t.Fatalf("se borra solo el que no usa nadie: %v", borrados)
	}
	if shas, _ := st.PorBorrarDelAlmacen(ctx, 10); len(shas) != 0 {
		t.Fatalf("quedaron anotados: %v", shas)
	}
	// Si borrar el archivo falla, queda anotado para la próxima.
	if _, err := st.Pool.Exec(ctx, `INSERT INTO almacen_por_borrar (sha256) VALUES ('roto')`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BorrarArchivoSiNadieLoUsa(ctx, "roto", func(context.Context) error { return errors.New("R2 no responde") }); err == nil {
		t.Fatal("tenía que fallar")
	}
	if shas, _ := st.PorBorrarDelAlmacen(ctx, 10); len(shas) != 1 {
		t.Fatalf("lo que falló sigue anotado: %v", shas)
	}
}

func TestPurgarDeAUnoYLotesQueQuedanVacios(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	x := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{"a": {cbteDe("30708888881", 1, false)}, "b": {cbteDe("30708888881", 2, false)}}, "a", "b")
	y := cargar(t, st, cliY, "", "", map[string][]modelo.ResultadoCbte{"c": {cbteDe("30708888881", 3, false)}}, "c")
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][0], x.cbts[1][0]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error { _, err := st.BorrarLote(ctx, tx, y.lote, "diego", nil); return err })
	if err != nil {
		t.Fatal(err)
	}
	// Borrar ya uno solo: ese comprobante y su documento.
	p, err := st.Purgar(ctx, store.FiltroPurga{Items: []store.RefPapelera{{Tipo: store.PapeleraComprobante, ID: x.cbts[0][0]}}})
	if err != nil || p.Comprobantes != 1 || p.Documentos != 1 || p.Lotes != 0 {
		t.Fatalf("borrar ya: %+v %v", p, err)
	}
	if _, n, _ := st.Papelera(ctx, nil, time.Now(), 30, 10, 0); n != 2 {
		t.Fatalf("en la papelera quedan dos: %d", n)
	}
	// Vaciar: lo demás, y el lote x, que se quedó sin documentos, también se va.
	p, err = st.Purgar(ctx, store.FiltroPurga{Todo: true})
	if err != nil || p.Comprobantes != 2 || p.Documentos != 2 || p.Lotes != 2 {
		t.Fatalf("vaciar: %+v %v", p, err)
	}
	var lotes int
	_ = st.Pool.QueryRow(ctx, `SELECT count(*) FROM lotes`).Scan(&lotes)
	if lotes != 0 {
		t.Fatalf("lotes: %d", lotes)
	}
	if _, err := st.Purgar(ctx, store.FiltroPurga{Items: []store.RefPapelera{{Tipo: "otro", ID: 1}}}); !errors.Is(err, store.ErrFiltro) {
		t.Fatalf("tipo raro: %v", err)
	}
}

func TestCambiarClienteLote(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	x := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{"a": {cbteDe("30708888881", 1, false), cbteDe("30708888881", 2, false)}}, "a")
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][1]}, "diego", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CambiarClienteLote(ctx, x.lote, cliY, []string{cliY}); !errors.Is(err, store.ErrNoEncontrado) {
		t.Fatalf("quien no ve el cliente de antes no lo cambia: %v", err)
	}
	c, err := st.CambiarClienteLote(ctx, x.lote, "30-71111111-1", nil)
	if err != nil || c.Antes != cliX || c.Despues != cliY || c.Comprobantes != 2 {
		t.Fatalf("cambiar: %+v %v", c, err)
	}
	if _, n, _ := st.Comprobantes(ctx, store.Filtro{Cliente: cliY}); n != 1 {
		t.Fatalf("los comprobantes vivos pasaron a Y: %d", n)
	}
	items, _, _ := st.Papelera(ctx, []string{cliY}, time.Now(), 30, 10, 0)
	if len(items) != 1 || items[0].ClienteCUIT != cliY {
		t.Fatalf("también el de la papelera: %+v", items)
	}
	if c, _ := st.CambiarClienteLote(ctx, x.lote, cliY, nil); c.Comprobantes != 0 {
		t.Fatalf("al mismo cliente no cambia nada: %+v", c)
	}
}
