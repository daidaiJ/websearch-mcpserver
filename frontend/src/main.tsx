import Settings, { SettingsCommands } from './Settings'
import { CredentialCommands } from './SettingsCredentials'
import { createRoot } from 'react-dom/client'
import type { PropsWithChildren } from 'react'
import App from './App'
import { DraftProvider } from './draft'
import Sources from './Sources'
import SourcesCommands, { SourcesProvider } from './SourcesCommands'
import Capabilities from './Capabilities'
import Diagnostics from './Diagnostics'
import CapabilitiesCommands from './CapabilitiesCommands'
import Traces from './Traces'
import './tokens.css'

const views = { settings: Settings, sources: Sources, traces: Traces, capabilities: Capabilities, diagnostics: Diagnostics }
function Features({ children, refresh }: PropsWithChildren<{ refresh: number }>) {
  return <SourcesProvider>
    <SourcesCommands refresh={refresh} />
    <SettingsCommands /><CredentialCommands />
    <CapabilitiesCommands refresh={refresh}>{children}</CapabilitiesCommands>
  </SourcesProvider>
}
createRoot(document.getElementById('root')!).render(
  <DraftProvider><App views={views} Features={Features} /></DraftProvider>
)

