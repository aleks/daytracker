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

export interface SearchResult {
  date: string
  type: 'activity' | 'task'
  source: string
  title: string
  url: string
}

export interface StatsPeriod {
  from: string
  to: string
  active_days: number
}

export interface StatsSummary {
  tasks_total: number
  tasks_done: number
  activities_total: number
}

export interface StatsGitHub {
  authored_total: number
  authored_merged: number
  authored_open: number
  authored_draft: number
  authored_approved: number
  authored_changes_requested: number
  authored_in_review: number
  authored_closed: number
  reviewed_total: number
  reviewed_merged: number
  reviewed_open: number
  reviewed_draft: number
  reviewed_closed: number
}

export interface StatsJira {
  total: number
  done: number
  in_progress: number
  todo: number
}

export interface StatsConfluence {
  total: number
  created: number
  edited: number
}

export interface StatsDayBucket {
  date: string
  tasks_done: number
  github: number
  jira: number
  confluence: number
  total: number
}

export interface StatsTopDay {
  date: string
  total: number
}

export interface StatsResponse {
  period: StatsPeriod
  summary: StatsSummary
  github: StatsGitHub
  jira: StatsJira
  confluence: StatsConfluence
  timeline: StatsDayBucket[]
  top_days: StatsTopDay[]
}

export interface ConnectorState {
  id: number
  name: string
  last_sync_at: string | null
  last_error: string
  updated_at: string
}
