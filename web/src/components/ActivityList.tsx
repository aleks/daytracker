import type { ActivityItem } from '../types'

const SOURCE_LABELS: Record<string, string> = {
  github: 'GitHub',
  jira: 'Jira',
  confluence: 'Confluence',
}

const SOURCE_COLORS: Record<string, string> = {
  github: 'var(--color-github)',
  jira: 'var(--color-jira)',
  confluence: 'var(--color-confluence)',
}

interface KindMeta {
  label: string
  badge: 'neutral' | 'open' | 'draft' | 'review' | 'approved' | 'changes' | 'merged' | 'closed'
}

const KIND_META: Record<string, KindMeta> = {
  pr_authored:           { label: 'authored',          badge: 'neutral'  },
  pr_reviewed:           { label: 'reviewed',          badge: 'neutral'  },
  pr_open:               { label: 'open',              badge: 'open'     },
  pr_draft:              { label: 'draft',             badge: 'draft'    },
  pr_in_review:          { label: 'in review',         badge: 'review'   },
  pr_approved:           { label: 'approved',          badge: 'approved' },
  pr_changes_requested:  { label: 'changes requested', badge: 'changes'  },
  pr_merged:             { label: 'merged',            badge: 'merged'   },
  pr_closed:             { label: 'closed',            badge: 'closed'   },
  in_progress:           { label: 'in progress',       badge: 'open'     },
  in_review:             { label: 'in review',         badge: 'review'   },
  done:                  { label: 'done',              badge: 'merged'   },
  page_created:          { label: 'created',           badge: 'neutral'  },
  page_edited:           { label: 'edited',            badge: 'neutral'  },
  comment_added:         { label: 'comment',           badge: 'neutral'  },
}

interface Props {
  activities: ActivityItem[]
}

export function ActivityList({ activities }: Props) {
  if (activities.length === 0) {
    return <p class="activity-empty">No activity recorded.</p>
  }

  const bySource = activities.reduce<Record<string, ActivityItem[]>>((acc, item) => {
    ;(acc[item.source] ??= []).push(item)
    return acc
  }, {})

  return (
    <div class="activity-list">
      {Object.entries(bySource).map(([source, items]) => (
        <div key={source} class="activity-group">
          <h4 class="activity-source-label">{SOURCE_LABELS[source] ?? source}</h4>
          <ul class="activity-items">
            {items.map(item => {
              const meta = KIND_META[item.kind]
              return (
                <li
                  key={item.id}
                  class="activity-item"
                  style={{ borderLeftColor: SOURCE_COLORS[source] ?? 'var(--color-border)' }}
                >
                  {item.url ? (
                    <a href={item.url} target="_blank" rel="noopener noreferrer">
                      {item.title}
                    </a>
                  ) : (
                    <span>{item.title}</span>
                  )}
                  <span class={`activity-badge activity-badge--${meta?.badge ?? 'neutral'}`}>
                    {meta?.label ?? item.kind}
                  </span>
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </div>
  )
}
