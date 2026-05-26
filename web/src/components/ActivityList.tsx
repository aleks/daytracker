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

type BadgeVariant = 'neutral' | 'open' | 'draft' | 'review' | 'approved' | 'changes' | 'merged' | 'closed'

interface KindMeta {
  label: string
  badge: BadgeVariant
}

// State suffix → badge/label. Used for both authored_* and reviewed_* kinds.
const STATE_META: Record<string, KindMeta> = {
  open:               { label: 'open',              badge: 'open'     },
  draft:              { label: 'draft',             badge: 'draft'    },
  in_review:          { label: 'in review',         badge: 'review'   },
  approved:           { label: 'approved',          badge: 'approved' },
  changes_requested:  { label: 'changes requested', badge: 'changes'  },
  merged:             { label: 'merged',            badge: 'merged'   },
  closed:             { label: 'closed',            badge: 'closed'   },
}

// Fallback for non-prefixed kinds (Jira, Confluence, legacy).
const KIND_META: Record<string, KindMeta> = {
  in_progress:    { label: 'in progress', badge: 'open'    },
  in_review:      { label: 'in review',   badge: 'review'  },
  done:           { label: 'done',        badge: 'merged'  },
  page_created:   { label: 'created',     badge: 'neutral' },
  page_edited:    { label: 'edited',      badge: 'neutral' },
  comment_added:  { label: 'comment',     badge: 'neutral' },
}

function kindMeta(kind: string): KindMeta {
  // Prefixed kinds: "authored_merged", "reviewed_in_review", etc.
  const us = kind.indexOf('_')
  if (us >= 0) {
    const state = kind.slice(us + 1)
    if (state in STATE_META) return STATE_META[state]
  }
  return KIND_META[kind] ?? { label: kind, badge: 'neutral' }
}

// Returns "authored" | "reviewed" | null for non-GitHub kinds.
function prRole(kind: string): 'authored' | 'reviewed' | null {
  if (kind.startsWith('authored_')) return 'authored'
  if (kind.startsWith('reviewed_')) return 'reviewed'
  return null
}

const ROLE_LABELS: Record<string, string> = {
  authored: 'My PRs',
  reviewed: 'Reviewed',
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
      {Object.entries(bySource).map(([source, items]) => {
        const color = SOURCE_COLORS[source] ?? 'var(--color-border)'

        // For GitHub, split into authored / reviewed subsections.
        const hasRoles = source === 'github' && items.some(i => prRole(i.kind) !== null)

        if (hasRoles) {
          const authored = items.filter(i => prRole(i.kind) === 'authored')
          const reviewed = items.filter(i => prRole(i.kind) === 'reviewed')
          const other    = items.filter(i => prRole(i.kind) === null)

          return (
            <div key={source} class="activity-group">
              <h4 class="activity-source-label">{SOURCE_LABELS[source] ?? source}</h4>
              {([['authored', authored], ['reviewed', reviewed], [null, other]] as const)
                .filter(([, list]) => list.length > 0)
                .map(([role, list]) => (
                  <div key={role ?? 'other'} class="activity-subgroup">
                    {role && <span class="activity-subgroup-label">{ROLE_LABELS[role]}</span>}
                    <ul class="activity-items">
                      {list.map(item => <ActivityRow key={item.id} item={item} color={color} />)}
                    </ul>
                  </div>
                ))}
            </div>
          )
        }

        return (
          <div key={source} class="activity-group">
            <h4 class="activity-source-label">{SOURCE_LABELS[source] ?? source}</h4>
            <ul class="activity-items">
              {items.map(item => <ActivityRow key={item.id} item={item} color={color} />)}
            </ul>
          </div>
        )
      })}
    </div>
  )
}

function ActivityRow({ item, color }: { item: ActivityItem; color: string }) {
  const meta = kindMeta(item.kind)
  return (
    <li class="activity-item" style={{ borderLeftColor: color }}>
      {item.url ? (
        <a href={item.url} target="_blank" rel="noopener noreferrer">{item.title}</a>
      ) : (
        <span>{item.title}</span>
      )}
      <span class={`activity-badge activity-badge--${meta.badge}`}>{meta.label}</span>
    </li>
  )
}
