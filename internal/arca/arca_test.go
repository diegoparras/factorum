package arca

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestCUIT(t *testing.T) {
	for _, ok := range []string{"30711111111", "20-22222222-3", "20123456786", "30-70000001-6"} {
		if !CUITValido(ok) {
			t.Errorf("%s debería ser válido", ok)
		}
	}
	for _, malo := range []string{"30711111117", "2022222222", "99222222223", "", "abc"} {
		if CUITValido(malo) {
			t.Errorf("%s debería ser inválido", malo)
		}
	}
	if FormatearCUIT("30711111111") != "30-71111111-1" {
		t.Fatal(FormatearCUIT("30711111111"))
	}
}

func TestTipos(t *testing.T) {
	if tp, ok := Tipo(1); !ok || tp.Letra != "A" || tp.Clase != ClaseFactura {
		t.Fatal("tipo 1")
	}
	if TipoDesde(ClaseNotaCredito, "B", false) != 8 || TipoDesde(ClaseFactura, "A", true) != 201 {
		t.Fatal("TipoDesde")
	}
	if TipoDesde(ClaseFactura, "Z", false) != 0 {
		t.Fatal("letra inexistente")
	}
}

func TestParsearQR(t *testing.T) {
	j := `{"ver":1,"fecha":"2026-11-03","cuit":30711111111,"ptoVta":2,"tipoCmp":1,"nroCmp":1543,"importe":121000,"moneda":"PES","ctz":1,"tipoDocRec":80,"nroDocRec":20222222223,"tipoCodAut":"E","codAut":76451234567890}`
	std := base64.StdEncoding.EncodeToString([]byte(j))
	casos := []string{
		"https://www.afip.gob.ar/fe/qr/?p=" + std,
		"https://www.arca.gob.ar/fe/qr/?p=" + strings.TrimRight(std, "="),
		"https://www.afip.gob.ar/fe/qr/?p=" + base64.URLEncoding.EncodeToString([]byte(j)),
		"https://www.afip.gob.ar/fe/qr/?p=" + strings.ReplaceAll(std, "+", " "),
		"  https://www.afip.gob.ar/fe/qr/?p=" + std + "\n",
	}
	for _, c := range casos {
		q, err := ParsearQR(c)
		if err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		if q.Cuit != "30711111111" || q.PtoVta != 2 || q.TipoCmp != 1 || q.NroCmp != 1543 || q.Importe != 121000 ||
			q.Fecha != "2026-11-03" || q.NroDocRec != "20222222223" || q.CodAut != "76451234567890" {
			t.Fatalf("datos: %+v", q)
		}
	}
}

func TestParsearQRTolerante(t *testing.T) {
	// Números como texto, fecha AAAAMMDD, importe con coma, sin moneda ni cotización.
	j := `{"ver":"1","fecha":"20261103","cuit":"30-71111111-1","ptoVta":"0002","tipoCmp":"6","nroCmp":"20077","importe":"60500,50","tipoDocRec":99,"nroDocRec":0,"codAut":"76451111122223"}`
	q, err := ParsearQR("https://www.afip.gob.ar/fe/qr/?p=" + base64.StdEncoding.EncodeToString([]byte(j)))
	if err != nil {
		t.Fatal(err)
	}
	if q.Fecha != "2026-11-03" || q.Cuit != "30711111111" || q.PtoVta != 2 || q.Importe != 60500.5 || q.Moneda != "PES" || q.Ctz != 1 || q.TipoCodAut != "E" {
		t.Fatalf("%+v", q)
	}
}

func TestParsearQRRechaza(t *testing.T) {
	for _, s := range []string{
		"https://example.com/?p=e30=",
		"no es una url",
		"https://www.afip.gob.ar/fe/qr/",
		"https://www.afip.gob.ar/fe/qr/?p=" + base64.StdEncoding.EncodeToString([]byte(`{"ver":1}`)),
		"https://www.afip.gob.ar/fe/qr/?p=%%%",
	} {
		if _, err := ParsearQR(s); err == nil {
			t.Errorf("%q debería fallar", s)
		}
	}
}

func TestQRURLIdaYVuelta(t *testing.T) {
	q := &QR{Ver: 1, Fecha: "2026-11-03", Cuit: "30711111111", PtoVta: 2, TipoCmp: 1, NroCmp: 1543, Importe: 121000.5, Moneda: "PES", Ctz: 1, TipoDocRec: 80, NroDocRec: "20222222223", TipoCodAut: "E", CodAut: "76451234567890"}
	r, err := ParsearQR(q.URL())
	if err != nil || *r != *q {
		t.Fatalf("%v\n%+v\n%+v", err, r, q)
	}
}
