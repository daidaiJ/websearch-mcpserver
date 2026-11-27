import * as Dialog from '@radix-ui/react-dialog';
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { api } from './api';
import { useDraft } from './draft';
import { t, useLocale } from './i18n';
import { Icon, useNavigate, useRegisterCommands, type Command } from './navigation';
import './sources.css';

export type SourceCategory = 'engine' | 'api' | 'academic' | 'mcphub';
export type OfficialProvider = 'tavily' | 'tinyfish';
export type DiagnosticOption = { id: string; name: string; confirm: boolean };
export type Source = {
  id: string; name: string; group: string; category: SourceCategory;
  enabled: boolean; control_path?: string; score_key?: string; inverted?: boolean;
  condition: string; participation: string; configured: boolean; active: boolean;
  status: string; observation_status?: string; state?: string; last_seen_at?: string;
  last_error?: string; suspended: boolean; suspended_until?: string; suspend_reason?: string;
  actual_cooldown?: { engine: string; kind: string; reason: string; until: string; remaining_seconds: number } | null;
  today?: { day: string; requests: number; successes: number; failures: number; duration_ms: number };
  recent_outcomes?: boolean[] | null; p95_ms?: number; error_kinds?: { kind: string; count: number }[];
  quota_queryable: boolean; quota_skip_reason?: string;
};
type QuotaNumbers = { status: string; used?: number; limit?: number; remaining?: number; unit?: string; reset_at?: string; auto_reset?: string };
export type QuotaItem = QuotaNumbers & {
  provider: string; configured: boolean; plan?: string; source?: string; official_query_failed?: boolean;
  local_usage?: QuotaNumbers & { enforced: boolean }; error?: string; details?: Record<string, unknown>;
};
type Catalog = { sources: Source[] | null; academic_sources: Source[] | null; diagnostics?: { sources?: DiagnosticOption[] | null } };
type RuntimeAction = { type: 'release'; engine: string; name?: string } | { type: 'quota'; provider?: OfficialProvider };
type ReadState<T> = { data?: T; error?: string; loading: boolean };
const emptySources: Source[] = [];

export const cooldownSeconds = (source: Source, now: number) => {
  const seconds = Math.ceil((Date.parse(source.actual_cooldown?.until ?? '') - now) / 1000);
  return seconds > 0 ? seconds : 0;
};

export function useCooldownClock(sources: Source[]) {
  const [now, setNow] = useState(Date.now);
  useEffect(() => {
    const current = Date.now();
    setNow(current);
    let timer = 0;
    function tick() {
      const current = Date.now();
      setNow(current);
      schedule(current);
    }
    function schedule(current: number) {
      if (sources.some(source => cooldownSeconds(source, current) > 0)) timer = window.setTimeout(tick, 1000);
      else {
        const deadlines = sources.map(source => source.suspended ? Date.parse(source.suspended_until ?? '') : NaN).filter(until => until > current);
        if (deadlines.length) timer = window.setTimeout(tick, Math.min(...deadlines) - current);
      }
    }
    schedule(current);
    return () => window.clearTimeout(timer);
  }, [sources]);
  return now;
}

export function useRuntimeAccess() {
  const { settings, loadError, phase } = useDraft();
  const reason = loadError ? t('web.runtime_action.permission_unavailable')
    : !settings ? t('web.runtime_action.permission_loading')
    : settings.local_action_allowed !== true ? t('web.runtime_action.source_not_allowed')
    : ['saving', 'verifying'].includes(phase) ? t('web.note.applying_configuration') : '';
  return { allowed: !reason, reason };
}

function useSourceWorkspace() {
  const locale = useLocale();
  const { allowed, reason } = useRuntimeAccess();
  const [overview, setOverview] = useState<ReadState<Catalog>>({ loading: true });
  const [quotas, setQuotas] = useState<ReadState<QuotaItem[]>>({ loading: false });
  const [action, setAction] = useState<RuntimeAction>();
  const [running, setRunning] = useState(false);
  const [feedback, setFeedback] = useState<{ message: string; error: boolean }>();
  const overviewRequest = useRef<AbortController>(null);
  const quotaRequest = useRef<AbortController>(null);
  const busy = useRef(false);
  const actionOpener = useRef<HTMLElement>(null);

  const readOverview = useCallback(async () => {
    overviewRequest.current?.abort();
    const request = new AbortController();
    overviewRequest.current = request;
    setOverview(current => ({ ...current, loading: true, error: undefined }));
    try {
      const data = await api<Catalog>('overview', { signal: AbortSignal.any([request.signal, AbortSignal.timeout(8000)]) });
      if (!(data.sources === null || Array.isArray(data.sources)) || !(data.academic_sources === null || Array.isArray(data.academic_sources))) throw new Error(t('web.sources.catalog_missing'));
      if (!request.signal.aborted) setOverview({ data, loading: false });
    } catch (error) {
      if (!request.signal.aborted) setOverview(current => ({ ...current, loading: false, error: message(error) }));
    }
  }, [locale]);

  const readQuotas = useCallback(async () => {
    quotaRequest.current?.abort();
    const request = new AbortController();
    quotaRequest.current = request;
    setQuotas(current => ({ ...current, loading: true, error: undefined }));
    try {
      const data = await api<QuotaItem[]>('quotas', { signal: AbortSignal.any([request.signal, AbortSignal.timeout(18000)]) });
      if (!Array.isArray(data)) throw new Error(t('web.msg.response_is_not_valid_json'));
      if (!request.signal.aborted) setQuotas({ data, loading: false });
    } catch (error) {
      if (!request.signal.aborted) setQuotas(current => ({ ...current, loading: false, error: message(error) }));
    }
  }, [locale]);

  useEffect(() => { void readOverview(); }, [readOverview]);
  useEffect(() => () => {
    overviewRequest.current?.abort();
    quotaRequest.current?.abort();
  }, []);

  const sources = useMemo(() => overview.data ? [...overview.data.sources ?? [], ...overview.data.academic_sources ?? []] : emptySources, [overview.data]);
  const diagnosticOptions = overview.data?.diagnostics?.sources ?? [];

  function requestAction(next: RuntimeAction) {
    if (!allowed || busy.current) return;
    actionOpener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setAction(next);
  }

  function cancelAction() { if (!busy.current) setAction(undefined); }

  async function runAction() {
    if (!action || !allowed || busy.current) return;
    busy.current = true;
    setFeedback(undefined);
    setRunning(true);
    try {
      if (action.type === 'release') {
        const result = await post<{ cleared: number }>('sources/cooldowns/release', { engine: action.engine, confirm: true }, 8000);
        if (result.cleared > 0) setOverview(current => {
          if (!current.data) return current;
          const clear = (source: Source) => action.engine === 'all' || source.actual_cooldown?.engine === action.engine ? { ...source, actual_cooldown: null } : source;
          return { ...current, data: { ...current.data, sources: current.data.sources?.map(clear) ?? null, academic_sources: current.data.academic_sources?.map(clear) ?? null } };
        });
        setFeedback({ message: t('web.runtime_action.released', { Count: result.cleared }), error: false });
        void readOverview();
      } else {
        quotaRequest.current?.abort();
        const result = await post<{ items: QuotaItem[]; refreshed: boolean }>('quotas/refresh', { confirm: true, ...(action.provider && { provider: action.provider }) }, 18000);
        if (!Array.isArray(result.items)) throw new Error(t('web.msg.response_is_not_valid_json'));
        quotaRequest.current?.abort();
        setQuotas(current => {
          if (!action.provider) return { data: result.items, loading: false };
          const merged = new Map(current.data?.map(item => [item.provider, item]));
          for (const item of result.items) merged.set(item.provider, item);
          return { data: [...merged.values()], loading: false };
        });
        const failed = result.items.some(item => item.official_query_failed || item.status === 'query_failed' || item.details?.fetch_error);
        setFeedback({ message: t(failed ? 'web.runtime_action.quota_partial' : result.refreshed ? 'web.runtime_action.quota_refreshed' : 'web.runtime_action.quota_debounced'), error: failed });
        if (action.provider && !quotas.data) void readQuotas();
      }
    } catch (error) {
      if (action.type === 'quota') setQuotas(current => ({ ...current, loading: false }));
      setFeedback({ message: message(error), error: true });
    } finally {
      busy.current = false;
      setRunning(false);
      setAction(undefined);
    }
  }

  return { sources, overview, quotas, diagnosticOptions, readOverview, readQuotas, allowed, reason, feedback, dismissFeedback: () => setFeedback(undefined), action, requestAction, cancelAction, running, runAction, actionOpener };
}

function message(error: unknown) {
  return error instanceof Error ? error.message : t('web.status.read_failed');
}

function post<T>(endpoint: string, data: object, timeout: number) {
  return api<T>(endpoint, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data), signal: AbortSignal.timeout(timeout) });
}

const SourceContext = createContext<ReturnType<typeof useSourceWorkspace> | null>(null);

export function SourcesProvider({ children }: { children: ReactNode }) {
  const workspace = useSourceWorkspace();
  return <SourceContext.Provider value={workspace}>{children}</SourceContext.Provider>;
}

export function useSourcesData() {
  const context = useContext(SourceContext);
  if (!context) throw new Error('SourcesProvider is required');
  return context;
}

export default function SourcesCommands({ refresh = 0 }: { refresh?: number }) {
  const locale = useLocale();
  const navigate = useNavigate();
  const workspace = useSourcesData();
  const initialRefresh = useRef(refresh);
  const { sources, readOverview, action, allowed, running, feedback } = workspace;
  const workspaceRef = useRef(workspace);
  workspaceRef.current = workspace;
  const now = useCooldownClock(sources);
  const hasCooldown = sources.some(source => cooldownSeconds(source, now) > 0);
  const canRefreshQuota = sources.some(source => source.quota_queryable && source.configured);

  useEffect(() => {
    if (initialRefresh.current === refresh) return;
    initialRefresh.current = refresh;
    void readOverview();
  }, [refresh, readOverview]);

  const commands = useMemo<Command[]>(() => {
    const group = t('web.page.search_sources');
    const commands: Command[] = sources.map(source => ({ id: `source-${source.id}`, label: source.name, group, icon: 'layers',
      keywords: [source.id, source.category, t('web.config.source_parameters')].join(' '), run: () => navigate({ page: 'sources', item: source.id }) }));
    if (allowed && hasCooldown) commands.push({ id: 'sources-release-all', group, label: t('web.runtime_action.release_all'), icon: 'refresh', run: () => workspaceRef.current.requestAction({ type: 'release', engine: 'all' }) });
    if (allowed && canRefreshQuota) commands.push({ id: 'sources-refresh-quotas', group, label: t('web.runtime_action.refresh_quota'), icon: 'refresh', run: () => workspaceRef.current.requestAction({ type: 'quota' }) });
    return commands;
  }, [locale, navigate, sources, allowed, hasCooldown, canRefreshQuota]);
  useRegisterCommands(commands);

  const title = action?.type === 'release' ? (action.engine === 'all' ? t('web.runtime_action.release_all') : t('web.runtime_action.release_source', { Name: action.name }))
    : t('web.runtime_action.refresh_quota');
  const description = action?.type === 'release' ? t('web.runtime_action.release_confirm')
    : action?.provider ? t('web.sources.refresh_provider_confirm', { Name: action.provider === 'tavily' ? 'Tavily' : 'TinyFish' }) : t('web.runtime_action.quota_confirm');

  return <>
    {feedback && <p className="next-sources-toast" data-error={feedback.error || undefined} role={feedback.error ? 'alert' : 'status'}>{feedback.message}<button type="button" aria-label={t('web.next.close')} onClick={workspace.dismissFeedback}><Icon name="close" size={14} /></button></p>}
    <Dialog.Root open={Boolean(action)} onOpenChange={open => { if (!open) workspace.cancelAction(); }}>
      <Dialog.Portal><Dialog.Overlay className="next-sources-action-overlay" />
        <Dialog.Content className="next-sources-action-dialog" onCloseAutoFocus={event => {
          event.preventDefault();
          const target = workspace.actionOpener.current;
          if (target?.isConnected && target instanceof HTMLButtonElement && !target.disabled) target.focus();
          else (document.querySelector<HTMLElement>('.next-source-inspector [data-close-source]') ?? document.querySelector<HTMLElement>('.next-search-pill'))?.focus();
        }}>
          <Dialog.Title>{title}</Dialog.Title><Dialog.Description>{description}</Dialog.Description>
          {!allowed && <p>{workspace.reason}</p>}
          <footer><Dialog.Close disabled={running}>{t('web.action.cancel')}</Dialog.Close>
            <button type="button" data-danger={action?.type === 'release' || undefined} disabled={!allowed || running} onClick={() => void workspace.runAction()}>{t(running ? 'web.note.refreshing' : 'web.action.confirm')}</button></footer>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  </>;
}
