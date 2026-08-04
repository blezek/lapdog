import { useEffect } from 'react'
import { NavLink, Navigate, Route, Routes } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'

import { api } from './api'
import { applyTheme } from './theme'
import { Icon } from './components/ui'
import { hm } from './format'

import { Dashboard } from './pages/Dashboard'
import { Sessions } from './pages/Sessions'
import { Laps } from './pages/Laps'
import { Export } from './pages/Export'
import { Settings } from './pages/Settings'

/** nav is the four-page structure plus settings, pinned to the bottom. */
const nav = [
  { to: '/dashboard', label: 'Dashboard', icon: 'speedometer' },
  { to: '/sessions', label: 'Sessions', icon: 'flag-checkered' },
  { to: '/laps', label: 'Laps', icon: 'timer-outline' },
  { to: '/export', label: 'Export', icon: 'download' },
]

export function App() {
  // The theme preference lives in settings, so it is applied as soon as it loads.
  const { data: config } = useQuery({ queryKey: ['settings'], queryFn: api.settings })
  useEffect(() => {
    if (config?.theme) applyTheme(config.theme)
  }, [config?.theme])

  return (
    <div className="shell">
      <nav className="sidebar">
        <div className="brand">
          <Icon name="racing-helmet" />
          LapDog
        </div>

        {nav.map((item) => (
          <NavLink
            key={item.to}
            to={item.to}
            className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
          >
            <Icon name={item.icon} />
            {item.label}
          </NavLink>
        ))}

        <div className="nav-spacer" />
        <NavLink
          to="/settings"
          className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}
        >
          <Icon name="cog" />
          Settings
        </NavLink>

        <ConnectionStatus />
      </nav>

      <main className="main">
        <Routes>
          <Route path="/" element={<Navigate to="/dashboard" replace />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/sessions" element={<Sessions />} />
          <Route path="/laps" element={<Laps />} />
          <Route path="/export" element={<Export />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="*" element={<Navigate to="/dashboard" replace />} />
        </Routes>
      </main>
    </div>
  )
}

/**
 * ConnectionStatus shows whether the simulator is being read.
 *
 * The state is always stated in words as well as by the dot colour, since colour
 * alone must never carry meaning.
 */
function ConnectionStatus() {
  const { data: status } = useQuery({
    queryKey: ['status'],
    queryFn: api.status,
    // The collector's state changes on its own, so this is the one query that
    // polls. Two seconds matches the tray's own refresh.
    refetchInterval: 2000,
  })

  if (!status) return null

  const colour = status.paused
    ? 'var(--status-warning)'
    : status.connected
      ? 'var(--status-good)'
      : 'var(--text-muted)'
  const word = status.paused ? 'Paused' : status.connected ? 'Connected' : 'Not connected'

  return (
    <div className="status-pill" title={`LapDog ${status.version}`}>
      <span className="status-dot" style={{ background: colour }} />
      <span>
        {word}
        {status.sessionLabel && (
          <>
            <br />
            {status.sessionLabel}
            {status.drivingSeconds > 0 && ` · ${hm(status.drivingSeconds)}`}
          </>
        )}
      </span>
    </div>
  )
}
