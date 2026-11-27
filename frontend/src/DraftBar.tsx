import * as Dialog from '@radix-ui/react-dialog';
import { useEffect, useRef, useState, type FormEvent, type MouseEvent } from 'react';
import { useDraft, type DiffLine, type SettingValue } from './draft';
import { t, useLocale } from './i18n';
import { destinationHash } from './navigation';
import './draft.css';

const display = (value: SettingValue | undefined) => value === undefined ? '—' : JSON.stringify(value);

export function DraftDiff({ lines }: { lines: DiffLine[] }) {
  return <ul className="next-draft-lines">{lines.map(line => <li key={line.key}>
    <div><code>{line.key}</code>{line.file && <small>{line.file}</small>}</div>
    <p><del>{display(line.before)}</del><span aria-hidden="true"> → </span><ins>{display(line.after)}</ins></p>
  </li>)}</ul>;
}

export function DraftBar({ showWriteReason = true }: { showWriteReason?: boolean }) {
  useLocale();
  const draft = useDraft();
  const [intent, setIntent] = useState<'review' | 'retry'>();
  const [dismissed, setDismissed] = useState<string>();
  const opener = useRef<HTMLButtonElement | null>(null);
  const authorizationRequired = Boolean(intent && !draft.hasCredentials);
  const invalid = Object.entries(draft.errors);
  const active = draft.count > 0 || draft.pendingRestart || draft.phase !== 'idle';
  const modalOpen = Boolean(intent || draft.phase === 'preview' || draft.phase === 'conflict');
  const notice = draft.phase === 'previewing' ? t('web.draft.validating') : draft.phase === 'saving' ? t('web.draft.saving') :
    draft.phase === 'verifying' ? t('web.note.restarting_and_verifying_the_effective_config') :
    draft.restart?.error ? t('web.msg.saved_not_confirmed') + draft.restart.error : draft.message?.text;

  useEffect(() => {
    setDismissed(undefined);
    if (!draft.message || draft.message.error) return;
    const timeout = window.setTimeout(() => setDismissed(draft.message?.text), 5000);
    return () => window.clearTimeout(timeout);
  }, [draft.message]);

  function start(event: MouseEvent<HTMLButtonElement>, action: 'review' | 'retry') {
    opener.current = event.currentTarget;
    if (action === 'review' && draft.hasCredentials) void draft.review();
    else setIntent(action);
  }

  function submitAccess(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const auth = authorizationRequired ? { password: String(data.get('password') ?? ''), username: String(data.get('username') ?? '') } : undefined;
    event.currentTarget.reset();
    setIntent(undefined);
    if (intent === 'retry') void draft.retryRestart(auth);
    else void draft.review(auth);
  }

  function submitConflict(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    draft.reconcile(new Set(new FormData(event.currentTarget).getAll('keep').map(String)));
  }

  function close() {
    setIntent(undefined);
    if (draft.phase === 'preview' || draft.phase === 'conflict') draft.cancelReview();
  }

  const title = intent === 'retry' ? t('web.action.retry_restart') : intent ? t('web.draft.admin_access') :
    draft.phase === 'conflict' ? t('web.draft.configuration_changed') : t('web.draft.review');

  return <>
    {active && <section className="next-draft-bar" data-active aria-label={t('web.draft.region')}>
      <div className="next-draft-summary">
        <strong>{draft.count ? t('web.draft.count', { Count: draft.count }) : t('web.draft.restart_pending')}</strong>
        {invalid.length > 0 && <span role="alert" className="next-draft-error">{invalid[0][0]} · {invalid[0][1].message}</span>}
        {showWriteReason && !draft.canWrite && <span>{draft.writeReason}</span>}
        {notice && notice !== dismissed && <span role={draft.restart?.error || draft.message?.error ? 'alert' : 'status'} className={draft.restart?.error || draft.message?.error ? 'next-draft-error' : ''}>{notice}</span>}
      </div>
      <div className="next-draft-actions">
        {draft.count > 0 && <>
          <button type="button" disabled={draft.phase !== 'idle'} onClick={() => {
            draft.undo();
            document.querySelector<HTMLElement>('[aria-current="page"]')?.focus();
          }}>{t('web.action.discard_all_unsaved_changes')}</button>
          <button type="button" className="next-draft-primary" disabled={!draft.canWrite || invalid.length > 0 || draft.phase !== 'idle'} onClick={event => start(event, 'review')}>{t('web.draft.review')}</button>
        </>}
        {draft.restart?.error && <a className="next-draft-recovery" href={destinationHash({ page: 'settings', item: 'service:recovery' })}>{t('web.page.config_recovery')}</a>}
        {draft.count === 0 && draft.pendingRestart && <button type="button" className="next-draft-primary" disabled={!draft.canWrite || draft.phase !== 'idle'} onClick={event => start(event, 'retry')}>{t('web.action.retry_restart')}</button>}
      </div>
    </section>}
    {!active && notice && notice !== dismissed && <p className="next-draft-notice" role="status">{notice}</p>}
    <Dialog.Root open={modalOpen} onOpenChange={open => { if (!open) close(); }}>
      <Dialog.Portal>
        <Dialog.Overlay className="next-draft-overlay" />
        <Dialog.Content className="next-draft-dialog" onCloseAutoFocus={event => {
          event.preventDefault();
          const trigger = opener.current;
          if (trigger?.isConnected && !trigger.disabled) trigger.focus();
          else document.querySelector<HTMLElement>('[aria-current="page"]')?.focus();
        }}>
          <Dialog.Title>{title}</Dialog.Title>
          {intent && <form onSubmit={submitAccess}>
            <Dialog.Description>{intent === 'retry' ? t('web.draft.retry_description') : t('web.draft.admin_description')}</Dialog.Description>
            {authorizationRequired && <div className="next-draft-auth">
              {draft.settings?.admin_username_configured && <label>{t('web.label.username')}<input name="username" autoComplete="off" required /></label>}
              <label>{t('web.label.password')}<input type="password" name="password" autoComplete="off" required /></label>
            </div>}
            <div className="next-draft-actions"><Dialog.Close>{t('web.action.cancel')}</Dialog.Close>
              <button type="submit" className={intent === 'retry' ? 'next-draft-danger' : 'next-draft-primary'}>{intent === 'retry' ? t('web.action.confirm_and_restart_the_service') : t('web.draft.review')}</button>
            </div>
          </form>}
          {!intent && draft.phase === 'preview' && <>
            <Dialog.Description>{t('web.draft.preview_description')}</Dialog.Description>
            <DraftDiff lines={draft.diff} />
            <div className="next-draft-actions"><Dialog.Close>{t('web.action.cancel')}</Dialog.Close>
              <button type="button" className="next-draft-danger" disabled={!draft.canWrite} onClick={() => void draft.save()}>{t('web.action.save_and_restart')}</button>
            </div>
          </>}
          {!intent && draft.phase === 'conflict' && <form onSubmit={submitConflict}>
            <Dialog.Description>{t('web.draft.reconcile_description')}</Dialog.Description>
            {draft.conflicts.length > 0 && <ul className="next-draft-lines">{draft.conflicts.map(line => <li key={line.key}>
              <div><code>{line.key}</code>{line.file && <small>{line.file}</small>}</div>
              <p>{t('web.draft.original')} <code>{display(line.before)}</code></p>
              <p>{t('web.draft.latest')} <code>{display(line.latest)}</code></p>
              <label><input name="keep" value={line.key} type="checkbox" disabled={Boolean(line.reason)} /> {t('web.draft.keep')} <code>{display(line.after)}</code></label>
              {line.reason && <p className="next-draft-error">{line.reason}</p>}
            </li>)}</ul>}
            <div className="next-draft-actions"><Dialog.Close>{t('web.action.cancel')}</Dialog.Close>
              <button type="submit" className="next-draft-primary">{t('web.draft.reconcile')}</button>
            </div>
          </form>}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </>;
}
