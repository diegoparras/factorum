package store_test

// El buscador de la cabecera contra Postgres real: número, CUIT, razón social, importe y lotes, con
// el alcance de quien busca, sin lo de la papelera, con el límite por grupo y sin que el texto que se
// escribe funcione como patrón.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

func cbteBuscar(emisor, razon string, pv int, numero int64, total modelo.Importe) modelo.ResultadoCbte {
	c := modelo.Comprobante{Tipo: 6, Letra: "B", PuntoVenta: pv, Numero: numero, Fecha: "2026-11-22", CUITEmisor: emisor,
		RazonSocialEmisor: razon, NetoGravado: total, Total: total}
	return modelo.ResultadoCbte{Comprobante: c, Estado: modelo.EstadoOK, Confianza: 1}
}

func TestBuscar(t *testing.T) {
	st := abrir(t)
	ctx := context.Background()
	const trebol, otro = "30708888881", "30711111111"
	x := cargar(t, st, cliX, "", "", map[string][]modelo.ResultadoCbte{
		"a": {cbteBuscar(trebol, "EL TREBOL S.R.L.", 3, 45678, 4406250)},
		"b": {cbteBuscar(trebol, "EL TREBOL S.R.L.", 3, 45690, 1830000)},
		"c": {cbteBuscar(otro, "100% NATURAL_SA", 1, 7, 99900)},
	}, "a", "b", "c")
	y := cargar(t, st, cliY, "", "", map[string][]modelo.ResultadoCbte{
		"d": {cbteBuscar(trebol, "EL TREBOL S.R.L.", 3, 45700, 500000)},
	}, "d")
	todos := func(q string) *store.ResultadoBusqueda {
		t.Helper()
		r, err := st.Buscar(ctx, store.Busqueda{Q: q})
		if err != nil {
			t.Fatalf("buscar %q: %v", q, err)
		}
		return r
	}

	// Razón social (sin distinguir mayúsculas), con el proveedor y cuántos comprobantes tiene.
	r := todos("trebol")
	if r.TotalComprobantes != 3 || len(r.Comprobantes) != 3 || len(r.Proveedores) != 1 || r.Proveedores[0].Comprobantes != 3 ||
		r.Proveedores[0].CUIT != trebol || r.Proveedores[0].RazonSocial != "EL TREBOL S.R.L." {
		t.Fatalf("trebol: %+v", r)
	}
	// Por punto de venta y número, con ceros o sin ellos; y por el número solo.
	for _, q := range []string{"0003-00045678", "3-45678", "45678"} {
		if r := todos(q); len(r.Comprobantes) != 1 || r.Comprobantes[0].ID != x.cbts[0][0] {
			t.Fatalf("%s: %+v", q, r.Comprobantes)
		}
	}
	// Por CUIT del emisor (con guiones o sin ellos, entero o el principio) y del cliente.
	for _, q := range []string{"30-70888888-1", "30708888881", "307088"} {
		if r := todos(q); r.TotalComprobantes != 3 || len(r.Proveedores) != 1 {
			t.Fatalf("%s: %+v", q, r)
		}
	}
	if r := todos("30-71111111-1"); r.TotalComprobantes != 2 || len(r.Lotes) != 1 || r.Lotes[0].ID != y.lote {
		t.Fatalf("CUIT del cliente Y (y el emisor del 100%%): %+v", r)
	}
	// Por importe, como se escribe en la pantalla o con punto decimal.
	for _, q := range []string{"44.062,50", "$ 44062,5", "44062.50"} {
		if r := todos(q); len(r.Comprobantes) != 1 || r.Comprobantes[0].ID != x.cbts[0][0] || r.Comprobantes[0].Total != "44062.50" {
			t.Fatalf("importe %s: %+v", q, r.Comprobantes)
		}
	}
	if r := todos("5.000"); len(r.Comprobantes) != 1 || r.Comprobantes[0].ID != y.cbts[0][0] {
		t.Fatalf("importe con punto de miles: %+v", r.Comprobantes)
	}
	// Lo que se escribe no es un patrón: % y _ se buscan tal cual.
	if r := todos("100%"); len(r.Comprobantes) != 1 || r.Comprobantes[0].RazonSocial != "100% NATURAL_SA" {
		t.Fatalf("100%%: %+v", r.Comprobantes)
	}
	if r := todos("L_SA"); len(r.Comprobantes) != 1 {
		t.Fatalf("L_SA: %+v", r.Comprobantes)
	}
	if r := todos("TR%OL"); len(r.Comprobantes) != 0 {
		t.Fatalf("%% no es comodín: %+v", r.Comprobantes)
	}
	// Lotes por nombre y por número.
	if r := todos("lote de 2022"); len(r.Lotes) != 1 || r.Lotes[0].ID != x.lote || r.Lotes[0].Comprobantes != 3 {
		t.Fatalf("lote por nombre: %+v", r.Lotes)
	}
	// Muy corto: nada.
	if r := todos("t"); len(r.Comprobantes)+len(r.Proveedores)+len(r.Lotes) != 0 {
		t.Fatalf("una letra: %+v", r)
	}

	// El alcance: quien ve solo al cliente X no ve lo de Y, ni en los comprobantes ni en la cuenta del proveedor.
	r, err := st.Buscar(ctx, store.Busqueda{Q: "trebol", Alcance: []string{cliX}})
	if err != nil {
		t.Fatal(err)
	}
	if r.TotalComprobantes != 2 || r.Proveedores[0].Comprobantes != 2 {
		t.Fatalf("alcance X: %+v", r)
	}
	if r, _ := st.Buscar(ctx, store.Busqueda{Q: "trebol", Alcance: []string{}}); len(r.Comprobantes)+len(r.Proveedores)+len(r.Lotes) != 0 {
		t.Fatalf("sin clientes no ve nada: %+v", r)
	}
	// El límite por grupo, con el total de todo lo que coincide.
	if r, _ := st.Buscar(ctx, store.Busqueda{Q: "trebol", Limite: 1}); len(r.Comprobantes) != 1 || r.TotalComprobantes != 3 {
		t.Fatalf("límite: %+v", r)
	}
	// Desde una fecha.
	if r, _ := st.Buscar(ctx, store.Busqueda{Q: "trebol", Desde: "2026-11-23"}); len(r.Comprobantes) != 0 {
		t.Fatalf("desde: %+v", r.Comprobantes)
	}

	// La papelera: un comprobante suelto y un lote entero dejan de aparecer.
	if _, err := st.BorrarComprobantes(ctx, []int64{x.cbts[0][0]}, "prueba", nil); err != nil {
		t.Fatal(err)
	}
	if err := pgx.BeginFunc(ctx, st.Pool, func(tx pgx.Tx) error { _, err := st.BorrarLote(ctx, tx, y.lote, "prueba", nil); return err }); err != nil {
		t.Fatal(err)
	}
	r = todos("trebol")
	if r.TotalComprobantes != 1 || r.Comprobantes[0].ID != x.cbts[1][0] || r.Proveedores[0].Comprobantes != 1 {
		t.Fatalf("con la papelera: %+v", r)
	}
	if r := todos("lote de 3071"); len(r.Lotes) != 0 {
		t.Fatalf("el lote borrado no aparece: %+v", r.Lotes)
	}
	if r := todos("45678"); len(r.Comprobantes) != 0 {
		t.Fatalf("el comprobante borrado no aparece: %+v", r.Comprobantes)
	}
}
