export type Lang = 'it' | 'en' | 'zh'
export const LANGS: Lang[] = ['it', 'en', 'zh']

export interface I18nText { it: string; en: string; zh: string }

export function t(text: I18nText | string, lang: Lang): string {
  if (typeof text === 'string') return text
  return text[lang] || text.it || ''
}

export const SECTIONS_PRIMARY = [
  { id: 'degustazioni', it: 'Degustazioni', en: 'Tastings',    zh: '品鉴' },
  { id: 'cantine',      it: 'Cantine',      en: 'Producers',   zh: '酒庄' },
  { id: 'itinerari',    it: 'Itinerari',    en: 'Itineraries', zh: '酒旅' },
  { id: 'territori',    it: 'Territori',    en: 'Terroirs',    zh: '风土' },
  { id: 'abbinamenti',  it: 'Abbinamenti',  en: 'Pairings',    zh: '餐酒' },
] as const

export const SECTIONS_SECONDARY = [
  { id: 'eventi',        it: 'Eventi',         en: 'Events',         zh: '活动' },
  { id: 'interviste',    it: 'Interviste',      en: 'Interviews',     zh: '人物' },
  { id: 'guida',         it: 'Guida acquisto',  en: 'Buying guide',   zh: '选购指南' },
  { id: 'sostenibilita', it: 'Sostenibilità',   en: 'Sustainability', zh: '可持续' },
] as const

export const ALL_SECTION_IDS = [
  'degustazioni', 'cantine', 'itinerari', 'territori', 'abbinamenti',
  'eventi', 'interviste', 'guida', 'sostenibilita',
] as const

export type SectionId = typeof ALL_SECTION_IDS[number]

export function isSectionId(slug: string): slug is SectionId {
  return (ALL_SECTION_IDS as readonly string[]).includes(slug)
}

export function switchLang(currentPath: string, fromLang: Lang, toLang: Lang): string {
  if (currentPath === `/${fromLang}` || currentPath === `/${fromLang}/`) {
    return `/${toLang}/`
  }
  return currentPath.replace(`/${fromLang}/`, `/${toLang}/`)
}

