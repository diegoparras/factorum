package ajustes

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestBuscarAjustes(t *testing.T) {
	for _, s := range Secciones {
		if TituloSeccion[s] == "" || palabrasSeccion[s] == "" {
			t.Errorf("la sección %s no tiene título o palabras para el buscador", s)
		}
	}
	tiene := func(l []HallazgoAjuste, sec, clave string) bool {
		for _, h := range l {
			if h.Seccion == sec && h.Clave == clave {
				return true
			}
		}
		return false
	}
	// Por el título de la sección, por otra forma de nombrarla y sin tildes.
	if l := BuscarAjustes("copias", Secciones, 10); !tiene(l, "copias", "") || l[0].Clave != "" {
		t.Fatalf("copias: %+v", l)
	}
	if l := BuscarAjustes("smtp", Secciones, 10); !tiene(l, "correo_salida", "") || !tiene(l, "correo_salida", "smtp.host") {
		t.Fatalf("smtp: %+v", l)
	}
	if l := BuscarAjustes("vision", Secciones, 10); !tiene(l, "ia", "") {
		t.Fatalf("vision sin tilde: %+v", l)
	}
	// Por el título del campo y por su variable de entorno.
	if l := BuscarAjustes("url pública", Secciones, 10); !tiene(l, "general", "general.url_publica") || !tiene(l, "arcanum", "arcanum.url_publica") {
		t.Fatalf("url pública: %+v", l)
	}
	if l := BuscarAjustes("FACTORUM_COPIAS_HORA", Secciones, 10); !tiene(l, "copias", "copias.hora") {
		t.Fatalf("por la variable: %+v", l)
	}
	// Solo lo de las secciones que se ven (un admin, Clientes y canales), sin lo interno y con el límite.
	if l := BuscarAjustes("url", []string{"clientes"}, 10); len(l) != 0 {
		t.Fatalf("un admin no ve la URL pública: %+v", l)
	}
	if l := BuscarAjustes("remitentes", []string{"clientes"}, 10); !tiene(l, "clientes", "") || tiene(l, "correo_entrada", "") {
		t.Fatalf("admin, remitentes: %+v", l)
	}
	if l := BuscarAjustes("aviso de la ia", Secciones, 10); len(l) != 0 {
		t.Fatalf("lo interno no se busca: %+v", l)
	}
	if l := BuscarAjustes("a", Secciones, 10); len(l) != 0 {
		t.Fatalf("una letra: %+v", l)
	}
	if l := BuscarAjustes("correo", Secciones, 2); len(l) != 2 {
		t.Fatalf("límite: %+v", l)
	}
}

func TestErrorDeCampo(t *testing.T) {
	c, _ := CampoPorClave("copias.diarias")
	_, err := decodificar(c, json.RawMessage(`9999`))
	if !errors.Is(err, ErrInvalido) || CampoDelError(err) != "copias.diarias" {
		t.Fatalf("error de un campo: %v %q", err, CampoDelError(err))
	}
	c, _ = CampoPorClave("avisos.lote_a")
	if _, err := decodificar(c, json.RawMessage(`"no-es-mail"`)); CampoDelError(err) != "avisos.lote_a" {
		t.Fatalf("emails: %q", CampoDelError(err))
	}
	if CampoDelError(errors.New("otro")) != "" {
		t.Fatal("un error cualquiera no es de un campo")
	}
}
