import { useRef, useState, type FormEvent } from 'react'
import { api } from './api'
import { useDraft } from './draft'
import { t, useLocale } from './i18n'
import { useRuntimeAccess, type QuotaItem } from './SourcesCommands'
import { DataReadNote, SettingsDataDialog, dataError, jsonPost, useSettingsRead } from './settingsData'

const resetKey = 'dashboard.quotas.reset'
const dayKey = 'dashboard.quotas.reset_day'
const enforceKey = 'dashboard.quotas.enforce_providers'
const limitsKey = 'dashboard.quotas.limits'
const validQuotas = (items: QuotaItem[]) => Array.isArray(items) && items.every(item => item && typeof item.provider === 'string')
const officialProvider = (id: string) => id === 'tavily' || id === 'tinyfish'
const isNumber = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value) && value >= 0

function providerLabel(id: string) {
  return ({
    anysearch: 'AnySearch', baidu: t('web.name.baidu_qianfan'), doubao: t('web.name.doubao_search'), exa: 'Exa',
    keenable: 'Keenable', tavily: 'Tavily', tinyfish: 'TinyFish', yuanbao: t('web.name.yuanbao'),
  } as Record<string, string>)[id] ?? id
}

function periodLabel(period: string) {
  return ({
    monthly: t('web.settingsData.monthly'), weekly: t('web.settingsData.weekly'), daily: t('web.settingsData.daily'),
    none: t('web.label.manual_only'),
  } as Record<string, string>)[period] ?? period
}

function SettingsPolicy() {
  const draft = useDraft()
  const providers = draft.settings?.options?.[enforceKey] ?? []
  const enforcement = draft.value(enforceKey)
  const selected = Array.isArray(enforcement) ? enforcement.filter((key): key is string => typeof key === 'string') : []
  const limits = draft.value(limitsKey)
  const configured = limits && typeof limits === 'object' && !Array.isArray(limits) ? limits : {}
  const period = String(draft.value(resetKey) ?? '')
  const limitRows = [...new Set([...providers, ...Object.keys(configured)])]
  const reason = draft.fieldLockReason
  const blocked = (key: string) => !draft.canWrite || draft.phase !== 'idle' || Boolean(reason(key))
  const dayError = draft.errors[dayKey]
  const limitsError = draft.errors[limitsKey]
  const invalid: Record<string, string> = limitsError?.rawValue ? JSON.parse(limitsError.rawValue) : {}

  function changeLimit(provider: string, raw: string, badInput: boolean) {
    const next = { ...configured }
    const failures = { ...invalid }
    if (badInput) failures[provider] = raw
    else if (!raw) {
      delete next[provider]
      delete failures[provider]
      draft.stage(limitsKey, next)
    } else {
      const value = Number(raw)
      if (Number.isInteger(value) && value >= 1 && value <= 1_000_000_000) {
        next[provider] = value
        delete failures[provider]
        draft.stage(limitsKey, next)
      } else failures[provider] = raw
    }
    draft.setFieldError(limitsKey, Object.keys(failures).length ? t('web.settingsData.limit_invalid') : '', JSON.stringify(failures))
  }

  return <section className="settings-data-panel" aria-labelledby="quota-policy-title">
    <header className="settings-data-header"><div><h2 id="quota-policy-title">{t('web.settingsData.quota_policy')}</h2>
      <p>{t('web.settingsData.quota_policy_help')}</p></div></header>
    {draft.settings && <>
        <div className="settings-policy-cycle">
          <label>{t('web.config.quota_reset')}
            <select value={period} disabled={blocked(resetKey)} title={reason(resetKey)} onChange={event => {
              const next = event.target.value
              if (draft.stage(resetKey, next) && next !== 'monthly' && dayError) draft.setFieldError(dayKey, '')
            }}>
              {(draft.settings.options?.[resetKey] ?? ['monthly', 'weekly', 'daily', 'none']).map(value => <option key={value} value={value}>{periodLabel(value)}</option>)}
            </select>
          </label>
          <label><span id="quota-reset-day-label">{t('web.config.quota_reset_day')}</span>
            <input type="number" min={1} max={28} step={1} inputMode="numeric"
              value={dayError?.rawValue ?? String(draft.value(dayKey) ?? '')} aria-invalid={Boolean(dayError)}
              aria-labelledby="quota-reset-day-label" aria-describedby="quota-reset-day-note" disabled={blocked(dayKey) || period !== 'monthly'}
              onChange={event => {
                const { value: raw, valueAsNumber: value } = event.currentTarget
                if (Number.isInteger(value) && value >= 1 && value <= 28) draft.stage(dayKey, value)
                else draft.setFieldError(dayKey, t('web.settingsData.day_invalid'), raw)
              }} />
            <small id="quota-reset-day-note" className={dayError ? 'settings-data-error' : undefined}>{dayError?.message || reason(dayKey) || t('web.config.monthly_only')}</small>
          </label>
        </div>
        <fieldset className="settings-policy-fieldset" disabled={blocked(enforceKey)}>
          <legend>{t('web.config.quota_enforced_providers')}</legend>
          <p>{t('web.config.quota_enforced_help')}{reason(enforceKey) && <> · {reason(enforceKey)}</>}</p>
          <div className="settings-policy-providers">{providers.map(provider => <label key={provider}>
            <input type="checkbox" checked={selected.includes(provider)} onChange={event => {
              const next = event.target.checked ? [...selected, provider] : selected.filter(id => id !== provider)
              draft.stage(enforceKey, providers.filter(id => next.includes(id)))
            }} />{providerLabel(provider)}
          </label>)}</div>
        </fieldset>
        <fieldset className="settings-policy-fieldset" disabled={blocked(limitsKey)}>
          <legend>{t('web.settingsData.per_key_limits')}</legend>
          <p>{t('web.settingsData.per_key_help')}{reason(limitsKey) && <> · {reason(limitsKey)}</>}</p>
          {limitsError && <p className="settings-data-error" id="quota-limits-issue" role="alert">{limitsError.message}</p>}
          <div className="settings-policy-limits">{limitRows.map(provider => <label key={provider}>
            <span>{providerLabel(provider)}</span>
            <input type="number" min={1} max={1_000_000_000} step={1} inputMode="numeric"
              aria-label={t('web.settingsData.provider_limit', { Name: providerLabel(provider) })}
              aria-invalid={Object.hasOwn(invalid, provider)} aria-describedby={limitsError ? 'quota-limits-issue' : undefined}
              value={invalid[provider] ?? String(configured[provider] ?? '')} placeholder="1000"
              onChange={event => changeLimit(provider, event.currentTarget.value, event.currentTarget.validity.badInput)} />
          </label>)}</div>
        </fieldset>
      </>}
  </section>
}

function Meter({ name, used, limit, remaining, unit }: {
  name: string; used?: number; limit?: number; remaining?: number; unit?: string
}) {
  const locale = useLocale()
  const format = new Intl.NumberFormat(locale, { maximumFractionDigits: 2 })
  const text = t('web.quota.amount', {
    Used: isNumber(used) ? format.format(used) : '—',
    Limit: isNumber(limit) ? format.format(limit) : '—',
    Unit: t(unit === 'credits' ? 'web.quota.credits' : 'web.quota.calls'),
  })
  const ratio = isNumber(used) && limit && limit > 0 ? used / limit : undefined
  const tone = ratio !== undefined && ratio >= 1 ? 'danger' : ratio !== undefined && ratio >= .8 ? 'warn' : ''
  return <div className="settings-quota-meter" data-tone={tone}>
    <div><span>{name}</span><b>{text}</b></div>
    {ratio !== undefined && <progress aria-label={`${name} · ${text}`} max={1} value={Math.min(ratio, 1)} />}
    {(tone || isNumber(remaining)) && <small>{[tone && t(tone === 'danger' ? 'web.quota.limit_reached' : 'web.quota.near_limit'),
      isNumber(remaining) && t('web.quota.remaining', { Count: format.format(remaining) })].filter(Boolean).join(' · ')}</small>}
  </div>
}

function Window({ period, reset }: { period?: string; reset?: string }) {
  const locale = useLocale()
  const usable = reset && Number.isFinite(Date.parse(reset))
  return <p className="settings-data-note">
    {period && period !== 'official' && <span>{periodLabel(period)}{usable ? ' · ' : ''}</span>}
    {usable && <>{t('web.label.next_automatic_reset')} · <time dateTime={reset}>
      {new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(reset))}
    </time></>}
  </p>
}

function OfficialQuota({ item }: { item: QuotaItem }) {
  if (!officialProvider(item.provider)) return <p className="settings-data-note">{t('web.settingsData.no_official')}</p>
  const details = item.details ?? {}
  const main = item.source !== 'local' && item.status === 'available'
  const failed = item.official_query_failed || item.status === 'query_failed'
  const fetchUsed = details.fetch_used
  const fetchLimit = details.fetch_limit
  const fetchReady = isNumber(fetchUsed) && isNumber(fetchLimit)
  return <>
    {failed && <p className="settings-data-warning">{t('web.quota.official_failed')}</p>}
    {main && <>
      <Meter name={item.provider === 'tinyfish' ? t('web.quota.tinyfish_search_reference') : item.plan || t('web.quota.key_usage')} {...item} />
      {item.reset_at ? <Window period="official" reset={item.reset_at} /> : item.provider === 'tavily' && <p className="settings-data-note">{t('web.quota.tavily_reset')}</p>}
    </>}
    {item.provider === 'tinyfish' && fetchReady && <>
      <Meter name={t('web.quota.fetch')} used={fetchUsed} limit={fetchLimit} remaining={Math.max(0, fetchLimit - fetchUsed)} unit="calls" />
      {typeof details.fetch_reset_at === 'string' && details.fetch_reset_at !== item.reset_at && <Window period="official" reset={details.fetch_reset_at} />}
    </>}
    {item.provider === 'tinyfish' && !fetchReady && typeof details.fetch_error === 'string' && <p className="settings-data-warning">{t('web.quota.fetch_failed')}</p>}
    {item.provider === 'tavily' && main && <div className="settings-quota-account">
      {([
        ['web.quota.plan_usage', details.account_plan_usage, details.account_plan_limit],
        ['web.quota.paygo_usage', details.paygo_usage, details.paygo_limit],
      ] as const).map(([label, used, limit]) => isNumber(used) && isNumber(limit) &&
        <Meter key={label} name={t(label)} unit="credits" used={used} limit={limit} />)}
    </div>}
    {!main && !failed && !fetchReady && <p className="settings-data-note">{t(item.configured ? 'web.quota.no_data' : 'web.status.not_configured')}</p>}
  </>
}

type Action = { kind: 'reset' | 'adjust'; item: QuotaItem } | { kind: 'official' }
type Feedback = { error: boolean; text: string }

export default function SettingsQuota({ refresh }: { refresh: number }) {
  const draft = useDraft()
  const runtime = useRuntimeAccess()
  const locale = useLocale()
  const format = new Intl.NumberFormat(locale)
  const snapshot = useSettingsRead('quotas', refresh, validQuotas, 18000)
  const [action, setAction] = useState<Action>()
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>()
  const [dialogError, setDialogError] = useState('')
  const opener = useRef<HTMLButtonElement>(null)
  const refreshButton = useRef<HTMLButtonElement>(null)
  const locked = busy || snapshot.loading || Boolean(snapshot.error) || draft.phase !== 'idle'
  const canAdmin = draft.canWrite && !locked
  const target = action && action.kind !== 'official' ? action.item : undefined
  const credentialForm = Boolean(target && !draft.hasCredentials)
  const name = target ? providerLabel(target.provider) : ''
  const adjust = action?.kind === 'adjust'

  function begin(next: Action, trigger: HTMLButtonElement) {
    opener.current = trigger
    setDialogError('')
    setFeedback(undefined)
    setAction(next)
  }

  async function confirm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!action || locked || (action.kind === 'official' ? !runtime.allowed : !canAdmin)) return
    const form = event.currentTarget
    const data = new FormData(form)
    const credentials = credentialForm ? {
      username: String(data.get('username') ?? '').trim(), password: String(data.get('password') ?? ''),
    } : undefined
    const password = form.elements.namedItem('password')
    if (password instanceof HTMLInputElement) password.value = ''
    const raw = String(data.get('used') ?? '')
    const used = Number(raw)
    if (adjust && (!raw || !Number.isSafeInteger(used) || used < 0)) {
      setDialogError(t('web.settingsData.adjust_invalid'))
      return
    }
    setBusy(true)
    setDialogError('')
    try {
      if (action.kind === 'official') {
        const result = await api<{ items: QuotaItem[]; refreshed: boolean }>('quotas/refresh', {
          ...jsonPost({ confirm: true }), signal: AbortSignal.timeout(18000),
        })
        if (!validQuotas(result.items) || typeof result.refreshed !== 'boolean') throw new Error(t('web.msg.response_is_not_valid_json'))
        const latest = await snapshot.reload()
        if (!latest) setFeedback({ error: true, text: t('web.settingsData.quota_unverified') })
        else {
          const partial = result.items.some(item => item.status === 'query_failed' || item.official_query_failed || item.details?.fetch_error)
          setFeedback({ error: false, text: t(partial ? 'web.runtime_action.quota_partial'
            : result.refreshed ? 'web.runtime_action.quota_refreshed' : 'web.runtime_action.quota_debounced') })
        }
      } else {
        const provider = action.item.provider
        await draft.adminRequest(`quotas/${action.kind}`, jsonPost(action.kind === 'adjust' ? { provider, set_used: used } : { provider }), credentials)
        const latest = await snapshot.reload()
        const local = latest?.find(item => item.provider === provider)?.local_usage
        if (local?.status === 'available' && isNumber(local.used)) {
          setFeedback({ error: false, text: t('web.settingsData.quota_after', { Name: providerLabel(provider), Count: format.format(local.used) }) })
        } else setFeedback({ error: true, text: t('web.settingsData.quota_unverified') })
      }
      setAction(undefined)
    } catch (error) {
      setDialogError(dataError(error))
    } finally {
      setBusy(false)
    }
  }

  const rows = snapshot.data ?? []
  return <div className="settings-data-stack">
    <SettingsPolicy />
    <section className="settings-data-panel" aria-labelledby="quota-ledger-title">
      <header className="settings-data-header">
        <div><h2 id="quota-ledger-title">{t('web.settingsData.ledger')}</h2><p>{t('web.settingsData.ledger_help')}</p></div>
        <div className="settings-data-actions">
          <button type="button" ref={refreshButton} disabled={snapshot.loading || busy} onClick={() => void snapshot.reload()}>{t('web.settingsData.quota_reload')}</button>
          {rows.some(item => officialProvider(item.provider) && item.configured) && <button type="button" title={runtime.reason}
            disabled={locked || !runtime.allowed} onClick={event => begin({ kind: 'official' }, event.currentTarget)}>{t('web.runtime_action.refresh_quota')}</button>}
        </div>
      </header>
      <DataReadNote loading={snapshot.loading} error={snapshot.error} stale={Boolean(snapshot.data)} />
      {snapshot.data && (rows.length ? <div className="settings-quota-scroll" tabIndex={0} role="region" aria-labelledby="quota-ledger-title">
        <table className="settings-quota-table">
          <thead><tr><th scope="col">{t('web.label.provider')}</th><th scope="col">{t('web.quota.official')}</th><th scope="col">{t('web.settingsData.local_ledger')}</th></tr></thead>
          <tbody>{rows.map(item => {
            const local = item.local_usage
            const available = local?.status === 'available' && isNumber(local.used)
            const provider = providerLabel(item.provider)
            return <tr key={item.provider}>
              <th scope="row">{provider}</th>
              <td><OfficialQuota item={item} /></td>
              <td>{available && local ? <>
                <Meter name={t('web.quota.search')} {...local} />
                <Window period={local.auto_reset} reset={local.reset_at} />
                <p className="settings-data-note">{t(local.enforced ? 'web.settingsData.enforced' : 'web.settingsData.observed')}</p>
                <div className="settings-data-actions">
                  <button type="button" disabled={!canAdmin} aria-label={t('web.settingsData.adjust_provider', { Name: provider })}
                    onClick={event => begin({ kind: 'adjust', item }, event.currentTarget)}>{t('web.action.adjust')}</button>
                  <button type="button" disabled={!canAdmin} aria-label={t('web.settingsData.reset_provider', { Name: provider })}
                    className="settings-data-danger-outline" onClick={event => begin({ kind: 'reset', item }, event.currentTarget)}>{t('web.action.reset')}</button>
                </div>
              </> : <p className={item.error && item.source === 'local' ? 'settings-data-warning' : 'settings-data-note'}>
                {item.source === 'local' && item.error || t('web.quota.no_data')}</p>}</td>
            </tr>
          })}</tbody>
        </table>
      </div> : <p className="settings-data-note">{t('web.quota.no_data')}</p>)}
      {feedback && <p role={feedback.error ? 'alert' : 'status'} className={feedback.error ? 'settings-data-error' : 'settings-data-note'}>{feedback.text}</p>}
    </section>
    <SettingsDataDialog open={Boolean(action)} title={target ? t(adjust ? 'web.settingsData.adjust_provider' : 'web.settingsData.reset_provider', { Name: name })
      : t('web.runtime_action.refresh_quota')} detail={target ? t(adjust ? 'web.settingsData.adjust_detail' : 'web.settingsData.reset_detail')
      : t('web.runtime_action.quota_confirm')} busy={busy} close={() => setAction(undefined)}
      returnFocus={() => (opener.current?.disabled ? refreshButton.current : opener.current)?.focus()}>
      <form onSubmit={event => void confirm(event)}>
        {target && <div className="settings-quota-current">
          <p>{t('web.settingsData.current_used', { Count: format.format(target.local_usage?.used ?? 0) })}</p>
          <Window period={target.local_usage?.auto_reset} reset={target.local_usage?.reset_at} />
        </div>}
        {adjust && <label>{t('web.settingsData.new_used')}<input name="used" type="number" min={0} max={Number.MAX_SAFE_INTEGER} step={1}
          defaultValue={target?.local_usage?.used} required disabled={busy} /></label>}
        {credentialForm && <>
          {draft.settings?.admin_username_configured && <label>{t('web.label.username')}<input name="username" autoComplete="off" required disabled={busy} /></label>}
          <label>{t('web.label.password')}<input type="password" name="password" autoComplete="off" required disabled={busy} /></label>
        </>}
        {dialogError && <p className="settings-data-error" role="alert">{dialogError}</p>}
        <div className="settings-data-actions">
          <button type="button" onClick={() => setAction(undefined)} disabled={busy}>{t('web.action.cancel')}</button>
          <button type="submit" className={action?.kind === 'reset' ? 'settings-data-danger' : 'settings-data-primary'}
            disabled={locked || (target ? !canAdmin : !runtime.allowed)}>{t(busy ? 'web.note.loading_2' : 'web.action.confirm')}</button>
        </div>
      </form>
    </SettingsDataDialog>
  </div>
}
