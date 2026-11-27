import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { useEffect, useMemo } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import en from '../../pkg/i18n/locales/web.en.json';
import zh from '../../pkg/i18n/locales/web.zh.json';
import App from './App';
import { setLanguage } from './i18n';
import { useRegisterCommands } from './navigation';
import type { Brand } from './preferences';

const { refreshDraft } = vi.hoisted(() => ({ refreshDraft: vi.fn() }));
vi.mock('./draft', () => ({ useDraft: () => ({ refresh: refreshDraft }) }));
vi.mock('./DraftBar', () => ({ DraftBar: () => null }));
vi.mock('./Overview', () => ({
  default: ({ refresh, onRefreshStatus, onSource }: {
    refresh: number;
    onRefreshStatus: (status: 'success') => void;
    onSource: (name: string) => void;
  }) => {
    useEffect(() => { if (refresh > 0) onRefreshStatus('success'); }, [refresh, onRefreshStatus]);
    return <button onClick={() => onSource('openalex')}>Open affected source</button>;
  },
}));

const bootWindow = window as Window & { __BRAND__?: Brand };
const scheme = Object.assign(new EventTarget(), { matches: false });

beforeEach(() => {
  localStorage.clear();
  history.replaceState(null, '', '/dashboard/');
  window.__DEFAULT_LOCALE__ = 'en';
  window.__I18N_CATALOGS__ = { en, zh };
  bootWindow.__BRAND__ = { title: 'Search research', theme: 'blue', accent: '#4260bd', footer: false };
  scheme.matches = false;
  vi.stubGlobal('matchMedia', vi.fn(() => scheme));
  vi.stubGlobal('scrollTo', vi.fn());
  refreshDraft.mockClear();
  setLanguage('auto');
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('dashboard shell', () => {
  it('filters commands, supports keyboard navigation, and restores focus on Escape', async () => {
    const user = userEvent.setup();
    render(<App />);
    const refresh = screen.getByRole('button', { name: 'Refresh all data now' });
    refresh.focus();
    await user.keyboard('{Control>}k{/Control}');
    const palette = screen.getByRole('dialog', { name: 'Command palette' });
    const query = within(palette).getByRole('combobox', { name: 'Search commands' });
    expect(query).toHaveFocus();
    await user.type(query, 'unknown query');
    expect(within(palette).getByText('No matching commands')).toBeInTheDocument();
    await user.keyboard('{Escape}');
    expect(screen.queryByRole('dialog', { name: 'Command palette' })).toBeNull();
    await waitFor(() => expect(refresh).toHaveFocus());

    await user.click(screen.getByRole('button', { name: 'Search' }));
    await user.keyboard('{ArrowDown}{Enter}');
    const heading = screen.getByRole('heading', { name: 'Traces', level: 1 });
    expect(heading).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Traces' })).toHaveAttribute('aria-current', 'page');
    expect(location.hash).toBe('#traces');
    await waitFor(() => expect(heading).toHaveFocus());
  });

  it('previews theme, language, density and branding and returns focus to preferences', async () => {
    const user = userEvent.setup();
    render(<App />);
    expect(screen.getByText('Search research')).toBeInTheDocument();
    expect(screen.queryByRole('contentinfo')).toBeNull();
    const html = document.documentElement;
    expect(html).toHaveAttribute('data-brand', 'blue');
    expect(html.style.getPropertyValue('--brand-accent')).toBe('#4260bd');
    act(() => { scheme.matches = true; scheme.dispatchEvent(new Event('change')); });
    expect(html).toHaveAttribute('data-theme', 'dark');

    await user.click(screen.getByRole('button', { name: 'Preferences' }));
    const prefs = screen.getByRole('dialog', { name: 'Preferences' });
    await user.click(within(prefs).getByRole('button', { name: 'Light' }));
    await user.click(within(prefs).getByRole('button', { name: 'Compact' }));
    await user.click(within(prefs).getByRole('button', { name: 'Graphite' }));
    expect(html).toHaveAttribute('data-theme', 'light');
    expect(html).toHaveAttribute('data-density', 'compact');
    expect(html).toHaveAttribute('data-brand', 'mono');
    expect(html.style.getPropertyValue('--brand-accent')).toBe('');
    expect(localStorage.getItem('websearch.theme')).toBe('light');
    await user.click(within(prefs).getByRole('button', { name: 'Use server accent' }));
    expect(html.style.getPropertyValue('--brand-accent')).toBe('#4260bd');
    await user.click(within(prefs).getByRole('button', { name: '中文' }));
    expect(html.lang).toBe('zh-CN');
    expect(screen.getByRole('dialog', { name: '偏好设置' })).toBeInTheDocument();
    await user.keyboard('{Escape}');
    await waitFor(() => expect(screen.getByRole('button', { name: '偏好设置' })).toHaveFocus());
  });

  it('runs the requested refresh and opens the source named by Overview', async () => {
    const user = userEvent.setup();
    render(<App views={{ sources: ({ item }) => <p>{item}</p> }} />);
    await user.click(screen.getByRole('button', { name: 'Refresh all data now' }));
    expect(refreshDraft).toHaveBeenCalledTimes(1);
    expect(screen.getByRole('status')).toHaveTextContent('Updated at');
    await user.click(screen.getByRole('button', { name: 'Open affected source' }));
    expect(screen.getByRole('heading', { name: 'Search sources', level: 1 })).toHaveFocus();
    expect(screen.getByText('openalex')).toBeInTheDocument();
    expect(location.hash).toBe('#sources/openalex');
  });

  it('runs indexed commands registered by a view', async () => {
    const run = vi.fn();
    function Sources() {
      const commands = useMemo(() => [{ id: 'source-brave', label: 'Brave', group: 'Source index', icon: 'globe' as const, run }], []);
      useRegisterCommands(commands);
      return null;
    }
    const user = userEvent.setup();
    history.replaceState(null, '', '#sources');
    render(<App views={{ sources: Sources }} />);
    await user.keyboard('{Control>}k{/Control}');
    await user.type(screen.getByRole('combobox'), 'brave');
    await user.click(screen.getByRole('option', { name: 'Brave' }));
    expect(run).toHaveBeenCalledTimes(1);
  });
});
