import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import type { ConnectorState } from '../types'

export function ConnectorStatus() {
  const [connectors, setConnectors] = useState<ConnectorState[]>([])

  useEffect(() => {
    api.listConnectors().then(setConnectors).catch(console.error)
  }, [])

  if (connectors.length === 0) return null

  return (
    <div class="connector-status">
      {connectors.map(c => (
        <span key={c.name} class={c.last_error ? 'connector-chip error' : 'connector-chip'}>
          {c.name}
          {c.last_sync_at && (
            <span class="connector-sync-time">
              {new Date(c.last_sync_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
            </span>
          )}
          {c.last_error && <span class="connector-error" title={c.last_error}>!</span>}
        </span>
      ))}
    </div>
  )
}
