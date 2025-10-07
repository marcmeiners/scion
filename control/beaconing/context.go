package beaconing

import (
	"context"

	"github.com/scionproto/scion/pkg/addr"
)

type localIACtxKey struct{}

// ContextWithLocalIA annotates ctx with the local IA used for segment operations.
func ContextWithLocalIA(ctx context.Context, ia addr.IA) context.Context {
	return context.WithValue(ctx, localIACtxKey{}, ia)
}

// LocalIAFromContext extracts the local IA from ctx if present.
func LocalIAFromContext(ctx context.Context) (addr.IA, bool) {
	if ctx == nil {
		return 0, false
	}
	ia, ok := ctx.Value(localIACtxKey{}).(addr.IA)
	return ia, ok
}
