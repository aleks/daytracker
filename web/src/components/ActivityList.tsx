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
            {items.map(item => (
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
                <span class="activity-kind">{item.kind}</span>
              </li>
            ))}
          </ul>
        </div>
      ))}
    </div>
  )
}
