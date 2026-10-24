package mcpserver

import "sync"

// ToolState describes registration, not configuration or historical calls.
type ToolState struct {
	Name       string `json:"name"`
	Registered bool   `json:"registered"`
	State      string `json:"state"`
}

var toolRegistry = struct {
	sync.RWMutex
	states []ToolState
}{states: pendingTools()}

func pendingTools() []ToolState {
	return []ToolState{
		{Name: "smartsearch", State: "pending"},
		{Name: "academicsearch", State: "pending"},
		{Name: "cleanfetch", State: "pending"},
		{Name: "pdf_parser", State: "pending"},
		{Name: "file_search", State: "unprobed"},
		{Name: "search_history", State: "pending"},
	}
}

// ToolSnapshot never initializes a transport or probes Everything.
func ToolSnapshot() []ToolState {
	toolRegistry.RLock()
	defer toolRegistry.RUnlock()
	return append([]ToolState(nil), toolRegistry.states...)
}

func publishTools(states []ToolState) {
	toolRegistry.Lock()
	defer toolRegistry.Unlock()
	toolRegistry.states = append([]ToolState(nil), states...)
}

func smartsearchAvailable() bool {
	if searchGroup != nil {
		return searchGroup.Primary != nil || searchGroup.Fallback != nil
	}
	return searchapi != nil || fallbackSearch != nil
}
