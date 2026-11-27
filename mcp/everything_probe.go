package mcpserver

import (
	"context"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/fetch/everything"
	"websearch/pkg/i18n"
	"websearch/pkg/log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Protected by everythingMu. A server whose initial probe fails can register
// file_search after the administrator explicitly probes again.
var waitingForEverything []*mcp.Server

var everythingProbeSlot = make(chan struct{}, 1)

func registerEverything(server *mcp.Server) {
	everythingMu.Lock()
	if everythingInst != nil {
		addEverythingTool(server)
		setEverythingState("registered")
		everythingMu.Unlock()
		return
	}
	if everythingLazyCfg == nil {
		setEverythingState("unavailable")
		everythingMu.Unlock()
		return
	}
	cfg := *everythingLazyCfg
	waitingForEverything = append(waitingForEverything, server)
	everythingMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := ReprobeEverything(ctx, cfg); err != nil {
		log.InfoT("log.mcp.everything_http_server_probe_failed_file", "err", err)
	}
}

// ReprobeEverything updates connectivity on demand and makes a recovered
// endpoint available to already-connected MCP clients.
func ReprobeEverything(ctx context.Context, cfg config.EverythingConfig) error {
	select {
	case everythingProbeSlot <- struct{}{}:
		defer func() { <-everythingProbeSlot }()
	case <-ctx.Done():
		return ctx.Err()
	}
	client := everything.New(cfg.URL, cfg.Username, cfg.Password, time.Duration(cfg.TimeoutSec)*time.Second)
	err := client.Probe(ctx)
	everythingMu.Lock()
	defer everythingMu.Unlock()
	if err != nil {
		if everythingInst == nil {
			setEverythingState("unavailable")
		}
		setEverythingConnectivity("unavailable")
		return err
	}
	everythingInst, everythingRoots, everythingMaxResults = client, cfg.Roots, cfg.MaxResults
	everythingNoise, everythingMinAlign = everything.NoiseDirSet(cfg.NoiseDirs), cfg.MinAlignment
	if len(waitingForEverything) > 0 {
		for _, server := range waitingForEverything {
			addEverythingTool(server)
		}
		waitingForEverything = nil
		setEverythingState("registered")
	}
	setEverythingConnectivity("available")
	return nil
}

func addEverythingTool(server *mcp.Server) {
	desc := i18n.T("mcp.tool.file_search.desc")
	if len(everythingRoots) > 0 {
		desc += i18n.T("mcp.tool.file_search.roots", "RootCount", len(everythingRoots))
	}
	desc += i18n.T("mcp.tool.file_search.time")
	desc += i18n.T("mcp.tool.file_search.paging")
	addTool(server, &mcp.Tool{Name: "file_search", Description: desc}, FileSearch)
	log.InfoT("log.mcp.available_tool_file_search")
}

func setEverythingState(state string) {
	toolRegistry.Lock()
	defer toolRegistry.Unlock()
	for i := range toolRegistry.states {
		if toolRegistry.states[i].Name == "file_search" {
			toolRegistry.states[i].State = state
			toolRegistry.states[i].Registered = state == "registered"
		}
	}
}

func setEverythingConnectivity(status string) {
	toolRegistry.Lock()
	defer toolRegistry.Unlock()
	toolRegistry.everythingConnectivity = status
}

func EverythingConnectivity() string {
	toolRegistry.RLock()
	defer toolRegistry.RUnlock()
	return toolRegistry.everythingConnectivity
}
