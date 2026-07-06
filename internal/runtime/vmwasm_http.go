package runtime

// Catálogo de nu.http sobre el backend wasm (M13b, §8). Contraparte de http.go
// para la petición de un tiro: nu.http.request(opts) -> {status, headers, body}
// (⏸). Reusa el cliente reutilizable del Runtime (rt.http) y su `do` VM-agnóstico,
// idéntico al backend gopher; el IO corre en la goroutine de fondo del scheduler.
//
// nu.http.stream (el handle Stream con chunks/events/close y su backpressure) usa
// el despacho de métodos-de-handle ⏸ ya validado; se añade en una iteración
// aparte (aquí sólo request).

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/dbareagimeno/nu/internal/vmwasm"
)

func registerHTTPWasm(p *vmwasm.Pool, rt *Runtime) {
	// nu.http.request(opts) -> {status, headers, body} ⏸
	p.RegisterSuspending("http.request", func(inst *vmwasm.Instance, args []any) ([]any, error) {
		o, err := parseReqOptsWasm(arg(args, 0))
		if err != nil {
			return nil, err
		}
		status, headers, body, derr := rt.http.do(o)
		if derr != nil {
			return nil, httpErrWasm(derr)
		}
		h := make(map[string]any, len(headers))
		for k, v := range headers {
			h[k] = v
		}
		return []any{map[string]any{
			"status":  int64(status),
			"headers": h,
			"body":    body,
		}}, nil
	})
}

// parseReqOptsWasm construye un reqOpts desde el mapa `opts` que cruzó el wire.
// Mismo contrato que parseReqOpts (§8): url obligatoria, method/body/headers/
// timeout_ms/tls/proxy opcionales; un valor inválido → EINVAL.
func parseReqOptsWasm(v any) (reqOpts, error) {
	o := reqOpts{method: http.MethodGet, timeout: httpDefaultTimeout}
	m, ok := v.(map[string]any)
	if !ok {
		return o, einvalHTTP("opts debe ser una tabla")
	}
	url, _ := m["url"].(string)
	if url == "" {
		return o, einvalHTTP("opts.url es obligatoria (string no vacío)")
	}
	o.rawURL = url
	if mth, ok := m["method"].(string); ok && mth != "" {
		o.method = strings.ToUpper(mth)
	}
	if b, ok := m["body"].(string); ok {
		o.body = b
		o.hasBody = true
	}
	// timeout_ms: número positivo.
	if tv, present := m["timeout_ms"]; present && tv != nil {
		tm, ok := httpNum(tv)
		if !ok {
			return o, einvalHTTP("opts.timeout_ms debe ser un número")
		}
		if tm <= 0 {
			return o, einvalHTTP("opts.timeout_ms debe ser positivo")
		}
		o.timeout = time.Duration(tm) * time.Millisecond
	}
	// headers: tabla string→string.
	if hv, present := m["headers"]; present && hv != nil {
		h, ok := hv.(map[string]any)
		if !ok {
			return o, einvalHTTP("opts.headers debe ser una tabla")
		}
		o.headers = make(map[string]string, len(h))
		for k, val := range h {
			s, ok := val.(string)
			if !ok {
				return o, einvalHTTP("opts.headers debe ser una tabla de string→string")
			}
			o.headers[k] = s
		}
	}
	// tls por petición (G12): {ca_file?, insecure?}.
	if tv, present := m["tls"]; present && tv != nil {
		tls, ok := tv.(map[string]any)
		if !ok {
			return o, einvalHTTP("opts.tls debe ser una tabla")
		}
		if ca, ok := tls["ca_file"].(string); ok {
			o.caFile = ca
			o.caFileSet = true
		}
		o.insecure, _ = tls["insecure"].(bool)
	}
	// proxy por petición (G12).
	if px, ok := m["proxy"].(string); ok {
		o.proxy = px
		o.proxySet = true
	}
	return o, nil
}

func httpNum(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func einvalHTTP(msg string) error {
	return &vmwasm.StructuredError{Code: CodeEINVAL, Message: "nu.http.request: " + msg}
}

// httpErrWasm traduce el error de rt.http.do (un *httpError con code/msg) al error
// estructurado de la frontera. Mismo mapeo que raiseHTTPError.
func httpErrWasm(err error) error {
	var he *httpError
	if errors.As(err, &he) {
		return &vmwasm.StructuredError{Code: he.code, Message: he.msg}
	}
	return &vmwasm.StructuredError{Code: CodeENET, Message: err.Error()}
}
