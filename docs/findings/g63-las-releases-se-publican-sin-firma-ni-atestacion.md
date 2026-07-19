---
title: "Las releases se publican sin firma ni atestación de procedencia: el checksum viaja por el mismo canal que el binario"
type: "hallazgo"
id: "G63"
status: "abierto"
date: "2026-07-19"
origin: "SEC-06 de la auditoría de seguridad 2026-07-16, elevado a grieta por decisión del operador en la auditoría del feedback 2026-07-19 (tensión T5)"
affected: ["release.yml", "install.sh", "docs/ops/release.md", "adr-013"]
---
# G63 · Las releases se publican sin firma ni atestación de procedencia — release.yml / install.sh / ADR-013

**Problema.** `install.sh` verifica el SHA-256 del tarball, pero
`checksums.txt` se descarga **del mismo release de GitHub que el binario**:
quien comprometa el canal de publicación (la cuenta, el workflow, o GitHub
mismo como punto único) puede falsificar ambos a la vez. No hay firma de tags
ni de binarios, ni atestación de procedencia del build, ni SBOM: un usuario no
puede responder con evidencia criptográfica «quién construyó este binario,
desde qué commit y con qué dependencias» — solo confiar en el canal. La
[auditoría de seguridad 2026-07-16](../audits/auditoria-seguridad-2026-07-16.md)
lo registró como **SEC-06** y lo triaje como bug de infraestructura fuera del
flujo `G##`; [ADR-013](../decisions/adr/adr-013-integracion-continua-y-publicacion.md)
§5 dejó la firma (cosign/GPG) «como mejora futura», y `release.yml` fija
`provenance: false` de forma deliberada («el binario ya es reproducible y el
runbook lo verifica»).

**Por qué ahora es grieta.** El operador **invirtió ese triaje** el 2026-07-19
(auditoría del [feedback «10/10»](../audits/auditoria-feedback-10-de-10-2026-07-19.md),
tensión T5): con el reposicionamiento de ADR-025 —enu como infraestructura que
terceros despliegan— la cadena de confianza de la release deja de ser DevOps
interno y pasa a ser **parte de la promesa del producto**. La base ya existe:
el build es reproducible (`CGO_ENABLED=0`, `-trimpath`, versión verificada
contra el tag) y los permisos de los workflows son mínimos; lo que falta es la
evidencia verificable por el consumidor.

**Opciones sobre la mesa** (pendientes de discusión, no decididas):

- **(a) Atestación nativa de GitHub** (artifact attestations, SLSA provenance
  con `id-token: write` + `attestations: write`): coste mínimo, sin gestión de
  claves propia; verificación con `gh attestation verify`. Cubre procedencia;
  no cubre la firma de tags.
- **(b) cosign keyless** (OIDC de Actions, Sigstore/Rekor) firmando tarballs,
  `checksums.txt` y la imagen GHCR (ADR-028): verificación sin depender de
  GitHub como único canal; añade dependencia del ecosistema Sigstore.
- **(c) GPG con clave del mantenedor** (tags y checksums firmados): máxima
  autonomía, pero introduce gestión y rotación manual de claves — el coste que
  ADR-013 quiso evitar.
- **SBOM por release** (p. ej. `go version -m` empaquetado, o syft) puede
  acompañar a cualquiera de las tres.

**Al resolverse** tocará `release.yml`, `install.sh` (verificación opcional de
la firma/atestación con degradación honesta si la herramienta no está),
`docs/ops/release.md` (pasos y verificación) y cerrará formalmente el
disparador de ADR-013 §5 — probablemente con un ADR propio que registre la
elección. La auditoría externa de seguridad pre-1.0 que pide el feedback se
evalúa como paso final de esta misma grieta.

**Disparador de reapertura.** — (abierta).
