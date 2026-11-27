import * as Dialog from '@radix-ui/react-dialog';
import { createContext, useContext, useEffect, useId, useRef, useState, type KeyboardEvent } from 'react';
import { t } from './i18n';

const iconPaths = {
  grid: 'M3 3h7v7H3z M14 3h7v7h-7z M3 14h7v7H3z M14 14h7v7h-7z',
  search: 'M20 20l-5-5 M17 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0',
  layers: 'm3 8 9-5 9 5-9 5z M3 12l9 5 9-5 M3 16l9 5 9-5',
  activity: 'M2 12h5l3-8 4 16 3-8h5',
  sliders: 'M3 6h8m5 0h5M3 17h3m5 0h10 M11 3v6m-5 5v6',
  settings: 'M10 3h4l.7 2.4 2 .9 2.3-.7 2 3.5-1.7 1.7v2.4l1.7 1.7-2 3.5-2.3-.7-2 .9L14 21h-4l-.7-2.4-2-.9-2.3.7-2-3.5 1.7-1.7v-2.4L3 9.1l2-3.5 2.3.7 2-.9z M15 12a3 3 0 1 1-6 0 3 3 0 1 1 6 0',
  box: 'm3 7 9-5 9 5v10l-9 5-9-5z M3 7l9 5 9-5m-9 5v10',
  arrow: 'M4 12h15m-6-6 6 6-6 6',
  close: 'm6 6 12 12M18 6 6 18',
  globe: 'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0M3 12h18M12 3q-8 9 0 18 8-9 0-18z',
  refresh: 'M20 8a8 8 0 1 0 .3 7M20 3v6h-6',
  menu: 'M3 5h18M3 12h18M3 19h18',
  brand: 'M3 6.5 7.7 18 12 8l4.3 10L21 6.5',
  lab: 'M9 3h6m-5 0v6L4 19q-.3 2 2 2h12q2.3 0 2-2l-6-10V3M7 15h10',
  sun: 'M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.4 1.4m11.2 11.2L19 19M5 19l1.4-1.4M17.6 6.4 19 5',
  moon: 'M20 15a9 9 0 0 1-11-11 9 9 0 1 0 11 11z',
  monitor: 'M3 4h18v13H3z M8 21h8m-4-4v4',
  shield: 'm12 2 8 3v6q0 7-8 11-8-4-8-11V5z M8 12l3 3 5-6',
};

export type IconName = keyof typeof iconPaths;

export function Icon({ name, size = 17 }: { name: IconName; size?: number }) {
  return (
    <svg aria-hidden="true" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.65" strokeLinecap="round" strokeLinejoin="round">
      <path d={iconPaths[name]} />
    </svg>
  );
}

export const PAGE_IDS = ['overview', 'traces', 'sources', 'capabilities', 'diagnostics', 'settings'] as const;
export type PageId = typeof PAGE_IDS[number];
export type Destination = { page: PageId; item?: string };

type Page = { id: PageId; label: string; icon: IconName };
export function navigationGroups(): Array<{ label: string; pages: Page[] }> {
  return [
    { label: t('web.next.monitor'), pages: [
      { id: 'overview', label: t('web.page.overview_2'), icon: 'grid' },
      { id: 'traces', label: t('web.next.traces'), icon: 'activity' },
    ] },
    { label: t('web.next.manage'), pages: [
      { id: 'sources', label: t('web.page.search_sources'), icon: 'layers' },
      { id: 'capabilities', label: t('web.page.tools_capabilities'), icon: 'box' },
      { id: 'diagnostics', label: t('web.next.diagnostics'), icon: 'lab' },
    ] },
    { label: t('web.next.system'), pages: [
      { id: 'settings', label: t('web.page.settings'), icon: 'sliders' },
    ] },
  ];
}

export function destinationFromHash(): Destination {
  const [id, item] = location.hash.slice(1).split('/');
  if (id === 'tools') return { page: 'capabilities' };
  if (id === 'events' || id === 'usage') return { page: 'traces' };
  const page = PAGE_IDS.find(candidate => candidate === id) ?? 'overview';
  if (!item || id !== page) return { page };
  try {
    return { page, item: decodeURIComponent(item) };
  } catch {
    return { page };
  }
}

export function destinationHash({ page, item }: Destination): string {
  return `#${page}${item ? `/${encodeURIComponent(item)}` : ''}`;
}

export const NavigationContext = createContext<((destination: Destination) => void) | null>(null);
export function useNavigate() {
  const navigate = useContext(NavigationContext);
  if (!navigate) throw new Error('Dashboard navigation context is required');
  return navigate;
}

export type Command = {
  id: string;
  label: string;
  group: string;
  icon: IconName;
  keywords?: string;
  run: () => void;
};

type RegisterCommands = (commands: readonly Command[]) => () => void;
export const CommandRegistry = createContext<RegisterCommands | null>(null);

export function useRegisterCommands(commands: readonly Command[]) {
  const register = useContext(CommandRegistry);
  // Memoize command arrays so registrations only change when their entries change.
  useEffect(() => register?.(commands), [register, commands]);
}

export function CommandPalette({ commands, close }: { commands: readonly Command[]; close: () => void }) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState('');
  const [index, setIndex] = useState(0);
  const listId = useId();
  const terms = query.trim().toLocaleLowerCase().split(/\s+/);
  const grouped = new Map<string, Command[]>();

  for (const command of commands) {
    const haystack = [command.label, command.group, command.keywords].join(' ').toLocaleLowerCase();
    if (!terms.every(term => haystack.includes(term))) continue;
    const entries = grouped.get(command.group);
    if (entries) entries.push(command);
    else grouped.set(command.group, [command]);
  }

  const matches = Array.from(grouped.values()).flat();
  const selected = Math.min(index, matches.length - 1);
  const optionId = (command: Command) => `${listId}-${command.id}`;

  function select(command: Command) {
    close();
    command.run();
  }

  function onInputKey(event: KeyboardEvent<HTMLInputElement>) {
    if (event.nativeEvent.isComposing || !matches.length) return;
    if (event.key === 'Enter') {
      event.preventDefault();
      select(matches[selected]);
    } else if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      const next = (selected + step + matches.length) % matches.length;
      setIndex(next);
      document.getElementById(optionId(matches[next]))?.scrollIntoView?.({ block: 'nearest' });
    }
  }

  return (
    <>
      <Dialog.Title className="next-sr-only">{t('web.next.command_palette')}</Dialog.Title>
      <Dialog.Description className="next-sr-only">{t('web.next.command_help')}</Dialog.Description>
      <div className="next-command-query">
        <Icon name="search" size={19} />
        <input
          ref={inputRef}
          type="text"
          role="combobox"
          aria-label={t('web.next.search_commands')}
          aria-expanded="true"
          aria-controls={listId}
          aria-autocomplete="list"
          aria-activedescendant={matches[selected] && optionId(matches[selected])}
          placeholder={t('web.next.command_placeholder')}
          autoComplete="off"
          spellCheck={false}
          value={query}
          onKeyDown={onInputKey}
          onChange={event => { setQuery(event.target.value); setIndex(0); }}
        />
        <Dialog.Close className="next-command-esc" aria-label={t('web.next.close')}><kbd>Esc</kbd></Dialog.Close>
      </div>
      <div id={listId} role="listbox" aria-label={t('web.next.search_commands')} className="next-command-results">
        {Array.from(grouped, ([group, entries]) => (
          <div role="group" aria-label={group} key={group}>
            <h3 aria-hidden="true">{group}</h3>
            {entries.map(command => (
              <div
                key={command.id}
                id={optionId(command)}
                role="option"
                aria-selected={matches[selected] === command}
                className="next-command-option"
                onPointerMove={() => setIndex(matches.indexOf(command))}
                onMouseDown={event => event.preventDefault()}
                onClick={() => select(command)}
              >
                <Icon name={command.icon} size={14} />{command.label}
              </div>
            ))}
          </div>
        ))}
      </div>
      {!matches.length && <p className="next-command-empty" role="status">{t('web.next.no_commands')}</p>}
      <div className="next-command-foot" aria-hidden="true"><kbd>↑</kbd> <kbd>↓</kbd> {t('web.next.move')} <span>·</span> <kbd>↵</kbd> {t('web.next.open')}</div>
    </>
  );
}
