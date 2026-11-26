package pipeline

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/factorum/internal/extractor"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/rubro"
)

// Tickets de prueba (scripts/tickets.js): imagen limpia y foto degradada de cada uno. Necesita
// tesseract con el idioma español; sin él, se salta (en CI se instala).
func nuevoConOCR(t testing.TB) *Pipeline {
	t.Helper()
	p := nuevo(t)
	ocr := extractor.NuevoTesseract("spa", 60*time.Second)
	if ocr == nil {
		t.Skip("sin tesseract")
	}
	p.OCR = ocr
	return p
}

type ticketEsperado struct {
	tipo       int
	pv         int
	nro        int64
	fecha      string
	hora       string
	emisor     string
	razon      string
	receptor   string
	neto       modelo.Importe
	iva21      modelo.Importe
	iva105     modelo.Importe
	total      modelo.Importe
	rubro      string
	sumaCierra bool
}

var ticketsEsperados = map[string]ticketEsperado{
	"01_super_tfb": {tipo: 82, pv: 5, nro: 12345, fecha: "2026-11-21", hora: "13:45:10", emisor: "30712223339", razon: "SUPERMERCADOS LA ECONOMIA S.A.",
		neto: 1399774, iva21: 115500, iva105: 89226, total: 1604500, rubro: rubro.Supermercado, sumaCierra: true},
	"02_ypf_tique": {tipo: 83, pv: 3, nro: 45678, fecha: "2026-11-22", hora: "08:12:45", emisor: "30708888881", razon: "EL TREBOL S.R.L.",
		total: 4406250, rubro: rubro.Combustible},
	"03_farmacia_tfc": {tipo: 111, pv: 2, nro: 789, fecha: "2026-11-23", hora: "19:03:22", emisor: "27272727274", razon: "GONZALEZ ANA MARIA",
		total: 555000, rubro: rubro.Farmacia},
	"04_parrilla_tfa": {tipo: 81, pv: 4, nro: 1234, fecha: "2026-11-20", hora: "21:30:05", emisor: "30715554441", razon: "LA PARRILLA DE DON TITO S.R.L.",
		receptor: "20222222223", neto: 4000000, iva21: 840000, total: 4840000, rubro: rubro.Gastronomia, sumaCierra: true},
}

// En las fotos degradadas el OCR puede perder algún dato secundario; estos son los que tienen que
// salir siempre, en todas las versiones.
var camposObligatorios = []string{"tipo", "fecha", "cuit_emisor", "total", "rubro"}

func TestTicketsImagenes(t *testing.T) {
	p := nuevoConOCR(t)
	p.Ahora = func() time.Time { return time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC) }
	var nombres []string
	for n := range ticketsEsperados {
		nombres = append(nombres, n)
	}
	sort.Strings(nombres)
	for _, base := range nombres {
		for _, archivo := range []string{base + ".png", base + "_foto.jpg"} {
			t.Run(archivo, func(t *testing.T) {
				e := ticketsEsperados[base]
				r := p.Procesar(context.Background(), archivo, leer(t, filepath.Join("tickets", archivo)), Opciones{})
				if r.Error != "" || len(r.Comprobantes) != 1 {
					t.Fatalf("%+v", r)
				}
				rc := r.Comprobantes[0]
				c := rc.Comprobante
				if !c.EsTique {
					t.Fatalf("no se reconoció como tique: %+v", c)
				}
				// Lo que se encontró, para medir el OCR (go test -v).
				var ok, mal []string
				chk := func(campo string, got, want any) {
					if got == want {
						ok = append(ok, campo)
					} else {
						mal = append(mal, campo)
						for _, o := range camposObligatorios {
							if o == campo {
								t.Errorf("%s = %v, quería %v", campo, got, want)
							}
						}
					}
				}
				chk("tipo", c.Tipo, e.tipo)
				chk("punto_venta", c.PuntoVenta, e.pv)
				chk("numero", c.Numero, e.nro)
				chk("fecha", c.Fecha, e.fecha)
				chk("hora", c.Hora, e.hora)
				chk("cuit_emisor", c.CUITEmisor, e.emisor)
				chk("razon_social_emisor", c.RazonSocialEmisor, e.razon)
				if e.receptor != "" {
					chk("doc_nro_receptor", c.DocNroReceptor, e.receptor)
				}
				if e.neto != 0 {
					chk("neto_gravado", c.NetoGravado, e.neto)
				}
				if e.iva21 != 0 {
					chk("iva_21", c.IVA21, e.iva21)
				}
				if e.iva105 != 0 {
					chk("iva_10_5", c.IVA105, e.iva105)
				}
				chk("total", c.Total, e.total)
				chk("rubro", c.Rubro, e.rubro)
				t.Logf("%s: %d/%d campos bien; mal: %s; estado %s; extractores %v", archivo, len(ok), len(ok)+len(mal), strings.Join(mal, ", "), rc.Estado, r.Extractores)
				// Nunca se inventa: lo que no coincide no puede venir de un dato que no estaba.
				if c.Origen("total") != modelo.OrigenOCR || c.Origen("rubro") != modelo.OrigenOCR {
					t.Errorf("orígenes: %v", c.Origenes)
				}
				hall := map[string]modelo.Nivel{}
				for _, h := range rc.Hallazgos {
					hall[h.Codigo] = h.Nivel
				}
				if _, ok := hall["sin_cae"]; ok {
					t.Error("un tique no lleva CAE")
				}
				if hall["tique_sin_constatacion"] != modelo.NivelInfo {
					t.Errorf("falta el aviso de que los tiques no se constatan: %+v", rc.Hallazgos)
				}
				if e.sumaCierra && len(mal) == 0 {
					if _, ok := hall["suma_no_cierra"]; ok {
						t.Errorf("las sumas tenían que cerrar: %+v", rc.Hallazgos)
					}
				}
				// Lo que sale del OCR siempre lo revisa una persona.
				if rc.Estado == modelo.EstadoOK {
					t.Error("un ticket leído por OCR no puede quedar en ok")
				}
			})
		}
	}
}
