import { StructureBuilder } from 'sanity/structure'

const MARKETS = [
  { id: 'italy', label: '🇮🇹 Italy' },
  { id: 'usa',   label: '🇺🇸 USA'   },
  { id: 'china', label: '🇨🇳 China'  },
]

export const structure = (S: StructureBuilder) =>
  S.list()
    .title('ENOICA')
    .items([
      S.listItem()
        .title('Articles')
        .child(
          S.list()
            .title('Articles by market')
            .items(
              MARKETS.map(({ id, label }) =>
                S.listItem()
                  .title(label)
                  .child(
                    S.documentList()
                      .title(`${label} articles`)
                      .filter('_type == "article" && market == $market')
                      .params({ market: id })
                      .defaultOrdering([{ field: 'approvedAt', direction: 'desc' }])
                  )
              )
            )
        ),
      S.divider(),
      S.listItem()
        .title('Ads')
        .child(
          S.documentList()
            .title('All ads')
            .filter('_type == "ad"')
            .defaultOrdering([{ field: 'priority', direction: 'desc' }])
        ),
      S.divider(),
      ...S.documentTypeListItems().filter(
        t => !['article', 'ad'].includes(t.getId() ?? '')
      ),
    ])
