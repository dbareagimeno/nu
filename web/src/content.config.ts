import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

// Cuatro colecciones de contenido, todas con el glob loader:
//
//  - `wiki`: los .md REALES del repo bajo docs/ (fuente de verdad de la
//    documentación). Sin frontmatter — schema laxo/opcional. Se excluye
//    README.md (es el mapa por capas, no una página de la wiki).
//  - `empezar`: las páginas de "empezar" locales, con frontmatter
//    title/description.
//  - `extensiones`: páginas locales de las extensiones oficiales que no tienen
//    contrato propio en docs/ (mcp, repl, toolkit) más el índice (extensiones),
//    con frontmatter title/description como `empezar`.
//  - `referencia`: los 16 .md de la referencia de la API, con frontmatter
//    title/description. NO se tocan: el detector check-drift y el CI dependen
//    de ellos.
const wiki = defineCollection({
  loader: glob({ pattern: ['*.md', '!README.md'], base: '../docs' }),
  // Los .md del repo no llevan frontmatter: todo opcional.
  schema: z.object({
    title: z.string().optional(),
    description: z.string().optional(),
  }),
});

const empezar = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/docs/empezando' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

const extensiones = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/docs/extensiones' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

const referencia = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/docs/referencia' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

// Colecciones EN (W-04): instantánea traducida del contenido ES bajo
// src/content/en/. Mismos slugs y mismos schemas que sus gemelas: `wiki_en` sin
// frontmatter (los .md de docs/ traducidos no lo llevan), el resto con
// title/description ya traducido. Las páginas EN (/en/docs, /en/api) las
// consumen; check-drift sigue mirando SOLO la referencia ES.
const wiki_en = defineCollection({
  loader: glob({ pattern: ['*.md', '!README.md'], base: './src/content/en/wiki' }),
  schema: z.object({
    title: z.string().optional(),
    description: z.string().optional(),
  }),
});

const empezar_en = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/en/empezando' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

const extensiones_en = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/en/extensiones' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

const referencia_en = defineCollection({
  loader: glob({ pattern: '*.md', base: './src/content/en/referencia' }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
  }),
});

export const collections = {
  wiki,
  empezar,
  extensiones,
  referencia,
  wiki_en,
  empezar_en,
  extensiones_en,
  referencia_en,
};
