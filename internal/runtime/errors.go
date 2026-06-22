package runtime

import (
	lua "github.com/yuin/gopher-lua"
)

// Errores estructurados del core (api.md §1.4). Las primitivas Go **lanzan**
// (vía `error()` de Lua) una tabla `{ code, message, detail? }` que el código
// Lua captura con `pcall`. Frente al estilo `res, err`, los errores
// estructurados componen mejor a través de capas de extensiones y nunca se
// ignoran en silencio.
//
// Este fichero es el puente Go↔Lua de ese contrato: construye la tabla, la
// lanza preservando su forma, y recupera la forma estructurada cuando un error
// vuelve cruzando la frontera (p. ej. el resultado de un chunk). El invariante
// que blinda S02 (inventario 🔒): un código reservado **nunca se traga ni se
// reescribe** al cruzar el puente.

// Códigos de error reservados v1 (§1.4). El core los emite y nadie más debe
// acuñarlos: las extensiones crean los suyos con la misma forma pero fuera de
// esta lista (p. ej. `EPROVIDER`). `ECANCELED` y `EBUDGET` nombran además los
// abortos *no capturables* de §1.3 (cancelación y watchdog); aquí solo se
// declara su nombre —su semántica de desenrollado llega con el scheduler
// (S08/S09).
const (
	CodeENOENT    = "ENOENT"    // recurso inexistente
	CodeEEXIST    = "EEXIST"    // ya existe (p. ej. write{exclusive}, G17)
	CodeEACCES    = "EACCES"    // permiso denegado
	CodeEIO       = "EIO"       // fallo de IO / backpressure desbordado
	CodeEHTTP     = "EHTTP"     // error de protocolo HTTP
	CodeENET      = "ENET"      // fallo de transporte de red
	CodeETIMEOUT  = "ETIMEOUT"  // expiró un plazo
	CodeECANCELED = "ECANCELED" // task cancelada (solo observable, §1.3)
	CodeEBUDGET   = "EBUDGET"   // presupuesto de slice excedido (watchdog, §1.3)
	CodeEINVAL    = "EINVAL"    // argumento o uso inválido
	CodeECLOSED   = "ECLOSED"   // handle cerrado
)

// reservedCodes es el conjunto de códigos que el core se reserva (§1.4, §17).
// Sirve para auditar que el puente respeta el invariante 🔒 de S02 y para que
// futuras primitivas comprueben que no acuñan uno ajeno por error.
var reservedCodes = map[string]bool{
	CodeENOENT:    true,
	CodeEEXIST:    true,
	CodeEACCES:    true,
	CodeEIO:       true,
	CodeEHTTP:     true,
	CodeENET:      true,
	CodeETIMEOUT:  true,
	CodeECANCELED: true,
	CodeEBUDGET:   true,
	CodeEINVAL:    true,
	CodeECLOSED:   true,
}

// IsReservedCode informa de si `code` es uno de los códigos reservados del core
// (§1.4). Las extensiones lo usan para no pisar el espacio del core al acuñar
// los suyos.
func IsReservedCode(code string) bool {
	return reservedCodes[code]
}

// newErrorTable construye la tabla `{ code, message, detail? }` (§1.4) en el
// estado `L`. `detail` se omite cuando es nil o `lua.LNil`: la clave solo
// aparece si hay algo que contar, para que `err.detail == nil` distinga
// "sin detalle" de "detalle vacío".
func newErrorTable(L *lua.LState, code, message string, detail lua.LValue) *lua.LTable {
	t := L.NewTable()
	t.RawSetString("code", lua.LString(code))
	t.RawSetString("message", lua.LString(message))
	if detail != nil && detail != lua.LNil {
		t.RawSetString("detail", detail)
	}
	return t
}

// raiseError lanza un error estructurado desde una primitiva Go hacia Lua: es
// el equivalente Go de `error({ code, message, detail })`. Desenrolla la pila
// de la goroutine actual (panic interno de gopher-lua) hasta el `PCall` que la
// envuelve, que recupera la **misma tabla** intacta. No reescribe el código:
// el `code` que entra es el que ve quien captura.
//
// Solo debe llamarse desde dentro de una función Go invocada por Lua (donde hay
// un frame que desenrollar), igual que `L.Error`.
func raiseError(L *lua.LState, code, message string, detail lua.LValue) {
	L.Error(newErrorTable(L, code, message, detail), 1)
}

// StructuredError es la cara Go de un error estructurado (§1.4) que ha cruzado
// la frontera Lua→Go (p. ej. al evaluar un chunk con `EvalString`). Conserva el
// `code` y el `message` ya copiados a strings Go, y mantiene `Detail`/`Value`
// como `LValue` para quien quiera inspeccionarlos mientras el estado siga vivo.
type StructuredError struct {
	Code    string
	Message string
	Detail  lua.LValue // detalle opcional; lua.LNil si no había
	Value   lua.LValue // la tabla original tal cual la lanzó Lua
}

// Error implementa la interfaz `error` de Go. No inventa formato: expone código
// y mensaje, que es lo que un test o un log necesitan.
func (e *StructuredError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

// structuredFromError intenta recuperar la forma estructurada de un error
// devuelto por `PCall`. Devuelve `(se, true)` solo si el error lleva una tabla
// con un campo `code` de tipo string —la forma del contrato §1.4—; en cualquier
// otro caso `(nil, false)`, para que el llamante deje pasar el error tal cual
// (un error de sintaxis, un `error("string")` de un plugin, etc.).
//
// Es la mitad del invariante 🔒 de S02: el puente **no se traga** el error
// (recupera la tabla) ni lo **reescribe** (copia el `code` literal, sin mapear
// ni renombrar).
func structuredFromError(err error) (*StructuredError, bool) {
	apiErr, ok := err.(*lua.ApiError)
	if !ok {
		return nil, false
	}
	return structuredFromValue(apiErr.Object)
}

// structuredFromValue recupera la forma estructurada (§1.4) de un **valor Lua**
// lanzado tal cual (no envuelto en un `*lua.ApiError`): es lo que guarda una task
// en `errValue` cuando su `fn` lanza (scheduler.go usa `raisedValue`). Devuelve
// `(se, true)` solo si el valor es una tabla con un campo `code` de tipo string;
// si no, `(nil, false)` para que el llamante deje pasar el error tal cual. Es la
// misma mitad del invariante 🔒 de S02 que `structuredFromError` —de hecho aquélla
// delega aquí—, pero accesible desde el lado de las tasks (`EvalTaskString`), donde
// el error no llega como `error` de Go sino como el `LValue` crudo.
func structuredFromValue(v lua.LValue) (*StructuredError, bool) {
	tbl, ok := v.(*lua.LTable)
	if !ok {
		return nil, false
	}
	code, ok := tbl.RawGetString("code").(lua.LString)
	if !ok {
		return nil, false
	}

	se := &StructuredError{
		Code:   string(code),
		Detail: tbl.RawGetString("detail"),
		Value:  tbl,
	}
	if msg, ok := tbl.RawGetString("message").(lua.LString); ok {
		se.Message = string(msg)
	}
	return se, true
}
