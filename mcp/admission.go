package mcpserver

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"websearch/pkg/admission"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
	"websearch/pkg/log"
	"websearch/pkg/memguard"
)

type admissionGates struct {
	ingress  *admission.Gate
	outbound *admission.Gate
}

// currentAdmissionGates holds the gates built for the active server so /metrics
// can render them without threading the server instance through the HTTP layer.
var currentAdmissionGates atomic.Pointer[admissionGates]

// publishAdmissionGates builds the two process-wide gates from config and makes
// them observable. Negative config values disable the corresponding gate.
func publishAdmissionGates(conf config.Config) (*admission.Gate, *admission.Gate) {
	wait := conf.Limits.QueueWait()
	ingress := admission.NewGate(conf.Limits.MaxInflightOrDefault(), wait)
	outbound := admission.NewGate(conf.Limits.MaxOutboundOrDefault(), wait)
	currentAdmissionGates.Store(&admissionGates{ingress: ingress, outbound: outbound})
	return ingress, outbound
}

// AdmissionStats returns live gate counters for /metrics.
func AdmissionStats() (ingress, outbound admission.Stats, ok bool) {
	gates := currentAdmissionGates.Load()
	if gates == nil {
		return admission.Stats{}, admission.Stats{}, false
	}
	return gates.ingress.Stats(), gates.outbound.Stats(), true
}

// admissionMiddleware bounds concurrent tools/call and hands the outbound gate
// to downstream search/fetch code through the request context.
//
// The gate's buffered channel is the queue: a call either gets a slot
// immediately, waits within the configured budget, or is shed with a tool-level
// error. Shedding here keeps burst load from multiplying into goroutines,
// sockets and page buffers inside a small VM.
func admissionMiddleware(ingress, outbound *admission.Gate, mem *memguard.Controller) func(mcp.MethodHandler) mcp.MethodHandler {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}

			release, err := ingress.Acquire(ctx)
			if err != nil {
				if errors.Is(err, admission.ErrBusy) {
					s := ingress.Stats()
					log.WarnT("log.mcp.admission_shed", "tool", toolName(req), "inflight", s.Inflight, "queued", s.Queued)
					return busyResult(), nil
				}
				// Client went away while queued: let the transport unwind.
				return nil, err
			}
			defer release()

			mem.Begin()
			defer mem.End()

			return next(admission.WithGate(ctx, outbound), method, req)
		}
	}
}

func toolName(req mcp.Request) string {
	if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
		return call.Params.Name
	}
	return ""
}

func busyResult() *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: i18n.T("tool.limits.server_busy")}},
	}
}
