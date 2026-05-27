import { useEffect, useRef, useState } from 'preact/hooks'
import { api } from '../api'
import type { SearchResult } from '../types'

interface Props {
  onSelect: (date: string) => void
}

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + 'T00:00:00')
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })
}

const SOURCE_LABELS: Record<string, string> = {
  github: 'GitHub',
  jira: 'Jira',
  confluence: 'Confluence',
}

export function Search({ onSelect }: Props) {
  const [query, setQuery] = useState('')
  const [source, setSource] = useState('')
  const [sources, setSources] = useState<string[]>([])
  const [results, setResults] = useState<SearchResult[]>([])
  const [open, setOpen] = useState(false)
  const [activeIdx, setActiveIdx] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    api.listSources().then(setSources).catch(() => {})
  }, [])

  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    if (query.trim().length < 2) {
      setResults([])
      setOpen(false)
      return
    }
    debounceRef.current = setTimeout(() => {
      api.search(query.trim(), source || undefined)
        .then(r => {
          setResults(r)
          setOpen(r.length > 0)
          setActiveIdx(-1)
        })
        .catch(() => {})
    }, 200)
  }, [query, source])

  // Close dropdown when clicking outside.
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const select = (result: SearchResult) => {
    setQuery('')
    setOpen(false)
    setResults([])
    onSelect(result.date)
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (!open) return
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      setActiveIdx(i => Math.min(i + 1, results.length - 1))
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      setActiveIdx(i => Math.max(i - 1, 0))
    } else if (e.key === 'Enter' && activeIdx >= 0) {
      e.preventDefault()
      select(results[activeIdx])
    } else if (e.key === 'Escape') {
      setOpen(false)
    }
  }

  return (
    <div class="search" ref={containerRef}>
      <div class="search-row">
        <select
          class="search-source"
          value={source}
          onChange={e => setSource((e.target as HTMLSelectElement).value)}
          aria-label="Filter by source"
        >
          <option value="">All</option>
          <option value="tasks">Tasks</option>
          {sources.map(s => (
            <option key={s} value={s}>{SOURCE_LABELS[s] ?? s}</option>
          ))}
        </select>
        <input
          ref={inputRef}
          class="search-input"
          type="search"
          placeholder="Search…"
          value={query}
          onInput={e => setQuery((e.target as HTMLInputElement).value)}
          onKeyDown={handleKeyDown}
          onFocus={() => results.length > 0 && setOpen(true)}
          autocomplete="off"
        />
      </div>

      {open && (
        <ul class="search-dropdown" role="listbox">
          {results.map((r, i) => (
            <li
              key={`${r.date}-${r.title}`}
              class={`search-result${i === activeIdx ? ' search-result--active' : ''}`}
              role="option"
              aria-selected={i === activeIdx}
              onMouseDown={() => select(r)}
              onMouseEnter={() => setActiveIdx(i)}
            >
              <span class="search-result-title">{r.title}</span>
              <span class="search-result-meta">
                {r.source ? (SOURCE_LABELS[r.source] ?? r.source) + ' · ' : ''}
                {formatDate(r.date)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
