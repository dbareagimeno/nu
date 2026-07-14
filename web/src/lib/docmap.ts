// El mapa de la wiki: fuente única del orden lineal y de los grupos. Alimenta el
// sidebar (grupos), la navegación n/p (prev/next), la posición `n/22` de la
// statusline y —vía la ruta git de cada entrada— el bloque «última edición» del
// carril derecho. Fases posteriores (404) lo reutilizan como manifiesto.
//
// Orden y grupos EXACTOS (handoff §12a). El slug es el fichero sin `.md`. Las
// seis primeras salen de la colección `empezar` (páginas locales con
// frontmatter); el resto de `wiki` (los .md reales del repo bajo docs/).

export type GrupoId = 'empezar' | 'espec' | 'extensiones' | 'proceso';
export type Coleccion = 'wiki' | 'empezar';

export interface DocEntry {
  /** fichero sin `.md`, y segmento de la URL /docs/<slug> */
  slug: string;
  /** colección de contenido de la que sale */
  collection: Coleccion;
  /** grupo al que pertenece en el sidebar */
  grupo: GrupoId;
  /** ruta del fichero fuente relativa a la raíz del repo (para gitMeta) */
  gitPath: string;
}

export interface Grupo {
  id: GrupoId;
  /** clave i18n de la etiqueta del grupo (s1..s4) */
  i18nKey: 's1' | 's2' | 's3' | 's4';
  slugs: string[];
}

// Definición declarativa: grupo → clave i18n → slugs en orden.
const DEF: { id: GrupoId; i18nKey: Grupo['i18nKey']; collection: Coleccion; slugs: string[] }[] = [
  {
    id: 'empezar',
    i18nKey: 's1',
    collection: 'empezar',
    slugs: ['que-es-nu', 'instalacion', 'inicio-rapido', 'primer-script', 'primer-agente', 'conceptos'],
  },
  {
    id: 'espec',
    i18nKey: 's2',
    collection: 'wiki',
    slugs: ['filosofia', 'arquitectura', 'modelo-ejecucion', 'api', 'adr'],
  },
  {
    id: 'extensiones',
    i18nKey: 's3',
    collection: 'wiki',
    slugs: ['providers', 'agente', 'sesiones', 'chat', 'guia-plugins', 'malla'],
  },
  {
    id: 'proceso',
    i18nKey: 's4',
    collection: 'wiki',
    slugs: ['problemas', 'pospuesto', 'pseudocodigo', 'implementacion', 'decisiones-implementacion'],
  },
];

// El `base` del sitio es '/nu' SIN barra final (astro.config). Se normaliza
// aquí para construir URLs correctas (`/nu/docs/<slug>`) sea cual sea la forma
// del valor: robustece frente a un `base` con o sin barra final. Igual que hace
// el plugin remark de la wiki con sus enlaces.
const BASE = import.meta.env.BASE_URL.replace(/\/$/, '');

/** URL absoluta (con base) de la página de un slug. */
export function urlDoc(slug: string): string {
  return `${BASE}/docs/${slug}`;
}

function gitPath(collection: Coleccion, slug: string): string {
  return collection === 'wiki'
    ? `docs/${slug}.md`
    : `web/src/content/docs/empezando/${slug}.md`;
}

// Grupos con sus slugs, para el sidebar/drawer.
export const GRUPOS: Grupo[] = DEF.map((d) => ({ id: d.id, i18nKey: d.i18nKey, slugs: d.slugs }));

// Lista lineal en orden de lectura: alimenta prev/next y la posición n/22.
export const DOCS: DocEntry[] = DEF.flatMap((d) =>
  d.slugs.map((slug) => ({
    slug,
    collection: d.collection,
    grupo: d.id,
    gitPath: gitPath(d.collection, slug),
  })),
);

export const TOTAL = DOCS.length; // 22

const PORSLUG = new Map(DOCS.map((d, i) => [d.slug, i]));

/** Índice lineal (0-based) de un slug, o -1 si no existe. */
export function indiceDe(slug: string): number {
  return PORSLUG.has(slug) ? (PORSLUG.get(slug) as number) : -1;
}

/** Entrada del mapa para un slug. */
export function docDe(slug: string): DocEntry | undefined {
  const i = indiceDe(slug);
  return i >= 0 ? DOCS[i] : undefined;
}
