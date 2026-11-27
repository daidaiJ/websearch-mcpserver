import * as Dialog from '@radix-ui/react-dialog'
import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { api } from './api'
import { t, useLocale } from './i18n'
import './settingsData.css'

export const dataError = (error: unknown) => error instanceof Error ? error.message : t('web.status.unknown_error')
export const jsonPost = (payload: object): RequestInit => ({
  method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
})

export function useSettingsRead<T>(path: string, refresh: number, validate: (data: T) => boolean, timeout = 8000) {
  const locale = useLocale()
  const pending = useRef<AbortController>(null)
  const [state, setState] = useState<{ data?: T; error?: string; loading: boolean }>({ loading: true })
  const reload = useCallback(async () => {
    pending.current?.abort()
    const controller = new AbortController()
    pending.current = controller
    setState(prior => ({ data: prior.data, loading: true }))
    try {
      const data = await api<T>(path, { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(timeout)]) })
      if (!validate(data)) throw new Error(t('web.msg.response_is_not_valid_json'))
      if (!controller.signal.aborted) {
        setState({ data, loading: false })
        return data
      }
    } catch (error) {
      if (!controller.signal.aborted) setState(prior => ({ ...prior, error: dataError(error), loading: false }))
    }
  }, [path, timeout, validate])
  useEffect(() => {
    void reload()
    return () => pending.current?.abort()
  }, [reload, refresh, locale])
  return { ...state, reload }
}

export function SettingsDataDialog({ open, title, detail, close, returnFocus, busy, children }: {
  open: boolean; title: string; detail: string; close: () => void; returnFocus: () => void; busy: boolean; children: ReactNode
}) {
  return <Dialog.Root open={open} onOpenChange={next => { if (!next && !busy) close() }}>
    <Dialog.Portal>
      <Dialog.Overlay className="settings-data-overlay" />
      <Dialog.Content className="settings-data-dialog" onCloseAutoFocus={event => { event.preventDefault(); returnFocus() }}>
        <Dialog.Title>{title}</Dialog.Title>
        <Dialog.Description>{detail}</Dialog.Description>
        {children}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
}

export function DataReadNote({ loading, error, stale }: { loading: boolean; error?: string; stale?: boolean }) {
  if (loading) return <p role="status" className="settings-data-note">{t('web.note.loading_2')}</p>
  if (error) return <p role="alert" className="settings-data-error">{t(stale ? 'web.note.read_failed_the_records_shown_may_be_stale' : 'web.note.read_failed')}{error}</p>
  return null
}
