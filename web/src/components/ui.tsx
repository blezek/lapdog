/* Shared presentational pieces. */

import { type ReactNode, useState } from 'react'

/** Icon renders a vendored Material Design icon from the Go binary. */
export function Icon({ name, className }: { name: string; className?: string }) {
  return (
    <svg viewBox="0 0 24 24" className={className} aria-hidden="true">
      <use href={`/icons/${name}.svg#mdi-${name}`} />
    </svg>
  )
}

/**
 * Card wraps a titled block.
 *
 * table, when provided, renders a table view of the same data behind a toggle.
 * Every chart in the interface has one: it is what makes the figures reachable
 * without reading colour, and it is required by the relief rule for the series
 * colours that sit below 3:1 against the light surface.
 */
export function Card({
  title,
  actions,
  table,
  children,
}: {
  title: string
  actions?: ReactNode
  table?: ReactNode
  children: ReactNode
}) {
  const [showTable, setShowTable] = useState(false)
  return (
    <section className="card">
      <div className="card-head">
        <span className="card-title">{title}</span>
        <div className="card-actions">
          {actions}
          {table && (
            <button
              type="button"
              className="ghost-btn"
              aria-pressed={showTable}
              onClick={() => setShowTable((v) => !v)}
            >
              {showTable ? 'Chart' : 'Table'}
            </button>
          )}
        </div>
      </div>
      {showTable && table ? table : children}
    </section>
  )
}

/** Stat is a single headline value, with an optional note and meter. */
export function Stat({
  label,
  value,
  note,
  noteGood,
  meter,
}: {
  label: string
  value: string
  note?: string
  noteGood?: boolean
  meter?: number
}) {
  return (
    <div className="card">
      <div className="stat-label">{label}</div>
      <div className="stat-value">{value}</div>
      {note && <div className={`stat-note${noteGood ? ' good' : ''}`}>{note}</div>}
      {meter != null && (
        <div className="meter">
          <i style={{ width: `${Math.max(0, Math.min(100, meter * 100))}%` }} />
        </div>
      )}
    </div>
  )
}

/**
 * Legend names every series.
 *
 * A legend is always present for two or more series, so identity is never carried
 * by colour alone.
 */
export function Legend({ items }: { items: { label: string; colour: string }[] }) {
  if (items.length === 0) return null
  return (
    <div className="legend">
      {items.map((it) => (
        <span key={it.label}>
          <span className="legend-swatch" style={{ background: it.colour }} />
          {it.label}
        </span>
      ))}
    </div>
  )
}

/** Empty states what is missing rather than showing a blank panel. */
export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

/** Banner carries a status message with an icon, never colour alone. */
export function Banner({
  kind,
  children,
}: {
  kind: 'good' | 'warn' | 'bad'
  children: ReactNode
}) {
  const icon =
    kind === 'good'
      ? 'check-circle-outline'
      : kind === 'warn'
        ? 'alert-circle-outline'
        : 'alert-circle-outline'
  return (
    <div className={`banner ${kind}`}>
      <Icon name={icon} />
      <div>{children}</div>
    </div>
  )
}

/** Loading is a quiet placeholder while a query is in flight. */
export function Loading() {
  return <div className="empty">Loading…</div>
}

/** ErrorNote shows the server's own message, which is more useful than a code. */
export function ErrorNote({ error }: { error: unknown }) {
  const msg = error instanceof Error ? error.message : String(error)
  return <Banner kind="bad">{msg}</Banner>
}
