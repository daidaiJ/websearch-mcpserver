import { useMemo, useRef, type ReactNode } from 'react';
import { CapabilityContext, useCapabilitySnapshot } from './CapabilityData';
import { MODULE_IDS, moduleNames } from './CapabilityFields';
import { DIAGNOSTIC_TARGETS } from './Diagnostics';
import { t, useLocale } from './i18n';
import { useNavigate, useRegisterCommands, type Command } from './navigation';

export default function CapabilitiesCommands({ children, refresh }: { children: ReactNode; refresh: number }) {
  const navigate = useNavigate();
  const go = useRef(navigate);
  go.current = navigate;
  const locale = useLocale();
  const snapshot = useCapabilitySnapshot(refresh);
  const sources = snapshot.error ? undefined : snapshot.data?.diagnostics?.sources;
  const commands = useMemo((): Command[] => {
    const modules = moduleNames();
    const capabilities = t('web.page.tools_capabilities');
    const diagnostics = t('web.next.diagnostics');
    const targets = [
      { id: DIAGNOSTIC_TARGETS.proxy, label: t('web.capNext.proxy_probe'), keywords: 'proxy' },
      { id: DIAGNOSTIC_TARGETS.search, label: t('web.capNext.single_search'), keywords: 'search' },
      { id: DIAGNOSTIC_TARGETS.fetch, label: t('web.capNext.single_fetch'), keywords: 'fetch url' },
    ];
    return [
      ...MODULE_IDS.map((id): Command => ({ id: `cap-module-${id}`, label: modules[id], group: capabilities, icon: 'box',
        keywords: id, run: () => go.current({ page: 'capabilities', item: id }) })),
      ...targets.map(({ id, label, keywords }): Command => ({ id: `cap-diagnostic-${id}`, label, group: diagnostics, icon: 'lab',
        keywords, run: () => go.current({ page: 'diagnostics', item: id }) })),
      ...(sources ?? []).map((source): Command => ({ id: `cap-diagnostic-source-${source.id}`,
        label: t('web.capNext.command_for_source', { Source: source.name }), group: diagnostics, icon: 'search',
        keywords: source.id, run: () => go.current({ page: 'diagnostics', item: source.id }) })),
    ];
  }, [locale, sources]);
  useRegisterCommands(commands);
  return <CapabilityContext.Provider value={snapshot}>{children}</CapabilityContext.Provider>;
}
