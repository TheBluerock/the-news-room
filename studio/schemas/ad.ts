import { defineType, defineField } from 'sanity'

export const ad = defineType({
  name: 'ad',
  title: 'Ad',
  type: 'document',

  fields: [
    defineField({
      name: 'slot',
      title: 'Slot',
      type: 'string',
      options: { list: ['leaderboard', 'mpu'], layout: 'radio' },
      validation: R => R.required(),
    }),
    defineField({
      name: 'brand',
      title: 'Brand / Headline',
      type: 'string',
      validation: R => R.required().max(80),
    }),
    defineField({
      name: 'copy',
      title: 'Body copy',
      type: 'string',
      validation: R => R.max(160),
    }),
    defineField({
      name: 'url',
      title: 'Click URL',
      type: 'url',
    }),
    defineField({
      name: 'image',
      title: 'Creative',
      type: 'image',
      options: { hotspot: false },
    }),
    defineField({
      name: 'markets',
      title: 'Markets',
      type: 'array',
      of: [{ type: 'string' }],
      options: { list: ['italy', 'usa', 'china'], layout: 'grid' },
      description: 'Empty = all markets.',
    }),
    defineField({
      name: 'languages',
      title: 'Languages',
      type: 'array',
      of: [{ type: 'string' }],
      options: { list: ['it', 'en', 'zh'], layout: 'grid' },
      validation: R => R.required().min(1),
    }),
    defineField({
      name: 'sections',
      title: 'Sections',
      type: 'array',
      of: [{ type: 'string' }],
      options: {
        list: [
          'territori', 'degustazioni', 'itinerari', 'interviste',
          'abbinamenti', 'produttori', 'mercati', 'cultura',
        ],
        layout: 'grid',
      },
      description: 'Empty = all sections.',
    }),
    defineField({
      name: 'priority',
      title: 'Priority',
      type: 'number',
      initialValue: 10,
      validation: R => R.required().min(1).max(100),
    }),
    defineField({
      name: 'active',
      title: 'Active',
      type: 'boolean',
      initialValue: true,
    }),
    defineField({
      name: 'startDate',
      title: 'Start date',
      type: 'date',
    }),
    defineField({
      name: 'endDate',
      title: 'End date',
      type: 'date',
    }),
  ],

  preview: {
    select: {
      title:    'brand',
      subtitle: 'slot',
      active:   'active',
    },
    prepare({ title, subtitle, active }) {
      return {
        title,
        subtitle: `${subtitle ?? ''} · ${active ? 'LIVE' : 'paused'}`,
      }
    },
  },
})
