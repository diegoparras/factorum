package arcanum

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/modelo"
)

func bptr(b bool) *bool { return &b }

func TestVeredictoTique(t *testing.T) {
	tique := func(e modelo.Estado) *modelo.ResultadoCbte {
		return &modelo.ResultadoCbte{Estado: e, Comprobante: modelo.Comprobante{Tipo: 83, EsTique: true, CUITEmisor: "30708888881"}}
	}
	apocOK := &Constatacion{Apoc: &Apoc{Consultado: true, EsApocrifo: bptr(false)}}
	apocMal := &Constatacion{Apoc: &Apoc{Consultado: true, EsApocrifo: bptr(true)}}
	casos := []struct {
		nombre string
		rc     *modelo.ResultadoCbte
		k      *Constatacion
		quiero string
	}{
		{"controlado", tique(modelo.EstadoRevisar), apocOK, "tique_controlado"},
		{"apócrifo", tique(modelo.EstadoRevisar), apocMal, "apocrifa"},
		{"apócrifo gana a los errores", tique(modelo.EstadoError), apocMal, "apocrifa"},
		{"errores locales", tique(modelo.EstadoError), apocOK, "con_errores"},
		{"sin Arcanum", tique(modelo.EstadoOK), nil, "no_verificable"},
		{"apócrifos sin respuesta", tique(modelo.EstadoOK), &Constatacion{Error: "Arcanum respondió 502"}, "no_verificable"},
		// Un tique 82 sin EsTique (datos viejos): igual se reconoce por el tipo.
		{"por tipo", &modelo.ResultadoCbte{Estado: modelo.EstadoOK, Comprobante: modelo.Comprobante{Tipo: 82}}, apocOK, "tique_controlado"},
		// Una factura con solo apócrifos (sin WSCDC) no es un tique controlado.
		{"factura", &modelo.ResultadoCbte{Estado: modelo.EstadoOK, Comprobante: modelo.Comprobante{Tipo: 6}}, apocOK, "no_verificable"},
	}
	for _, c := range casos {
		if v, msg := Veredicto(c.rc, c.k); v != c.quiero {
			t.Errorf("%s: %s (%s), quería %s", c.nombre, v, msg, c.quiero)
		}
	}
	if _, msg := Veredicto(tique(modelo.EstadoOK), apocOK); !strings.Contains(msg, "no se constatan en ARCA") {
		t.Errorf("mensaje: %s", msg)
	}
}

func TestExtraerPadronActividad(t *testing.T) {
	var d map[string]any
	_ = json.Unmarshal([]byte(`{
	  "datosGenerales": {"razonSocial": "ESTACION EL TREBOL SRL", "tipoPersona": "JURIDICA"},
	  "datosRegimenGeneral": {
	    "actividad": [
	      {"idActividad": "471120", "descripcionActividad": "VENTA AL POR MENOR EN SUPERMERCADOS", "orden": "2"},
	      {"idActividad": "473000", "descripcionActividad": "VENTA AL POR MENOR DE COMBUSTIBLE", "orden": "1"}
	    ],
	    "impuesto": {"idImpuesto": 30, "descripcionImpuesto": "IVA"}
	  }
	}`), &d)
	p := ExtraerPadron(d)
	if p.Razon != "ESTACION EL TREBOL SRL" || p.Actividad == nil || p.Actividad.Codigo != "473000" || p.Actividad.Orden != 1 {
		t.Fatalf("%+v %+v", p, p.Actividad)
	}
	// Monotributista: la actividad viene como objeto, con el número sin comillas.
	d = nil
	_ = json.Unmarshal([]byte(`{"datosGenerales": {"apellido": "GONZALEZ", "nombre": "ANA MARIA"},
	  "datosMonotributo": {"actividad": {"idActividad": 477311, "descripcionActividad": "VENTA AL POR MENOR DE PRODUCTOS FARMACEUTICOS", "orden": 1}}}`), &d)
	p = ExtraerPadron(d)
	if p.Razon != "GONZALEZ ANA MARIA" || p.Actividad == nil || p.Actividad.Codigo != "477311" {
		t.Fatalf("%+v %+v", p, p.Actividad)
	}
	if p := ExtraerPadron(map[string]any{"datosGenerales": map[string]any{"razonSocial": "X"}}); p.Actividad != nil {
		t.Fatal("sin actividades")
	}
}

// Arcanum falso: el 20123456786 es apócrifo, el 30711111111 da error de credenciales (código 201).
func arcanumFalso(t *testing.T, apoc, cdc *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/apoc/"):
			apoc.Add(1)
			if r.URL.Query().Get("cuit") != "20222222223" {
				t.Errorf("consultante %q", r.URL.Query().Get("cuit"))
			}
			cuit := strings.TrimPrefix(r.URL.Path, "/api/apoc/")
			res := map[string]any{"ok": true, "cuit": cuit, "esApocrifo": cuit == "20123456786", "codigo": nil}
			if cuit == "20123456786" {
				res["fechaCondicion"] = "2024-03-01"
			}
			if cuit == "30711111111" {
				res["codigo"], res["error"], res["descripcion"] = "201", "WSAPOC_CREDENCIALES_201", "Error al validar credenciales"
			}
			_ = json.NewEncoder(w).Encode(res)
		case r.URL.Path == "/api/constatacion":
			cdc.Add(1)
			var b struct {
				Items []map[string]any `json:"items"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			var res []map[string]any
			for range b.Items {
				res = append(res, map[string]any{"valido": true, "resultado": "A", "observaciones": []any{}, "apoc": map[string]any{"consultado": true, "esApocrifo": false}})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "constatacion": map[string]any{"resultados": res}})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestConstatarTodo(t *testing.T) {
	var apoc, cdc atomic.Int32
	srv := arcanumFalso(t, &apoc, &cdc)
	defer srv.Close()
	cl := Nuevo(srv.URL, "k", "20222222223", 5*time.Second)
	cbtes := []*modelo.Comprobante{
		{Tipo: 83, EsTique: true, CUITEmisor: "30708888881"},
		{Tipo: 1, CUITEmisor: "30711111111", PuntoVenta: 2, Numero: 1, Fecha: "2026-11-01", CAE: "76451234567890", Total: 100},
		{Tipo: 82, EsTique: true, CUITEmisor: "20123456786"},
		{Tipo: 111, EsTique: true, CUITEmisor: "30708888881"}, // mismo CUIT: no se vuelve a consultar
		{Tipo: 81, EsTique: true, CUITEmisor: "30711111111"},  // ARCA no validó las credenciales
		{Tipo: 83, EsTique: true, CUITEmisor: "123"},
	}
	ks, err := cl.ConstatarTodo(context.Background(), "", cbtes)
	if err != nil {
		t.Fatal(err)
	}
	if apoc.Load() != 3 || cdc.Load() != 1 {
		t.Fatalf("consultas a apócrifos %d, a WSCDC %d", apoc.Load(), cdc.Load())
	}
	if k := ks[0]; k.Valido != nil || k.Apoc == nil || !k.Apoc.Consultado || *k.Apoc.EsApocrifo || k.Fecha == "" {
		t.Fatalf("tique controlado: %+v", k)
	}
	if k := ks[1]; k.Valido == nil || !*k.Valido {
		t.Fatalf("factura constatada: %+v", k)
	}
	if k := ks[2]; k.Apoc == nil || !*k.Apoc.EsApocrifo || k.Apoc.FechaCondicion == nil || *k.Apoc.FechaCondicion != "2024-03-01" {
		t.Fatalf("apócrifo: %+v", k)
	}
	if ks[3].Apoc == nil || ks[3].Valido != nil {
		t.Fatalf("mismo CUIT: %+v", ks[3])
	}
	if k := ks[4]; k.Apoc != nil || !strings.Contains(k.Error, "credenciales") {
		t.Fatalf("un error de credenciales no es un 'no apócrifo': %+v", k)
	}
	if ks[5].Error == "" {
		t.Fatalf("CUIT inválido: %+v", ks[5])
	}
	// Veredictos de punta a punta.
	rc := &modelo.ResultadoCbte{Estado: modelo.EstadoRevisar, Comprobante: *cbtes[0]}
	if v, _ := Veredicto(rc, &ks[0]); v != "tique_controlado" {
		t.Fatal(v)
	}
	rc.Comprobante = *cbtes[2]
	if v, _ := Veredicto(rc, &ks[2]); v != "apocrifa" {
		t.Fatal(v)
	}

	// Sin conexión con Arcanum: error (la cola reintenta).
	srv.Close()
	if _, err := cl.ConstatarTodo(context.Background(), "", cbtes[:1]); err == nil {
		t.Fatal("sin Arcanum tenía que fallar")
	}
}

func TestMisRecibidos(t *testing.T) {
	var ruta, clave string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ruta, clave = r.URL.RequestURI(), r.Header.Get("X-API-Key")
		if strings.Contains(r.URL.Path, "20263630743") {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok":false,"error":"No autorizado para ese CUIT"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"cuit":"30711111118","desde":"2026-10-01","hasta":"2026-10-31","total":1,"tope":20000,"truncado":false,
			"cobertura":{"sincronizado":true,"cubre":false,"rangos":[{"desde":"2026-01-01","hasta":"2026-10-20"}],"faltan":[{"desde":"2026-10-21","hasta":"2026-10-31"}],
			"ultimaSincronizacion":{"corrida":4,"terminada":"2026-10-20T12:00:00.000Z","desde":"2026-01-01","hasta":"2026-10-20","cantidad":9},"ultimaCorrida":null,"enCurso":null,"credencial":true,"sincronizarDiaria":false},
			"comprobantes":[{"fecha":"2026-10-05","cbteTipo":1,"puntoVenta":3,"numeroDesde":7,"numeroHasta":7,"cae":"7612","docTipo":80,"cuitEmisor":"30500000001",
			"emisor":"Proveedor SA","moneda":"PES","tipoCambio":1,"netoGravado":1000.1,"noGravado":0,"exento":0,"iva":210.02,"total":1210.12,"emisorApocrifo":null}]}`))
	}))
	defer srv.Close()
	c := Nuevo(srv.URL, "ark_secreta", "", time.Second)
	r, err := c.MisRecibidos(context.Background(), "30-71111111-8", "2026-10-01", "2026-10-31")
	if err != nil {
		t.Fatal(err)
	}
	if ruta != "/api/mis-comprobantes/30711111118/recibidos?desde=2026-10-01&hasta=2026-10-31" || clave != "ark_secreta" {
		t.Fatalf("pedido: %s (%s)", ruta, clave)
	}
	if len(r.Comprobantes) != 1 || r.Comprobantes[0].Total != 1210.12 || r.Comprobantes[0].CUITEmisor != "30500000001" || r.Cobertura.Cubre ||
		len(r.Cobertura.Faltan) != 1 || r.Cobertura.UltimaSincronizacion.Hasta != "2026-10-20" {
		t.Fatalf("respuesta: %+v", r)
	}
	_, err = c.MisRecibidos(context.Background(), "20263630743", "2026-10-01", "2026-10-31")
	if st, msg, ok := ErrHTTP(err); !ok || st != 403 || !strings.Contains(msg, "No autorizado") {
		t.Fatalf("403: %v", err)
	}
	srv.Close()
	if _, err := c.MisRecibidos(context.Background(), "30711111118", "2026-10-01", "2026-10-31"); !EsErrConexion(err) {
		t.Fatalf("sin Arcanum tiene que ser un error de conexión: %v", err)
	}
}
