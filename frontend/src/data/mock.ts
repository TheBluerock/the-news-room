import type { Article, Review, SommarioItem } from '../lib/queries'

export const MOCK_HERO: Article = {
  _id: 'mock-hero',
  articleId: 'a0000000-0000-0000-0000-000000000001',
  slug: 'barolo-vendemmia-2025-ridisegna-le-langhe',
  market: 'italy',
  language: 'it',
  title: 'Barolo, la vendemmia 2025 ridisegna le Langhe',
  excerpt: 'Caldo asciutto, notti fresche, rese basse. Nove produttori raccontano un\'annata che obbliga a ripensare il tempo del Nebbiolo.',
  section: 'territori',
  byline: 'Caterina Rossi',
  tags: ['Nebbiolo', 'Vendemmia 2025', 'Langhe', 'Cru', 'Climate'],
  readingTime: 13,
  content: 'Il Nebbiolo, in Langa, è un orologio. Lo sanno bene i produttori delle dodici colline che da Verduno a Serralunga disegnano l\'arco del Barolo: novembre porta la potatura, marzo i lavori del verde, agosto la conta dei grappoli, ottobre la vendemmia. L\'annata 2025, però, ha rotto il quadrante.\n\nA maggio, le notti fredde hanno rallentato la fioritura di dieci giorni; l\'estate è arrivata di colpo, secca e ventilata, e ha richiesto irrigazioni di soccorso anche dove la vigna non l\'aveva mai chiesta. A settembre, una pioggia precisa ha rimesso a posto gli zuccheri, ma le rese sono basse: in media 38 ettolitri per ettaro contro i 52 del 2023.\n\nAbbiamo percorso le Langhe in nove tappe, una per produttrice. Ne è uscito un ritratto polifonico, in cui la parola più ripetuta è stata "attenzione".\n\nL\'orologio, dunque, si è rotto: ma è interessante notare come la maggior parte dei produttori abbia preferito non rimetterlo subito a posto. Si è scelta piuttosto la via dell\'ascolto: meno interventi, vinificazioni più lunghe, qualche fermentazione a temperatura libera.',
  qualityScore: 0.95,
  approvedAt: '2026-05-17T00:00:00Z',
}

export const MOCK_ARTICLES: Article[] = [
  MOCK_HERO,
  {
    _id: 'mock-1',
    articleId: 'a0000000-0000-0000-0000-000000000002',
    slug: 'arianna-occhipinti-il-vino-e-una-forma-di-disobbedienza',
    market: 'italy',
    language: 'it',
    title: 'Arianna Occhipinti: «Il vino è una forma di disobbedienza»',
    excerpt: 'A Vittoria, fra Frappato e Albanello, una conversazione lunga.',
    section: 'interviste',
    byline: 'Marco Ferretti',
    tags: ['Frappato', 'Sicilia', 'Biodinamico'],
    readingTime: 9,
    content: 'A Vittoria, fra Frappato e Albanello, una conversazione che dura tre ore.',
    qualityScore: 0.92,
    approvedAt: '2026-05-08T00:00:00Z',
  },
  {
    _id: 'mock-2',
    articleId: 'a0000000-0000-0000-0000-000000000003',
    slug: 'franciacorta-in-bicicletta-78-km-fra-le-bollicine',
    market: 'italy',
    language: 'it',
    title: 'Franciacorta in bicicletta, 78 km fra le bollicine',
    excerpt: 'Una mappa, dodici cantine, tre osterie. Pendenza media 2,4%.',
    section: 'itinerari',
    byline: 'Sofia Bianchi',
    tags: ['Franciacorta', 'Metodo classico', 'Itinerario'],
    readingTime: 7,
    content: 'Una mappa, dodici cantine, tre osterie e una pendenza media del 2,4%.',
    qualityScore: 0.89,
    approvedAt: '2026-05-04T00:00:00Z',
  },
  {
    _id: 'mock-3',
    articleId: 'a0000000-0000-0000-0000-000000000004',
    slug: 'soave-classico-garganega-torna-in-cima-al-vulcano',
    market: 'italy',
    language: 'it',
    title: 'Soave classico: il Garganega torna in cima al vulcano',
    excerpt: 'I cru del Foscarino e Monte Tenda, anno per anno.',
    section: 'territori',
    byline: 'Luca Monti',
    tags: ['Garganega', 'Soave', 'Veneto', 'Vulcanico'],
    readingTime: 8,
    content: 'I cru del Foscarino e Monte Tenda raccontati anno per anno.',
    qualityScore: 0.91,
    approvedAt: '2026-04-29T00:00:00Z',
  },
]

export const MOCK_REVIEWS: Review[] = [
  { producer: 'G.D. Vajra',   wine: 'Barolo Bricco delle Viole 2021', score: '95', region: 'Piemonte' },
  { producer: 'Cos',          wine: 'Pithos Rosso 2023',               score: '93', region: 'Sicilia'  },
  { producer: 'Pra',          wine: 'Soave Monte Grande 2023',          score: '92', region: 'Veneto'   },
  { producer: 'Foradori',     wine: 'Granato 2022',                     score: '94', region: 'Trentino' },
  { producer: 'Salvioni',     wine: 'Brunello di Montalcino 2020',      score: '96', region: 'Toscana'  },
  { producer: 'De Bartoli',   wine: 'Marsala Vergine Riserva 1988',     score: '97', region: 'Sicilia'  },
]

export const MOCK_SOMMARIO: SommarioItem[] = [
  { num: '004', label: { it: 'Editoriale',              en: 'Editorial',           zh: '编者按' },       page: '004' },
  { num: '012', label: { it: 'Barolo \'25',             en: 'Barolo \'25',         zh: '巴罗洛 2025' },  page: '012' },
  { num: '028', label: { it: 'Etna, sei contrade',      en: 'Etna, six contradas', zh: '埃特纳六小产区' }, page: '028' },
  { num: '044', label: { it: 'Intervista — Occhipinti', en: 'Interview — Occhipinti', zh: '访谈 奥基平蒂' }, page: '044' },
  { num: '058', label: { it: 'Franciacorta in bici',    en: 'Franciacorta by bike', zh: '骑行弗朗齐亚柯达' }, page: '058' },
  { num: '070', label: { it: 'Schede di degustazione',  en: 'Tasting notes',       zh: '品鉴卡' },       page: '070' },
  { num: '088', label: { it: 'Abbinamenti — pesce azzurro', en: 'Pairings — blue fish', zh: '餐酒 蓝鱼' }, page: '088' },
  { num: '102', label: { it: 'Guida acquisto',          en: 'Buying guide',        zh: '选购指南' },      page: '102' },
  { num: '118', label: { it: 'Agenda eventi',           en: 'Events agenda',       zh: '活动日程' },      page: '118' },
]

export const MOCK_TAGS = [
  'Nebbiolo', 'Sangiovese', 'Nerello Mascalese', 'Garganega', 'Glera', 'Pinot Nero',
  'metodo classico', 'macerazione', 'anfora', 'biodinamico', 'PIWI', 'vigne vecchie',
  'cru', 'vendemmia tardiva', 'rosato', 'passito', 'agricoltura rigenerativa', 'calcare',
  'tufo', 'vulcanico', 'pet-nat', 'solfiti', 'assemblaggio', 'annata fresca',
]

export const MOCK_MOTTO = {
  it: ['LA PIÙ', 'AMPIA', 'GUIDA', 'AL VINO', 'D\'AUTORE'],
  en: ['THE', 'LARGEST', 'GUIDE', 'TO GROWER', 'WINE'],
  zh: ['作者派', '葡萄酒', '最广泛', '指南', '于此'],
}
