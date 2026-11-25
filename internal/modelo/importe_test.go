package modelo

import "testing"

func TestParsearImporte(t *testing.T) {
	casos := map[string]Importe{
		"1234.5":         123450,
		"1234,50":        123450,
		"1.234,50":       123450,
		"1,234.50":       123450,
		"$ 3570000,00":   357000000,
		"3570000":        357000000,
		"-12,3":          -1230,
		"(100,00)":       -10000,
		"1.234.567,89":   123456789,
		"0,00":           0,
		"238345.50":      23834550,
		"1.000":          100000, // miles
		"10,5":           1050,
		"1180.5":         118050,
		"12.345,678":     1234568, // tres decimales con miles: redondea
		" 1.500,00": 150000,
	}
	for in, want := range casos {
		got, err := ParsearImporte(in)
		if err != nil {
			t.Errorf("%q: error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %d, quería %d", in, got, want)
		}
	}
	for _, malo := range []string{"", "abc", "$"} {
		if _, err := ParsearImporte(malo); err == nil {
			t.Errorf("%q debería dar error", malo)
		}
	}
}

func TestImporteFormatos(t *testing.T) {
	i := Importe(123456789)
	if i.String() != "1234567.89" || i.AR() != "1.234.567,89" {
		t.Fatalf("formatos: %s %s", i.String(), i.AR())
	}
	if Importe(-5).String() != "-0.05" || Importe(-123456).AR() != "-1.234,56" {
		t.Fatalf("negativos: %s %s", Importe(-5).String(), Importe(-123456).AR())
	}
	b, _ := Importe(100).MarshalJSON()
	if string(b) != "1.00" {
		t.Fatalf("json: %s", b)
	}
	var j Importe
	if err := j.UnmarshalJSON([]byte(`"1.234,56"`)); err != nil || j != 123456 {
		t.Fatalf("unmarshal: %v %d", err, j)
	}
}
