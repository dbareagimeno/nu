# Makefile de nu-docker: envoltorio fino sobre docker-compose.yml para
# compilar/testear/ejecutar `nu` sin instalar Go en el host. No sustituye al
# build nativo del README (`CGO_ENABLED=0 go build ...`); es la vía Docker
# para quien no quiere tocar su toolchain local.
#
# Uso típico:
#   make build   # compila y deja el binario en ../dist/nu
#   make test    # go test -race (mismo filtro que ci.yml)
#   make blob    # regenera internal/vmwasm/nu.wasm (build.sh hermético)
#   make shell   # contenedor de desarrollo con el repo montado
#   make run     # ejecuta el nu ya compilado (chat interactivo)
#   make clean   # borra imágenes, cachés y ../dist

COMPOSE ?= docker compose
REPO_ROOT := $(abspath ..)

.PHONY: help build image test blob shell run run-agent clean

help:
	@echo "Targets disponibles:"
	@echo "  build      - compila nu (bind mount) -> $(REPO_ROOT)/dist/nu"
	@echo "  image      - construye la imagen final autocontenida (target runtime)"
	@echo "  test       - go test -race sobre el arbol (excluye internal/vmwasm)"
	@echo "  blob       - regenera internal/vmwasm/nu.wasm (build.sh hermetico)"
	@echo "  shell      - shell de desarrollo interactiva con el repo montado"
	@echo "  run        - ejecuta nu compilado en modo interactivo (chat)"
	@echo "  run-agent  - ejecuta nu headless: nu -e '<lua>' (usa CMD=\"...\")"
	@echo "  clean      - borra imagenes/cachés/volumenes de este compose y dist/"

# Compila con bind mount: el binario aterriza directamente en dist/ del repo,
# sin docker cp. Reusa la caché de módulos/build entre invocaciones (volúmenes
# nombrados go-mod-cache/go-build-cache).
build:
	@mkdir -p "$(REPO_ROOT)/dist"
	$(COMPOSE) run --rm build
	@echo "binario en $(REPO_ROOT)/dist/nu"

# Construye la imagen final (multi-stage completo, sin bind mount): la vía
# reproducible para publicar/distribuir una imagen de nu.
image:
	$(COMPOSE) build nu

test:
	$(COMPOSE) run --rm test

# Regenera el blob wasm directamente sobre el repo del host (bind mount): tras
# correrlo, `git status` en la raíz del repo debe mostrar el diff de
# internal/vmwasm/nu.wasm si las fuentes cambiaron.
blob:
	$(COMPOSE) run --rm blob

shell:
	$(COMPOSE) run --rm dev

# Chat interactivo sobre NU_WORKSPACE (por defecto, el propio repo). Ejemplo
# para apuntar a otro proyecto: `make run NU_WORKSPACE=/ruta/a/mi/proyecto`.
run:
	NU_WORKSPACE=$(or $(NU_WORKSPACE),$(REPO_ROOT)) $(COMPOSE) run --rm nu

# Agente headless de un tiro, ej.: make run-agent CMD="return nu.version.api"
run-agent:
	NU_WORKSPACE=$(or $(NU_WORKSPACE),$(REPO_ROOT)) $(COMPOSE) run --rm nu -e "$(CMD)"

clean:
	$(COMPOSE) down -v --rmi local
	rm -rf "$(REPO_ROOT)/dist"
