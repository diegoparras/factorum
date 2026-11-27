package correo_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"testing"
)

func hmacHex(clave, msg string) string {
	m := hmac.New(sha256.New, []byte(clave))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// netListen abre un puerto que acepta conexiones y nunca contesta.
func netListen(t *testing.T) (int, net.Listener) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { c.Close() })
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ln
}
