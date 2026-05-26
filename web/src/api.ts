import type { ConnectorState, DayDetail, Task } from './types'

const BASE = '/api'

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    headers: { 'Content-Type': 'application/json' },
    ...options,
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(body.error ?? res.statusText)
  }
  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  listDays: () => request<{ id: number; date: string }[]>('/days'),

  getDay: (date: string) => request<DayDetail>(`/days/${date}`),

  createTask: (date: string, title: string) =>
    request<Task>(`/days/${date}/tasks`, {
      method: 'POST',
      body: JSON.stringify({ title }),
    }),

  updateTask: (id: number, patch: { done?: boolean; title?: string }) =>
    request<Task>(`/tasks/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  deleteTask: (id: number) =>
    request<void>(`/tasks/${id}`, { method: 'DELETE' }),

  listConnectors: () => request<ConnectorState[]>('/connectors'),

  syncConnector: (name: string) =>
    request<void>(`/connectors/${name}/sync`, { method: 'POST' }),
}
