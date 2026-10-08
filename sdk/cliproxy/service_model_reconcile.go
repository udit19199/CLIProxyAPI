package cliproxy

import (
	"context"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// reconcileRegisteredModelStates refreshes the registry state for an auth after its
// models have been (re)registered.
func (s *Service) reconcileRegisteredModelStates(ctx context.Context, auth *coreauth.Auth) {
	if auth == nil || s.coreManager == nil {
		return
	}
	s.coreManager.ReconcileRegistryModelStates(ctx, auth.ID)
}
