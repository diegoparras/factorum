package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/diegoparras/factorum/internal/arcanum"
	"github.com/diegoparras/factorum/internal/modelo"
	"github.com/diegoparras/factorum/internal/store"
)

func TestTextosDeTickets(t *testing.T) {
	c := modelo.Comprobante{Tipo: 83, EsTique: true, PuntoVenta: 3, Numero: 45678, Fecha: "2026-11-22", Hora: "08:12:45",
		CUITEmisor: "30708888881", RazonSocialEmisor: "EL TREBOL S.R.L.", Rubro: "combustible", Total: 4406250}
	v := &Verificacion{Veredicto: "tique_controlado", Mensaje: "Ticket controlado.",
		Resultado:    modelo.Resultado{Comprobantes: []modelo.ResultadoCbte{{Comprobante: c}}},
		Comprobantes: []VeredictoComprobante{{Veredicto: "tique_controlado"}}}
	s := TextoVerificacion("ypf.jpg", v)
	for _, want := range []string{"ypf.jpg: TICKET CONTROLADO.", "Tique 00003-00045678", "22/11/2026 08:12:45", "$ 44.062,50", "combustible"} {
		if !strings.Contains(s, want) {
			t.Errorf("falta %q en:\n%s", want, s)
		}
	}
	r := TextoResumen(&store.Lote{}, []store.Cbte{{Estado: modelo.EstadoRevisar, Datos: c}})
	if !strings.Contains(r, "Tique 00003-00045678, EL TREBOL S.R.L., $ 44.062,50 (combustible): revisar") {
		t.Errorf("resumen:\n%s", r)
	}
	if gravedad["valida"] >= gravedad["tique_controlado"] || gravedad["tique_controlado"] >= gravedad["no_verificable"] {
		t.Fatal("tique_controlado va entre valida y no_verificable")
	}
}

func TestConstatacionDeTique(t *testing.T) {
	no := false
	k := &arcanum.Constatacion{Apoc: &arcanum.Apoc{Consultado: true, EsApocrifo: &no}}
	h := hallazgosConstatacion(k)
	if len(h) != 1 || h[0].Codigo != "apoc_controlado" || h[0].Nivel != modelo.NivelInfo || !codigosConstatacion["apoc_controlado"] {
		t.Fatalf("%+v", h)
	}
	b, _ := json.Marshal(k)
	if estadoConstatacion(b) != "controlado" {
		t.Fatal(estadoConstatacion(b))
	}
}
