#!/bin/sh
# Build reproducible del spike lua-wasm.
# Requisitos: clang>=18 con wasm-ld, wasi-libc, libclang-rt-*-dev-wasm32, git, go.
#   Ubuntu: apt install clang lld wasi-libc libclang-rt-18-dev-wasm32
set -e
cd "$(dirname "$0")"

[ -d lua-5.4.7 ] || git clone --depth 1 --branch v5.4.7 https://github.com/lua/lua lua-5.4.7

LUA_SRC=$(ls lua-5.4.7/*.c | grep -v -E "lua\.c|luac\.c|onelua|liolib|loslib|loadlib|linit|ldblib" | tr '\n' ' ')

# gate.wasm: el experimento-compuerta del trampolín (sin Lua)
clang --target=wasm32-wasi --sysroot=/usr -I/usr/include/wasm32-wasi -L/usr/lib/wasm32-wasi \
  -O2 -mexec-model=reactor shim/gate.c -o gate.wasm -Wl,--export=__stack_pointer

# lua.wasm: PUC-Lua 5.4.7 oficial + shim, con el trampolín de desenrollado
# (spike_unwind.h) en vez de setjmp/longjmp — cero parches a las fuentes.
clang --target=wasm32-wasi --sysroot=/usr -I/usr/include/wasm32-wasi -L/usr/lib/wasm32-wasi \
  -O2 -mexec-model=reactor -D_WASI_EMULATED_SIGNAL -D_WASI_EMULATED_PROCESS_CLOCKS \
  -include shim/spike_unwind.h -Ishim -Ilua-5.4.7 \
  $LUA_SRC shim/lua_shim.c -o lua.wasm \
  -lwasi-emulated-signal -lwasi-emulated-process-clocks \
  -Wl,--export=__stack_pointer -Wl,--export=malloc

echo "OK: $(ls -la lua.wasm | awk '{print $5}') bytes"
echo "tests:      cd go && go test ./..."
echo "benchmarks: cd go && go test -run XX -bench . -benchtime 2s"
