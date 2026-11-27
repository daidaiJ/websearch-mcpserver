import { getLocale, t } from './i18n';

const apiBase = 'http://dashboard.local/__admin/api/';

export class ApiError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
    this.name = 'ApiError';
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const base = new URL(apiBase);
  const target = new URL(path.replace(/^\//, ''), base);
  if (target.origin !== base.origin || !target.pathname.startsWith(base.pathname) || target.hash) {
    throw new TypeError('Invalid dashboard API path');
  }
  const headers = new Headers(init?.headers);
  headers.set('Accept', 'application/json');
  headers.set('Accept-Language', getLocale());
  const response = await fetch(target.pathname + target.search, {
    cache: 'no-store', ...init, credentials: 'same-origin', headers,
  });
  const body: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const problem = body && typeof body === 'object' && 'error' in body ? body.error : undefined;
    throw new ApiError(response.status, typeof problem === 'string' ? problem : `HTTP ${response.status}`);
  }
  if (body === null) throw new ApiError(response.status, t('web.msg.response_is_not_valid_json'));
  return body as T;
}

// The global command indexes and the active view read the same snapshot.
// Share only simultaneous, unprivileged overview reads; refresh always starts
// a new request once the previous response has settled.
interface PendingRead {
  result: Promise<unknown>;
  controller: AbortController;
  readers: number;
}
const overviewReads = new Map<string, PendingRead>();
export function api<T>(path: string, init?: RequestInit): Promise<T> {
  if (path !== 'overview' || Object.keys(init ?? {}).some(option => option !== 'signal')) {
    return request<T>(path, init);
  }
  const signal = init?.signal;
  if (signal?.aborted) return Promise.reject(signal.reason);
  const locale = getLocale();
  let read = overviewReads.get(locale);
  if (!read) {
    const controller = new AbortController();
    const entry: PendingRead = { controller, readers: 0, result: request(path, { signal: controller.signal }) };
    read = entry;
    overviewReads.set(locale, entry);
    const clear = () => {
      if (overviewReads.get(locale) === entry) overviewReads.delete(locale);
    };
    entry.result.then(clear, clear);
  }
  const entry = read;
  entry.readers++;
  return new Promise<T>((resolve, reject) => {
    let finished = false;
    function release() {
      if (finished) return false;
      finished = true;
      signal?.removeEventListener('abort', abort);
      entry.readers--;
      return true;
    }
    function abort() {
      if (!release()) return;
      reject(signal?.reason);
      if (!entry.readers && overviewReads.get(locale) === entry) {
        overviewReads.delete(locale);
        entry.controller.abort();
      }
    }
    signal?.addEventListener('abort', abort, { once: true });
    entry.result.then(value => { if (release()) resolve(value as T); }, error => { if (release()) reject(error); });
  });
}
