package api_test

// El buscador de la cabecera por la API: cada rol ve lo suyo (los comprobantes de sus clientes, los
// ajustes solo quien configura), lo de la papelera no aparece y un error de validación de la
// configuración dice de qué campo es.

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/diegoparras/factorum/internal/pruebas"
)

func TestBuscador(t *testing.T) {
	e := armarOpc(t, opcEntorno{url: pruebas.BaseNueva(t)})
	ctx := context.Background()
	sup := e.crearSuperadmin(t)
	adm, _ := e.altaUsuario(t, sup, "admin@ejemplo.com", "admin", nil)
	ana, _ := e.altaUsuario(t, sup, "ana@ejemplo.com", "usuario", []string{cuitX})
	lector, _ := e.altaUsuario(t, sup, "lector@ejemplo.com", "lectura", nil)
	sem := e.sembrar(t, cuitX, cuitY)
	if _, err := e.app.Store.Pool.Exec(ctx, `UPDATE comprobantes SET razon_social_emisor='EL TREBOL S.R.L.'`); err != nil {
		t.Fatal(err)
	}

	buscar := func(n *navegador, q string) map[string]any {
		t.Helper()
		st, m := n.pedir("GET", "/api/buscar?q="+url.QueryEscape(q), nil)
		if st != 200 {
			t.Fatalf("buscar %q: %d %v", q, st, m)
		}
		return m
	}
	largo := func(m map[string]any, grupo string) int { l, _ := m[grupo].([]any); return len(l) }
	tieneAjuste := func(m map[string]any, sec string) bool {
		for _, x := range m["ajustes"].([]any) {
			if x.(map[string]any)["seccion"] == sec {
				return true
			}
		}
		return false
	}

	// Comprobantes y proveedores: el superadmin y el lector ven todo; ana, solo lo de su cliente.
	for _, n := range []*navegador{sup, adm, lector} {
		if m := buscar(n, "trebol"); largo(m, "comprobantes") != 2 || m["total_comprobantes"] != 2.0 || largo(m, "proveedores") != 1 {
			t.Fatalf("todos los clientes: %v", m)
		}
	}
	m := buscar(ana, "trebol")
	if largo(m, "comprobantes") != 1 || m["comprobantes"].([]any)[0].(map[string]any)["id"] != float64(sem[cuitX][2]) ||
		m["proveedores"].([]any)[0].(map[string]any)["comprobantes"] != 1.0 {
		t.Fatalf("ana solo ve lo de X: %v", m)
	}
	if m := buscar(ana, "20-33333333-4"); largo(m, "lotes") != 0 {
		t.Fatalf("un CUIT que no es de ana: %v", m)
	}
	// Una clave de API busca con su alcance, pero nunca ajustes.
	st, mk := e.pedirJSON(t, "GET", "/api/buscar?q=copias", claveLect, nil)
	if st != 200 || largo(mk, "ajustes") != 0 {
		t.Fatalf("clave de API: %d %v", st, mk)
	}
	if st, _ := e.navegador(t).pedir("GET", "/api/buscar?q=trebol", nil); st != 401 {
		t.Fatalf("sin sesión: %d", st)
	}
	if st, _ := sup.pedir("GET", "/api/buscar?q=trebol&desde=ayer", nil); st != 400 {
		t.Fatalf("desde inválido: %d", st)
	}

	// Ajustes: el superadmin, de todas las secciones; un admin, solo Clientes y canales; los demás, nada.
	if m := buscar(sup, "copias"); !tieneAjuste(m, "copias") {
		t.Fatalf("superadmin, copias: %v", m["ajustes"])
	}
	if m := buscar(adm, "copias"); tieneAjuste(m, "copias") {
		t.Fatalf("un admin no configura las copias: %v", m["ajustes"])
	}
	if m := buscar(adm, "clientes y canales"); !tieneAjuste(m, "clientes") {
		t.Fatalf("un admin, clientes: %v", m["ajustes"])
	}
	for _, n := range []*navegador{ana, lector} {
		if m := buscar(n, "clientes y canales"); largo(m, "ajustes") != 0 {
			t.Fatalf("sin configuración no hay ajustes: %v", m["ajustes"])
		}
	}

	// La papelera: lo borrado no aparece.
	if st, m := sup.pedir("POST", "/api/comprobantes/borrar", map[string]any{"ids": []int64{sem[cuitY][2]}}); st != 200 {
		t.Fatalf("borrar: %d %v", st, m)
	}
	if m := buscar(sup, "trebol"); largo(m, "comprobantes") != 1 || m["proveedores"].([]any)[0].(map[string]any)["comprobantes"] != 1.0 {
		t.Fatalf("con la papelera: %v", m)
	}
	if st, _ := sup.pedir("DELETE", fmt.Sprintf("/api/lotes/%d", sem[cuitX][0]), nil); st != 200 {
		t.Fatalf("borrar el lote: %d", st)
	}
	if m := buscar(sup, "lote 20222"); largo(m, "lotes")+largo(m, "comprobantes") != 0 {
		t.Fatalf("el lote borrado no aparece: %v", m)
	}

	// Un valor inválido en la configuración dice de qué campo es (la pantalla lo marca).
	st, mc := sup.pedir("PUT", "/api/configuracion", map[string]any{"cambios": map[string]any{"general.nombre": "Estudio", "copias.diarias": 9999}})
	if st != 400 || mc["campo"] != "copias.diarias" {
		t.Fatalf("error de un campo: %d %v", st, mc)
	}
	if e.app.Config().Nombre == "Estudio" {
		t.Fatal("con un error no se guarda nada")
	}
}
