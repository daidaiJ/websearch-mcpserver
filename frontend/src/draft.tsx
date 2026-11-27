import { createContext, useContext, useEffect, useRef, useState, type ReactNode } from 'react';
import { api, ApiError } from './api';
import { t, useLocale } from './i18n';

export type SettingValue = null | boolean | number | string | SettingValue[] | { [key: string]: SettingValue };
type Values = Record<string, SettingValue>;
type Files = Record<string, string>;

export interface SettingsView {
  values: Values;
  saved_values?: Values;
  pending_changes?: Values;
  field_files?: Files;
  locked_fields?: Record<string, string>;
  options?: Record<string, string[]>;
  revision: string;
  backups?: { name: string; created_at: string }[];
  credential_status?: { pending_restart: boolean };
  admin_password_configured: boolean;
  admin_username_configured?: boolean;
  write_access: { allowed: boolean; reason?: string };
  local_action_allowed?: boolean;
  config_error?: string;
}

interface SettingsResult {
  applied: boolean;
  restart_required: boolean;
  changes: Values;
  revision: string;
  field_files?: Files;
}

export type DraftCredentials = { password: string; username?: string };
type FieldError = { message: string; rawValue?: string };
type Phase = 'idle' | 'previewing' | 'preview' | 'saving' | 'verifying' | 'conflict';
export type DiffLine = { key: string; file?: string; before?: SettingValue; after: SettingValue };
type ConflictLine = DiffLine & { latest?: SettingValue; reason?: string };
type PendingRestart = { targets: Values; error?: string; verifyCredentials?: boolean };

const emptyDraft = { revision: '', draft: {} as Values, before: {} as Values, errors: {} as Record<string, FieldError> };
const Context = createContext<ReturnType<typeof useDraftModel> | null>(null);
const saved = (settings: SettingsView) => settings.saved_values ?? settings.values;

export function settingsEqual(left: SettingValue | undefined, right: SettingValue | undefined): boolean {
  if (left === right) return true;
  if (Array.isArray(left)) {
    return Array.isArray(right) && left.length === right.length && left.every((value, index) => settingsEqual(value, right[index]));
  }
  if (!left || !right || typeof left !== 'object' || typeof right !== 'object' || Array.isArray(right)) return false;
  return Object.keys(left).length === Object.keys(right).length && Object.entries(left).every(([key, value]) => Object.hasOwn(right, key) && settingsEqual(value, right[key]));
}

function fieldBlock(settings: SettingsView, key: string) {
  return settings.locked_fields?.[key] || (!Object.hasOwn(settings.field_files ?? {}, key) ? t('web.draft.field_unavailable') : '');
}

function validationError(settings: SettingsView, key: string, value: SettingValue): string {
  const original = saved(settings)[key];
  const options = settings.options?.[key];
  if (value === null && (key === 'proxy.enabled' || key === 'keenable.enabled')) return '';
  if ((original != null && (value == null || typeof original !== typeof value || Array.isArray(original) !== Array.isArray(value))) ||
    (typeof value === 'number' && !Number.isFinite(value)) ||
    (options?.length && typeof value === 'string' && !options.includes(value))) return t('web.draft.invalid_value');
  return '';
}

async function getSettings(signal?: AbortSignal): Promise<SettingsView> {
  const timeout = AbortSignal.timeout(5000);
  const settings = await api<SettingsView>('settings', { signal: signal ? AbortSignal.any([signal, timeout]) : timeout });
  if (!settings || typeof settings !== 'object' || !settings.values || typeof settings.values !== 'object' || Array.isArray(settings.values) || typeof settings.write_access?.allowed !== 'boolean') {
    throw new Error(t('web.msg.settings_response_is_missing_the_values_field'));
  }
  return settings;
}

const isApiStatus = (error: unknown, ...codes: number[]) => error instanceof ApiError && codes.includes(error.status);
const errorMessage = (error: unknown) => error instanceof Error ? error.message : t('web.msg.response_is_not_valid_json');
const wait = (ms: number) => new Promise(resolve => setTimeout(resolve, ms));
const runtimeMatches = (settings: SettingsView, targets: Values) => !settings.config_error && Object.entries(targets).every(([key, desired]) => settingsEqual(settings.values[key], desired) && settingsEqual(saved(settings)[key], desired));

function settingsResultMatches(result: SettingsResult, draft: Values) {
  return result && typeof result.revision === 'string' && typeof result.restart_required === 'boolean' &&
    result.changes && typeof result.changes === 'object' && !Array.isArray(result.changes) &&
    Object.keys(result.changes).length === Object.keys(draft).length && Object.keys(draft).every(key => Object.hasOwn(result.changes, key));
}

function useDraftModel() {
  const locale = useLocale();
  const [settings, setSettings] = useState<SettingsView>();
  const [loadError, setLoadError] = useState('');
  const [buffer, setBuffer] = useState(emptyDraft);
  const [phase, setPhase] = useState<Phase>('idle');
  const [preview, setPreview] = useState<SettingsResult>();
  const [conflictSettings, setConflictSettings] = useState<SettingsView>();
  const [message, setMessage] = useState<{ text: string; error: boolean }>();
  const [restart, setRestart] = useState<PendingRestart>();
  const [credentialsCached, setHasCredentials] = useState(false);
  const credentials = useRef<DraftCredentials | undefined>(undefined);
  const busy = useRef(false);
  const readSequence = useRef(0);

  const { draft, before, errors, revision } = buffer;
  const keys = new Set([...Object.keys(draft), ...Object.keys(errors)]);
  const canWrite = Boolean(!loadError && settings?.revision && settings.write_access.allowed && settings.admin_password_configured && !settings.config_error);
  const hasCredentials = credentialsCached && Boolean(credentials.current?.password) && (!settings?.admin_username_configured || Boolean(credentials.current?.username));
  const writeReason = loadError || (!settings ? t('web.note.reading_write_permission') : settings.config_error || settings.write_access.reason ||
    (!settings.revision ? t('web.note.read_only_config_revision_unavailable') : !settings.admin_password_configured ? t('web.note.read_only_admin_password_not_configured') : t('web.note.read_only_this_connection_has_no_write_access')));

  async function refresh(signal?: AbortSignal) {
    const request = ++readSequence.current;
    try {
      const next = await getSettings(signal);
      if (request === readSequence.current) { setSettings(next); setLoadError(''); }
      return next;
    } catch (error) {
      if (!signal?.aborted && request === readSequence.current) setLoadError(errorMessage(error));
    }
  }

  useEffect(() => {
    const load = new AbortController();
    void refresh(load.signal);
    return () => load.abort();
  }, [locale]);

  const fieldLockReason = (key: string) => (settings && (canWrite ? fieldBlock(settings, key) : settings.locked_fields?.[key])) || '';
  const fieldReason = (key: string) => canWrite ? fieldLockReason(key) : writeReason;
  const value = (key: string) => Object.hasOwn(draft, key) ? draft[key] : settings && saved(settings)[key];

  function writeField(key: string, next: SettingValue | undefined, issue?: FieldError) {
    if (busy.current || phase !== 'idle' || !settings || fieldReason(key)) return false;
    setMessage(undefined);
    setBuffer(current => {
      const draft = { ...current.draft };
      const before = { ...current.before };
      const errors = { ...current.errors };
      if (issue) errors[key] = issue;
      else {
        delete errors[key];
        if (next !== undefined) {
          const original = Object.hasOwn(before, key) ? before[key] : saved(settings)[key];
          if (settingsEqual(original, next)) delete draft[key];
          else draft[key] = next;
        }
      }
      if (Object.hasOwn(draft, key) || Object.hasOwn(errors, key)) {
        if (!Object.hasOwn(before, key)) before[key] = saved(settings)[key];
      } else delete before[key];
      if (!Object.keys(before).length) return emptyDraft;
      return { draft, before, errors, revision: current.revision || settings.revision };
    });
    return !issue;
  }

  function stage(key: string, next: SettingValue) {
    if (!settings) return false;
    const error = validationError(settings, key, next);
    return writeField(key, next, error ? { message: error } : undefined);
  }

  function setFieldError(key: string, message: string, rawValue?: string) {
    return writeField(key, undefined, message ? { message, rawValue } : undefined);
  }

  function undo() {
    if (busy.current) return;
    setBuffer(emptyDraft);
    setPreview(undefined);
    setConflictSettings(undefined);
    setPhase('idle');
    setMessage(undefined);
  }

  function remember(next?: DraftCredentials) {
    if (!next) return;
    credentials.current = next;
    setHasCredentials(true);
  }

  async function adminRequest<T>(path: string, init: RequestInit = {}, next?: DraftCredentials): Promise<T> {
    remember(next);
    const auth = credentials.current;
    if (!auth?.password || (settings?.admin_username_configured && !auth.username)) throw new Error(t('web.msg.cancelled_this_action_needs_the_admin_password'));
    const headers = new Headers(init.headers);
    headers.set('X-Admin-Password', auth.password);
    if (settings?.admin_username_configured && auth.username) headers.set('X-Admin-User', auth.username);
    try {
      return await api<T>(path, { ...init, headers, redirect: 'error', signal: init.signal ?? AbortSignal.timeout(10000) });
    } catch (error) {
      if (isApiStatus(error, 401, 403)) {
        credentials.current = undefined;
        setHasCredentials(false);
      }
      throw error;
    }
  }

  function securePost<T>(path: string, payload: object): Promise<T> {
    return adminRequest<T>(path, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
  }

  async function handleWriteError(error: unknown) {
    setPreview(undefined);
    setMessage({ text: errorMessage(error), error: true });
    if (isApiStatus(error, 409)) {
      const latest = await refresh();
      if (latest?.revision && !latest.config_error) {
        setConflictSettings(latest);
        setPhase('conflict');
        return;
      }
    }
    setPhase('idle');
  }

  async function review(next?: DraftCredentials) {
    if (busy.current || phase !== 'idle' || !canWrite || !revision || !Object.keys(draft).length || Object.keys(errors).length) return;
    busy.current = true;
    remember(next);
    setPhase('previewing');
    setMessage(undefined);
    try {
      const result = await securePost<SettingsResult>('settings', { changes: draft, revision, confirm: false });
      if (!settingsResultMatches(result, draft) || result.applied !== false || result.revision !== revision) throw new Error(t('web.draft.preview_incomplete'));
      setPreview(result);
      setPhase('preview');
    } catch (error) {
      await handleWriteError(error);
    } finally {
      busy.current = false;
    }
  }

  const diff: DiffLine[] = Object.entries(preview?.changes ?? {}).map(([key, after]) => ({ key, after, before: before[key], file: preview?.field_files?.[key] }));
  const conflicts: ConflictLine[] = conflictSettings ? Object.entries(draft).flatMap(([key, after]) => {
    const latest = saved(conflictSettings)[key];
    const reason = fieldBlock(conflictSettings, key);
    if (settingsEqual(latest, after) || (!reason && settingsEqual(before[key], latest))) return [];
    return [{ key, after, before: before[key], latest, reason, file: conflictSettings.field_files?.[key] }];
  }) : [];

  function reconcile(keepKeys: ReadonlySet<string>) {
    if (busy.current || !conflictSettings) return;
    const draft = { ...buffer.draft };
    const before: Values = {};
    for (const [key, desired] of Object.entries(draft)) {
      const latest = saved(conflictSettings)[key];
      const concurrent = !settingsEqual(latest, buffer.before[key]);
      if (fieldBlock(conflictSettings, key) || settingsEqual(latest, desired) || (concurrent && !keepKeys.has(key))) delete draft[key];
      else before[key] = latest;
    }
    setBuffer(Object.keys(draft).length ? { draft, before, revision: conflictSettings.revision, errors: {} } : emptyDraft);
    setConflictSettings(undefined);
    setMessage(undefined);
    setPhase('idle');
  }

  function cancelReview() {
    if (busy.current) return;
    setPreview(undefined);
    setConflictSettings(undefined);
    setPhase('idle');
  }

  async function verify(targets: Values, verifyCredentials = false): Promise<boolean> {
    setPhase('verifying');
    const pending = { targets, verifyCredentials };
    setRestart(pending);
    type RuntimeSnapshot = { runtime?: { started_at?: string } };
    let original: string | undefined;
    try {
      original = (await api<RuntimeSnapshot>('overview', { signal: AbortSignal.timeout(5000) })).runtime?.started_at;
      if (!original) throw new Error(t('web.draft.active_values_unconfirmed'));
    } catch (error) {
      setRestart({ ...pending, error: errorMessage(error) });
      return false;
    }
    let restartError: unknown;
    try {
      await securePost('restart', { confirm: true });
    } catch (error) {
      // If the connection drops after the restart was accepted, the active config can still be verified.
      restartError = error;
      if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
        setRestart({ ...pending, error: errorMessage(error) });
        return false;
      }
    }
    const deadline = Date.now() + 60000;
    while (Date.now() < deadline) {
      await wait(Math.min(1000, deadline - Date.now()));
      if (Date.now() >= deadline) break;
      const signal = AbortSignal.timeout(Math.max(1, Math.min(3000, deadline - Date.now())));
      try {
        const [active, overview] = await Promise.all([getSettings(signal), api<RuntimeSnapshot>('overview', { signal })]);
        if (overview.runtime?.started_at && overview.runtime.started_at !== original && runtimeMatches(active, targets) && (!verifyCredentials || active.credential_status?.pending_restart === false)) {
          ++readSequence.current;
          setSettings(active);
          setLoadError('');
          setRestart(undefined);
          setMessage({ text: t('web.msg.effective_config_confirmed'), error: false });
          window.dispatchEvent(new Event('websearch:configuration-applied'));
          return true;
        }
      } catch {
        // The endpoint can be temporarily unavailable while the daemon restarts.
      }
    }
    setRestart({ ...pending, error: restartError ? errorMessage(restartError) : t('web.msg.saved_not_confirmed_within_60_seconds') });
    return false;
  }

  async function save() {
    if (busy.current || phase !== 'preview' || !preview || !canWrite) return;
    busy.current = true;
    setPhase('saving');
    try {
      const result = await securePost<SettingsResult>('settings', { changes: draft, revision, confirm: true });
      if (!settingsResultMatches(result, draft) || result.applied !== true) throw new Error(t('web.draft.save_not_applied'));
      const targets = { ...settings?.pending_changes, ...preview.changes, ...result.changes };
      setBuffer(emptyDraft);
      setPreview(undefined);
      if (result.restart_required || settings?.credential_status?.pending_restart || Object.keys(settings?.pending_changes ?? {}).length) {
        await verify(targets, !!settings?.credential_status?.pending_restart);
      }
      else {
        const active = await refresh();
        if (active && runtimeMatches(active, targets)) setMessage({ text: t('web.msg.effective_config_confirmed'), error: false });
        else setRestart({ targets, error: t('web.draft.active_values_unconfirmed') });
      }
      setPhase('idle');
    } catch (error) {
      await handleWriteError(error);
    } finally {
      busy.current = false;
    }
  }

  async function restartAndVerify(targets: Values, next?: DraftCredentials, options: { verifyCredentials?: boolean; discardDraft?: boolean } = {}) {
    if (busy.current || phase !== 'idle' || !canWrite) return false;
    busy.current = true;
    remember(next);
    if (options.discardDraft) setBuffer(emptyDraft);
    setMessage(undefined);
    try { return await verify(targets, options.verifyCredentials || !!settings?.credential_status?.pending_restart); }
    finally { setPhase('idle'); busy.current = false; }
  }

  async function retryRestart(next?: DraftCredentials) {
    await restartAndVerify(restart?.targets ?? settings?.pending_changes ?? {}, next, { verifyCredentials: restart?.verifyCredentials });
  }

  return {
    settings, loadError, adminRequest, revision, draft, errors, count: keys.size, phase, canWrite, writeReason, fieldReason, fieldLockReason, value, stage, setFieldError,
    undo, refresh, review, diff, cancelReview, save, conflicts, reconcile, hasCredentials, message, restart, retryRestart, restartAndVerify,
    pendingRestart: Boolean(restart || settings?.credential_status?.pending_restart || Object.keys(settings?.pending_changes ?? {}).length),
  };
}

export function DraftProvider({ children }: { children: ReactNode }) {
  const draft = useDraftModel();
  return <Context.Provider value={draft}>{children}</Context.Provider>;
}

export function useDraft() {
  const context = useContext(Context);
  if (!context) throw new Error('DraftProvider is required');
  return context;
}
