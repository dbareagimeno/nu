package spike

import "testing"

func TestMiniCasos(t *testing.T) {
	cases := []struct{ name, chunk string }{
		{"pcall-ok", `local ok = pcall(function() return 1 end); return tostring(ok)`},
		{"pcall-error-string", `local ok, e = pcall(function() error("b") end); return tostring(ok)`},
		{"pcall-error-directo", `local ok = pcall(error, "b"); return tostring(ok)`},
		{"error-con-nivel-0", `local ok, e = pcall(function() error("b", 0) end); return tostring(ok)..":"..tostring(e)`},
		{"upvalue-tras-pcall", `local X; local set=function(v) X=v end; pcall(function() error("x",0) end); set(9); return tostring(X)`},
	}
	for _, c := range cases {
		lw := boot(t)
		out, lerr, err := lw.Eval(c.chunk)
		t.Logf("%s → out=%q lerr=%q err=%v", c.name, out, lerr, err)
		lw.Close()
	}
}
