// Comando lockatus-falso levanta el Lockatus de mentira de oidctest como programa aparte, para
// probar el inicio de sesión desde un navegador sin un Lockatus de verdad:
//
//	go run ./internal/oidc/oidctest/lockatus-falso -puerto 9099
//
// En Factorum: emisor http://127.0.0.1:9099, client ID factorum y client secret el de -secreto. La
// pantalla de autorización deja elegir email, nombre, rol y si el email está verificado.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/diegoparras/factorum/internal/oidc/oidctest"
)

func main() {
	puerto := flag.Int("puerto", 9099, "puerto")
	cliente := flag.String("cliente", "factorum", "client ID")
	secreto := flag.String("secreto", "secreto-de-prueba", "client secret")
	flag.Parse()
	f := oidctest.Nuevo(*cliente, *secreto)
	emisor := fmt.Sprintf("http://127.0.0.1:%d", *puerto)
	f.Cambiar(func(f *oidctest.Falso) { f.Emisor = emisor })
	log.Printf("Lockatus de prueba en %s (client ID %s)", emisor, *cliente)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("127.0.0.1:%d", *puerto), f))
}
