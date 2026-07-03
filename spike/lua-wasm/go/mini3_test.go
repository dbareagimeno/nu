package spike

import (
	"testing"
)

func TestNestedPuroC(t *testing.T) {
	lw := boot(t)
	r, err := lw.mod.ExportedFunction("spike_test_nested").Call(lw.ctx)
	t.Logf("nested C: r=%v err=%v (7 = sándwich OK)", r, err)
	if err != nil || int32(r[0]) != 7 {
		t.Fatalf("el anidamiento del trampolín falla en C puro: r=%v err=%v", r, err)
	}
}
