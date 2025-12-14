package httpapi

import (
	"context"
)

type principalKeyType struct{}

var principalKey = principalKeyType{}

const (
	PermCanSearch = "can_search"
	PermCanUpload = "can_upload"
	PermCanUpdate = "can_update"
	PermCanDelete = "can_delete"
)

type Principal struct {
	ID          string
	Permissions map[string]struct{}
	Source      string
}

func newPrincipalFromAPIKey(key *APIKey) *Principal {
	perms := make(map[string]struct{}, len(key.Permissions))
	for _, p := range key.Permissions {
		perms[p] = struct{}{}
	}
	return &Principal{
		ID:          key.ID,
		Permissions: perms,
		Source:      "apikey",
	}
}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey, p)
}

func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	if ctx == nil {
		return nil, false
	}
	p, ok := ctx.Value(principalKey).(*Principal)
	return p, ok && p != nil
}

func (p *Principal) HasPermission(perm string) bool {
	if p == nil {
		return false
	}
	_, ok := p.Permissions[perm]
	return ok
}
