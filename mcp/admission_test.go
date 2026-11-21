package mcpserver

import (
	"context"
	"testing"

	"websearch/pkg/admission"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAdmissionMiddlewareShedsWhenIngressIsFull(t *testing.T) {
	ingress := admission.NewGate(1, 0)
	outbound := admission.NewGate(2, 0)
	held, err := ingress.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold ingress slot: %v", err)
	}
	defer held()

	called := false
	handler := admissionMiddleware(ingress, outbound, nil)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	})

	result, err := handler(context.Background(), "tools/call", &mcp.CallToolRequest{})
	if err != nil {
		t.Fatalf("busy call must return a tool result, got error %v", err)
	}
	if called {
		t.Fatal("downstream handler ran without an ingress slot")
	}
	callResult, ok := result.(*mcp.CallToolResult)
	if !ok || !callResult.IsError {
		t.Fatalf("expected an error tool result, got %#v", result)
	}
	if s := ingress.Stats(); s.Shed != 1 {
		t.Fatalf("ingress shed = %d, want 1", s.Shed)
	}
}

func TestAdmissionMiddlewareInjectsOutboundGate(t *testing.T) {
	ingress := admission.NewGate(2, 0)
	outbound := admission.NewGate(2, 0)
	handler := admissionMiddleware(ingress, outbound, nil)(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		if admission.FromContext(ctx) != outbound {
			t.Fatal("outbound gate missing from request context")
		}
		release, err := admission.Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire outbound slot: %v", err)
		}
		release()
		return &mcp.CallToolResult{}, nil
	})

	if _, err := handler(context.Background(), "tools/call", &mcp.CallToolRequest{}); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if s := ingress.Stats(); s.Inflight != 0 || s.Admitted != 1 {
		t.Fatalf("ingress stats = %+v", s)
	}
	if s := outbound.Stats(); s.Inflight != 0 {
		t.Fatalf("outbound slot leaked: %+v", s)
	}
}

func TestAdmissionMiddlewareSkipsOtherMethods(t *testing.T) {
	ingress := admission.NewGate(1, 0)
	held, err := ingress.Acquire(context.Background())
	if err != nil {
		t.Fatalf("hold slot: %v", err)
	}
	defer held()

	called := false
	handler := admissionMiddleware(ingress, nil, nil)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return &mcp.CallToolResult{}, nil
	})
	if _, err := handler(context.Background(), "initialize", nil); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !called {
		t.Fatal("non tools/call methods must bypass admission")
	}
}
