import * as Dialog from '@radix-ui/react-dialog';
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ComponentType, type MouseEvent, type PropsWithChildren } from 'react';
import { api } from './api';
import { DraftBar } from './DraftBar';
import { useDraft } from './draft';
import { setLanguage, t, useLocale } from './i18n';
import { CommandPalette, CommandRegistry, NavigationContext, Icon, destinationFromHash, destinationHash, navigationGroups, type Command, type Destination, type PageId } from './navigation';
import Overview, { type OverviewResponse } from './Overview';
import { Preferences, appearanceOptions, languageOptions, usePreferences, type Appearance, type Brand } from './preferences';
import './shell.css';

type PageProps = { item?: string; refresh: number };
type RegisteredPages = Partial<Record<Exclude<PageId, 'overview'>, ComponentType<PageProps>>>;
type RefreshStatus = 'idle' | 'loading' | 'success' | 'error';

function initialBrand(): Brand {
  return (window as Window & { __BRAND__?: Brand }).__BRAND__ ?? {};
}

function BrandLogo({ logo }: { logo?: string }) {
  const [broken, setBroken] = useState(false);
  return logo === '/__admin/api/brand/logo' && !broken
    ? <img className="next-brand-logo" alt="" width={37} height={37} src={logo} onError={() => setBroken(true)} />
    : <span className="next-mark"><Icon name="brand" size={26} /></span>;
}

function NavContents({ page, brand, go, mobile = false }: {
  page: PageId;
  brand: Brand;
  go: (destination: Destination) => void;
  mobile?: boolean;
}) {
  const customTitle = brand.title && brand.title !== 'WebSearch 控制中心' ? brand.title : null;
  function follow(event: MouseEvent<HTMLAnchorElement>, nextPage: PageId) {
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    go({ page: nextPage });
  }

  return (
    <>
      <div className="next-brand">
        <BrandLogo key={brand.logo} logo={brand.logo} />
        <div><strong>{customTitle ?? 'WebSearch'}</strong><small>{t('web.page.control_center')}</small></div>
        {mobile && (
          <Dialog.Close className="next-round next-close-nav" aria-label={t('web.next.close')}>
            <Icon name="close" size={15} />
          </Dialog.Close>
        )}
      </div>
      {navigationGroups().map(group => (
        <nav className="next-nav-group" key={group.pages[0].id} aria-label={group.label}>
          <h2>{group.label}</h2>
          {group.pages.map(entry => (
            <a href={destinationHash({ page: entry.id })} key={entry.id} onClick={event => follow(event, entry.id)} aria-current={page === entry.id ? 'page' : undefined}>
              <Icon name={entry.icon} />{entry.label}
            </a>
          ))}
        </nav>
      ))}
      {brand.footer !== false && (
        <footer className="next-rail-footer">
          <Icon name="shield" size={20} />
          <span>{t('web.next.footer')}</span>
        </footer>
      )}
    </>
  );
}

const NoFeatures = ({ children }: PropsWithChildren<{ refresh: number }>) => <>{children}</>;
export default function App({ views, Features = NoFeatures }: { views?: RegisteredPages; Features?: ComponentType<PropsWithChildren<{ refresh: number }>> }) {
  const locale = useLocale();
  const { refresh: refreshDraft } = useDraft();
  const [destination, setDestination] = useState(destinationFromHash);
  const { page, item } = destination;
  const [brand, setBrand] = useState(initialBrand);
  const [refresh, setRefresh] = useState(0);
  const [status, setStatus] = useState<RefreshStatus>('idle');
  const [updated, setUpdated] = useState<Date | null>(null);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [prefsOpen, setPrefsOpen] = useState(false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [registry, setRegistry] = useState<Map<symbol, readonly Command[]>>(() => new Map());
  const headingRef = useRef<HTMLHeadingElement>(null);
  const commandTrigger = useRef<HTMLButtonElement>(null);
  const commandReturnFocus = useRef<HTMLElement | null>(null);
  const pendingPageFocus = useRef(false);
  const focusAfterNavigation = useRef(false);
  const settings = usePreferences(brand);

  const pages = navigationGroups().flatMap(group => group.pages);
  const entry = pages.find(candidate => candidate.id === page)!;
  const documentTitle = brand.title && brand.title !== 'WebSearch 控制中心' ? brand.title : t('web.page.websearch_control_center');
  const View = page === 'overview' ? undefined : views?.[page];
  const register = useCallback((commands: readonly Command[]) => {
    const key = Symbol();
    setRegistry(current => new Map(current).set(key, commands));
    return () => setRegistry(current => {
      const remaining = new Map(current);
      remaining.delete(key);
      return remaining;
    });
  }, []);

  useLayoutEffect(() => { document.title = `${entry.label} · ${documentTitle}`; }, [entry.label, documentTitle]);
  useEffect(() => {
    const heading = headingRef.current;
    const pointer = () => heading?.setAttribute('data-pointer-focus', '');
    const keyboard = () => heading?.removeAttribute('data-pointer-focus');
    window.addEventListener('pointerdown', pointer, true);
    window.addEventListener('keydown', keyboard, true);
    return () => {
      window.removeEventListener('pointerdown', pointer, true);
      window.removeEventListener('keydown', keyboard, true);
    };
  }, []);
  useEffect(() => {
    function update() {
      const next = destinationFromHash();
      if (['#events', '#usage', '#tools'].includes(location.hash)) history.replaceState(null, '', destinationHash(next));
      setDestination(next);
    }
    if (['#events', '#usage', '#tools'].includes(location.hash)) update();
    window.addEventListener('hashchange', update);
    return () => window.removeEventListener('hashchange', update);
  }, []);

  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 861px)');
    const change = () => { if (desktop.matches) setMobileOpen(false); };
    desktop.addEventListener('change', change);
    return () => desktop.removeEventListener('change', change);
  }, []);

  useEffect(() => {
    const onApplied = () => {
      setStatus('loading');
      setRefresh(current => current + 1);
    };
    window.addEventListener('websearch:configuration-applied', onApplied);
    return () => window.removeEventListener('websearch:configuration-applied', onApplied);
  }, []);

  const go = useCallback((next: Destination) => {
    const hash = destinationHash(next);
    if (location.hash !== hash) history.pushState(null, '', hash);
    pendingPageFocus.current = paletteOpen || mobileOpen;
    focusAfterNavigation.current = !pendingPageFocus.current;
    setDestination(next);
    setMobileOpen(false);
    window.scrollTo(0, 0);
  }, [paletteOpen, mobileOpen]);

  useLayoutEffect(() => {
    if (!focusAfterNavigation.current) return;
    focusAfterNavigation.current = false;
    headingRef.current?.focus({ preventScroll: true });
  }, [destination]);

  function focusHeadingAfterClose(event: Event) {
    if (!pendingPageFocus.current) return false;
    pendingPageFocus.current = false;
    event.preventDefault();
    headingRef.current?.focus({ preventScroll: true });
    return true;
  }

  const openPalette = useCallback(() => {
    const active = document.activeElement;
    commandReturnFocus.current = prefsOpen || !(active instanceof HTMLElement) ? commandTrigger.current : active;
    setPrefsOpen(false);
    setPaletteOpen(true);
  }, [prefsOpen]);

  useEffect(() => {
    const openFromKeyboard = (event: KeyboardEvent) => {
      if ((!event.ctrlKey && !event.metaKey) || event.altKey || event.key.toLowerCase() !== 'k') return;
      event.preventDefault();
      if (document.querySelector('[role="dialog"][data-state="open"]:not(.next-preferences), dialog[open]')) return;
      openPalette();
    };
    window.addEventListener('keydown', openFromKeyboard);
    return () => window.removeEventListener('keydown', openFromKeyboard);
  }, [openPalette]);

  const updateBrand = useCallback(({ brand: next }: OverviewResponse) => { if (next) setBrand(next); }, []);
  const trackRefresh = useCallback((next: Exclude<RefreshStatus, 'idle'>) => {
    setStatus(next);
    if (next === 'success') setUpdated(new Date());
  }, []);

  const overviewActive = page === 'overview';
  useEffect(() => {
    if (overviewActive || status !== 'loading') return;
    const abort = new AbortController();
    void api<OverviewResponse>('overview', { signal: abort.signal }).then(data => {
      if (abort.signal.aborted) return;
      updateBrand(data);
      trackRefresh('success');
    }).catch(() => { if (!abort.signal.aborted) trackRefresh('error'); });
    return () => abort.abort();
  }, [overviewActive, refresh, status, trackRefresh, updateBrand]);

  function refreshData() {
    if (status === 'loading') return;
    setStatus('loading');
    setRefresh(current => current + 1);
    void refreshDraft();
  }

  const refreshLabel = status === 'loading' ? t('web.note.refreshing')
    : status === 'error' ? t('web.status.read_failed')
    : updated ? t('web.next.refreshed_at', { Time: updated.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', second: '2-digit' }) })
    : t('web.note.not_refreshed_yet');

  const appearanceIcons = { light: 'sun', dark: 'moon', system: 'monitor' } as const;
  const commands: Command[] = pages.map(({ id, label, icon }) => ({
    id, label, group: t('web.next.workspace'), icon, keywords: id, run: () => go({ page: id }),
  }));
  commands.push(
    ...Array.from(registry.values()).flat(),
    ...(Object.entries(appearanceOptions()) as Array<[Appearance, string]>).map(([option, label]) => ({
      id: `appearance-${option}`, label, group: t('web.next.appearance'), icon: appearanceIcons[option],
      keywords: 'theme ' + option, run: () => settings.setAppearance(option),
    })),
    ...(Object.entries(languageOptions()) as Array<['auto' | 'zh' | 'en', string]>).map(([option, label]) => ({
      id: `language-${option}`, label, group: t('web.next.language'), icon: 'globe' as const,
      keywords: 'language ' + option, run: () => setLanguage(option),
    })),
    { id: 'refresh', label: t('web.action.refresh_all_data_now'), group: t('web.label.actions'), icon: 'refresh', keywords: 'refresh', run: refreshData },
  );

  const isApple = /Mac|iPhone|iPad/.test(navigator.platform);
  return (
    <CommandRegistry.Provider value={register}><NavigationContext.Provider value={go}><Features refresh={refresh}>
      <div className="next-frame">
        <a className="next-skip" href="#next-content" onClick={event => { event.preventDefault(); headingRef.current?.focus(); }}>{t('web.next.skip_content')}</a>
        <aside className="next-rail"><NavContents page={page} brand={brand} go={go} /></aside>
        <div className="next-workspace">
          <header className="next-topbar">
            <div className="next-context">
              <Dialog.Root open={mobileOpen} onOpenChange={setMobileOpen}>
                <Dialog.Trigger className="next-round next-mobile-toggle" aria-label={t('web.next.navigation')}>
                  <Icon name="menu" />
                </Dialog.Trigger>
                <Dialog.Portal>
                  <Dialog.Overlay className="next-mobile-overlay" />
                  <Dialog.Content className="next-rail next-mobile-rail" aria-describedby={undefined} onCloseAutoFocus={focusHeadingAfterClose}>
                    <Dialog.Title className="next-sr-only">{t('web.next.navigation')}</Dialog.Title>
                    <NavContents page={page} brand={brand} go={go} mobile />
                  </Dialog.Content>
                </Dialog.Portal>
              </Dialog.Root>
              <span className="next-context-group">{t('web.next.workspace')}<span aria-hidden="true">/</span></span>
              <span className="next-current">{entry.label}</span>
            </div>
            <Dialog.Root open={paletteOpen} onOpenChange={setPaletteOpen}>
              <Dialog.Trigger ref={commandTrigger} className="next-search-pill" aria-keyshortcuts="Control+K Meta+K" onClick={openPalette}>
                <Icon name="search" size={15} />{t('web.next.search')}<kbd aria-hidden="true">{isApple ? '⌘ K' : 'Ctrl K'}</kbd>
              </Dialog.Trigger>
              <Dialog.Portal>
                <Dialog.Overlay className="next-command-overlay" />
                <Dialog.Content className="next-command" onCloseAutoFocus={event => {
                  if (focusHeadingAfterClose(event)) return;
                  event.preventDefault();
                  const target = commandReturnFocus.current;
                  if (target?.isConnected && target !== document.body) target.focus({ preventScroll: true });
                  else commandTrigger.current?.focus({ preventScroll: true });
                }}>
                  <CommandPalette commands={commands} close={() => setPaletteOpen(false)} />
                </Dialog.Content>
              </Dialog.Portal>
            </Dialog.Root>
            <div className="next-actions">
              <span className="next-refresh-status" data-status={status} role="status"><i />{refreshLabel}</span>
              <button type="button" className="next-round next-refresh" title={t('web.action.refresh_all_data_now')} aria-label={t('web.action.refresh_all_data_now')} aria-disabled={status === 'loading'} onClick={refreshData}>
                <Icon name="refresh" size={15} />
              </button>
              <Preferences settings={settings} open={prefsOpen} onOpenChange={setPrefsOpen} />
            </div>
          </header>
          <main className="next-main" id="next-content">
            <div className="next-heading">
              <h1 ref={headingRef} tabIndex={-1}>{entry.label}</h1>
            </div>
            {overviewActive ? <Overview onSource={name => go({ page: 'sources', item: name })} refresh={refresh} onSnapshot={updateBrand} onRefreshStatus={trackRefresh} />
              : View ? <View item={item} refresh={refresh} />
              : null}
          </main>
        </div>
      </div>
      <DraftBar showWriteReason={page !== 'settings' || item === 'storage'} />
    </Features></NavigationContext.Provider></CommandRegistry.Provider>
  );
}
