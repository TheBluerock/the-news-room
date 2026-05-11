import type { it } from './it'

export const en = {
  seo: {
    homeTitle:       'ENOICA — Italian Wine Magazine · 001',
    homeDescription: 'ENOICA: the Italian wine magazine. Tastings, territories, wineries, travel and a guide to drinking well. Newsletter every Friday.',
  },

  header: {
    liveEdition:  'LIVE EDITION',
    archive:      'Archive',
    home:         'Home',
    tagline:      'Online magazine · new articles every Friday',
    mastheadSub:  'Online magazine on wine · wine travel · drinking culture',
    publishedFrom:'Published from Milan, Verona and Palermo',
    nav:          ['Newsletter', 'Podcast', 'Events', 'Contact'] as [string, string, string, string],
  },

  footer: {
    col: {
      magazine:  'Magazine',
      sections:  'Sections',
      services:  'Services',
      events:    'Events',
      publisher: 'Publisher',
    },
    links: {
      editorial:     'Editorial',
      issues:        'Issues',
      subscriptions: 'Subscriptions',
      newsstand:     'Newsstand',
      slowWine:      'Slow Wine Fair',
      vinitaly:      'Vinitaly',
      cernilli:      'Cernilli Tasting',
      calendar:      'Calendar',
      about:         'About',
      careers:       'Careers',
      advertising:   'Advertising',
      privacy:       'Privacy',
    },
    tagline: 'Online magazine · updated every Friday',
    editor:  'Editor-in-chief: G. Roveda',
  },

  editorial: {
    today:          'TODAY',
    todayGuideLabel:'TODAY\'S GUIDE',
    todayGuide:     'Tasting visit in Valpolicella · 6pm',
    search:         'Search',
    searchPh:       'Grape, region, winery…',
    channels:       'Channels',
    opening:        'Opening',
    issueNo:        'ISSUE 001',
    newGuide:       'The new guide',
    wineMap:        'Wine map 2026',
    newGuideQuote:  '"An atlas of terroirs, grapes and people. Fourteen stops to take slowly, from Collio to Etna."',
  },

  hero: {
    coverStory: 'COVER STORY',
    readStory:  'READ THE STORY',
  },

  motto: {
    howToVisit: 'HOW TO VISIT',
    open:       'Open to visitors',
    small:      '14 stops · 9 regions · 312 producers surveyed — an atlas for travellers with a notebook.',
    visitBody:  'May to September, Tuesday to Saturday 10am–6pm. The guided tasting (€18) includes five wines, local bread and an Ascolana olive. Booking recommended.',
  },

  sommario: {
    contents: 'CONTENTS',
    pages:    'Pages 004 — 132',
  },

  reviews: {
    tastingNotes: 'TASTING NOTES',
    desc:         'Six wines blind-tasted by the ENOICA panel — score out of 100, region and vintage in focus.',
  },

  tags: {
    head: 'TOPICS · TAGS · INDEX',
  },

  newsletter: {
    weekIn:      'THE WEEK',
    weekSub:     'in a glass — every Friday at 5pm',
    body:        'A sober newsletter: three articles, one wine of the week, two events. No push, no spam, one-click unsubscribe.',
    placeholder: 'your@email.com',
    btn:         'Subscribe',
    ok:          '✓ Thank you. We\'ll write to you on Friday.',
    err:         'Error. Please try again shortly.',
    gdpr:        'Data handled under EU Reg. 2016/679 · see privacy policy',
  },

  category: {
    articlesPublished: 'articles published',
    gridView:          'GRID VIEW · 3 COL',
    filters:           ['Latest', 'Most read', 'Verticals', 'Long reads', 'Video'] as [string, string, string, string, string],
  },

  article: {
    section:       'SECTION',
    date:          'DATE',
    readTime:      'READING TIME',
    share:         'SHARE',
    keepReading:   'KEEP READING',
    loadMore:      'Load more articles',
    shareX:        'X · Twitter',
    shareWhatsApp: 'WhatsApp',
    shareWeChat:   'WeChat 微信',
    shareEmail:    'Email',
    tags:          'TAGS',
    adMpu:         'ADVERTISING · 300 × 250',
  },

  ads: {
    label:       'ADVERTISING',
    leaderboard: 'Sponsored space · 970 × 120 banner',
    mpu:         'Sponsored space · 300 × 250',
  },

  sectionSub: {
    degustazioni: 'Tasting notes, blind panels, verticals and horizontals.',
    cantine:      'Portraits of producers, from the Alps to Sicily.',
    itinerari:    'Vineyard travels on foot, by bike, by regional train.',
    territori:    'Maps, soils, microclimates. Italian regions told from the roots up.',
    abbinamenti:  'Food and wine: a grammar with a few useful exceptions.',
    eventi:       'Fairs, awards, previews: the calendar for the next six months.',
    interviste:   'Long conversations with those who make wine and those who write it.',
    guida:        'What to buy, where, at what price. An honest compass.',
    sostenibilita:'Soil, water, labour: the ethics behind the label.',
  },

  notFound: {
    backHome: 'Back to home',
  },
} satisfies typeof it
