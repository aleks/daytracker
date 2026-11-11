import { useEffect, useRef, useState } from 'preact/hooks'
import { api } from '../api'
import type { WorkLogEntry } from '../types'

interface Props {
  date: string
  entries: WorkLogEntry[]
  onAdded: (entry: WorkLogEntry) => void
  onUpdated: (entry: WorkLogEntry) => void
  onDeleted: (id: number) => void
}

export function WorkLog({ date, entries, onAdded, onUpdated, onDeleted }: Props) {
  const [content, setContent] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editingContent, setEditingContent] = useState('')
  const [pendingDeleteId, setPendingDeleteId] = useState<number | null>(null)
  const [busyId, setBusyId] = useState<number | null>(null)
  const editRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    if (editingId !== null) editRef.current?.focus()
  }, [editingId])

  const submit = async (event: Event) => {
    event.preventDefault()
    if (!content.trim() || saving) return
    setSaving(true)
    setError(null)
    try {
      const entry = await api.createWorkLog(date, content)
      onAdded(entry)
      setContent('')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save journal entry')
    } finally {
      setSaving(false)
    }
  }

  const startEdit = (entry: WorkLogEntry) => {
    if (editingId !== null || busyId !== null) return
    setError(null)
    setPendingDeleteId(null)
    setEditingContent(entry.content)
    setEditingId(entry.id)
  }

  const saveEdit = async (entry: WorkLogEntry) => {
    const trimmed = editingContent.trim()
    if (!trimmed || busyId !== null) return
    if (trimmed === entry.content) {
      setEditingId(null)
      return
    }
    setBusyId(entry.id)
    setError(null)
    try {
      onUpdated(await api.updateWorkLog(entry.id, trimmed))
      setEditingId(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not update journal entry')
    } finally {
      setBusyId(null)
    }
  }

  const remove = async (entry: WorkLogEntry) => {
    if (busyId !== null) return
    setBusyId(entry.id)
    setError(null)
    try {
      await api.deleteWorkLog(entry.id)
      onDeleted(entry.id)
      setPendingDeleteId(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not delete journal entry')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <div class="work-log">
      <form class="work-log-form" onSubmit={submit}>
        <textarea
          aria-label="Work journal entry"
          placeholder="Add a note about your day…"
          rows={3}
          disabled={saving}
          value={content}
          onInput={e => setContent((e.target as HTMLTextAreaElement).value)}
          onKeyDown={e => {
            if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.isComposing) submit(e)
          }}
        />
        <button type="submit" disabled={saving || !content.trim()}>{saving ? 'Saving…' : 'Add note'}</button>
      </form>
      {error && <p class="error-message" role="alert">{error}</p>}
      {entries.length === 0 ? <p class="task-empty">No notes yet.</p> : (
        <ul class="work-log-entries">
          {entries.map(entry => (
            <li key={entry.id} class="work-log-entry">
              <div class="work-log-entry-header">
                <time class="work-log-time" dateTime={entry.created_at} title={new Date(entry.created_at).toLocaleString()}>
                  {new Date(entry.created_at).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })}
                </time>
                {editingId !== entry.id && (pendingDeleteId === entry.id ? (
                  <span class="task-delete-confirm">
                    <span class="task-delete-label">Delete?</span>
                    <button class="task-delete-yes" disabled={busyId !== null} onClick={() => remove(entry)}>Yes</button>
                    <button class="task-delete-no" disabled={busyId !== null} onClick={() => setPendingDeleteId(null)}>No</button>
                  </span>
                ) : (
                  <span class="work-log-actions">
                    <button class="work-log-action" disabled={busyId !== null || editingId !== null} onClick={() => startEdit(entry)} aria-label="Edit journal entry">Edit</button>
                    <button class="work-log-action" disabled={busyId !== null || editingId !== null} onClick={() => setPendingDeleteId(entry.id)} aria-label="Delete journal entry">×</button>
                  </span>
                ))}
              </div>
              {editingId === entry.id ? (
                <div class="work-log-edit">
                  <textarea ref={editRef} aria-label="Edit journal entry" rows={3} value={editingContent} disabled={busyId !== null}
                    onInput={e => setEditingContent((e.target as HTMLTextAreaElement).value)}
                    onKeyDown={e => {
                      if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !e.isComposing) { e.preventDefault(); saveEdit(entry) }
                      if (e.key === 'Escape' && busyId === null) setEditingId(null)
                    }} />
                  <div class="work-log-edit-buttons">
                    <button class="work-log-save" disabled={busyId !== null || !editingContent.trim()} onClick={() => saveEdit(entry)}>Save</button>
                    <button class="work-log-cancel" disabled={busyId !== null} onClick={() => setEditingId(null)}>Cancel</button>
                  </div>
                </div>
              ) : (
                <div class="work-log-content" onDblClick={() => startEdit(entry)} title="Double-click to edit">{entry.content}</div>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
