package mcpserver

import "context"

type authIdentityCtxKey struct{}

// WithAuthIdentity returns a copy of ctx carrying the resolved identity. The
// HTTP transport's auth middleware sets this after resolving a bearer token so
// downstream shell commands can read the caller's identity from the
// AUTH_SUBJECT / AUTH_NAME / AUTH_CLAIM_* environment variables.
func WithAuthIdentity(ctx context.Context, identity *AuthIdentityInfo) context.Context {
	return context.WithValue(ctx, authIdentityCtxKey{}, identity)
}

// AuthIdentityFromContext returns the resolved identity stored on ctx, if any.
func AuthIdentityFromContext(ctx context.Context) (*AuthIdentityInfo, bool) {
	identity, ok := ctx.Value(authIdentityCtxKey{}).(*AuthIdentityInfo)
	if !ok || identity == nil {
		return nil, false
	}
	return identity, true
}
