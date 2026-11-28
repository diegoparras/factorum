package store

import "testing"

func TestImporteCentavos(t *testing.T) {
	casos := map[string]int64{"44.062,50": 4406250, "$ 44.062,50": 4406250, "44062,5": 4406250, "44062.50": 4406250,
		"44.062": 4406200, "1.234.567": 123456700, "100": 10000, "0,99": 99}
	for q, want := range casos {
		if got, ok := importeCentavos(q); !ok || got != want {
			t.Errorf("%q: %d %v, quería %d", q, got, ok, want)
		}
	}
	for _, q := range []string{"", "abc", "1,2,3", "12,345", "30-70888888-1", "$", "1.2.3,456"} {
		if got, ok := importeCentavos(q); ok {
			t.Errorf("%q no es un importe y dio %d", q, got)
		}
	}
	if patronLike(`100%_\`) != `100\%\_\\` {
		t.Errorf("patronLike: %q", patronLike(`100%_\`))
	}
}
