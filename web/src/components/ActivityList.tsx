import type { ActivityItem } from '../types'

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

const STATE_META: Record<string, KindMeta> = {
  open:               { label: 'open',              badge: 'open'     },
  draft:              { label: 'draft',             badge: 'draft'    },
  in_review:          { label: 'in review',         badge: 'review'   },
  approved:           { label: 'approved',          badge: 'approved' },
  changes_requested:  { label: 'changes requested', badge: 'changes'  },
  merged:             { label: 'merged',            badge: 'merged'   },
  closed:             { label: 'closed',            badge: 'closed'   },
}

const KIND_META: Record<string, KindMeta> = {
  jira_todo:            { label: 'to do',       badge: 'neutral' },
  jira_in_progress:     { label: 'in progress', badge: 'open'    },
  jira_done:            { label: 'done',        badge: 'merged'  },
  confluence_created:   { label: 'created',     badge: 'open'    },
  confluence_edited:    { label: 'edited',      badge: 'neutral' },
  confluence_commented: { label: 'commented',   badge: 'review'  },
}

function kindMeta(kind: string): KindMeta {
  const us = kind.indexOf('_')
  if (us >= 0) {
    const state = kind.slice(us + 1)
    if (state in STATE_META) return STATE_META[state]
  }
  return KIND_META[kind] ?? { label: kind, badge: 'neutral' }
}

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
  source: string
}

export function ActivityList({ activities, source }: Props) {
  if (activities.length === 0) {
    return <p class="activity-empty">No activity recorded.</p>
  }

  const color = SOURCE_COLORS[source] ?? 'var(--color-border)'
  const hasRoles = source === 'github' && activities.some(i => prRole(i.kind) !== null)

  if (hasRoles) {
    const authored = activities.filter(i => prRole(i.kind) === 'authored')
    const reviewed = activities.filter(i => prRole(i.kind) === 'reviewed')
    const other    = activities.filter(i => prRole(i.kind) === null)

    return (
      <div class="activity-list">
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
    <ul class="activity-items">
      {activities.map(item => <ActivityRow key={item.id} item={item} color={color} />)}
    </ul>
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
