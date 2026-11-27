import { formatters } from './formatters'
import * as Dialog from '@radix-ui/react-dialog'
import * as Popover from '@radix-ui/react-popover'
import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { api } from './api'
import { t, useLocale } from './i18n'
import { Icon } from './navigation'
import './traces.css'

interface TraceEvent {
  id: number
  occurred_at: string
  kind: string
  tool: string
  provider: string
  success: boolean
  duration_ms: number
  cache_hit: boolean
  result_count: number
  request_id?: string
  client?: string
  application?: string
  project?: string
  outcome?: string
  detail?: string
  skip_reason?: string
  error_kind?: string
  error_summary?: string
  query_hash?: string
  query_chars?: number
  query_topic?: string
  query_language?: string
  query_keywords?: string
  attempt_chain?: string
}

type Filter = 'error_kind' | 'tool' | 'provider' | 'application' | 'project' | 'cache_hit'
type Filters = Partial<Record<Filter | 'status', string>>
type Option = [value: string, label: string]
type Calls = { rows: TraceEvent[]; hasMore: boolean }
type Data<T> = { value?: T; error?: string; loading: boolean }
type Catalog = {
  configured_tools?: { name: string; label: string }[]
  tools?: { name: string }[] | null
  sources?: { id: string; name: string }[]
  academic_sources?: { id: string; name: string }[]
  providers?: { name: string }[] | null
}
type Usage = { usage: { application: string; project: string; tool: string }[] | null }

const pageSize = 50
const dimensions = [
  { key: 'error_kind', label: 'web.label.error_kind' },
  { key: 'tool', label: 'web.label.tool', free: true },
  { key: 'provider', label: 'web.label.source', free: true },
  { key: 'application', label: 'web.label.application', free: true },
  { key: 'project', label: 'web.label.project', free: true },
  { key: 'cache_hit', label: 'web.label.cache' },
] as const
const errors = {
  rate_limit: 'web.status.rate_limited',
  captcha: 'web.status.captcha',
  access_denied: 'web.status.access_denied',
  timeout: 'web.status.timeout',
  parse: 'web.status.parse_failed',
  no_result: 'web.status.no_results',
  network: 'web.status.network_server',
  unknown: 'web.status.unknown_error',
}
const statuses: Option[] = [['', 'web.label.all'], ['success', 'web.status.success'], ['failure', 'web.status.failure']]
const appNames: Record<string, string> = {
  codex: 'Codex', workbuddy: 'WorkBuddy', claude: 'Claude', cursor: 'Cursor', vscode: 'VS Code',
  qwen: 'Qwen Code', roo: 'Roo Code', gemini: 'Gemini', dsh: 'DeepSeek Harness', copilot: 'GitHub Copilot',
}

function applicationName(value = '') {
  return !value || value === 'unknown' ? t('web.label.unrecognized_application') : appNames[value] ?? value
}

function errorName(value: string) {
  const key = errors[value as keyof typeof errors]
  return key ? t(key) : value
}

function statusLabel(event: TraceEvent) {
  return event.kind === 'quota_skip' || event.outcome === 'skipped' ? t('web.status.skipped') : event.success ? t('web.status.success') : t('web.status.failure')
}

function statusClass(event: TraceEvent) {
  return event.kind === 'quota_skip' || event.outcome === 'skipped' ? 'trace-skipped' : event.success ? 'trace-success' : 'trace-failure'
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

function callPath(query: string, before?: TraceEvent) {
  const parameters = new URLSearchParams(query)
  parameters.set('limit', String(pageSize))
  if (before) {
    const timestamp = Math.floor(Date.parse(before.occurred_at) / 1000)
    if (!Number.isSafeInteger(timestamp) || timestamp <= 0 || !Number.isSafeInteger(before.id) || before.id <= 0) {
      throw new Error(t('web.tracesNext.invalid_calls'))
    }
    parameters.set('before', `${timestamp}:${before.id}`)
  }
  return 'calls?' + parameters.toString()
}

async function readCalls(query: string, signal: AbortSignal, before?: TraceEvent) {
  const rows = await api<TraceEvent[]>(callPath(query, before), { signal })
  if (!Array.isArray(rows) || rows.length > pageSize || rows.some((row, index) => {
    if (row?.kind !== 'tool' || !Number.isSafeInteger(row.id) || row.id <= 0) return true
    const date = Date.parse(row.occurred_at)
    if (!Number.isFinite(date)) return true
    const prior = index ? rows[index - 1] : before
    if (!prior) return false
    const upper = Date.parse(prior.occurred_at)
    return date > upper || (date === upper && row.id >= prior.id)
  })) {
    throw new Error(t('web.tracesNext.invalid_calls'))
  }
  return rows
}

async function readAttempts(requestId: string, signal: AbortSignal, requiredCallId?: number) {
  const rows = await api<TraceEvent[]>('request-events?' + new URLSearchParams({ request_id: requestId }), { signal })
  if (!Array.isArray(rows) || rows.some(row => row?.request_id !== requestId) ||
      (requiredCallId !== undefined && !rows.some(row => row.kind === 'tool' && row.id === requiredCallId))) {
    throw new Error(t('web.msg.malformed_request_details_response'))
  }
  return rows.filter(row => row.kind === 'provider' || row.kind === 'quota_skip')
}

function useRead<T>(key: string, refresh: number, read: (signal: AbortSignal) => Promise<T>) {
  const [load, setLoad] = useState<Data<T>>({ loading: true })
  const [retry, setRetry] = useState(0)
  const [loadedKey, setLoadedKey] = useState(key)
  const readRef = useRef(read)
  useEffect(() => { readRef.current = read }, [read])
  useEffect(() => {
    const controller = new AbortController()
    setLoad({ loading: true })
    setLoadedKey(key)
    if (key) {
      const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(15000)])
      void readRef.current(signal).then(value => {
        if (!controller.signal.aborted) setLoad({ value, loading: false })
      }, error => {
        if (!controller.signal.aborted) setLoad({ error: errorText(error), loading: false })
      })
    }
    return () => controller.abort()
  }, [key, refresh, retry])
  return { ...(loadedKey === key ? load : { loading: true }), setLoad, retry: () => setRetry(value => value + 1) } as Data<T> & {
    setLoad: typeof setLoad; retry: () => void
  }
}

function TraceButton({ children, onClick, disabled }: { children: React.ReactNode; onClick: () => void; disabled?: boolean }) {
  return <button type="button" className="trace-button" onClick={onClick} disabled={disabled}>{children}</button>
}

function FilterCapsule({ label, value, options, select, free = false }: {
  label: string; value: string; options: Option[]; select: (next: string) => void; free?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState('')
  const search = input.trim().toLocaleLowerCase()
  const matching = options.filter(([id, name]) => id.toLocaleLowerCase().includes(search) || name.toLocaleLowerCase().includes(search))
  const exact = input.trim()
  function choose(next: string) { select(next); setOpen(false) }
  function focusOption(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return
    const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[data-trace-option]')]
    const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
    const delta = event.key === 'ArrowDown' ? 1 : -1
    const next = index < 0 ? (delta > 0 ? 0 : buttons.length - 1) : (index + delta + buttons.length) % buttons.length
    if (buttons[next]) { event.preventDefault(); buttons[next].focus() }
  }
  return <Popover.Root open={open} onOpenChange={next => { if (next) setInput(''); setOpen(next) }}>
    <Popover.Trigger className="trace-filter-button" data-active={!!value}>
      <span>{label}</span>{value && <b>{options.find(([id]) => id === value)?.[1] ?? value}</b>}
      <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" aria-hidden="true"><path d="m3 4.5 3 3 3-3" /></svg>
    </Popover.Trigger>
    <Popover.Portal>
      <Popover.Content className="trace-popover" aria-label={label} sideOffset={7} align="start" collisionPadding={12} onKeyDown={focusOption}>
        {free && <form onSubmit={event => {
          event.preventDefault()
          if (exact) choose(options.find(([id, name]) => id === exact || name === exact)?.[0] ?? exact)
        }}>
          <input value={input} onChange={event => setInput(event.target.value)} aria-label={t('web.tracesNext.filter_search', { Label: label })} placeholder={t('web.tracesNext.filter_search', { Label: label })} />
        </form>}
        <div className="trace-option-list">
          <button type="button" data-trace-option aria-pressed={!value} onClick={() => choose('')}>{t('web.label.all')}</button>
          {matching.map(([id, name]) => <button type="button" data-trace-option key={id} aria-pressed={value === id} onClick={() => choose(id)}>{name}</button>)}
          {free && exact && !options.some(([id, name]) => id === exact || name === exact) &&
            <button type="button" data-trace-option onClick={() => choose(exact)}>{t('web.tracesNext.exact_filter', { Value: exact })}</button>}
        </div>
      </Popover.Content>
    </Popover.Portal>
  </Popover.Root>
}

function logTime(value: string, locale: string, compact = false) {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return '—'
  return formatters(locale)[compact ? 'clock' : 'logTime'].format(date)
}

function CallInspector({ call, previous, next, onClose, restoreFocus, refresh, sourceName }: {
  call: TraceEvent; previous?: () => void; next?: () => void; onClose: () => void; restoreFocus: () => void
  refresh: number; sourceName: (key: string) => string
}) {
  const locale = useLocale()
  const requestId = call.request_id ?? ''
  const details = useRead(requestId && `${locale}:${requestId}`, refresh, signal => readAttempts(requestId, signal))
  const [clipboard, setClipboard] = useState<{ id: string; ok: boolean }>()
  const number = formatters(locale).count
  const latency = (value: number) => `${number.format(value)} ms`
  const entries = [
    [t('web.label.tool'), call.tool], [t('web.label.time'), logTime(call.occurred_at, locale)],
    [t('web.label.application'), applicationName(call.application)], [t('web.label.project'), call.project || t('web.label.untagged_project')],
    [t('web.label.client'), call.client], [t('web.label.latency'), latency(call.duration_ms)],
    [t('web.label.results'), number.format(call.result_count)], [t('web.label.cache'), call.cache_hit ? t('web.status.hit') : t('web.status.miss')],
    [t('web.label.topic_language'), [call.query_topic, call.query_language].filter(Boolean).join(' · ')],
    [t('web.label.keywords'), call.query_keywords], [t('web.label.query_hash'), call.query_hash],
    [t('web.label.query_length'), call.query_chars], [t('web.label.attempt_chain'), call.attempt_chain],
    [t('web.label.error_kind'), call.error_kind && errorName(call.error_kind)], [t('web.label.error_summary'), call.error_summary],
  ]
  const attempts = details.value ?? []
  const asTimeline = attempts.map(row => {
    const end = Date.parse(row.occurred_at)
    return { row, start: end - Math.max(0, row.duration_ms), end }
  })
  const callEnd = Date.parse(call.occurred_at)
  const bounds = asTimeline.reduce((bounds, attempt) => [
    Number.isFinite(attempt.start) ? Math.min(bounds[0], attempt.start) : bounds[0],
    Number.isFinite(attempt.end) ? Math.max(bounds[1], attempt.end) : bounds[1],
  ], [callEnd - call.duration_ms, callEnd])
  const [start, end] = bounds
  const span = Math.max(1, end - start)
  function switchOnArrow(event: KeyboardEvent<HTMLDivElement>) {
    const move = event.key === 'ArrowUp' ? previous : event.key === 'ArrowDown' ? next : undefined
    if (move) { event.preventDefault(); move() }
  }
  return <Dialog.Root open modal={false} onOpenChange={open => { if (!open) onClose() }}>
    <Dialog.Portal>
      <div className="trace-inspector-shade" aria-hidden="true" />
      <Dialog.Content id="trace-inspector" className="trace-inspector" onKeyDown={switchOnArrow}
        onInteractOutside={event => event.preventDefault()} onCloseAutoFocus={event => { event.preventDefault(); restoreFocus() }}>
        <header className="trace-inspector-head">
          <div><Dialog.Title>{t('web.tracesNext.details')}</Dialog.Title><Dialog.Description>{t('web.tracesNext.inspector_description')}</Dialog.Description></div>
          <Dialog.Close className="trace-icon-button" aria-label={t('web.next.close')}><Icon name="close" size={17} /></Dialog.Close>
        </header>
        <div className="trace-inspector-body">
          <div className="trace-inspector-actions">
            <TraceButton onClick={() => previous?.()} disabled={!previous}>{t('web.tracesNext.previous_call')}</TraceButton>
            <TraceButton onClick={() => next?.()} disabled={!next}>{t('web.tracesNext.next_call')}</TraceButton>
          </div>
          <div className="trace-request">
            <div><span>{t('web.label.request_id')}</span><code>{requestId || '—'}</code></div>
            {requestId && <TraceButton onClick={() => {
              void Promise.resolve().then(() => navigator.clipboard.writeText(requestId))
                .then(() => setClipboard({ id: requestId, ok: true }), () => setClipboard({ id: requestId, ok: false }))
            }}>{t('web.tracesNext.copy')}</TraceButton>}
          </div>
          {clipboard?.id === requestId && <p className="trace-copy-feedback" role="status">{t(clipboard.ok ? 'web.tracesNext.copied' : 'web.tracesNext.copy_failed')}</p>}
          <dl className="trace-meta">{entries.filter(([, value]) => value !== undefined && value !== '').map(([label, value]) =>
            <div key={label}><dt>{label}</dt><dd>{value}</dd></div>)}</dl>
          <h3>{t('web.tracesNext.source_attempts')}</h3>
          {!requestId && <p>{t('web.note.this_history_entry_has_no_request_id_so_source_details_canno')}</p>}
          {requestId && details.loading && <p role="status">{t('web.note.loading_source_details')}</p>}
          {requestId && details.error && <div className="trace-feedback">
            <p role="alert">{t('web.note.read_failed')}{details.error}</p>
            <TraceButton onClick={details.retry}>{t('web.action.retry_loading_source_details')}</TraceButton>
          </div>}
          {requestId && details.value && (attempts.length ? <>
            <p className="trace-inspector-note">{t('web.tracesNext.timing_precision')}</p>
            <ol className="trace-attempts">{asTimeline.map(({ row, start: offset, end }) => {
              const left = Number.isFinite(offset) ? (offset - start) / span * 100 : 0
              const width = Number.isFinite(end) ? (end - offset) / span * 100 : 0
              const skipped = statusClass(row) === 'trace-skipped'
              return <li key={row.id}>
                <div className="trace-attempt-head"><strong>{sourceName(row.provider)}</strong><time dateTime={row.occurred_at}>{logTime(row.occurred_at, locale, true)}</time></div>
                <div className="trace-attempt-track" aria-hidden="true"><i className={statusClass(row)} style={{ left: `${Math.min(99, left)}%`, width: `${width}%` }} /></div>
                <div className="trace-attempt-info"><span className={`trace-status ${statusClass(row)}`}>{statusLabel(row)}</span>
                  {row.error_kind && <span className="trace-kind">{errorName(row.error_kind)}</span>}
                  {!skipped && <span>{latency(row.duration_ms)} · {t('web.tracesNext.result_count', { Count: number.format(row.result_count) })}</span>}</div>
                {(row.skip_reason || row.error_summary) && <p className="trace-attempt-error">{row.skip_reason || row.error_summary}</p>}
              </li>
            })}</ol>
          </> : <p>{t('web.note.no_source_details_were_retained')}</p>)}
        </div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
}

function csvCell(value: string | number | undefined) {
  const text = String(value ?? '').replace(/[\r\n]+/g, ' ')
  const safe = typeof value === 'string' && /^(?:\s*[=+@-]|[\t\r\n])/.test(value) ? "'" + text : text
  return '"' + safe.replace(/"/g, '""') + '"'
}

function makeCSV(rows: TraceEvent[]) {
  const header = [
    t('web.label.time'), t('web.label.kind'), t('web.label.tool'), t('web.label.source'), t('web.label.parsed_source'),
    t('web.label.status'), t('web.label.error_kind'), t('web.label.latency_ms'), t('web.label.results'), t('web.label.cache'),
    t('web.label.theme'), t('web.label.language'), t('web.label.keywords'), t('web.label.query_hash'), t('web.label.query_length'),
    t('web.label.error_summary'), t('web.label.request_id'), t('web.label.attempt_chain'), t('web.label.application'), t('web.label.client'), t('web.label.project'),
  ]
  const body = rows.map(row => [
    row.occurred_at, row.kind, row.tool, row.provider, row.detail, statusLabel(row), row.error_kind, row.duration_ms, row.result_count,
    row.cache_hit ? t('web.label.yes') : t('web.label.no'), row.query_topic, row.query_language, row.query_keywords, row.query_hash, row.query_chars,
    row.skip_reason || row.error_summary, row.request_id, row.attempt_chain, applicationName(row.application), row.client, row.project,
  ].map(csvCell).join(','))
  return new Blob(['\uFEFF', header.map(csvCell).join(','), '\r\n', body.join('\r\n')], { type: 'text/csv;charset=utf-8' })
}

function downloadCsv(blob: Blob) {
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `websearch-traces-${new Date().toISOString().slice(0, 19).replaceAll(':', '-')}.csv`
  document.body.append(link)
  try { link.click() } finally {
    link.remove()
    setTimeout(() => URL.revokeObjectURL(url), 2000)
  }
}

function optionsFor(catalog: Catalog | undefined, usage: Usage | undefined, calls: TraceEvent[]) {
  const tool = new Map<string, string>((catalog?.configured_tools ?? []).map(row => [row.name, row.label]))
  const provider = new Map<string, string>([...(catalog?.sources ?? []), ...(catalog?.academic_sources ?? [])].map(row => [row.id, row.name]))
  for (const row of catalog?.tools ?? []) if (!tool.has(row.name)) tool.set(row.name, row.name)
  for (const row of catalog?.providers ?? []) if (!provider.has(row.name)) provider.set(row.name, row.name)
  const applications = new Map<string, string>()
  const projects = new Map<string, string>()
  for (const row of [...(usage?.usage ?? []), ...calls]) {
    if (row.application) applications.set(row.application, applicationName(row.application))
    if (row.project) projects.set(row.project, row.project)
    if (row.tool && !tool.has(row.tool)) tool.set(row.tool, row.tool)
  }
  const sort = (map: Map<string, string>) => [...map].sort(([, a], [, b]) => a.localeCompare(b))
  return {
    tool: sort(tool), provider: sort(provider), application: sort(applications), project: sort(projects),
    error_kind: Object.entries(errors).map(([value, key]) => [value, t(key)] as Option),
    cache_hit: [['true', t('web.status.hit')], ['false', t('web.status.miss')]] as Option[],
  }
}

export default function Traces({ item, refresh }: { item?: string; refresh: number }) {
  const locale = useLocale()
  const [filters, setFilters] = useState<Filters>(() => item ? { provider: item } : {})
  const [selectedId, setSelectedId] = useState<number>()
  const [localRefresh, setLocalRefresh] = useState(0)
  const [paging, setPaging] = useState(false)
  const [pageError, setPageError] = useState('')
  const [exporting, setExporting] = useState(false)
  const [exportNote, setExportNote] = useState<{ error: boolean; text: string }>()
  const buttons = useRef(new Map<number, HTMLButtonElement>())
  const scrollRef = useRef<HTMLDivElement>(null)
  const returnId = useRef<number>(undefined)
  const pagingJob = useRef<AbortController | null>(null)
  const exportJob = useRef<AbortController | null>(null)
  const query = new URLSearchParams(Object.entries(filters).filter(([, value]) => value)).toString()
  const load = useRead<Calls>(`${locale}:${query}`, refresh + localRefresh, async signal => {
    const rows = await readCalls(query, signal)
    return { rows, hasMore: rows.length === pageSize }
  })
  const catalog = useRead<Catalog>(locale, refresh, signal => api('overview', { signal }))
  const usage = useRead<Usage>(locale, refresh, signal => api('clients?days=30', { signal }))
  const calls = load.value?.rows ?? []
  const options = optionsFor(catalog.value, usage.value, calls)
  const selected = calls.findIndex(row => row.id === selectedId)
  const number = formatters(locale).count
  const providerName = (id: string) => options.provider.find(([value]) => id === value)?.[1] ?? id

  useEffect(() => { setFilters(item ? { provider: item } : {}); setSelectedId(undefined) }, [item])
  useEffect(() => {
    setPaging(false)
    setPageError('')
    setExporting(false)
    setExportNote(undefined)
    return () => { pagingJob.current?.abort(); exportJob.current?.abort() }
  }, [query, refresh, localRefresh, locale])

  function selectFilter(key: Filter | 'status', value: string) {
    setFilters(current => ({ ...current, [key]: value }))
    setSelectedId(undefined)
  }

  function openRow(id: number) {
    returnId.current = id
    setSelectedId(id)
  }
  function moveRow(index: number, event: KeyboardEvent<HTMLButtonElement>) {
    const delta = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
    if (!delta || !calls[index + delta]) return
    event.preventDefault()
    const id = calls[index + delta].id
    buttons.current.get(id)?.focus()
    if (selectedId !== undefined) openRow(id)
  }

  async function loadMore() {
    const before = calls.at(-1)
    if (!before || paging || !load.value?.hasMore) return
    const controller = new AbortController()
    pagingJob.current = controller
    setPaging(true)
    setPageError('')
    try {
      const more = await readCalls(query, AbortSignal.any([controller.signal, AbortSignal.timeout(15000)]), before)
      if (!controller.signal.aborted) load.setLoad({ loading: false, value: { rows: [...calls, ...more], hasMore: more.length === pageSize } })
    } catch (error) {
      if (!controller.signal.aborted) setPageError(errorText(error))
    } finally {
      if (!controller.signal.aborted) setPaging(false)
    }
  }

  async function exportAll() {
    if (exporting || !calls.length || !load.value) return
    const controller = new AbortController()
    exportJob.current = controller
    const signal = controller.signal
    const withTimeout = () => AbortSignal.any([signal, AbortSignal.timeout(15000)])
    setExporting(true)
    setExportNote(undefined)
    try {
      const included = new Set<string>()
      const records: TraceEvent[] = []
      let page = calls
      let hasMore = load.value.hasMore
      for (;;) {
        for (const call of page) {
          records.push(call)
          if (call.request_id && !included.has(call.request_id)) {
            included.add(call.request_id)
            records.push(...await readAttempts(call.request_id, withTimeout(), call.id))
          }
        }
        if (!hasMore) break
        page = await readCalls(query, withTimeout(), page.at(-1))
        hasMore = page.length === pageSize
      }
      if (signal.aborted) return
      downloadCsv(makeCSV(records))
      setExportNote({ error: false, text: t('web.tracesNext.exported', { Count: number.format(records.length) }) })
    } catch (error) {
      if (!signal.aborted) setExportNote({ error: true, text: t('web.tracesNext.export_failed') + ' ' + errorText(error) })
    } finally {
      if (!signal.aborted) setExporting(false)
    }
  }

  return <div className="trace-page">
    <section className="trace-controls" aria-label={t('web.tracesNext.filters')}>
      <div className="trace-toolbar">
        <div className="trace-segments" role="group" aria-label={t('web.label.status')}>
          {statuses.map(([value, label]) => <button type="button" key={value} aria-pressed={(filters.status ?? '') === value} onClick={() => selectFilter('status', value)}>{t(label)}</button>)}
        </div>
        <div className="trace-actions">
          <TraceButton onClick={() => setLocalRefresh(n => n + 1)} disabled={load.loading || exporting}><Icon name="refresh" size={14} />{t('web.action.refresh_call_log')}</TraceButton>
          <TraceButton onClick={() => void exportAll()} disabled={!calls.length || load.loading || exporting}>
            <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.4" aria-hidden="true"><path d="M8 2v8m-3-3 3 3 3-3M2 10v4h12v-4" /></svg>{t('web.action.export_csv')}
          </TraceButton>
        </div>
      </div>
      <div className="trace-capsules">
        {dimensions.map(dimension => <FilterCapsule key={dimension.key} label={t(dimension.label)} value={filters[dimension.key] ?? ''}
          options={options[dimension.key]} free={'free' in dimension} select={value => selectFilter(dimension.key, value)} />)}
        {query && <button type="button" className="trace-reset" onClick={() => { setFilters({}); setSelectedId(undefined) }}>{t('web.tracesNext.clear_filters')}</button>}
      </div>
    </section>
    <section className="trace-list" aria-label={t('web.next.traces')}>
      <header><h2>{t('web.tracesNext.recent_calls')}</h2>{!load.loading && !load.error && <small>{t('web.tracesNext.loaded_count', { Count: number.format(calls.length) })}</small>}</header>
      {exporting && <p className="trace-feedback" role="status">{t('web.note.exporting_calls_and_source_details')}</p>}
      {exportNote && <p className={`trace-feedback${exportNote.error ? ' trace-error' : ''}`} role={exportNote.error ? 'alert' : 'status'}>{exportNote.text}</p>}
      {load.loading && <div className="trace-load" role="status"><span>{t('web.note.loading_2')}</span><div aria-hidden="true">{Array.from({ length: 5 }, (_, index) => <i key={index} />)}</div></div>}
      {load.error && <div className="trace-feedback trace-error"><p role="alert">{t('web.note.read_failed')}{load.error}</p><TraceButton onClick={load.retry}>{t('web.overviewNext.retry')}</TraceButton></div>}
      {load.value && (calls.length ? <>
        <div className="trace-table-scroll" ref={scrollRef} tabIndex={0} role="region" aria-label={t('web.tracesNext.recent_calls')}>
          <table className="trace-table" role="table">
            <thead role="rowgroup"><tr role="row">
              <th scope="col" role="columnheader">{t('web.label.time')}</th><th scope="col" role="columnheader">{t('web.label.tool')}</th><th scope="col" role="columnheader">{t('web.label.application_project')}</th>
              <th scope="col" role="columnheader">{t('web.label.status')}</th><th scope="col" role="columnheader">{t('web.label.latency')}</th><th scope="col" role="columnheader">{t('web.label.results')}</th><th scope="col" role="columnheader">{t('web.label.error_summary')}</th>
            </tr></thead>
            <tbody role="rowgroup">{calls.map((row, index) => <tr role="row" key={row.id} data-selected={row.id === selectedId} onClick={() => openRow(row.id)}>
              <th scope="row" role="rowheader"><button type="button" className="trace-row-open" ref={element => { if (element) buttons.current.set(row.id, element); else buttons.current.delete(row.id) }}
                onClick={event => { event.stopPropagation(); openRow(row.id) }} onKeyDown={event => moveRow(index, event)}
                aria-haspopup="dialog" aria-expanded={row.id === selectedId} aria-controls={row.id === selectedId ? 'trace-inspector' : undefined}
                aria-label={t('web.tracesNext.inspect_row', { Tool: row.tool, Time: logTime(row.occurred_at, locale) })}>
                <time dateTime={row.occurred_at}>{logTime(row.occurred_at, locale)}</time>
              </button></th>
              <td role="cell"><code>{row.tool}</code></td>
              <td role="cell"><strong className="trace-app">{applicationName(row.application)}</strong><small title={t('web.note.self_reported_project_label_not_verified_by_the_system')}>{row.project || t('web.label.untagged_project')}</small></td>
              <td role="cell"><span className={`trace-status ${statusClass(row)}`}>{statusLabel(row)}</span></td>
              <td role="cell" className="trace-numeric">{number.format(row.duration_ms)} ms</td>
              <td role="cell" className="trace-numeric">{number.format(row.result_count)}</td>
              <td role="cell" className="trace-summary-cell" data-empty={!row.error_summary}><span className="trace-summary" title={row.error_summary}>{row.error_summary || '—'}</span></td>
            </tr>)}</tbody>
          </table>
        </div>
        {(load.value.hasMore || pageError) && <footer className="trace-page-footer">
          {pageError && <p className="trace-error" role="alert">{t('web.note.read_failed')}{pageError}</p>}
          <TraceButton onClick={() => void loadMore()} disabled={paging}>{t(paging ? 'web.note.loading_2' : 'web.tracesNext.load_older')}</TraceButton>
        </footer>}
      </> : <p className="trace-feedback">{t('web.note.no_calls_match_the_current_filters')}</p>)}
    </section>
    {selected >= 0 && <CallInspector call={calls[selected]} refresh={refresh + localRefresh} sourceName={providerName}
      previous={selected > 0 ? () => openRow(calls[selected - 1].id) : undefined}
      next={selected + 1 < calls.length ? () => openRow(calls[selected + 1].id) : undefined}
      onClose={() => setSelectedId(undefined)}
      restoreFocus={() => (returnId.current !== undefined ? buttons.current.get(returnId.current) ?? scrollRef.current : scrollRef.current)?.focus()} />}
  </div>
}
