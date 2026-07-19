# Contribuir a `enu`

Gracias por el interés. `enu` es software libre bajo la [Apache License
2.0](LICENSE) y las aportaciones son bienvenidas: issues, ideas, reproducciones
de bugs y parches.

Antes de nada, lee la guía del proyecto: [CLAUDE.md](CLAUDE.md) (flujo de
trabajo, idioma y estilo), [docs/core/filosofia.md](docs/core/filosofia.md) (lo que `enu`
es y lo que no) y, si tocas código, [docs/plan/implementacion.md](docs/plan/implementacion.md)
(el protocolo de construcción). Todo el repositorio está en español; la API y
los identificadores, en inglés `snake_case`.

## Cómo abrir una Pull Request

1. Haz fork del repositorio y clónalo:

   ```
   gh repo fork dbareagimeno/enu --clone
   cd enu
   ```

2. Crea una rama descriptiva para tu cambio **desde `develop`** (la rama de
   integración y por defecto del repo; `main` queda reservada para versiones
   estables):

   ```
   git checkout -b mi-cambio develop
   ```

3. Haz tus cambios y comitea. Sigue el idioma y estilo del repo (ver
   [CLAUDE.md](CLAUDE.md)): documentos y mensajes de commit en español, API e
   identificadores en inglés `snake_case`.

4. Antes de empujar, deja el repo en verde localmente (ver [Calidad](#calidad)
   más abajo).

5. Empuja tu rama y abre la PR contra `develop`:

   ```
   git push -u origin mi-cambio
   gh pr create --base develop --title "..." --body "..."
   ```

   (o, sin `gh`, desde la web de tu fork con el botón "Compare & pull
   request").

6. Las ramas `main` y `develop` están protegidas: tu PR no se puede fusionar hasta que los
   checks de CI (`.github/workflows/ci.yml`) estén en verde y haya al menos
   una revisión aprobada. Si tu PR viene de un fork, el mantenedor debe
   aprobar manualmente que corra el primer CI (política de seguridad de
   GitHub para workflows de forks).

7. Si el cambio es grande o toca la API (`docs/contracts/api.md`), abre antes un issue
   para discutirlo — evita trabajo perdido si la dirección no encaja (ver
   [Titularidad y licencia](#titularidad-y-licencia-de-las-contribuciones)
   más abajo).

## Calidad

Toda aportación de código debe dejar el repositorio en verde:

- `go build ./...`
- `go test -race ./...`
- `go vet ./...` y `gofmt` sin diferencias
- `golangci-lint run` con la versión fijada en la CI

La integración continua (ver [`.github/workflows/ci.yml`](.github/workflows/ci.yml))
comprueba esto en cada Pull Request, en Linux y macOS. La API del core es
**sagrada** ([docs/contracts/api.md](docs/contracts/api.md)): crece solo por adición; si crees que
falta algo, ábrelo como discusión antes de implementarlo.

## Titularidad y licencia de las contribuciones

La política de contribuciones es el **DCO** (Developer Certificate of Origin,
v1.1), decidida en
[ADR-030](docs/decisions/adr/adr-030-politica-de-contribuciones-dco.md):

- **Conservas el copyright de tu aportación.** No se pide cesión de derechos
  ni la firma de un CLA — ni caso por caso ni en general. Tu código entra en
  el proyecto bajo la misma [Apache 2.0](LICENSE) que el resto.
- A cambio, **cada commit lleva `Signed-off-by:`** (`git commit -s`), que
  certifica que tienes derecho a aportar ese código bajo la licencia del
  proyecto (el texto del certificado: <https://developercertificate.org>).
  Si un commit llega sin sign-off, se te pedirá corregirlo, no se rechazará
  el cambio.
- Por defecto, **abre un issue para discutir** un cambio antes de enviar un
  Pull Request grande. Los parches pequeños y las correcciones evidentes
  pueden ir directos.

La titularidad del *proyecto* (nombre, dirección, releases) sigue siendo de su
autor original; `enu` es libre para usar, estudiar, modificar y distribuir
bajo Apache 2.0, y las aportaciones externas entran como Apache 2.0 puro, sin
acuerdos adicionales.
