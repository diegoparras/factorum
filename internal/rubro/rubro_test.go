package rubro

import "testing"

func TestDesdeTexto(t *testing.T) {
	casos := []struct {
		enc, cuerpo, quiero string
	}{
		{"YPF ESTACION DE SERVICIO EL TREBOL S.R.L.", "NAFTA SUPER 35,250 L", Combustible},
		{"ESTACIÓN DE SERVICIO LA RUTA", "", Combustible},
		{"SHELL", "", Combustible},
		{"AXION energy", "", Combustible},
		{"Puma Energy", "", Combustible},
		{"EL TREBOL SRL", "INFINIA DIESEL 40 L", Combustible},
		{"FARMACIA DEL PUEBLO", "IBUPROFENO 400", Farmacia},
		{"Farmacity", "", Farmacia},
		{"SUPERMERCADOS LA ECONOMIA S.A.", "CAFE MOLIDO 500G", Supermercado}, // el café del cuerpo no le gana al encabezado
		{"COTO CICSA", "", Supermercado},
		{"AUTOSERVICIO EL SOL", "", Supermercado},
		{"LA PARRILLA DE DON TITO S.R.L.", "BIFE DE CHORIZO", Gastronomia},
		{"BAR NOTABLE", "", Gastronomia},
		{"Pizzería Güerrín", "", Gastronomia},
		{"AUSA", "PEAJE AU 25 DE MAYO", Transporte},
		{"ESTACIONAMIENTO CENTRO", "", Transporte},
		{"LAVADERO LA ESPUMA", "", Servicios},
		{"BARRACA SAN JUAN", "", ""}, // BAR no aparece dentro de BARRACA
		{"COMERCIAL XYZ S.A.", "ARTICULO 1", ""},
	}
	for _, c := range casos {
		if got := DesdeTexto(c.enc, c.cuerpo); got != c.quiero {
			t.Errorf("DesdeTexto(%q, %q) = %q, quería %q", c.enc, c.cuerpo, got, c.quiero)
		}
	}
}

func TestDesdeActividad(t *testing.T) {
	casos := []struct {
		cod, desc, quiero string
	}{
		{"473000", "VENTA AL POR MENOR DE COMBUSTIBLE PARA VEHÍCULOS AUTOMOTORES Y MOTOCICLETAS", Combustible},
		{"477311", "VENTA AL POR MENOR DE PRODUCTOS FARMACÉUTICOS Y DE HERBORISTERÍA", Farmacia},
		{"471120", "VENTA AL POR MENOR EN SUPERMERCADOS", Supermercado},
		{"471110", "", Supermercado},
		{"561011", "SERVICIOS DE RESTAURANTES Y CANTINAS SIN ESPECTÁCULO", Gastronomia},
		{"561040", "SERVICIOS DE EXPENDIO DE BEBIDAS EN BARES", Gastronomia},
		{"492110", "SERVICIO DE TRANSPORTE AUTOMOTOR URBANO DE PASAJEROS", Transporte},
		{"522020", "SERVICIOS DE PLAYAS DE ESTACIONAMIENTO Y GARAJES", Transporte},
		{"960990", "SERVICIOS PERSONALES N.C.P.", Servicios},
		{"620100", "SERVICIOS DE CONSULTORES EN INFORMÁTICA", Servicios},
		{"452101", "LAVADO AUTOMÁTICO Y MANUAL DE VEHÍCULOS", Servicios},
		{"475300", "VENTA AL POR MENOR DE ARTÍCULOS DE FERRETERÍA", Otros},
		{"", "VENTA AL POR MENOR EN HIPERMERCADOS", Supermercado},
		{"", "EXPENDIO DE COMBUSTIBLES", Combustible},
		{"", "SERVICIOS DE BARES Y CONFITERÍAS", Gastronomia},
		{"", "", ""},
	}
	for _, c := range casos {
		if got := DesdeActividad(c.cod, c.desc); got != c.quiero {
			t.Errorf("DesdeActividad(%q, %q) = %q, quería %q", c.cod, c.desc, got, c.quiero)
		}
	}
}

func TestValido(t *testing.T) {
	for _, r := range Todos {
		if !Valido(r) {
			t.Error(r)
		}
	}
	if Valido("") || Valido("Supermercado") || Valido("comida") {
		t.Fatal("rubro inválido aceptado")
	}
}
