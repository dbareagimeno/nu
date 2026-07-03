/* Compuerta del spike: ¿puede el trampolín de desenrollado (estilo emscripten
   invoke_*) funcionar sobre wazero? Valida, SIN Lua todavía:
     1. reentrancia host<->wasm (Go llama wasm, wasm llama host, host llama wasm);
     2. un "throw" (pánico Go en el host) recuperado en el host intermedio deja
        el MÓDULO USABLE y el __stack_pointer restaurado;
     3. anidamiento de dos niveles de try. */

extern int  spike_host_try(int depth);   /* import: corre t_body(depth) protegido */
extern void spike_host_throw(void);      /* import: paniquea en Go (no retorna) */

__attribute__((import_module("spike"), import_name("host_try")))
extern int spike_host_try(int depth);
__attribute__((import_module("spike"), import_name("host_throw")))
extern void spike_host_throw(void);

/* consume stack de verdad para detectar corrupcion del shadow-stack */
static int burn(int n, volatile int *acc) {
  volatile int local[32];
  for (int i = 0; i < 32; i++) local[i] = n + i;
  *acc += local[31];
  if (n <= 0) return *acc;
  return burn(n - 1, acc);
}

__attribute__((export_name("t_body")))
void t_body(int depth) {
  volatile int acc = 0;
  burn(8, &acc);
  if (depth == 1) spike_host_throw();       /* throw simple */
  if (depth == 2) {
    int st = spike_host_try(1);             /* try ANIDADO que lanza dentro */
    if (st != 1) spike_host_throw();        /* mal status: revienta hacia fuera */
    /* recuperado: seguimos vivos y NO relanzamos */
  }
}

__attribute__((export_name("t_outer")))
int t_outer(int depth) {
  volatile int acc = 0;
  burn(4, &acc);
  int st = spike_host_try(depth);
  burn(4, &acc);            /* si el shadow-stack quedo mal, esto lo delata */
  return st * 100 + (acc > 0 ? 1 : 0);
}
