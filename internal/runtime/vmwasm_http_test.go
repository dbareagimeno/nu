package runtime

// Tests de M13b: nu.http.request sobre wasm (§8). Petición real contra un servidor
// httptest; status/headers/body cruzan a Lua. La primitiva ⏸ corre en una task y
// el driver la lleva a término.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dbareagimeno/nu/internal/vmwasm"
)

func wasmHTTPRun(t *testing.T, rt *Runtime, setup string) string {
	t.Helper()
	p, err := vmwasm.NewPool()
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	registerHTTPWasm(p, rt)
	inst, err := p.NewInstance()
	if err != nil {
		t.Fatalf("NewInstance: %v", err)
	}
	t.Cleanup(func() { _ = inst.Close() })
	if _, lerr, err := inst.Eval(setup); err != nil || lerr != "" {
		t.Fatalf("setup: lerr=%q err=%v", lerr, err)
	}
	if err := inst.RunTasks(context.Background()); err != nil {
		t.Fatalf("RunTasks: %v", err)
	}
	out, _, _ := inst.Eval(`return tostring(out)`)
	return out
}

// M13b.http.1: request real — status, body y headers cruzan.
func TestHTTPWasmRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test", "hola")
		w.WriteHeader(201)
		fmt.Fprintf(w, "cuerpo:%s", r.Method)
	}))
	defer srv.Close()

	rt := &Runtime{http: newHTTPState("", "")}
	t.Cleanup(func() { rt.http.close() })
	out := wasmHTTPRun(t, rt, `
		nu.task.spawn(function()
			local res = nu.http.request({ url = "`+srv.URL+`", method = "POST" })
			out = tostring(res.status) .. ":" .. res.body .. ":" .. tostring(res.headers["X-Test"])
		end)`)
	if out != "201:cuerpo:POST:hola" {
		t.Fatalf("http.request: got %q", out)
	}
}

// M13b.http.2: request con headers y body de subida.
func TestHTTPWasmRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		fmt.Fprintf(w, "recibi:%s auth:%s", string(body), r.Header.Get("Authorization"))
	}))
	defer srv.Close()

	rt := &Runtime{http: newHTTPState("", "")}
	t.Cleanup(func() { rt.http.close() })
	out := wasmHTTPRun(t, rt, `
		nu.task.spawn(function()
			local res = nu.http.request({
				url = "`+srv.URL+`",
				method = "PUT",
				body = "datos",
				headers = { Authorization = "Bearer xyz" },
			})
			out = res.body
		end)`)
	if out != "recibi:datos auth:Bearer xyz" {
		t.Fatalf("http.request body/headers: got %q", out)
	}
}

// M13b.http.3: request sin url → EINVAL accionable.
func TestHTTPWasmRequestSinURL(t *testing.T) {
	rt := &Runtime{http: newHTTPState("", "")}
	t.Cleanup(func() { rt.http.close() })
	out := wasmHTTPRun(t, rt, `
		nu.task.spawn(function()
			local ok, e = pcall(function() return nu.http.request({ method = "GET" }) end)
			out = tostring(ok) .. ":" .. tostring(e.code)
		end)`)
	if out != "false:EINVAL" {
		t.Fatalf("http.request sin url: got %q", out)
	}
}
