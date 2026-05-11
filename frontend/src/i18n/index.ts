import type { Lang } from '../lib/i18n'
import { it } from './it'
import { en } from './en'
import { zh } from './zh'

export type Translations = typeof it
export type NewsletterCopy = Translations['newsletter']

const dict = { it, en, zh } satisfies Record<Lang, Translations>

export function useT(lang: Lang): Translations {
  return dict[lang]
}

// Re-export routing helpers so pages can import everything from one place
export { LANGS, SECTIONS_PRIMARY, SECTIONS_SECONDARY, ALL_SECTION_IDS, isSectionId, switchLang, t } from '../lib/i18n'
export type { Lang, I18nText, SectionId } from '../lib/i18n'
