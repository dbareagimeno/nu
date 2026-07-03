/* Shim del spike: la superficie que Go ve de lua.wasm.
   - spike_init: crea el estado y abre las libs SEGURAS (las mismas que deja el
     sandbox de nu: base/table/string/math/coroutine/utf8 — sin io/os, api.md §1.2).
   - spike_eval: carga+corre un chunk protegido; el resultado/error queda en un
     buffer legible desde Go (spike_buf/spike_result_len).
   - spike_co_*: corrutinas para el puente ⏸ (resume desde Go; nu_await yield-ea).
   - spike_call_pfunc: el callback del trampolín (ver spike_unwind.h). */

#include <string.h>
#include <stdlib.h>
#include "lua.h"
#include "lauxlib.h"
#include "lualib.h"

static lua_State *GL = NULL;

/* buffer único de intercambio Go<->wasm (chunks de entrada y resultados) */
#define BUF_CAP (256 * 1024)
static char BUF[BUF_CAP];
static int RESULT_LEN = 0;

__attribute__((export_name("spike_buf")))
char *spike_buf(void) { return BUF; }

__attribute__((export_name("spike_result_len")))
int spike_result_len(void) { return RESULT_LEN; }

static void set_result(lua_State *L, int idx) {
  size_t n = 0;
  const char *s = luaL_tolstring(L, idx, &n); /* respeta __tostring */
  if (n > BUF_CAP - 1) n = BUF_CAP - 1;
  memcpy(BUF, s, n);
  BUF[n] = 0;
  RESULT_LEN = (int)n;
  lua_pop(L, 1); /* el string de tolstring */
}

/* el callback del trampolín: Go nos re-entra aquí para correr el cuerpo
   protegido; f llega como índice de tabla (puntero a función wasm) */
typedef void (*pfunc_t)(lua_State *, void *);
__attribute__((export_name("spike_call_pfunc")))
void spike_call_pfunc(lua_State *L, pfunc_t f, void *ud) { f(L, ud); }

/* nu_await(...): la primitiva ⏸ de juguete — yield-ea al lado Go; los valores
   del resume se convierten en sus valores de retorno (patrón lua_yield-tailcall) */
static int l_await(lua_State *L) {
  return lua_yield(L, lua_gettop(L));
}

/* host_note(n): función host SÍNCRONA de juguete para el benchmark del peaje */
__attribute__((import_module("spike"), import_name("host_note")))
extern int spike_host_note(int n);
static int l_note(lua_State *L) {
  lua_pushinteger(L, spike_host_note((int)luaL_checkinteger(L, 1)));
  return 1;
}

/* host_render(ptr,len)->len: host con STRING de ida y vuelta (peaje de copia) */
__attribute__((import_module("spike"), import_name("host_render")))
extern int spike_host_render(const char *p, int n);
static int l_render(lua_State *L) {
  size_t n = 0;
  const char *s = luaL_checklstring(L, 1, &n);
  if (n > BUF_CAP - 1) n = BUF_CAP - 1;
  memcpy(BUF, s, n); /* al buffer compartido; Go lo lee y "renderiza" */
  int out = spike_host_render(BUF, (int)n);
  lua_pushlstring(L, BUF, (size_t)out); /* la "vuelta" renderizada */
  return 1;
}

__attribute__((export_name("spike_init")))
int spike_init(void) {
  GL = luaL_newstate();
  if (!GL) return 1;
  /* las libs que el baseline de nu conserva (api.md §1.2) */
  luaL_requiref(GL, LUA_GNAME, luaopen_base, 1);       lua_pop(GL, 1);
  luaL_requiref(GL, LUA_TABLIBNAME, luaopen_table, 1); lua_pop(GL, 1);
  luaL_requiref(GL, LUA_STRLIBNAME, luaopen_string, 1);lua_pop(GL, 1);
  luaL_requiref(GL, LUA_MATHLIBNAME, luaopen_math, 1); lua_pop(GL, 1);
  luaL_requiref(GL, LUA_COLIBNAME, luaopen_coroutine, 1); lua_pop(GL, 1);
  luaL_requiref(GL, LUA_UTF8LIBNAME, luaopen_utf8, 1); lua_pop(GL, 1);
  lua_register(GL, "nu_await", l_await);
  lua_register(GL, "host_note", l_note);
  lua_register(GL, "host_render", l_render);
  return 0;
}

/* eval protegido: 0 ok (resultado en BUF si el chunk devolvió algo),
   2 error (mensaje en BUF) */
__attribute__((export_name("spike_eval")))
int spike_eval(int len) {
  RESULT_LEN = 0;
  if (luaL_loadbuffer(GL, BUF, (size_t)len, "chunk") != LUA_OK) {
    set_result(GL, -1); lua_pop(GL, 1); return 2;
  }
  if (lua_pcall(GL, 0, 1, 0) != LUA_OK) {
    set_result(GL, -1); lua_pop(GL, 1); return 2;
  }
  if (!lua_isnil(GL, -1)) set_result(GL, -1);
  lua_pop(GL, 1);
  return 0;
}

/* --- corrutinas para el puente ⏸ --------------------------------------- */

/* crea una corrutina desde el chunk en BUF; la ancla en el registry y
   devuelve su ref (>0) o -1 si el chunk no compila (mensaje en BUF) */
__attribute__((export_name("spike_co_spawn")))
int spike_co_spawn(int len) {
  lua_State *co = lua_newthread(GL);            /* GL: [co] */
  if (luaL_loadbuffer(co, BUF, (size_t)len, "co") != LUA_OK) {
    set_result(co, -1);
    lua_pop(GL, 1);
    return -1;
  }
  return luaL_ref(GL, LUA_REGISTRYINDEX);       /* ancla y devuelve ref */
}

/* resume con un string opcional (BUF/len; len<0 = sin argumento).
   Devuelve: 0 done, 1 yield, 2 error. Resultado/lo-yieldeado/error en BUF. */
__attribute__((export_name("spike_co_resume")))
int spike_co_resume(int ref, int len) {
  lua_rawgeti(GL, LUA_REGISTRYINDEX, ref);
  lua_State *co = lua_tothread(GL, -1);
  lua_pop(GL, 1);
  int nargs = 0;
  if (len >= 0) { lua_pushlstring(co, BUF, (size_t)len); nargs = 1; }
  int nres = 0;
  int st = lua_resume(co, GL, nargs, &nres);
  RESULT_LEN = 0;
  if (st == LUA_YIELD) {
    if (nres > 0) set_result(co, -1);
    lua_pop(co, nres);
    return 1;
  }
  if (st == LUA_OK) {
    if (nres > 0) set_result(co, -1);
    lua_pop(co, nres);
    luaL_unref(GL, LUA_REGISTRYINDEX, ref);
    return 0;
  }
  set_result(co, -1);
  luaL_unref(GL, LUA_REGISTRYINDEX, ref);
  return 2;
}

/* --- aislamiento del fallo: ¿es el anidamiento del trampolín en sí? --------- */

static int inner_trivial(lua_State *L) { lua_pushinteger(L, 7); return 1; }

/* nivel 2 puro en C: lua_pcall(inner) desde dentro de otro lua_pcall */
static int mid_pcall(lua_State *L) {
  lua_pushcfunction(L, inner_trivial);
  int st = lua_pcall(L, 0, 1, 0);      /* rawrunprotected ANIDADO */
  lua_pushinteger(L, st == LUA_OK ? lua_tointeger(L, -1) : -st);
  return 1;
}

__attribute__((export_name("spike_test_nested")))
int spike_test_nested(void) {
  lua_pushcfunction(GL, mid_pcall);
  int st = lua_pcall(GL, 0, 1, 0);     /* nivel 1 */
  if (st != LUA_OK) { set_result(GL, -1); lua_pop(GL, 1); return -1; }
  int v = (int)lua_tointeger(GL, -1);
  lua_pop(GL, 1);
  return v;                             /* 7 si todo el sándwich funcionó */
}
