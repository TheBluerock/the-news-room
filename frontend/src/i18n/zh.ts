import type { it } from './it'

export const zh = {
  seo: {
    homeTitle:       'ENOICA — 意大利葡萄酒杂志 · 001',
    homeDescription: 'ENOICA：意大利葡萄酒杂志。品鉴、产区、酒庄、旅行与饮酒指南。每周五简报。',
  },

  header: {
    liveEdition:  '实时版',
    archive:      '往期',
    home:         '首页',
    tagline:      '线上杂志 · 每周五更新',
    mastheadSub:  '线上杂志 · 葡萄酒 · 酒乡之旅 · 饮文化',
    publishedFrom:'由米兰、维罗纳与巴勒莫编辑出品',
    nav:          ['邮件订阅', '播客', '活动', '联系'] as [string, string, string, string],
  },

  footer: {
    col: {
      magazine:  '刊物',
      sections:  '栏目',
      services:  '服务',
      events:    '活动',
      publisher: '出版方',
    },
    links: {
      editorial:     '编辑部',
      issues:        '往期',
      subscriptions: '订阅',
      newsstand:     '订购',
      slowWine:      '慢酒展',
      vinitaly:      '维罗纳酒展',
      cernilli:      '切尔尼利品鉴',
      calendar:      '日历',
      about:         '关于',
      careers:       '招聘',
      advertising:   '广告',
      privacy:       '隐私',
    },
    tagline: '线上杂志 · 每周五更新',
    editor:  '主编：G. Roveda',
  },

  editorial: {
    today:          '今日',
    todayGuideLabel:'今日导览',
    todayGuide:     '瓦尔波利切拉酒庄品鉴 · 18:00',
    search:         '搜索',
    searchPh:       '品种、产区、酒庄…',
    channels:       '频道',
    opening:        '卷首',
    issueNo:        '第 001 期',
    newGuide:       '新版指南',
    wineMap:        '2026 酒图',
    newGuideQuote:  '「一份关于风土、品种与人的图集。十四站，从科利奥到埃特纳，慢行细品。」',
  },

  hero: {
    coverStory: '封面报道',
    readStory:  '阅读全文',
  },

  motto: {
    howToVisit: '如何参观',
    open:       '对外开放',
    small:      '14 站 · 9 个产区 · 312 家酒庄 — 一本携笔旅行者的图集。',
    visitBody:  '5 月至 9 月，周二至周六 10:00–18:00。导览品鉴（€18）含五款葡萄酒、本地面包及一颗阿斯科拉橄榄。建议预约。',
  },

  sommario: {
    contents: '目录',
    pages:    '第 004 — 132 页',
  },

  reviews: {
    tastingNotes: '品鉴笔记',
    desc:         'ENOICA 品鉴小组盲品的六款葡萄酒——百分制评分，标注产区与年份。',
  },

  tags: {
    head: '专题 · 标签 · 索引',
  },

  newsletter: {
    weekIn:      '本周',
    weekSub:     '尽在一杯酒中 — 每周五 17:00',
    body:        '一份克制的邮件简报：三篇文章、本周一款酒、两场活动。无推送、无垃圾邮件、一键退订。',
    placeholder: 'your@email.com',
    btn:         '订阅',
    ok:          '✓ 感谢订阅。我们将于本周五与您联系。',
    err:         '出现错误，请稍后重试。',
    gdpr:        '依据欧盟 2016/679 号条例处理个人数据 · 详见隐私政策',
  },

  category: {
    articlesPublished: '已发布',
    gridView:          '网格视图 · 三列',
    filters:           ['最新', '最热', '垂直评测', '长读', '影像'] as [string, string, string, string, string],
  },

  article: {
    section:       '栏目',
    date:          '日期',
    readTime:      '阅读时长',
    share:         '分享',
    keepReading:   '继续阅读',
    loadMore:      '加载更多文章',
    shareX:        'X · Twitter',
    shareWhatsApp: 'WhatsApp',
    shareWeChat:   'WeChat 微信',
    shareEmail:    'Email',
    tags:          '标签',
    adMpu:         '广告 · 300×250',
  },

  ads: {
    label:       '广告',
    leaderboard: '赞助位 · 970×120 横幅',
    mpu:         '赞助位 · 300×250',
  },

  sectionSub: {
    degustazioni: '品鉴笔记、盲品评测、纵深与横向对比。',
    cantine:      '酒庄主人物志，自阿尔卑斯至西西里。',
    itinerari:    '徒步、骑行、乘地方铁路的酒旅。',
    territori:    '地图、土壤、微气候。从根脉处讲述意大利产区。',
    abbinamenti:  '菜与酒：一套语法，几条有用的例外。',
    eventi:       '展会、奖项、首发：未来六个月日程。',
    interviste:   '与酿酒人与书写者的长篇对谈。',
    guida:        '买什么、何处买、几何价。一份诚实的指南。',
    sostenibilita:'土壤、水、劳作：酒标背后的伦理。',
  },

  notFound: {
    backHome: '返回首页',
  },
} satisfies typeof it
