import type { ConnectorState, DayDetail, SearchResult, StatsResponse, Task, VelocityResponse } from './types'

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

  updateTask: (id: number, patch: { done?: boolean; title?: string; pinned?: boolean }) =>
    request<Task>(`/tasks/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),

  deleteTask: (id: number) =>
    request<void>(`/tasks/${id}`, { method: 'DELETE' }),

  listConnectors: () => request<ConnectorState[]>('/connectors'),

  syncConnector: (name: string) =>
    request<void>(`/connectors/${name}/sync`, { method: 'POST' }),

  search: (q: string, source?: string) => {
    const params = new URLSearchParams({ q })
    if (source) params.set('source', source)
    return request<SearchResult[]>(`/search?${params}`)
  },

  listSources: () => request<string[]>('/sources'),

  getVelocity: (from?: string, to?: string) => {
    const params = new URLSearchParams()
    if (from) params.set('from', from)
    if (to) params.set('to', to)
    const qs = params.toString()
    return request<VelocityResponse>(`/stats/velocity${qs ? '?' + qs : ''}`)
  },

  getStats: (from?: string, to?: string, mode?: 'unique' | 'raw') => {
    const params = new URLSearchParams()
    if (from) params.set('from', from)
    if (to) params.set('to', to)
    if (mode) params.set('mode', mode)
    const qs = params.toString()
    return request<StatsResponse>(`/stats${qs ? '?' + qs : ''}`)
  },
}
