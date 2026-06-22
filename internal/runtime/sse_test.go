package runtime

import (
	"reflect"
	"testing"
)

// Tests del parser SSE incremental (S20, api.md §8, lógica 🔒). Blindan la
// especificación de Server-Sent Events EN LO QUE EL CONTRATO PIDE (event/data/id) y,
// sobre todo, el invariante crítico: el parser es **incremental** —un evento puede
// llegar partido entre varios trozos de red (una línea a medias, un `\n\n` partido)
// y debe parsearse como UN evento completo correcto—. Estos tests son puros (sin
// red, sin token): ejercitan `feed`/`next`/`flush` directamente, alimentando los
// bytes en distintas particiones (un solo trozo, byte a byte, cortes en sitios
// adversos) y exigiendo el MISMO resultado.

// drainAll alimenta `raw` al parser **en los cortes dados** (cada elemento de
// `splits` es un trozo) y devuelve TODOS los eventos parseados (incluido el flush de
// EOF). Es la herramienta central: el mismo `raw` partido de N formas debe dar la
// misma secuencia de eventos.
func drainAll(p *sseParser, chunks [][]byte) []sseEvent {
	var out []sseEvent
	for _, c := range chunks {
		p.feed(c)
		for {
			ev, has := p.next()
			if !has {
				break
			}
			out = append(out, ev)
		}
	}
	// EOF: despacha el último evento pendiente y vacía la cola.
	for {
		ev, has := p.flush()
		if !has {
			break
		}
		out = append(out, ev)
	}
	return out
}

// splitEvery parte `s` en trozos de `n` bytes (el último puede ser menor). `n<=0`
// devuelve un único trozo con todo `s`.
func splitEvery(s string, n int) [][]byte {
	if n <= 0 {
		return [][]byte{[]byte(s)}
	}
	var out [][]byte
	b := []byte(s)
	for len(b) > 0 {
		k := n
		if k > len(b) {
			k = len(b)
		}
		out = append(out, b[:k])
		b = b[k:]
	}
	return out
}

// TestSSEParse es la tabla principal: cada caso da un `raw` SSE y los eventos
// esperados. CADA caso se ejecuta con VARIAS particiones del mismo `raw` (todo de
// una, byte a byte, de 2/3/7 bytes) para blindar que el parseo es independiente de
// los límites de los trozos (eventos partidos entre chunks).
func TestSSEParse(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []sseEvent
	}{
		{
			name: "data simple",
			raw:  "data: hola\n\n",
			want: []sseEvent{{data: "hola"}},
		},
		{
			name: "sin espacio tras los dos puntos",
			raw:  "data:hola\n\n",
			want: []sseEvent{{data: "hola"}},
		},
		{
			name: "data multilinea se concatena con \\n",
			raw:  "data: l1\ndata: l2\ndata: l3\n\n",
			want: []sseEvent{{data: "l1\nl2\nl3"}},
		},
		{
			name: "event + data + id",
			raw:  "event: ping\ndata: payload\nid: 42\n\n",
			want: []sseEvent{{data: "payload", event: "ping", hasEvent: true, id: "42", hasID: true}},
		},
		{
			name: "comentario ignorado",
			raw:  ": esto es un comentario\ndata: x\n\n",
			want: []sseEvent{{data: "x"}},
		},
		{
			name: "evento sin event (solo data)",
			raw:  "data: solo\n\n",
			want: []sseEvent{{data: "solo"}},
		},
		{
			name: "varios eventos seguidos",
			raw:  "data: uno\n\ndata: dos\n\ndata: tres\n\n",
			want: []sseEvent{{data: "uno"}, {data: "dos"}, {data: "tres"}},
		},
		{
			name: "terminadores \\r\\n",
			raw:  "event: e\r\ndata: con-crlf\r\n\r\n",
			want: []sseEvent{{data: "con-crlf", event: "e", hasEvent: true}},
		},
		{
			name: "terminadores \\r solo",
			raw:  "data: con-cr\r\r",
			want: []sseEvent{{data: "con-cr"}},
		},
		{
			name: "data vacio (campo sin valor)",
			raw:  "data\n\n",
			want: []sseEvent{{data: ""}},
		},
		{
			name: "evento sin data no se despacha (solo comentario)",
			raw:  ": keep-alive\n\n",
			want: nil,
		},
		{
			name: "retry ignorado pero data sale",
			raw:  "retry: 3000\ndata: y\n\n",
			want: []sseEvent{{data: "y"}},
		},
		{
			name: "id presente con event y data multilinea",
			raw:  "id: abc\nevent: msg\ndata: a\ndata: b\n\n",
			want: []sseEvent{{data: "a\nb", event: "msg", hasEvent: true, id: "abc", hasID: true}},
		},
		{
			name: "ultimo evento sin linea en blanco final (despachado en EOF)",
			raw:  "data: sin-cierre",
			want: []sseEvent{{data: "sin-cierre"}},
		},
		{
			name: "mezcla de eventos con campos distintos",
			raw:  "data: a\n\nevent: t\ndata: b\nid: 1\n\ndata: c\n\n",
			want: []sseEvent{
				{data: "a"},
				{data: "b", event: "t", hasEvent: true, id: "1", hasID: true},
				{data: "c"},
			},
		},
	}

	// Las particiones contra las que se prueba CADA caso. 1 = byte a byte (el corte
	// más adverso: parte cada "\r\n", cada "data:", cada evento). 0 = todo de una.
	splits := []int{0, 1, 2, 3, 7}

	for _, tc := range cases {
		for _, sp := range splits {
			t.Run(tc.name+"/split="+itoa(sp), func(t *testing.T) {
				var p sseParser
				got := drainAll(&p, splitEvery(tc.raw, sp))
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("eventos: got %#v, want %#v", got, tc.want)
				}
			})
		}
	}
}

// TestSSESplitAcrossChunksAdversarial blinda explícitamente el caso de la espec del
// criterio de hecho: un `\n\n` (separador de evento) partido EXACTAMENTE entre dos
// trozos, y una línea cortada a la mitad. Se construye el corte a mano (no por
// tamaño fijo) para clavar el límite delicado.
func TestSSESplitAcrossChunksAdversarial(t *testing.T) {
	// Un evento cuyo "\n\n" final queda partido entre el trozo 1 y el 2, y cuyo
	// "data: " queda partido en mitad de la palabra.
	chunks := [][]byte{
		[]byte("event: pi"),       // "event: pi" (línea a medias)
		[]byte("ng\ndata: hel"),   // "...ng\n" cierra event; "data: hel" a medias
		[]byte("lo mundo\n"),      // cierra la línea data
		[]byte("\n"),              // la línea en blanco (separada) despacha el evento
		[]byte("data: segundo\n"), // segundo evento
		[]byte("\n"),
	}
	var p sseParser
	got := drainAll(&p, chunks)
	want := []sseEvent{
		{data: "hello mundo", event: "ping", hasEvent: true},
		{data: "segundo"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eventos partidos: got %#v, want %#v", got, want)
	}
}

// TestSSECRLFSplitBetweenChunks blinda el caso más sutil del parser: un "\r\n"
// partido entre trozos (el "\r" al final de un trozo, el "\n" al inicio del
// siguiente). Si el parser tratara el "\r" como terminador inmediato, vería una
// línea en blanco espuria; debe ESPERAR al "\n".
func TestSSECRLFSplitBetweenChunks(t *testing.T) {
	chunks := [][]byte{
		[]byte("data: x\r"), // "\r" al final: ¿terminador o "\r\n" partido?
		[]byte("\n\r\n"),    // llega el "\n" (cierra la línea) + "\r\n" en blanco
	}
	var p sseParser
	got := drainAll(&p, chunks)
	want := []sseEvent{{data: "x"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("CRLF partido: got %#v, want %#v", got, want)
	}
}
