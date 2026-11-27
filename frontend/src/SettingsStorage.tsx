import { useRef, useState } from 'react'
import { api } from './api'
import { t, useLocale } from './i18n'
import { useRuntimeAccess } from './SourcesCommands'
import { DataReadNote, SettingsDataDialog, dataError, jsonPost, useSettingsRead } from './settingsData'

type StorageEntry = { enabled: boolean; count: number; bytes: number; error?: string }
type StorageStats = { cache: StorageEntry; history: StorageEntry; fetchdata: StorageEntry & { expired: number; clearable: number } }
type StorageAction = 'cache' | 'history' | 'expired' | 'all'

const isCount = (value: number) => Number.isSafeInteger(value) && value >= 0
const validStorage = (data: StorageStats) => Boolean(data && ['cache', 'history', 'fetchdata'].every(kind => {
  const value = data[kind as keyof StorageStats]
  return value && typeof value.enabled === 'boolean' && isCount(value.count) && isCount(value.bytes)
}) && isCount(data.fetchdata.expired) && isCount(data.fetchdata.clearable))
const operation = {
  cache: { title: 'web.action.clear_search_cache', detail: 'web.dialog.this_cannot_be_undone_but_monitoring_history_is_not_deleted', endpoint: 'cache/clear', kind: 'cache' },
  history: { title: 'web.storage.clear_history', detail: 'web.storage.history_confirm', endpoint: 'history/clear', kind: 'history' },
  expired: { title: 'web.storage.clear_expired', detail: 'web.storage.expired_confirm', endpoint: 'fetchdata/clear', kind: 'fetchdata' },
  all: { title: 'web.storage.clear_all', detail: 'web.storage.all_confirm', endpoint: 'fetchdata/clear', kind: 'fetchdata' },
} as const

function diskUse(bytes: number, locale: string) {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  let scale = 0
  while (bytes >= 1024 && scale < units.length - 1) {
    bytes /= 1024
    scale++
  }
  return `${new Intl.NumberFormat(locale, { maximumFractionDigits: 1 }).format(bytes)} ${units[scale]}`
}

export default function SettingsStorage({ refresh }: { refresh: number }) {
  const locale = useLocale()
  const format = new Intl.NumberFormat(locale)
  const { allowed, reason } = useRuntimeAccess()
  const snapshot = useSettingsRead('storage', refresh, validStorage)
  const [action, setAction] = useState<StorageAction>()
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<{ error: boolean; text: string }>()
  const opener = useRef<HTMLButtonElement>(null)
  const refreshButton = useRef<HTMLButtonElement>(null)
  const current = action && operation[action]
  const locked = !allowed || snapshot.loading || Boolean(snapshot.error) || busy

  async function clear() {
    if (!action || !current || locked) return
    setBusy(true)
    setFeedback(undefined)
    let result: { cleared: number } | undefined
    let failure = ''
    try {
      const data = await api<{ cleared: number }>(current.endpoint, {
        ...jsonPost({ confirm: true, ...(current.kind === 'fetchdata' ? { scope: action } : {}) }),
        signal: AbortSignal.timeout(20000),
      })
      if (!isCount(data.cleared)) throw new Error(t('web.msg.response_is_not_valid_json'))
      result = data
    } catch (error) {
      failure = dataError(error)
    }
    const latest = await snapshot.reload()
    const row = latest?.[current.kind]
    const feedbackParts: string[] = []
    if (failure) feedbackParts.push(failure)
    else if (result) feedbackParts.push(t('web.storage.cleared', { Count: format.format(result.cleared) }))
    if (row && !row.error && row.enabled) {
      feedbackParts.push(current.kind === 'fetchdata' && latest
        ? t('web.settingsData.fetch_after', {
          Total: format.format(row.count), Expired: format.format(latest.fetchdata.expired), Clearable: format.format(latest.fetchdata.clearable),
        })
        : t('web.settingsData.storage_after', { Count: format.format(row.count) }))
    } else feedbackParts.push(t('web.settingsData.storage_unverified'))
    setFeedback({ error: Boolean(failure || !row || row.error || !row.enabled), text: feedbackParts.join(' ') })
    setAction(undefined)
    setBusy(false)
  }

  const sections = [
    ['cache', t('web.storage.cache'), ['cache']],
    ['history', t('web.storage.history'), ['history']],
    ['fetchdata', t('web.storage.fetchdata'), ['expired', 'all']],
  ] as const
  return <section className="settings-data-panel" aria-labelledby="settings-storage-title">
    <header className="settings-data-header">
      <div><h2 id="settings-storage-title">{t('web.storage.heading')}</h2><p>{t('web.storage.scope')}</p></div>
      <button type="button" ref={refreshButton} disabled={snapshot.loading || busy} onClick={() => void snapshot.reload()}>{t('web.storage.refresh')}</button>
    </header>
    <DataReadNote loading={snapshot.loading} error={snapshot.error} stale={Boolean(snapshot.data)} />
    {reason && <p className="settings-data-note">{reason}</p>}
    {snapshot.data && <div className="settings-storage-list">
      {sections.map(([kind, title, actions]) => {
        const row = snapshot.data![kind]
        const fetchStats = kind === 'fetchdata' ? snapshot.data!.fetchdata : undefined
        return <section key={kind} aria-label={title} className="settings-storage-row">
          <div><h3>{title}</h3>
            {row.error ? <p className="settings-data-error" role="alert">{row.error}</p> :
              !row.enabled ? <p className="settings-data-note">{t('web.storage.disabled')}</p> :
                <dl>
                  <div><dt>{t('web.storage.count')}</dt><dd>{format.format(row.count)}</dd></div>
                  <div><dt>{t('web.storage.size')}</dt><dd>{diskUse(row.bytes, locale)}</dd></div>
                  {fetchStats && <>
                    <div><dt>{t('web.settingsData.expired')}</dt><dd>{format.format(fetchStats.expired)}</dd></div>
                    <div><dt>{t('web.settingsData.clearable')}</dt><dd>{format.format(fetchStats.clearable)}</dd></div>
                  </>}
                </dl>}
          </div>
          <div className="settings-data-actions">
            {actions.map(next => {
              const count = next === 'expired' ? fetchStats?.expired : next === 'all' ? fetchStats?.clearable : row.count
              return <button type="button" key={next} className="settings-data-danger-outline"
                disabled={locked || !row.enabled || !!row.error || !count} onClick={event => {
                  opener.current = event.currentTarget
                  setFeedback(undefined)
                  setAction(next)
                }}>{t(operation[next].title)}</button>
            })}
          </div>
        </section>
      })}
    </div>}
    {feedback && <p role={feedback.error ? 'alert' : 'status'} className={feedback.error ? 'settings-data-error' : 'settings-data-note'}>{feedback.text}</p>}
    <SettingsDataDialog open={Boolean(current)} title={current ? t(current.title) : ''} detail={current ? t(current.detail) : ''}
      busy={busy} close={() => setAction(undefined)} returnFocus={() => (opener.current?.disabled ? refreshButton.current : opener.current)?.focus()}>
      <div className="settings-data-actions">
        <button type="button" disabled={busy} onClick={() => setAction(undefined)}>{t('web.action.cancel')}</button>
        <button type="button" className="settings-data-danger" disabled={locked} onClick={() => void clear()}>{t(busy ? 'web.note.loading_2' : 'web.action.confirm')}</button>
      </div>
    </SettingsDataDialog>
  </section>
}
