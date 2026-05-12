import { defineType, defineField } from 'sanity'

const MARKETS   = ['italy', 'usa', 'china']
const LANGUAGES = ['it', 'en', 'zh']
const SECTIONS  = [
  'territori', 'degustazioni', 'itinerari', 'interviste',
  'abbinamenti', 'produttori', 'mercati', 'cultura',
]

export const article = defineType({
  name: 'article',
  title: 'Article',
  type: 'document',

  fields: [
    defineField({
      name: 'articleId',
      title: 'Article ID',
      type: 'string',
      description: 'UUID from the agent pipeline — idempotency key.',
      readOnly: true,
      validation: R => R.required(),
    }),
    defineField({
      name: 'slug',
      title: 'Slug',
      type: 'slug',
      options: { source: 'title', maxLength: 96 },
      validation: R => R.required(),
    }),
    defineField({
      name: 'market',
      title: 'Market',
      type: 'string',
      options: { list: MARKETS, layout: 'radio' },
      validation: R => R.required(),
    }),
    defineField({
      name: 'language',
      title: 'Language',
      type: 'string',
      options: { list: LANGUAGES, layout: 'radio' },
      validation: R => R.required(),
    }),
    defineField({
      name: 'section',
      title: 'Section',
      type: 'string',
      options: { list: SECTIONS },
      validation: R => R.required(),
    }),
    defineField({
      name: 'title',
      title: 'Title',
      type: 'string',
      validation: R => R.required().max(120),
    }),
    defineField({
      name: 'excerpt',
      title: 'Excerpt',
      type: 'text',
      rows: 3,
      validation: R => R.required().max(300),
    }),
    defineField({
      name: 'byline',
      title: 'Byline',
      type: 'string',
      validation: R => R.required(),
    }),
    defineField({
      name: 'tags',
      title: 'Tags',
      type: 'array',
      of: [{ type: 'string' }],
      options: { layout: 'tags' },
    }),
    defineField({
      name: 'content',
      title: 'Content',
      type: 'text',
      rows: 20,
      validation: R => R.required(),
    }),
    defineField({
      name: 'coverImage',
      title: 'Cover Image',
      type: 'image',
      options: { hotspot: true },
      description: 'Assign before publishing. Required for article pages.',
      fields: [
        defineField({
          name: 'alt',
          title: 'Alt text',
          type: 'string',
          validation: R => R.required(),
        }),
        defineField({
          name: 'caption',
          title: 'Caption',
          type: 'string',
        }),
      ],
    }),
    defineField({
      name: 'qualityScore',
      title: 'Quality Score',
      type: 'number',
      readOnly: true,
      description: 'Set by moderation pipeline (0–1).',
    }),
    defineField({
      name: 'approvedAt',
      title: 'Approved At',
      type: 'datetime',
      readOnly: true,
    }),
  ],

  preview: {
    select: {
      title:    'title',
      subtitle: 'byline',
      media:    'coverImage',
      section:  'section',
      lang:     'language',
    },
    prepare({ title, subtitle, media, section, lang }) {
      return {
        title,
        subtitle: `[${lang?.toUpperCase()}] ${section ?? ''} · ${subtitle ?? ''}`,
        media,
      }
    },
  },
})
