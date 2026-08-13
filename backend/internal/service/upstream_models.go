package service

import "context"

// FetchUpstreamSupportedModels preserves the admin sync API while delegating
// all provider-specific discovery to the shared live discoverer.
func (s *AccountTestService) FetchUpstreamSupportedModels(ctx context.Context, account *Account) ([]string, error) {
	if s == nil || s.modelDiscoverer == nil {
		return nil, newUpstreamModelSyncConfigError("Account test service is not configured", nil)
	}
	return s.modelDiscoverer.Discover(ctx, account)
}
