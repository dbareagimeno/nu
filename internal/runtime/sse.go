package runtime

import (
	"bytes"
	"strings"
)

// Parser SSE incremental (Server-Sent Events) — la lógica 🔒 de `Stream:events()`
// (api.md §8, sesión S20). Implementa la especificación de SSE del WHATWG
// (https://html.spec.whatwg.org/multipage/server-sent-events.html) en lo que el
// contrato pide: itera eventos `{event?, data, id?}`.
//
// LA EXIGENCIA CLAVE — INCREMENTAL (eventos partidos entre trozos). Un evento SSE
// llega por la red en trozos de TCP que NO respetan los límites de evento ni de
// línea: una línea puede partirse a la mitad, un `\n\n` (el separador de evento)
// puede llegar en dos trozos, varios eventos pueden venir en un mismo trozo. El
// parser **no asume nada** sobre los límites de los trozos: acumula los bytes
// recibidos en un buffer (`buf`), extrae solo las **líneas completas** (las que ya
// tienen su terminador), y mantiene el resto (`leftover`) para el siguiente trozo.
// Los eventos completos se encolan (`ready`) y `next()` los entrega de uno en uno;
// `feed()` añade bytes, `flush()` despacha el último evento pendiente en EOF.
//
// EL FORMATO (espec SSE, lo que blindan los tests):
//
//   - Líneas terminadas en "\n", "\r\n" o "\r" (los tres son válidos). El
//     terminador NO forma parte del contenido de la línea.
//   - Un evento se **despacha** en una línea EN BLANCO (una línea vacía marca el
//     fin del evento en curso). Un evento sin ningún campo `data` no se despacha
//     (la espec lo descarta).
//   - Campo `data`: varios `data:` en un evento se **concatenan con "\n"** entre
//     ellos; el `data` final NO lleva "\n" al final.
//   - Campo `event`: nombre del tipo de evento (ausente = el consumidor asume
//     "message"; el parser lo deja ausente para no inventar).
//   - Campo `id`: el "last event id"; se expone tal cual.
//   - Campo `retry`: la espec lo usa para la reconexión; el contrato solo pide
//     event/data/id, así que el parser lo **ignora**.
//   - Una línea que empieza por ":" es un **comentario**: se ignora entera.
//   - Tras el nombre del campo y los dos puntos, se quita **un** espacio opcional:
//     "data: x" y "data:x" producen ambos "x" (solo el primer espacio).
//   - Una línea sin ":" es un campo con valor vacío (p. ej. "data" sola = un
//     `data` vacío).

// sseEvent es un evento SSE ya parseado, con los campos que el contrato expone
// (§8). `data` siempre está (un evento sin data no se despacha); `event`/`id` son
// opcionales —los flags distinguen "ausente" de "presente pero vacío", para no
// inventar un `event="message"` que la espec deja al consumidor—.
type sseEvent struct {
	data     string
	event    string
	hasEvent bool
	id       string
	hasID    bool
}

// sseParser es el estado del parser incremental entre trozos. Solo lo toca el
// consumidor (la goroutine de la task, una llamada a `events()` cada vez), nunca
// la goroutine de fondo: no necesita candado.
type sseParser struct {
	buf    bytes.Buffer // bytes recibidos aún sin partir en líneas completas
	cur    eventBuilder // evento en construcción (campos acumulados del bloque actual)
	curHas bool         // el bloque actual ha recibido al menos un campo data (despachable)
	ready  []sseEvent   // eventos ya cerrados, pendientes de entregar por next()
}

// eventBuilder acumula los campos del evento en construcción mientras llegan sus
// líneas. `data` se va concatenando con "\n"; al cerrar el evento (línea en
// blanco) se vuelca a un `sseEvent`.
type eventBuilder struct {
	data     strings.Builder
	dataN    int // nº de líneas data añadidas (para el separador "\n" entre ellas)
	event    string
	hasEvent bool
	id       string
	hasID    bool
}

// feed añade un trozo crudo de bytes y procesa todas las líneas **completas** que
// ya se puedan extraer, encolando los eventos que cierren. Los bytes de una línea
// a medias quedan en `buf` para el siguiente `feed`. Es el corazón de "incremental".
func (p *sseParser) feed(chunk []byte) {
	p.buf.Write(chunk)
	p.drainLines()
}

// drainLines extrae del buffer todas las líneas completas (con terminador) y las
// procesa. Una línea es "completa" cuando hay un "\n" o un "\r" en el buffer; el
// caso delicado es "\r" al FINAL del buffer: podría ser un "\r\n" partido (el "\n"
// vendrá en el próximo trozo), así que ese "\r" final se deja sin consumir hasta
// saber qué le sigue.
func (p *sseParser) drainLines() {
	for {
		data := p.buf.Bytes()
		idx, adv := nextLineEnd(data)
		if idx < 0 {
			return // no hay línea completa todavía; espera más bytes
		}
		line := string(data[:idx])
		// Consume la línea y su terminador del buffer.
		rest := data[adv:]
		p.buf.Reset()
		p.buf.Write(rest)
		p.processLine(line)
	}
}

// nextLineEnd localiza el final de la primera línea en `data`: devuelve `(idx,
// adv)` donde `idx` es la longitud del contenido de la línea (sin terminador) y
// `adv` el nº de bytes a consumir (contenido + terminador). Devuelve `(-1, 0)` si
// no hay una línea completa todavía —incluido el caso de un "\r" al final del
// buffer, que se trata como incompleto por si es un "\r\n" partido entre trozos—.
func nextLineEnd(data []byte) (int, int) {
	for i := 0; i < len(data); i++ {
		switch data[i] {
		case '\n':
			return i, i + 1
		case '\r':
			if i+1 < len(data) {
				if data[i+1] == '\n' {
					return i, i + 2 // "\r\n"
				}
				return i, i + 1 // "\r" solo
			}
			// "\r" es el último byte: no sabemos si viene "\n". Trátalo como
			// incompleto hasta el próximo trozo (clave para no partir un "\r\n").
			return -1, 0
		}
	}
	return -1, 0
}

// processLine procesa una línea YA completa (sin terminador) según la espec SSE.
func (p *sseParser) processLine(line string) {
	// Línea en blanco: despacha el evento en construcción (si tiene data) y empieza
	// uno nuevo.
	if line == "" {
		p.dispatch()
		return
	}
	// Comentario: una línea que empieza por ":" se ignora entera (espec SSE).
	if line[0] == ':' {
		return
	}

	// Parte la línea en "field: value". Sin ":", toda la línea es el nombre del
	// campo con valor vacío (espec SSE).
	field, value := line, ""
	if i := strings.IndexByte(line, ':'); i >= 0 {
		field = line[:i]
		value = line[i+1:]
		// Quita UN espacio opcional tras los dos puntos ("data: x" -> "x").
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
	}

	switch field {
	case "data":
		// Varios `data:` se concatenan con "\n" entre ellos (el separador va ANTES
		// de cada línea data salvo la primera).
		if p.cur.dataN > 0 {
			p.cur.data.WriteByte('\n')
		}
		p.cur.data.WriteString(value)
		p.cur.dataN++
		p.curHas = true
	case "event":
		p.cur.event = value
		p.cur.hasEvent = true
	case "id":
		// La espec ignora un id que contenga un NUL; en la práctica de SSE de
		// providers no se da. Se expone tal cual (sin el NUL no llega aquí).
		p.cur.id = value
		p.cur.hasID = true
	case "retry":
		// Ignorado: el contrato (§8) solo pide event/data/id.
	default:
		// Campo desconocido: la espec lo ignora.
	}
}

// dispatch cierra el evento en construcción (lo provoca una línea en blanco): si
// recibió algún `data`, lo vuelca a `ready`; si no, lo descarta (espec: un evento
// sin data no se despacha). En ambos casos resetea el builder para el siguiente.
func (p *sseParser) dispatch() {
	if p.curHas {
		p.ready = append(p.ready, sseEvent{
			data:     p.cur.data.String(),
			event:    p.cur.event,
			hasEvent: p.cur.hasEvent,
			id:       p.cur.id,
			hasID:    p.cur.hasID,
		})
	}
	p.cur = eventBuilder{}
	p.curHas = false
}

// next entrega el siguiente evento ya cerrado, si lo hay. `(ev, true)` con evento;
// `(_, false)` si no hay ninguno listo (el consumidor debe pedir más bytes).
func (p *sseParser) next() (sseEvent, bool) {
	if len(p.ready) == 0 {
		return sseEvent{}, false
	}
	ev := p.ready[0]
	p.ready = p.ready[1:]
	return ev, true
}

// flush se llama en EOF del body: procesa cualquier resto del buffer como una
// última línea (un body que termina sin "\n" final) y despacha el evento en
// construcción aunque no llegara su línea en blanco final (un SSE bien formado las
// pone, pero un servidor que cierra la conexión sin la línea en blanco final no
// debe perder su último evento). Devuelve el siguiente evento listo, si lo hay.
func (p *sseParser) flush() (sseEvent, bool) {
	// Resto sin terminador: trátalo como la última línea (incluido un "\r" final
	// que `drainLines` dejó pendiente, que en EOF ya es un terminador definitivo).
	if p.buf.Len() > 0 {
		rest := p.buf.String()
		p.buf.Reset()
		// Quita un "\r" final colgado (era el terminador de la última línea).
		rest = strings.TrimSuffix(rest, "\r")
		if rest != "" {
			p.processLine(rest)
		}
	}
	// Despacha el evento en construcción (sin su línea en blanco final).
	p.dispatch()
	return p.next()
}
