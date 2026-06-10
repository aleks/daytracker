import { useState } from 'preact/hooks'
import type { ActivityItem } from '../types'

const SOURCE_COLORS: Record<string, string> = {
  github: 'var(--color-github)',
  jira: 'var(--color-jira)',
  confluence: 'var(--color-confluence)',
  youtrack: 'var(--color-youtrack)',
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
  youtrack_created:     { label: 'created',     badge: 'open'    },
  youtrack_edited:      { label: 'edited',      badge: 'neutral' },
  youtrack_work:        { label: 'time logged', badge: 'review'  },
  youtrack_resolved:    { label: 'resolved',    badge: 'merged'  },
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

// Find all Jira-style ticket keys (e.g. MOI-1234, ABC-99) in a string.
function extractKeys(s: string): string[] {
  return (s.match(/\b[A-Z][A-Z0-9]+-\d+\b/g) ?? []).map((k: string) => k.toUpperCase())
}

interface Props {
  activities: ActivityItem[]
  source: string
  allActivities?: ActivityItem[]
}

export function ActivityList({ activities, source, allActivities = [] }: Props) {
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
                {list.map(item => <ActivityRow key={item.id} item={item} color={color} allActivities={allActivities} />)}
              </ul>
            </div>
          ))}
      </div>
    )
  }

  return (
    <ul class="activity-items">
      {activities.map(item => (
        <ActivityRow key={item.id} item={item} color={color} allActivities={allActivities} />
      ))}
    </ul>
  )
}

function ActivityRow({ item, color, allActivities }: { item: ActivityItem; color: string; allActivities: ActivityItem[] }) {
  const meta = kindMeta(item.kind)
  const [expanded, setExpanded] = useState(false)

  // Jira row: find GitHub PRs whose title contains this ticket key.
  const linkedPRs = item.source === 'jira'
    ? allActivities.filter(a => a.source === 'github' && extractKeys(a.title).includes(item.external_id.toUpperCase()))
    : []

  // GitHub PR row: find Jira tickets whose key appears in this PR title.
  const linkedJira = item.source === 'github'
    ? (() => {
        const keys = extractKeys(item.title)
        return allActivities.filter(a => a.source === 'jira' && keys.includes(a.external_id.toUpperCase()))
      })()
    : []

  return (
    <li class={`activity-item${expanded ? ' activity-item--expanded' : ''}`} style={{ borderLeftColor: color }}>
      <div class="activity-item-main">
        {item.url ? (
          <a href={item.url} target="_blank" rel="noopener noreferrer">{item.title}</a>
        ) : (
          <span>{item.title}</span>
        )}
        <div class="activity-item-actions">
          {linkedJira.map(ticket => (
            <a
              key={ticket.id}
              class="activity-ticket-chip"
              href={ticket.url || undefined}
              target="_blank"
              rel="noopener noreferrer"
              title={ticket.title}
            >
              {ticket.external_id}
            </a>
          ))}
          {linkedPRs.length > 0 && (
            <button
              class={`activity-prs-btn${expanded ? ' activity-prs-btn--active' : ''}`}
              onClick={() => setExpanded(e => !e)}
            >
              {linkedPRs.length} {linkedPRs.length === 1 ? 'Pull Request' : 'Pull Requests'}
            </button>
          )}
          <span class={`activity-badge activity-badge--${meta.badge}`}>{meta.label}</span>
        </div>
      </div>
      {expanded && linkedPRs.length > 0 && (
        <ul class="activity-linked-prs">
          {linkedPRs.map(pr => {
            const prMeta = kindMeta(pr.kind)
            return (
              <li key={pr.id} class="activity-linked-pr">
                {pr.url ? (
                  <a href={pr.url} target="_blank" rel="noopener noreferrer">{pr.title}</a>
                ) : (
                  <span>{pr.title}</span>
                )}
                <span class={`activity-badge activity-badge--${prMeta.badge}`}>{prMeta.label}</span>
              </li>
            )
          })}
        </ul>
      )}
    </li>
  )
}
