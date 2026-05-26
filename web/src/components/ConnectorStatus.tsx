import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import type { ConnectorState } from '../types'

export function ConnectorStatus() {
  const [connectors, setConnectors] = useState<ConnectorState[]>([])
  const [syncing, setSyncing] = useState<string | null>(null)

  useEffect(() => {
    api.listConnectors().then(setConnectors).catch(console.error)
  }, [])

  if (connectors.length === 0) return null

  const sync = async (name: string) => {
    if (syncing) return
    setSyncing(name)
    try {
      await api.syncConnector(name)
    } catch (err) {
      console.error('sync:', err)
    } finally {
      setSyncing(null)
    }
  }

  return (
    <div class="connector-status">
      {connectors.map(c => (
        <button
          key={c.name}
          class={['connector-chip', c.last_error ? 'error' : '', syncing === c.name ? 'syncing' : ''].filter(Boolean).join(' ')}
          onClick={() => sync(c.name)}
          title={c.last_error || `Sync ${c.name}`}
          disabled={!!syncing}
        >
          <span class="connector-name">{c.name}</span>
          <span class="connector-sync-time">
            {syncing === c.name
              ? '…'
              : c.last_sync_at
                ? new Date(c.last_sync_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
                : '—'
            }
          </span>
          {c.last_error && <span class="connector-error" title={c.last_error}>!</span>}
        </button>
      ))}
    </div>
  )
}
