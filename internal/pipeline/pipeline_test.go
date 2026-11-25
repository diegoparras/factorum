package pipeline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/pdfx"
)

func nuevo(t testing.TB) *Pipeline {
	t.Helper()
	m := pdfx.NuevoPoppler(30 * time.Second)
	if err := m.Disponible(); err != nil {
		t.Skip("sin poppler: ", err)
	}
	p := Nuevo(m, nil, nil, 2, 1)
	p.Ahora = func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }
	return p
}

func leer(t testing.TB, nombre string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", nombre))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type esperado struct {
	tipo      int
	pv        int
	nro       int64
	fecha     string
	emisor    string
	razon     string
	receptor  string
	neto      modelo.Importe
	iva21     modelo.Importe
	iva105    modelo.Importe
	otros     modelo.Importe
	subtotal  modelo.Importe
	total     modelo.Importe
	cae       string
	estado    modelo.Estado
	hallazgos []string
	paginas   []int
}

func TestFixtures(t *testing.T) {
	p := nuevo(t)
	casos := map[string][]esperado{
		"01_FA_21.pdf": {{tipo: 1, pv: 2, nro: 1543, fecha: "2026-11-03", emisor: "30711111111", razon: "DISTRIBUIDORA EJEMPLO S.R.L.", receptor: "GOMEZ MARIA LAURA",
			neto: 10000000, iva21: 2100000, total: 12100000, cae: "76451234567890", estado: modelo.EstadoOK, paginas: []int{1, 2, 3}}},
		"02_FA_mixta.pdf": {{tipo: 1, pv: 2, nro: 1544, fecha: "2026-11-05", emisor: "30711111111", neto: 20000000, iva21: 2520000, iva105: 664550, otros: 650000,
			total: 23834550, estado: modelo.EstadoOK}},
		"03_FC_servicios.pdf": {{tipo: 11, pv: 5, nro: 91, emisor: "20123456786", razon: "PEREZ JUAN CARLOS", receptor: "DISTRIBUIDORA EJEMPLO S.R.L.",
			subtotal: 11210000, total: 11210000, estado: modelo.EstadoOK}},
		"04_FB_consumidor.pdf": {{tipo: 6, pv: 3, nro: 20077, subtotal: 6050000, total: 6050000, estado: modelo.EstadoOK}},
		"05_NCA.pdf":           {{tipo: 3, pv: 2, nro: 88, neto: 1000000, iva21: 210000, total: 1210000, estado: modelo.EstadoOK}},
		"06_FA_no_cierra.pdf":  {{tipo: 1, pv: 5, nro: 91, total: 11210000, estado: modelo.EstadoError, hallazgos: []string{"suma_no_cierra"}}},
		"07_FA_dolares.pdf":    {{tipo: 1, total: 121000, estado: modelo.EstadoRevisar, hallazgos: []string{"moneda_extranjera"}}},
		"08_dos_facturas.pdf": {
			{tipo: 11, pv: 5, nro: 91, total: 11210000, estado: modelo.EstadoOK, paginas: []int{1}},
			{tipo: 3, pv: 2, nro: 88, total: 1210000, estado: modelo.EstadoOK, paginas: []int{2}},
		},
		"09_FA_adulterada.pdf": {{tipo: 1, nro: 1546, total: 12100000, estado: modelo.EstadoError, hallazgos: []string{"qr_distinto_texto"}}},
	}
	for nombre, esp := range casos {
		t.Run(nombre, func(t *testing.T) {
			r := p.Procesar(context.Background(), nombre, leer(t, filepath.Join("pdf", nombre)), Opciones{})
			if r.Error != "" {
				t.Fatal(r.Error)
			}
			if len(r.Comprobantes) != len(esp) {
				t.Fatalf("%d comprobantes, quería %d", len(r.Comprobantes), len(esp))
			}
			if r.Extractores[0] != "qr" {
				t.Errorf("el QR tiene que ser la primera fuente: %v", r.Extractores)
			}
			for i, e := range esp {
				rc := r.Comprobantes[i]
				c := rc.Comprobante
				chk := func(campo string, got, want any, cero any) {
					if want != cero && got != want {
						t.Errorf("%s = %v, quería %v", campo, got, want)
					}
				}
				chk("tipo", c.Tipo, e.tipo, 0)
				chk("pv", c.PuntoVenta, e.pv, 0)
				chk("nro", c.Numero, e.nro, int64(0))
				chk("fecha", c.Fecha, e.fecha, "")
				chk("emisor", c.CUITEmisor, e.emisor, "")
				chk("razon", c.RazonSocialEmisor, e.razon, "")
				chk("receptor", c.RazonSocialReceptor, e.receptor, "")
				chk("neto", c.NetoGravado, e.neto, modelo.Importe(0))
				chk("iva21", c.IVA21, e.iva21, modelo.Importe(0))
				chk("iva105", c.IVA105, e.iva105, modelo.Importe(0))
				chk("otros", c.OtrosTributos, e.otros, modelo.Importe(0))
				chk("subtotal", c.Subtotal, e.subtotal, modelo.Importe(0))
				chk("total", c.Total, e.total, modelo.Importe(0))
				chk("cae", c.CAE, e.cae, "")
				if rc.Estado != e.estado {
					t.Errorf("estado %s, quería %s: %+v", rc.Estado, e.estado, rc.Hallazgos)
				}
				for _, cod := range e.hallazgos {
					ok := false
					for _, h := range rc.Hallazgos {
						ok = ok || h.Codigo == cod
					}
					if !ok {
						t.Errorf("faltó el hallazgo %s: %+v", cod, rc.Hallazgos)
					}
				}
				if e.paginas != nil && len(c.Paginas) != len(e.paginas) {
					t.Errorf("páginas %v, quería %v", c.Paginas, e.paginas)
				}
				if c.Origen("total") != modelo.OrigenQR || c.Origen("cuit_emisor") != modelo.OrigenQR {
					t.Errorf("total y CUIT tienen que salir del QR: %v", c.Origenes)
				}
			}
		})
	}
}

func TestImagenConQR(t *testing.T) {
	p := nuevo(t)
	r := p.Procesar(context.Background(), "foto.png", leer(t, "foto_fb.png"), Opciones{})
	if r.Error != "" || len(r.Comprobantes) != 1 {
		t.Fatalf("%+v", r)
	}
	c := r.Comprobantes[0].Comprobante
	if c.Tipo != 6 || c.Numero != 20077 || c.Total != 6050000 || c.Origen("total") != modelo.OrigenQR {
		t.Fatalf("%+v", c)
	}
	// Sin OCR ni IA solo están los datos del QR: falta la razón social, hay que revisarla.
	if r.Comprobantes[0].Estado != modelo.EstadoRevisar {
		t.Fatalf("estado %s", r.Comprobantes[0].Estado)
	}
}

func TestNoSoportado(t *testing.T) {
	p := nuevo(t)
	r := p.Procesar(context.Background(), "x.txt", []byte("hola, esto no es una factura"), Opciones{})
	if r.Error == "" {
		t.Fatal("tenía que fallar")
	}
	r = p.Procesar(context.Background(), "roto.pdf", []byte("%PDF-1.4 roto"), Opciones{})
	if r.Error == "" {
		t.Fatal("un PDF roto tenía que fallar sin pánico")
	}
}

// El modelo de visión solo completa lo que falta y nunca pisa al QR.
func TestVisionCompletaSinPisar(t *testing.T) {
	p := nuevo(t)
	var mu sync.Mutex
	llamadas := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		llamadas++
		mu.Unlock()
		var cuerpo map[string]any
		_ = json.NewDecoder(r.Body).Decode(&cuerpo)
		if cuerpo["response_format"] == nil {
			t.Error("falta response_format")
		}
		contenido := `{"clase":"factura","letra":"B","codigo_tipo":6,"punto_venta":3,"numero":99999,"fecha":"2026-11-20",
		  "cuit_emisor":"30711111111","razon_social_emisor":"DISTRIBUIDORA EJEMPLO S.R.L.","condicion_iva_emisor":"Responsable Inscripto",
		  "cuit_receptor":null,"razon_social_receptor":null,"neto_gravado":null,"no_gravado":null,"exento":null,"iva_27":null,"iva_21":null,
		  "iva_10_5":null,"iva_5":null,"iva_2_5":null,"otros_tributos":0,"subtotal":60500,"total":1,"moneda":"PES","cae":null,"vto_cae":"2026-11-30"}`
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": contenido}}}})
	}))
	defer srv.Close()
	p.Vision = &extractor.Vision{BaseURL: srv.URL, Modelo: "prueba"}
	r := p.Procesar(context.Background(), "foto.png", leer(t, "foto_fb.png"), Opciones{IA: true})
	if llamadas != 1 {
		t.Fatalf("llamadas al modelo: %d", llamadas)
	}
	c := r.Comprobantes[0].Comprobante
	if c.Numero != 20077 || c.Total != 6050000 {
		t.Fatalf("el modelo pisó datos del QR: %+v", c)
	}
	if c.RazonSocialEmisor != "DISTRIBUIDORA EJEMPLO S.R.L." || c.Origen("razon_social_emisor") != modelo.OrigenVision {
		t.Fatalf("el modelo no completó la razón social: %+v", c)
	}
	if r.Comprobantes[0].Estado != modelo.EstadoRevisar {
		t.Fatal("lo que aporta la IA se revisa")
	}

	// Con un PDF de ARCA completo, el modelo no se llama.
	llamadas = 0
	r = p.Procesar(context.Background(), "a.pdf", leer(t, "pdf/01_FA_21.pdf"), Opciones{IA: true})
	if llamadas != 0 || r.Comprobantes[0].Estado != modelo.EstadoOK {
		t.Fatalf("no hacía falta la IA: llamadas=%d estado=%s", llamadas, r.Comprobantes[0].Estado)
	}
}

func BenchmarkPDFARCA(b *testing.B) {
	p := nuevo(b)
	datos := leer(b, "pdf/02_FA_mixta.pdf")
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if r := p.Procesar(context.Background(), "x.pdf", datos, Opciones{}); r.Error != "" {
				b.Fatal(r.Error)
			}
		}
	})
}
