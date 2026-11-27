import * as Popover from '@radix-ui/react-popover';
import { useLayoutEffect, useState } from 'react';
import { getLanguage, setLanguage, t } from './i18n';
import { Icon } from './navigation';

const appearances = ['light', 'dark', 'system'] as const;
const densities = ['normal', 'compact'] as const;
const accents = ['green', 'blue', 'mono'] as const;
export type Appearance = typeof appearances[number];
type Density = typeof densities[number];
type Accent = typeof accents[number];

export type Brand = { title?: string; theme?: string; accent?: string; logo?: string; footer?: boolean };

function readPreference<T extends string>(key: string, options: readonly T[]) {
  try {
    const value = localStorage.getItem(key);
    return options.find(option => option === value);
  } catch {
    return undefined;
  }
}

function writePreference(key: string, value: string | null) {
  try {
    if (value === null) localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Browser policy can deny storage while allowing live preview.
  }
}

export function usePreferences(brand: Brand) {
  const [appearance, updateAppearance] = useState<Appearance>(() => readPreference('websearch.theme', appearances) ?? 'system');
  const [density, updateDensity] = useState<Density>(() => readPreference('websearch.density', densities) ?? 'normal');
  const [selectedAccent, updateAccent] = useState<Accent | null>(() => readPreference('websearch.brand', accents) ?? null);
  const serverAccent = accents.find(accent => accent === brand.theme) ?? 'green';
  const accent = selectedAccent ?? serverAccent;
  const customAccent = selectedAccent === null && /^#(?:[\da-f]{3}|[\da-f]{6})$/i.test(brand.accent ?? '') ? brand.accent : undefined;

  useLayoutEffect(() => {
    const dark = window.matchMedia('(prefers-color-scheme: dark)');
    const update = () => {
      document.documentElement.dataset.theme = appearance === 'system' ? (dark.matches ? 'dark' : 'light') : appearance;
    };
    update();
    if (appearance !== 'system') return;
    dark.addEventListener('change', update);
    return () => dark.removeEventListener('change', update);
  }, [appearance]);

  useLayoutEffect(() => {
    const root = document.documentElement;
    root.dataset.density = density;
    root.dataset.brand = accent;
    if (customAccent) root.style.setProperty('--brand-accent', customAccent);
    else root.style.removeProperty('--brand-accent');
  }, [density, accent, customAccent]);

  return {
    appearance, density, accent, customAccent, selectedAccent,
    setAppearance(next: Appearance) {
      updateAppearance(next);
      writePreference('websearch.theme', next);
    },
    setDensity(next: Density) {
      updateDensity(next);
      writePreference('websearch.density', next);
    },
    setAccent(next: Accent | null) {
      updateAccent(next);
      writePreference('websearch.brand', next);
    },
  };
}

export function appearanceOptions(): Record<Appearance, string> {
  return { light: t('web.next.light'), dark: t('web.next.dark'), system: t('web.next.system') };
}

export function languageOptions() {
  return { auto: t('web.next.auto'), zh: '中文', en: 'English' };
}

function Segments<T extends string>({ title, options, value, select, swatches = false }: {
  title: string;
  options: Record<T, string>;
  value: T | null;
  select: (value: T) => void;
  swatches?: boolean;
}) {
  return (
    <fieldset>
      <legend>{title}</legend>
      <div className="next-segments">
        {(Object.keys(options) as T[]).map(next => (
          <button key={next} type="button" aria-pressed={value === next} onClick={() => select(next)}>
            {swatches && <span className="next-swatch" data-tone={next} aria-hidden="true" />}{options[next]}
          </button>
        ))}
      </div>
    </fieldset>
  );
}

export function Preferences({ settings, open, onOpenChange }: {
  settings: ReturnType<typeof usePreferences>;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const title = t('web.next.preferences');
  return (
    <Popover.Root open={open} onOpenChange={onOpenChange}>
      <Popover.Trigger className="next-round" aria-label={title} title={title}>
        <Icon name="settings" />
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content align="end" sideOffset={12} collisionPadding={12} className="next-preferences" aria-label={title}>
          <h2>{title}</h2>
          <Segments title={t('web.next.appearance')} options={appearanceOptions()} value={settings.appearance} select={settings.setAppearance} />
          <Segments title={t('web.next.language')} options={languageOptions()} value={getLanguage()} select={setLanguage} />
          <Segments title={t('web.next.density')} options={{ normal: t('web.next.comfortable'), compact: t('web.next.compact') }} value={settings.density} select={settings.setDensity} />
          <Segments
            title={t('web.next.brand_accent')}
            options={{ green: t('web.next.pine'), blue: t('web.next.blue'), mono: t('web.next.graphite') }}
            value={settings.customAccent ? null : settings.accent}
            select={settings.setAccent}
            swatches
          />
          {settings.selectedAccent && (
            <button className="next-pref-reset" type="button" onClick={event => {
              event.currentTarget.closest('.next-preferences')?.querySelector('button')?.focus();
              settings.setAccent(null);
            }}>
              {t('web.next.use_server_accent')}
            </button>
          )}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
