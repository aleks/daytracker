import { useRef, useState } from 'preact/hooks'
import { api } from '../api'
import type { Task } from '../types'

interface Props {
  date: string
  tasks: Task[]
  onChanged: (tasks: Task[]) => void
  onCopyToToday?: (title: string) => Promise<void>
}

export function TaskList({ date, tasks, onChanged, onCopyToToday }: Props) {
  const [newTitle, setNewTitle] = useState('')
  const [adding, setAdding] = useState(false)
  const [copying, setCopying] = useState<number | null>(null)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [editingTitle, setEditingTitle] = useState('')
  const editInputRef = useRef<HTMLInputElement>(null)

  const add = async () => {
    const title = newTitle.trim()
    if (!title) return
    setAdding(true)
    try {
      const task = await api.createTask(date, title)
      onChanged([...tasks, task])
      setNewTitle('')
    } catch (err) {
      console.error('add task:', err)
    } finally {
      setAdding(false)
    }
  }

  const toggle = async (task: Task) => {
    const optimistic = tasks.map(t => t.id === task.id ? { ...t, done: !t.done } : t)
    onChanged(optimistic)
    try {
      await api.updateTask(task.id, { done: !task.done })
    } catch (err) {
      console.error('toggle task:', err)
      onChanged(tasks)
    }
  }

  const copyToToday = async (task: Task) => {
    if (!onCopyToToday) return
    setCopying(task.id)
    try {
      await onCopyToToday(task.title)
    } catch (err) {
      console.error('copy task:', err)
    } finally {
      setCopying(null)
    }
  }

  const startEdit = (task: Task) => {
    setEditingId(task.id)
    setEditingTitle(task.title)
    // Focus the input on the next tick after render
    setTimeout(() => editInputRef.current?.select(), 0)
  }

  const commitEdit = async (task: Task) => {
    const trimmed = editingTitle.trim()
    setEditingId(null)
    if (!trimmed || trimmed === task.title) return
    const optimistic = tasks.map(t => t.id === task.id ? { ...t, title: trimmed } : t)
    onChanged(optimistic)
    try {
      await api.updateTask(task.id, { title: trimmed })
    } catch (err) {
      console.error('rename task:', err)
      onChanged(tasks)
    }
  }

  const cancelEdit = () => {
    setEditingId(null)
    setEditingTitle('')
  }

  const remove = async (task: Task) => {
    const optimistic = tasks.filter(t => t.id !== task.id)
    onChanged(optimistic)
    try {
      await api.deleteTask(task.id)
    } catch (err) {
      console.error('delete task:', err)
      onChanged(tasks)
    }
  }

  const sorted = [...tasks].sort((a, b) => {
    if (a.done !== b.done) return a.done ? 1 : -1
    return a.id - b.id
  })

  return (
    <div class="task-list">
      <div class="task-add">
        <input
          type="text"
          placeholder="Add a task…"
          value={newTitle}
          onInput={e => setNewTitle((e.target as HTMLInputElement).value)}
          onKeyDown={e => e.key === 'Enter' && add()}
          disabled={adding}
        />
        <button onClick={add} disabled={adding || !newTitle.trim()}>Add</button>
      </div>

      {sorted.length === 0 && (
        <p class="task-empty">No tasks yet.</p>
      )}

      <ul class="task-items">
        {sorted.map(task => (
          <li key={task.id} class={task.done ? 'task-item done' : 'task-item'}>
            <input
              type="checkbox"
              checked={task.done}
              onChange={() => toggle(task)}
            />
            {editingId === task.id ? (
              <input
                ref={editInputRef}
                class="task-title-edit"
                type="text"
                value={editingTitle}
                onInput={e => setEditingTitle((e.target as HTMLInputElement).value)}
                onKeyDown={e => {
                  if (e.key === 'Enter') commitEdit(task)
                  if (e.key === 'Escape') cancelEdit()
                }}
                onBlur={() => commitEdit(task)}
              />
            ) : (
              <span class="task-title" onDblClick={() => startEdit(task)} title="Double-click to edit">{task.title}</span>
            )}
            {onCopyToToday && !task.done && (
              <button
                class="task-copy"
                onClick={() => copyToToday(task)}
                disabled={copying === task.id}
                title="Copy to today"
              >
                {copying === task.id ? '…' : '↑ today'}
              </button>
            )}
            <button class="task-delete" onClick={() => remove(task)} title="Delete">×</button>
          </li>
        ))}
      </ul>
    </div>
  )
}
