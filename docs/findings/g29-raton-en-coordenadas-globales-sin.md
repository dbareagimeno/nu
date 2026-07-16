# G29 · Ratón en coordenadas globales sin traducción a región (hit-testing) — `api.md` §9.1/§9.3 — **RESUELTO**

**Resolución** (aplicada en [guia-plugins.md](guia-plugins.md) §6): opción
(c) — el mapeo pantalla→contenido es del **toolkit**, no del core, por el
mismo reparto que G1 (relayout) y G22 (theming): lo que depende del layout
que el plugin posee es del plugin. La razón decisiva es que `Region:hit` (a)
solo podría hacer la **mitad trivial** — restar el origen `x,y` que el plugin
mismo fijó —, mientras la mitad valiosa (qué bloque/línea de un Block
envuelto y **scrolleado** se clicó) necesita el offset de scroll y el layout
del contenido, que el core no retiene (el blit de G28 es efímero). Añadir
`Region:hit` sería superficie sagrada para lo que el plugin ya tiene gratis,
y además ignoraría z-order/oclusión (una región tapada devolvería coords
igual). Descartada (b) entregar el ratón en coordenadas locales: rutear por
geometría dentro del core es meter un trozo de toolkit en el kernel, contra
el modelo de pila de §9.3. Si el toolkit demuestra que repite el mismo
cálculo en todas partes, *entonces* se promueve una primitiva — con
evidencia, no por adelantado.

**Problema.** El evento de ratón (`ev.type == "mouse"`) trae `x, y` en
coordenadas de **pantalla**, pero las regiones viven en coordenadas
**locales** (y su contenido, además, desplazado por el scroll de G28). No
hay `Region:contains(x,y)` ni traducción global→local. Para clicar un
widget — la cabecera de un bloque de tool para plegarlo, un botón de un
modal — el plugin rastrea a mano la geometría de cada región (que él mismo
fijó) y resuelve el hit-test sumando/restando origen y offset (ronda 6,
escenario 31).

**Impacto.** Todo widget clicable del toolkit reimplementa el mismo
cálculo; fricción repetida en la capa que más lo va a usar.

**Opciones.** (a) `Region:hit(x, y) -> (bx, by) | nil` — traduce
pantalla→local y devuelve `nil` si el punto cae fuera (con G28, contando el
offset de scroll); (b) entregar el evento de ratón ya en coordenadas
locales a la región bajo el puntero (cambia el modelo de pila de input de
§9.3, que hoy es global y por consumo); (c) documentar que el mapeo es
responsabilidad del toolkit, ya que el plugin conoce la geometría que fijó
(barato, pero deja el hit-test fuera del core para siempre).
