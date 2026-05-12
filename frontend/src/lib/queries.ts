import { sanity, isSanityConfigured } from './sanity'
import { MOCK_HERO, MOCK_ARTICLES, MOCK_REVIEWS, MOCK_SOMMARIO, MOCK_TAGS } from '../data/mock'
import type { Lang } from './i18n'

export interface Article {
  _id: string
  articleId: string
  slug: string
  market: string
  language: Lang
  content: string
  title: string
  excerpt: string
  byline: string
  section: string
  tags: string[]
  readingTime: number
  qualityScore: number
  approvedAt: string
  image?: {
    url: string
    width: number
    height: number
  }
}

export interface Review {
  producer: string
  wine: string
  score: string
  region: string
}

export interface SommarioItem {
  num: string
  label: { it: string; en: string; zh: string }
  page: string
}

// Projected fields — assumes the Sanity schema has been enriched beyond the
// minimal ArticleDoc written by the sanity service (which only stores articleId,
// market, language, content, qualityScore, approvedAt). Fields like title,
// excerpt, section, byline, tags must be added to the schema + agent pipeline.
const FIELDS = `
  _id, articleId, "slug": slug.current, market, language,
  content, title, excerpt, byline, section, tags,
  "readingTime": round(length(coalesce(content, "")) / 1000),
  qualityScore, approvedAt,
  "image": coverImage.asset->{url, "width": metadata.dimensions.width, "height": metadata.dimensions.height}
`

export async function getHeroArticle(lang: Lang): Promise<Article> {
  if (!isSanityConfigured) return MOCK_HERO
  const r = await sanity.fetch(
    `*[_type=="article" && language==$lang] | order(approvedAt desc) [0] { ${FIELDS} }`,
    { lang }
  )
  return r ?? MOCK_HERO
}

export async function getRecentArticles(lang: Lang, limit = 4): Promise<Article[]> {
  if (!isSanityConfigured) return MOCK_ARTICLES.slice(0, limit)
  const r = await sanity.fetch(
    `*[_type=="article" && language==$lang] | order(approvedAt desc) [0...$limit] { ${FIELDS} }`,
    { lang, limit }
  )
  return r?.length ? r : MOCK_ARTICLES.slice(0, limit)
}

export async function getArticlesBySection(lang: Lang, section: string): Promise<Article[]> {
  if (!isSanityConfigured) return MOCK_ARTICLES
  const r = await sanity.fetch(
    `*[_type=="article" && language==$lang && section==$section] | order(approvedAt desc) [0...12] { ${FIELDS} }`,
    { lang, section }
  )
  return r?.length ? r : MOCK_ARTICLES
}

export async function getAllArticleSlugs(): Promise<{ lang: Lang; slug: string }[]> {
  if (!isSanityConfigured) {
    return MOCK_ARTICLES.map(a => ({ lang: a.language, slug: a.slug }))
  }
  return sanity.fetch(`*[_type=="article"] { "lang": language, "slug": slug.current }`)
}

export async function getArticleBySlug(slug: string, lang: Lang): Promise<Article | null> {
  if (!isSanityConfigured) return MOCK_ARTICLES.find(a => a.slug === slug) ?? MOCK_ARTICLES[0]
  return sanity.fetch(
    `*[_type=="article" && slug.current==$slug && language==$lang] [0] { ${FIELDS} }`,
    { slug, lang }
  )
}

export interface Ad {
  _id: string
  slot: 'leaderboard' | 'mpu'
  brand: string
  copy?: string
  url?: string
  markets: string[]
  languages: string[]
  sections: string[]
  priority: number
}

export async function getAd(lang: Lang, slot: 'leaderboard' | 'mpu', section?: string): Promise<Ad | null> {
  if (!isSanityConfigured) return null
  const today = new Date().toISOString().slice(0, 10)
  return sanity.fetch(
    `*[
      _type == "ad" &&
      active == true &&
      $lang in languages &&
      slot == $slot &&
      (!defined(startDate) || startDate <= $today) &&
      (!defined(endDate)   || endDate   >= $today) &&
      (count(sections) == 0 || $section in sections)
    ] | order(priority desc) [0]`,
    { lang, slot, today, section: section ?? '' }
  )
}

export { MOCK_REVIEWS as getReviews, MOCK_SOMMARIO as getSommario, MOCK_TAGS as getTags }
