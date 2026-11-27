package admission

import "context"

type diagnosticKey struct{}

// WithDiagnostic marks a manually requested, isolated, single-attempt operation.
func WithDiagnostic(ctx context.Context) context.Context {
	return context.WithValue(ctx, diagnosticKey{}, true)
}

func IsDiagnostic(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	active, _ := ctx.Value(diagnosticKey{}).(bool)
	return active
}
