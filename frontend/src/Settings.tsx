import { useMemo } from 'react';
import { useDraft } from './draft';
import { t, useLocale } from './i18n';
import { destinationHash, useNavigate, useRegisterCommands } from './navigation';
import SettingsRoute from './SettingsRoute';
import SettingsCredentials from './SettingsCredentials';
import SettingsQuota from './SettingsQuota';
import SettingsStorage from './SettingsStorage';
import SettingsService from './SettingsService';
import './settings.css';

function sections() {
  return [
    { id: 'routing', label: t('web.settingsNext.routing'), panel: SettingsRoute },
    { id: 'credentials', label: t('web.settingsNext.credentials'), panel: SettingsCredentials },
    { id: 'quotas', label: t('web.settingsNext.quotas'), panel: SettingsQuota },
    { id: 'storage', label: t('web.settingsNext.storage'), panel: SettingsStorage },
    { id: 'service', label: t('web.settingsNext.service'), panel: SettingsService },
  ];
}

export function SettingsCommands() {
  const navigate = useNavigate();
  const locale = useLocale();
  const commands = useMemo(() => sections().map(({ id, label }) => ({
    id: 'settings-' + id, label, group: t('web.page.settings'), icon: 'sliders' as const,
    keywords: 'settings ' + id, run: () => navigate({ page: 'settings', item: id }),
  })), [locale, navigate]);
  useRegisterCommands(commands);
  return null;
}

export default function Settings({ item, refresh }: { item?: string; refresh: number }) {
  useLocale();
  const navigate = useNavigate();
  const { canWrite, writeReason, loadError } = useDraft();
  const [section, key] = (item ?? '').split(':');
  const pages = sections();
  const current = pages.find(page => page.id === section) ?? pages[0];
  const Panel = current.panel;
  return <div className="settings-workspace">
    <nav className="settings-subnav" aria-label={t('web.page.settings')}>
      {pages.map(({id, label}) => <a key={id} href={destinationHash({page:'settings', item:id})}
        aria-current={id === current.id ? 'page' : undefined} onClick={event => {
          if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
          event.preventDefault(); navigate({page:'settings', item:id});
        }}>{label}</a>)}
    </nav>
    <div className="settings-pane">
      {!canWrite && current.id !== 'storage' && <p className="settings-permission-note" role={loadError ? 'alert' : 'status'}>{writeReason}</p>}
      <Panel item={key} refresh={refresh} />
    </div>
  </div>;
}

