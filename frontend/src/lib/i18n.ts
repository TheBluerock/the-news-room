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

export const UI = {
  coverStory: { it: 'IN COPERTINA', en: 'COVER STORY', zh: '封面报道' },
  readStory:  { it: 'LEGGI L\'ARTICOLO', en: 'READ THE STORY', zh: '阅读全文' },
  today:      { it: 'GUIDA DEL GIORNO', en: 'TODAY\'S GUIDE', zh: '今日导览' },
  search:     { it: 'Cerca', en: 'Search', zh: '搜索' },
  searchPh:   { it: 'Vitigno, regione, cantina…', en: 'Grape, region, winery…', zh: '品种、产区、酒庄…' },
  channels:   { it: 'Canali', en: 'Channels', zh: '频道' },
  opening:    { it: 'In apertura', en: 'Opening', zh: '卷首' },
  issueNo:    { it: 'NUMERO 001', en: 'ISSUE 001', zh: '第 001 期' },
  newGuide:   { it: 'La nuova guida', en: 'The new guide', zh: '新版指南' },
  wineMap:    { it: 'Carta del vino 2026', en: 'Wine map 2026', zh: '2026 酒图' },
  howToVisit: { it: 'COME VISITARE', en: 'HOW TO VISIT', zh: '如何参观' },
  contents:   { it: 'SOMMARIO', en: 'CONTENTS', zh: '目录' },
  pages:      { it: 'Pagine 004 — 132', en: 'Pages 004 — 132', zh: '第 004 — 132 页' },
  tastingNotes: { it: 'SCHEDE DI DEGUSTAZIONE', en: 'TASTING NOTES', zh: '品鉴笔记' },
  tagsHead:   { it: 'TEMI · TAG · INDICE ANALITICO', en: 'TOPICS · TAGS · INDEX', zh: '专题 · 标签 · 索引' },
  weekIn:     { it: 'LA SETTIMANA', en: 'THE WEEK', zh: '本周' },
  weekSub:    { it: 'in un calice — ogni venerdì alle 17', en: 'in a glass — every Friday at 5pm', zh: '尽在一杯酒中 — 每周五 17:00' },
  subscribe:  { it: 'Iscriviti', en: 'Subscribe', zh: '订阅' },
  subscribed: { it: 'Grazie. Ti scriveremo venerdì.', en: 'Thank you. We\'ll write to you on Friday.', zh: '感谢订阅。我们将于本周五与您联系。' },
  gdpr:       { it: 'Trattamento dati ai sensi del Reg. UE 2016/679 · vedi privacy policy', en: 'Data handled under EU Reg. 2016/679 · see privacy policy', zh: '依据欧盟 2016/679 号条例处理个人数据 · 详见隐私政策' },
  newsletter: { it: 'Una newsletter sobria: tre articoli, un vino della settimana, due eventi. Niente push, niente spam, possibilità di disiscriversi con un clic.', en: 'A sober newsletter: three articles, one wine of the week, two events. No push, no spam, one-click unsubscribe.', zh: '一份克制的邮件简报：三篇文章、本周一款酒、两场活动。无推送、无垃圾邮件、一键退订。' },
  emailPh:    { it: 'la-tua@email.it', en: 'your@email.com', zh: 'your@email.com' },
  adLabel:    { it: 'PUBBLICITÀ', en: 'ADVERTISING', zh: '广告' },
  archive:    { it: 'Arretrati', en: 'Archive', zh: '往期' },
  loadMore:   { it: 'Carica altri articoli', en: 'Load more articles', zh: '加载更多文章' },
  section:    { it: 'SEZIONE', en: 'SECTION', zh: '栏目' },
  date:       { it: 'DATA', en: 'DATE', zh: '日期' },
  readTime:   { it: 'TEMPO DI LETTURA', en: 'READING TIME', zh: '阅读时长' },
  keepReading:{ it: 'CONTINUA A LEGGERE', en: 'KEEP READING', zh: '继续阅读' },
  share:      { it: 'CONDIVIDI', en: 'SHARE', zh: '分享' },
} satisfies Record<string, I18nText>
