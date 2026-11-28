package arm

import "context"

// Principal identifies the caller for authorization and auditing.
type Principal struct {
	ObjectID string
	TenantID string
	AppID    string
}

type principalKey struct{}

// WithPrincipal returns a copy of ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// PrincipalFrom returns the Principal stored on ctx, or the zero Principal if none is set.
func PrincipalFrom(ctx context.Context) Principal {
	if ctx == nil {
		return Principal{}
	}
	p, _ := ctx.Value(principalKey{}).(Principal)
	return p
}

// Authorizer checks whether p may perform action on scope.
// Return an *azerr.Error (e.g. 403 AuthorizationFailed) to deny; any other
// non-nil error fails closed as 500 InternalServerError.
type Authorizer interface {
	Authorize(ctx context.Context, p Principal, action string, scope ID) error
}

// Admitter evaluates a proposed PUT or PATCH document (r.Doc) before hooks and
// persistence. Return an *azerr.Error (e.g. 403 RequestDisallowedByPolicy) to
// deny; any other non-nil error fails closed as 500 InternalServerError.
type Admitter interface {
	Admit(ctx context.Context, r *Request) error
}
