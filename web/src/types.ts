export interface Task {
  id: number
  day_id: number
  title: string
  done: boolean
  created_at: string
}

export interface ActivityItem {
  id: number
  day_id: number
  source: string
  external_id: string
  kind: string
  title: string
  url: string
  metadata: string
  fetched_at: string
}

export interface Day {
  id: number
  date: string
  created_at: string
}

export interface DayDetail extends Day {
  tasks: Task[]
  activities: ActivityItem[]
}

export interface ConnectorState {
  id: number
  name: string
  last_sync_at: string | null
  last_error: string
  updated_at: string
}
