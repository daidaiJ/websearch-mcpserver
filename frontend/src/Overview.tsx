import { formatters } from './formatters'
import { useEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react'
import { api } from './api'
import { getLocale, t } from './i18n'
import './overview.css'

interface Daily {
  day: string
  requests: number
  successes: number
  failures: number
  cache_hits: number
  duration_ms: number
}

interface Health {
  name: string
  status: string
  today: Daily
  last_seen_at?: string
}

interface Source {
  id: string
  name: string
  active: boolean
  status: string
  observation_status?: string
  suspended?: boolean
  suspend_reason?: string
  last_error?: string
  actual_cooldown: { until: string; reason: string } | null
}

interface Tool {
  name: string
  label: string
  enabled: boolean
  registered: boolean
  state: string
  file?: { connectivity?: string }
}

interface Runtime {
  version: string
  uptime_seconds: number
  mcp_stateless: boolean
  mcp_resources: boolean
  proxy: { mode: 'auto' | 'explicit' | 'disabled'; resolved: boolean; api_providers: boolean }
  llm: { enabled: boolean; model_id: string; timeout_seconds: number }
  mcp_hub: { enabled: boolean; fetch_mode: string; servers: { name: string; enabled: boolean }[] }
}

export interface OverviewResponse {
  today: Daily
  trend: Daily[] | null
  sources: Source[]
  academic_sources?: Source[]
  configured_tools: Tool[]
  tools: Health[] | null
  system: { status: string; enabled: number; observed: number; degraded: number; down: number; suspended: number }
  runtime: Runtime
  brand?: { theme?: string; accent?: string; title?: string; logo?: string; footer?: boolean }
}

interface UsageRow {
  application: string
  project: string
  requests: number
  successes: number
  duration_ms: number
}

interface ClientResponse {
  window_days: number
  usage: UsageRow[] | null
}

type LoadStatus = 'loading' | 'success' | 'error'
type Resource<T> = { data?: T; status: LoadStatus; error?: string }

function useResource<T>(path: string, refresh: number): Resource<T> & { retry: () => void } {
  const locale = getLocale()
  const [attempt, setAttempt] = useState(0)
  const [result, setResult] = useState<Resource<T> & { path: string }>({ path, status: 'loading' })
  useEffect(() => {
    const controller = new AbortController()
    setResult(prior => ({ path, status: 'loading', data: prior.path === path ? prior.data : undefined }))
    api<T>(path, { signal: controller.signal }).then(data => {
      if (!controller.signal.aborted) setResult({ path, status: 'success', data })
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setResult(prior => ({ ...prior, path, status: 'error', error: error instanceof Error ? error.message : '' }))
    })
    return () => controller.abort()
  }, [path, refresh, locale, attempt])
  return { ...(result.path === path ? result : { status: 'loading' as const }), retry: () => setAttempt(value => value + 1) }
}

const number = (value: number) => formatters().count.format(value)
const percent = (ratio: number) => formatters().percent.format(ratio)
const asDate = (day: string) => new Date(`${day}T00:00:00Z`)
const dayText = (day: string, long = false) => formatters()[long ? 'longDay' : 'day'].format(asDate(day))
const ms = (value: number) => t('web.overviewNext.milliseconds', { Count: number(Math.round(value)) })
const daysText = (value: number) => t('web.overviewNext.days', { Count: value })
const onOff = (enabled: boolean) => enabled ? t('web.label.on') : t('web.label.off_2')
const calendarDay = 86_400_000
const series = [
  { key: 'successes', tone: 'success', label: 'web.status.success' },
  { key: 'failures', tone: 'failure', label: 'web.status.failure' },
  { key: 'cache_hits', tone: 'cache', label: 'web.trend.cache_hits' },
] as const

function LoadNote({ result }: { result: { status: LoadStatus; data?: unknown; error?: string; retry: () => void } }) {
  if (result.status === 'loading') return <p className="ov-note" role="status">{t('web.note.loading_2')}</p>
  if (result.status === 'success') return null
  return <div className="ov-note ov-load-error">
    <p role="alert">{t(result.data ? 'web.overviewNext.stale' : 'web.note.read_failed_refresh_to_retry')}{result.error && <> · {result.error}</>}</p>
    <button type="button" className="ov-button" onClick={result.retry}>{t('web.overviewNext.retry')}</button>
  </div>
}

function Panel({ id, title, detail, controls, children }: { id: string; title: string; detail?: string; controls?: ReactNode; children: ReactNode }) {
  return <section className="ov-panel" aria-labelledby={id}>
    <header className="ov-panel-header">
      <div><h2 id={id}>{title}</h2>{detail && <p>{detail}</p>}</div>
      {controls}
    </header>
    {children}
  </section>
}

function Trend({ today, history }: { today: Daily; history: Daily[] }) {
  const [range, setRange] = useState(7)
  const [hovered, setHovered] = useState<number>()
  const [focused, setFocused] = useState<number>()
  const [tab, setTab] = useState<number>()
  const [dismissed, setDismissed] = useState(false)
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  const end = asDate(today.day).getTime()
  const byDay = new Map(history.map(row => [row.day, row]))
  byDay.set(today.day, today)
  const rows = Number.isFinite(end) ? Array.from({ length: range }, (_, index) => {
    const day = new Date(end - (range - 1 - index) * calendarDay).toISOString().slice(0, 10)
    return byDay.get(day) ?? { day, successes: 0, failures: 0, cache_hits: 0, requests: 0 }
  }) : []
  const hasCalls = rows.some(row => row.requests > 0)
  const maximum = Math.max(3, Math.ceil(Math.max(...rows.flatMap(row => series.map(({ key }) => row[key]))) / 3) * 3)
  const guides = [maximum, maximum * 2 / 3, maximum / 3, 0]
  const x = (index: number) => 100 * (index + 0.5) / range
  const y = (count: number) => 100 * (1 - count / maximum)
  const activeIndex = hovered ?? focused
  const active = !dismissed && activeIndex !== undefined ? rows[activeIndex] : undefined
  return <Panel id="ov-trend-title" title={t('web.page.call_trend')} detail={t('web.overviewNext.daily_calls')}
    controls={<div className="ov-segments" role="group" aria-label={t('web.label.statistics_window')}>
      {[7, 14].map(days => <button key={days} type="button" aria-pressed={range === days} onClick={() => {
        setRange(days)
        setHovered(undefined)
        setFocused(undefined)
        setTab(undefined)
      }}>{daysText(days)}</button>)}
    </div>}>
    {hasCalls ? <>
      <ul className="ov-legend">{series.map(({ tone, label }) => <li key={tone}><i className={`ov-${tone}`} aria-hidden="true" />{t(label)}</li>)}</ul>
      <div className="ov-chart" style={{ '--ov-days': range } as CSSProperties}>
        <div className="ov-y-axis" aria-hidden="true">{guides.map(value => <span key={value}>{number(value)}</span>)}</div>
        <div className="ov-plot">
          <svg className="ov-plot-svg" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
            {guides.map(value => <line key={value} className="ov-guide" x1="0" x2="100" y1={y(value)} y2={y(value)} />)}
            {series.map(({ key, tone }) => <polyline key={key} className={`ov-trend-line ov-${tone}`} points={rows.map((row, index) => `${x(index)},${y(row[key])}`).join(' ')} />)}
          </svg>
          <div className="ov-chart-targets" role="group" aria-label={t('web.overviewNext.chart_keyboard')} onPointerLeave={() => setHovered(undefined)}>
            {rows.map((row, index) => <button key={row.day} type="button"
              ref={button => { buttons.current[index] = button }}
              aria-label={[dayText(row.day, true), ...series.map(({ key, label }) => `${t(label)} ${number(row[key])}`), `${t('web.label.calls')} ${number(row.requests)}`].join(' · ')}
              tabIndex={index === (tab ?? rows.length - 1) ? 0 : -1}
              onPointerEnter={() => { setDismissed(false); setHovered(index) }}
              onClick={() => { setDismissed(false); setHovered(index) }}
              onFocus={() => { setDismissed(false); setTab(index); setFocused(index) }}
              onBlur={() => setFocused(undefined)}
              onKeyDown={event => {
                const next = { ArrowLeft: index - 1, ArrowRight: index + 1, Home: 0, End: rows.length - 1 }[event.key]
                if (event.key === 'Escape') { setDismissed(true); event.stopPropagation() }
                else if (next !== undefined) {
                  event.preventDefault()
                  setHovered(undefined)
                  buttons.current[Math.max(0, Math.min(rows.length - 1, next))]?.focus()
                }
              }} />)}
          </div>
          {active && activeIndex !== undefined && <>
            <div className="ov-crosshair" style={{ left: `${x(activeIndex)}%` }} aria-hidden="true">
              {series.map(({ key, tone }) => <i key={key} className={`ov-chart-point ov-${tone}`} style={{ top: `${y(active[key])}%` }} />)}
            </div>
            <div className={`ov-tooltip ${activeIndex < range / 2 ? 'ov-tooltip-right' : 'ov-tooltip-left'}`} style={{ '--ov-point-x': `${x(activeIndex)}%` } as CSSProperties} aria-hidden="true">
              <strong>{dayText(active.day, true)}</strong>
              {series.map(({ key, tone, label }) => <div key={key}><span><i className={`ov-${tone}`} />{t(label)}</span><b>{number(active[key])}</b></div>)}
              <div className="ov-tooltip-total"><span>{t('web.label.calls')}</span><b>{number(active.requests)}</b></div>
            </div>
          </>}
        </div>
        <div className={`ov-x-axis${range === 7 ? ' ov-week-axis' : ''}`} aria-hidden="true">
          {rows.map((row, index) => <span className={range > 7 && index % 2 === 0 ? 'ov-minor-date' : undefined} key={row.day}>{dayText(row.day)}</span>)}
        </div>
      </div>
    </> : <p className="ov-note">{t('web.note.no_tool_layer_events_yet')}</p>}
  </Panel>
}

const systemStatus = (state: string) => ({
  healthy: { tone: 'success', label: t('web.status.running_normally') },
  down: { tone: 'failure', label: t('web.status.partially_failing') },
  degraded: { tone: 'warning', label: t('web.status.partially_degraded') },
  waiting: { tone: 'unknown', label: t('web.status.awaiting_real_calls') },
}[state] ?? { tone: 'unknown', label: t('web.status.unknown') })

function HealthSummary({ sources, system, cooled, onSource }: {
  sources: Source[]; system: OverviewResponse['system']; cooled: Map<string, number>; onSource: (name: string) => void
}) {
  const systemState = systemStatus(system.status)
  const issues = sources.flatMap(source => {
    const cooldown = cooled.get(source.id)
    if (cooldown !== undefined) return [{
      source, order: 0, tone: 'cooldown', label: t('web.overviewNext.cooling'),
      detail: t('web.overviewNext.remaining', { Count: cooldown, Seconds: number(cooldown) }),
    }]
    const observed = source.observation_status ?? source.status
    if (observed === 'down') return [{ source, order: 1, tone: 'failure', label: t('web.overviewNext.severe'), detail: source.last_error }]
    if (source.suspended || observed === 'degraded') return [{ source, order: 2, tone: 'warning', label: t('web.overviewNext.warning'), detail: source.suspend_reason || source.last_error }]
    return []
  }).sort((a, b) => a.order - b.order)
  return <Panel id="ov-health-title" title={t('web.overviewNext.issues')} detail={t('web.overviewNext.web_health')}>
    <div className="ov-health-summary">
      <span className={`ov-health-label ov-${systemState.tone}`}><i aria-hidden="true" />{systemState.label}</span>
      <span>{t('web.overviewNext.observed_sources', { Observed: number(system.observed), Enabled: number(system.enabled) })}</span>
    </div>
    {issues.length ? <ul className="ov-issues">{issues.map(({ source, tone, label, detail }) => <li key={source.id}>
      <button type="button" className={`ov-issue ov-${tone}`} onClick={() => onSource(source.name || source.id)}>
        <i aria-hidden="true" />
        <span><strong>{source.name || source.id}</strong><span className="ov-issue-state">{label}</span>{detail && <small>{detail}</small>}</span>
        <svg aria-hidden="true" viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.4"><path d="M3 8h9m-4-4 4 4-4 4" /></svg>
      </button>
    </li>)}</ul> : <p className="ov-note">{t('web.overviewNext.no_issues')}</p>}
  </Panel>
}

function Tools({ configured, observed }: { configured: Tool[]; observed: Health[] }) {
  const byName = new Map(observed.map(tool => [tool.name, tool]))
  const total = observed.reduce((sum, tool) => sum + tool.today.requests, 0)
  const registration = { unprobed: t('web.status.not_probed'), pending: t('web.status.awaiting_registration'), unavailable: t('web.status.unavailable') }
  const health = {
    healthy: { label: t('web.status.healthy'), tone: 'success' },
    degraded: { label: t('web.status.degraded'), tone: 'warning' },
    down: { label: t('web.status.failing'), tone: 'failure' },
  }
  return <Panel id="ov-tools-title" title={t('web.page.tool_modules')} detail={t('web.overviewNext.tools_detail', { Registered: number(configured.filter(tool => tool.registered).length), Total: number(configured.length) })}>
    {!configured.length ? <p className="ov-note">{t('web.note.the_overview_endpoint_returned_no_tool_list')}</p> :
      <ul className="ov-tools">{configured.map(tool => {
        const observation = byName.get(tool.name)
        const calls = observation?.today.requests ?? 0
        const status = observation?.last_seen_at ? health[observation.status as keyof typeof health] ?? { label: t('web.status.unknown'), tone: 'unknown' } : { label: t('web.status.no_observations'), tone: 'unknown' }
        const registered = tool.registered ? t('web.status.registered') : registration[tool.state as keyof typeof registration] ?? t('web.status.not_registered')
        return <li key={tool.name}>
          <div className="ov-tool-kind"><strong>{tool.label}</strong><code>{tool.name}</code></div>
          <div className="ov-tool-states">
            <span>{t('web.overviewNext.registration', { State: registered })}{!tool.enabled && <> · {t('web.status.disabled_in_config')}</>}</span>
            <span className={`ov-health-label ov-${status.tone}`}><i aria-hidden="true" />{t('web.overviewNext.observation', { State: status.label })}</span>
          </div>
          <small>{t('web.overviewNext.tool_calls_today', { Count: number(calls) })}{total > 0 && <> · {percent(calls / total)}</>}</small>
          {total > 0 && <div className="ov-tool-bar" aria-hidden="true"><i style={{ width: `${100 * calls / total}%` }} /></div>}
        </li>
      })}</ul>}
  </Panel>
}

function RuntimeSummary({ runtime, file }: { runtime: Runtime; file?: Tool['file'] }) {
  const { proxy, llm, mcp_hub: hub } = runtime
  const uptime = Math.max(0, runtime.uptime_seconds)
  const uptimeText = uptime >= 86400 ? t('web.runtime.uptime_days', { days: Math.floor(uptime / 86400), hours: Math.floor(uptime % 86400 / 3600) })
    : uptime >= 3600 ? t('web.runtime.uptime_hours', { hours: Math.floor(uptime / 3600), minutes: Math.floor(uptime % 3600 / 60) })
      : t('web.runtime.uptime_minutes', { minutes: Math.floor(uptime / 60), seconds: uptime % 60 })
  const proxyMode = { auto: t('web.runtime.proxy_auto'), explicit: t('web.runtime.proxy_explicit'), disabled: t('web.runtime.proxy_disabled') }[proxy.mode]
  const fetchMode = { off: t('web.label.off_2'), prefer: t('web.runtime.hub_fetch_prefer'), fallback: t('web.runtime.hub_fetch_fallback') }[hub.fetch_mode]
  const fileStatus = { available: t('web.overviewNext.connected'), unavailable: t('web.status.unavailable'), unprobed: t('web.status.not_probed'), not_configured: t('web.status.not_configured') }[file?.connectivity ?? '']
  const rows = [
    [t('web.runtime.version'), runtime.version],
    [t('web.runtime.uptime'), uptimeText],
    [t('web.runtime.proxy'), proxy.mode === 'disabled' ? proxyMode : [proxyMode, proxy.resolved ? t('web.runtime.proxy_resolved') : t('web.runtime.proxy_unresolved')].join(' · ')],
    [t('web.runtime.proxy_api_providers'), onOff(proxy.api_providers)],
    [t('web.runtime.llm_summary'), llm.enabled ? [onOff(true), llm.model_id, `${llm.timeout_seconds} ${t('web.label.seconds')}`].filter(Boolean).join(' · ') : onOff(false)],
    ['MCPHub', onOff(hub.enabled)],
    [t('web.runtime.hub_fetch'), fetchMode ?? hub.fetch_mode],
    [t('web.runtime.hub_servers'), hub.servers.map(server => server.enabled ? server.name : t('web.runtime.server_disabled', { name: server.name })).join(' → ')],
    [t('web.runtime.mcp_stateless'), onOff(runtime.mcp_stateless)],
    [t('web.runtime.mcp_resources'), onOff(runtime.mcp_resources)],
    ['Everything', fileStatus ?? t('web.status.unknown')],
  ]
  return <Panel id="ov-runtime-title" title={t('web.page.service_runtime')} detail={t('web.page.effective_config')}>
    <dl className="ov-runtime">{rows.map(([label, value]) => <div key={label}><dt>{label}</dt><dd>{value || '—'}</dd></div>)}</dl>
  </Panel>
}

const appLabels: Record<string, string> = {
  codex: 'Codex', workbuddy: 'WorkBuddy', claude: 'Claude', cursor: 'Cursor', qwen: 'Qwen Code', gemini: 'Gemini',
  windsurf: 'Windsurf', cherry: 'Cherry Studio', cline: 'Cline', roo: 'Roo Code', trae: 'Trae', zed: 'Zed', vscode: 'VS Code',
  dsh: 'DeepSeek Harness', opencode: 'OpenCode', openclaw: 'OpenClaw', hermes: 'Hermes Agent', antigravity: 'Antigravity',
  amp: 'Amp', factory: 'Factory Droid', kimi: 'Kimi', copilot: 'GitHub Copilot', pi: 'Pi', omp: 'Oh My Pi', kilo: 'Kilo Code',
  commandcode: 'Command Code', mimocode: 'MiMo Code', muse: 'Muse Code', zcode: 'ZCode', kiro: 'Kiro', codebuddy: 'CodeBuddy',
  proma: 'Proma', catpaw: 'CatPaw', qoder: 'Qoder', reasonix: 'Reasonix', lmstudio: 'LM Studio', unsloth: 'Unsloth Studio',
  devin: 'Devin', minimax: 'MiniMax Code', goose: 'Goose',
}

function Clients({ result, days, setDays }: { result: Resource<ClientResponse> & { retry: () => void }; days: number; setDays: (days: number) => void }) {
  const [dimension, setDimension] = useState<'application' | 'project'>('application')
  const [app, setApp] = useState('')
  const usage = result.data?.usage ?? []
  const appName = (key: string) => !key || key === 'unknown' ? t('web.label.unrecognized_application') : appLabels[key] ?? key
  const apps = [...new Set(usage.map(row => row.application))].sort((a, b) => appName(a).localeCompare(appName(b)))
  const selectedApp = apps.includes(app) ? app : ''
  const grouped = new Map<string, { requests: number; successes: number; duration: number }>()
  for (const row of usage) {
    if (selectedApp && row.application !== selectedApp) continue
    const key = row[dimension]
    const totals = grouped.get(key) ?? { requests: 0, successes: 0, duration: 0 }
    totals.requests += row.requests
    totals.successes += row.successes
    totals.duration += row.duration_ms
    grouped.set(key, totals)
  }
  const total = [...grouped.values()].reduce((sum, row) => sum + row.requests, 0)
  const dimensions = { application: t('web.label.application'), project: t('web.label.project') }
  const controls = <div className="ov-segments" role="group" aria-label={t('web.label.usage_dimension')}>
    {(['application', 'project'] as const).map(key => <button key={key} type="button" aria-pressed={dimension === key} onClick={() => setDimension(key)}>{dimensions[key]}</button>)}
  </div>
  return <Panel id="ov-clients-title" title={t('web.overviewNext.client_usage')} detail={result.data && t('web.overviewNext.last_days', { Count: result.data.window_days })} controls={controls}>
    <div className="ov-client-filters">
      <label>{t('web.label.statistics_window')} <select value={days} onChange={event => setDays(Number(event.target.value))}>
        {[1, 7, 30].map(value => <option key={value} value={value}>{daysText(value)}</option>)}
      </select></label>
      <label>{t('web.label.application_filter')} <select value={selectedApp} onChange={event => setApp(event.target.value)}>
        <option value="">{t('web.label.all_applications')}</option>
        {apps.filter(Boolean).map(key => <option key={key} value={key}>{appName(key)}</option>)}
      </select></label>
    </div>
    <LoadNote result={result} />
    {result.data && (total ? <div className="ov-client-table" tabIndex={0} role="region" aria-labelledby="ov-clients-title">
      <table>
        <thead><tr><th scope="col">{dimensions[dimension]}</th><th scope="col">{t('web.label.calls')}</th><th scope="col">{t('web.label.share')}</th><th scope="col">{t('web.label.success_rate')}</th><th scope="col">{t('web.label.average_response_time')}</th></tr></thead>
        <tbody>{[...grouped].sort(([a, aa], [b, bb]) => bb.requests - aa.requests || a.localeCompare(b)).map(([key, row]) =>
          <tr key={key}>
            <th scope="row">{dimension === 'application' ? appName(key) : key || t('web.label.untagged_project')}</th>
            <td>{number(row.requests)}</td>
            <td>{percent(row.requests / total)}</td>
            <td>{row.requests ? percent(row.successes / row.requests) : '—'}</td>
            <td>{row.requests ? ms(row.duration / row.requests) : '—'}</td>
          </tr>)}</tbody>
      </table>
    </div> : <p className="ov-note">{t('web.note.no_calls_in_this_window_yet')}</p>)}
  </Panel>
}

export interface OverviewProps {
  onSource: (name: string) => void
  refresh: number
  onSnapshot?: (data: OverviewResponse) => void
  onRefreshStatus?: (state: LoadStatus) => void
}

export function Overview({ onSource, refresh, onSnapshot, onRefreshStatus }: OverviewProps) {
  const [days, setDays] = useState(7)
  const snapshot = useResource<OverviewResponse>('overview', refresh)
  const clients = useResource<ClientResponse>(`clients?days=${days}`, refresh)
  const callbacks = useRef({ onSnapshot, onRefreshStatus })
  useEffect(() => { callbacks.current = { onSnapshot, onRefreshStatus } }, [onSnapshot, onRefreshStatus])
  useEffect(() => {
    if (snapshot.data) callbacks.current.onSnapshot?.(snapshot.data)
  }, [snapshot.data])
  useEffect(() => {
    callbacks.current.onRefreshStatus?.(snapshot.status === 'loading' || clients.status === 'loading' ? 'loading'
      : snapshot.status === 'error' || clients.status === 'error' ? 'error' : 'success')
  }, [snapshot.status, clients.status])

  const overview = snapshot.data
  return <div className="operate-overview">
    {(!overview || snapshot.status === 'error') && <Panel id="ov-unavailable-title" title={t('web.page.overview')}><LoadNote result={snapshot} /></Panel>}
    {overview && <OverviewSnapshot data={overview} onSource={onSource} />}
    <div className="ov-primary ov-secondary">
      <Clients result={clients} days={days} setDays={setDays} />
      {overview && <RuntimeSummary runtime={overview.runtime} file={overview.configured_tools.find(tool => tool.name === 'file_search')?.file} />}
    </div>
  </div>
}

function OverviewSnapshot({ data, onSource }: { data: OverviewResponse; onSource: OverviewProps['onSource'] }) {
  const [now, setNow] = useState(Date.now)
  useEffect(() => setNow(Date.now()), [data])
  const sources = [...data.sources, ...(data.academic_sources ?? [])].filter(source => source.active)
  const cooled = new Map(sources.flatMap(source => {
    const remaining = Math.ceil((Date.parse(source.actual_cooldown?.until ?? '') - now) / 1000)
    return remaining > 0 ? [[source.id, remaining] as const] : []
  }))
  const active = cooled.size > 0
  useEffect(() => {
    if (!active) return
    const tick = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(tick)
  }, [active])
  const { today } = data
  const metrics = [
    [t('web.label.calls'), number(today.requests), t('web.overviewNext.today_tools')],
    [t('web.label.success_rate'), today.requests ? percent(today.successes / today.requests) : '—', t('web.overviewNext.today_tools')],
    [t('web.label.average_response_time'), today.requests ? ms(today.duration_ms / today.requests) : '—', t('web.overviewNext.today_tools')],
    [t('web.overviewNext.routable'), `${number(sources.length - cooled.size)} / ${number(sources.length)}`,
      t('web.overviewNext.cooling_count', { Count: cooled.size })],
  ]
  return <>
    <dl className="ov-metrics">{metrics.map(([label, value, detail], index) => <div className="ov-metric" key={label}>
      <dt>{label}</dt><dd className={`ov-metric-value${index === 1 && today.requests > 0 && today.failures === 0 ? ' ov-success' : ''}`}>{value}</dd>
      <dd className={index === 3 && cooled.size ? 'ov-cooldown' : ''} aria-live={index === 3 ? 'polite' : undefined}>{detail}</dd>
    </div>)}</dl>
    <div className="ov-primary">
      <Trend today={today} history={data.trend ?? []} />
      <HealthSummary sources={sources} system={data.system} cooled={cooled} onSource={onSource} />
    </div>
    <Tools configured={data.configured_tools} observed={data.tools ?? []} />
  </>
}

export default Overview
