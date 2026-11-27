package auth

import (
	"sync"
	"time"
)

// Limitador frena los intentos de entrada fallidos por clave (una IP, un email): los primeros
// LibresIntentos son libres; desde ahí, cada fallo obliga a esperar el doble que el anterior,
// desde EsperaInicial hasta EsperaMaxima. Un acierto limpia la cuenta y una hora sin fallos, también.
type Limitador struct {
	LibresIntentos int
	EsperaInicial  time.Duration
	EsperaMaxima   time.Duration
	Ahora          func() time.Time

	mu      sync.Mutex
	m       map[string]*intentos
	barrido time.Time
}

type intentos struct {
	fallos int
	hasta  time.Time
	ultimo time.Time
}

// NuevoLimitador con los valores por defecto: 5 libres, después 5 s, 10 s, 20 s... hasta 15 min.
func NuevoLimitador() *Limitador {
	return &Limitador{LibresIntentos: 5, EsperaInicial: 5 * time.Second, EsperaMaxima: 15 * time.Minute, Ahora: time.Now}
}

func (l *Limitador) ahora() time.Time {
	if l.Ahora != nil {
		return l.Ahora()
	}
	return time.Now()
}

// Espera dice cuánto falta para poder intentar de nuevo con esas claves (0 = ya se puede).
func (l *Limitador) Espera(claves ...string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	ahora := l.ahora()
	var max time.Duration
	for _, k := range claves {
		if x := l.m[k]; x != nil {
			if d := x.hasta.Sub(ahora); d > max {
				max = d
			}
		}
	}
	return max
}

// Fallo anota un intento fallido para cada clave.
func (l *Limitador) Fallo(claves ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ahora := l.ahora()
	if l.m == nil {
		l.m = map[string]*intentos{}
	}
	l.barrer(ahora)
	for _, k := range claves {
		if k == "" {
			continue
		}
		x := l.m[k]
		if x == nil || ahora.Sub(x.ultimo) > time.Hour {
			x = &intentos{}
			l.m[k] = x
		}
		x.fallos++
		x.ultimo = ahora
		if n := x.fallos - l.LibresIntentos; n >= 0 {
			d := l.EsperaInicial
			for i := 0; i < n && d < l.EsperaMaxima; i++ {
				d *= 2
			}
			x.hasta = ahora.Add(min(d, l.EsperaMaxima))
		}
	}
}

// Exito limpia la cuenta de esas claves.
func (l *Limitador) Exito(claves ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range claves {
		delete(l.m, k)
	}
}

// barrer saca lo viejo cada tanto, para que el mapa no crezca sin fin.
func (l *Limitador) barrer(ahora time.Time) {
	if ahora.Sub(l.barrido) < 10*time.Minute {
		return
	}
	l.barrido = ahora
	for k, x := range l.m {
		if ahora.Sub(x.ultimo) > time.Hour && ahora.After(x.hasta) {
			delete(l.m, k)
		}
	}
}

// Cupo limita cuántos pedidos se aceptan por clave (una IP, un email) en una ventana corrediza:
// hasta N por Ventana. Sirve para lo que no es un intento fallido (pedir un link por mail).
type Cupo struct {
	N       int
	Ventana time.Duration
	Ahora   func() time.Time

	mu sync.Mutex
	m  map[string][]time.Time
}

// Tomar anota un pedido para cada clave si todas tienen lugar. Devuelve false (y no anota nada) si
// alguna ya llegó al tope.
func (c *Cupo) Tomar(claves ...string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	ahora := time.Now()
	if c.Ahora != nil {
		ahora = c.Ahora()
	}
	if c.m == nil {
		c.m = map[string][]time.Time{}
	}
	for k, ts := range c.m { // lo viejo se va
		var quedan []time.Time
		for _, t := range ts {
			if ahora.Sub(t) < c.Ventana {
				quedan = append(quedan, t)
			}
		}
		if len(quedan) == 0 {
			delete(c.m, k)
		} else {
			c.m[k] = quedan
		}
	}
	for _, k := range claves {
		if len(c.m[k]) >= c.N {
			return false
		}
	}
	for _, k := range claves {
		c.m[k] = append(c.m[k], ahora)
	}
	return true
}
