import { createContext, useContext, useEffect, useRef, useState } from 'react';
import { api } from './api';
import { t, useLocale } from './i18n';

export type DiagnosticOption = { id: string; name: string; confirm: boolean };
export type CapabilityTool = {
  name: string;
  enabled: boolean;
  registered: boolean;
  state: string;
  fetch_enabled?: boolean;
  file?: { connectivity?: string; allowed_root_count: number; whitelist_enforced: boolean; path_metadata_only: boolean };
  pdf?: { max_pages: number; mineru_page_limit: number; page_progress: string };
  history?: { retention_days: number };
};

export type CapabilityOverview = {
  configured_tools?: CapabilityTool[] | null;
  tools?: Array<{ name: string; status: string; today?: { requests: number } }> | null;
  diagnostics?: { sources: DiagnosticOption[] | null; fetch_services: string[] | null };
  runtime?: {
    proxy?: { mode: 'auto' | 'explicit' | 'disabled'; resolved: boolean; endpoint?: string };
    llm?: { enabled: boolean; model_id: string };
    mcp_hub?: { fetch_mode: 'off' | 'fallback' | 'prefer' };
  };
};

export type DiagnosticResult = {
  ok: boolean;
  elapsed_ms: number;
  endpoint?: string;
  error_kind?: string;
  error_detail?: string;
  result_count?: number;
  titles?: string[];
  links?: { title: string; url: string }[];
  fetch?: { node?: string; attempts: string[]; body_status: string; characters: number; remote_redirects: boolean };
};

type DiagnosticKind = 'search' | 'fetch' | 'proxy' | 'everything';

function runDiagnostic(kind: DiagnosticKind, payload: object, signal: AbortSignal) {
  return api<DiagnosticResult>(`diagnostics/${kind}`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
    signal: AbortSignal.any([signal, AbortSignal.timeout(20000)]),
  });
}

type Outcome = { result?: DiagnosticResult; error?: string };

export function useDiagnosticAction() {
  const pending = useRef<AbortController | null>(null);
  const [active, setActive] = useState<DiagnosticKind>();
  const [outcomes, setOutcomes] = useState<Partial<Record<DiagnosticKind, Outcome>>>({});
  useEffect(() => () => pending.current?.abort(), []);

  async function run(kind: DiagnosticKind, payload: object, onResult?: () => void) {
    if (pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setActive(kind);
    setOutcomes(current => ({ ...current, [kind]: undefined }));
    try {
      const result = await runDiagnostic(kind, payload, controller.signal);
      if (!controller.signal.aborted) {
        setOutcomes(current => ({ ...current, [kind]: { result } }));
        onResult?.();
      }
    } catch (error) {
      if (!controller.signal.aborted) setOutcomes(current => ({ ...current, [kind]: {
        error: error instanceof Error ? error.message : t('web.status.unknown_error'),
      } }));
    } finally {
      pending.current = null;
      if (!controller.signal.aborted) setActive(undefined);
    }
  }

  return { outcomes, active, run };
}

export function useCapabilitySnapshot(refresh: number) {
  const locale = useLocale();
  const [requested, setRequested] = useState(0);
  const [snapshot, setSnapshot] = useState<{ data?: CapabilityOverview; loading: boolean; error?: string }>({ loading: true });

  useEffect(() => {
    const controller = new AbortController();
    setSnapshot(previous => ({ data: previous.data, loading: true }));
    void api<CapabilityOverview>('overview', { signal: AbortSignal.any([controller.signal, AbortSignal.timeout(5000)]) })
      .then(data => { if (!controller.signal.aborted) setSnapshot({ data, loading: false }); })
      .catch(error => {
        if (!controller.signal.aborted) setSnapshot(previous => ({
          data: previous.data, loading: false, error: error instanceof Error ? error.message : t('web.status.read_failed'),
        }));
      });
    return () => controller.abort();
  }, [refresh, requested, locale]);

  return { ...snapshot, reload: () => setRequested(value => value + 1) };
}

export const CapabilityContext = createContext<ReturnType<typeof useCapabilitySnapshot> | null>(null);

export function useCapabilities() {
  const snapshot = useContext(CapabilityContext);
  if (!snapshot) throw new Error('CapabilitiesCommands is required');
  return snapshot;
}
