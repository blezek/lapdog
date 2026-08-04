import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'

import { api } from '../api'
import { num } from '../format'
import { useFilter } from '../useFilter'
import { Card, Icon } from '../components/ui'
import { Filters, describeFilter } from '../components/Filters'

type Scope = 'sessions' | 'laps' | 'positions'
type Format = 'csv' | 'json'

const scopes: { id: Scope; label: string; hint: string }[] = [
  {
    id: 'sessions',
    label: 'Sessions',
    hint: 'One row per session segment, with the three time counters, classification and results.',
  },
  {
    id: 'laps',
    label: 'Laps',
    hint: 'One row per completed lap, joined to its session’s track, car and category.',
  },
  {
    id: 'positions',
    label: 'Position changes',
    hint: 'One row per position change in a race, with the attributed cause and opponent.',
  },
]

/**
 * Export hands the current filter to the server.
 *
 * The counts shown are fetched with the same filter the download will use, so what
 * the page promises and what the file contains cannot drift apart.
 */
export function Export() {
  const { filter, state } = useFilter()
  const [scope, setScope] = useState<Scope>('sessions')
  const [format, setFormat] = useState<Format>('csv')

  const sessions = useQuery({
    queryKey: ['export-count-sessions', filter],
    queryFn: () => api.sessions({ ...filter, limit: 1 }),
  })
  const laps = useQuery({
    queryKey: ['export-count-laps', filter],
    queryFn: () => api.laps({ ...filter, limit: 1 }),
  })

  const counts: Record<Scope, number | undefined> = {
    sessions: sessions.data?.total,
    laps: laps.data?.total,
    // Position events are not separately counted: the export streams them and a
    // count query for a third scope would be a request for a number nobody acts
    // on. The session count bounds it.
    positions: undefined,
  }

  const href = api.exportUrl(filter, scope, format)

  return (
    <>
      <div className="page-head">
        <h1>Export</h1>
      </div>
      <p className="page-sub">
        Exports honour the filter below, so the file contains exactly what the other
        pages are showing.
      </p>

      <Filters />

      <div className="grid two-col">
        <Card title="What to export">
          {scopes.map((s) => (
            <label
              key={s.id}
              className="setting"
              style={{ alignItems: 'flex-start', cursor: 'pointer' }}
            >
              <input
                type="radio"
                name="scope"
                checked={scope === s.id}
                onChange={() => setScope(s.id)}
                style={{ marginTop: 3 }}
              />
              <span className="setting-label" style={{ flex: 1 }}>
                {s.label}
                {counts[s.id] != null && (
                  <span style={{ color: 'var(--text-muted)' }}> · {num(counts[s.id]!)} rows</span>
                )}
                <span className="setting-hint">{s.hint}</span>
              </span>
            </label>
          ))}
        </Card>

        <Card title="Format">
          <label className="setting" style={{ alignItems: 'flex-start', cursor: 'pointer' }}>
            <input
              type="radio"
              name="format"
              checked={format === 'csv'}
              onChange={() => setFormat('csv')}
              style={{ marginTop: 3 }}
            />
            <span className="setting-label" style={{ flex: 1 }}>
              CSV
              <span className="setting-hint">
                Opens directly in a spreadsheet. Empty cells mean no value was recorded.
              </span>
            </span>
          </label>
          <label className="setting" style={{ alignItems: 'flex-start', cursor: 'pointer' }}>
            <input
              type="radio"
              name="format"
              checked={format === 'json'}
              onChange={() => setFormat('json')}
              style={{ marginTop: 3 }}
            />
            <span className="setting-label" style={{ flex: 1 }}>
              JSON
              <span className="setting-hint">
                An array of objects, with nulls preserved and numbers left as numbers.
              </span>
            </span>
          </label>

          <div style={{ marginTop: 16 }}>
            <div style={{ color: 'var(--text-muted)', fontSize: 12, marginBottom: 9 }}>
              Exporting <strong style={{ color: 'var(--text-primary)' }}>{scope}</strong> for{' '}
              {describeFilter(state)}.
            </div>
            {/* A plain link rather than a fetch: the server sets
                Content-Disposition, so the browser handles the download and a
                multi-year export streams instead of buffering in the page. */}
            <a
              className="control control-active"
              href={href}
              download
              style={{ textDecoration: 'none', gap: 7 }}
            >
              <Icon name="download" className="legend-swatch" />
              Download {format.toUpperCase()}
            </a>
          </div>
        </Card>
      </div>
    </>
  )
}
