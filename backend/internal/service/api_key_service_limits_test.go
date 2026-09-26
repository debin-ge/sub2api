//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// limitsUserRepoStub 只提供 GetByID；其余方法未实现（调用即 panic，暴露非预期路径）。
type limitsUserRepoStub struct {
	UserRepository
}

func (s *limitsUserRepoStub) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Status: StatusActive}, nil
}

// limitsAPIKeyRepoStub 记录 Create 与 CountByUserID 调用，其余方法未实现。
type limitsAPIKeyRepoStub struct {
	APIKeyRepository
	count      int64
	countErr   error
	countCalls int
	created    []*APIKey
	existing   *APIKey
}

func (s *limitsAPIKeyRepoStub) CountByUserID(_ context.Context, _ int64) (int64, error) {
	s.countCalls++
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.count, nil
}

func (s *limitsAPIKeyRepoStub) Create(_ context.Context, key *APIKey) error {
	key.ID = int64(len(s.created) + 1)
	s.created = append(s.created, key)
	return nil
}

func (s *limitsAPIKeyRepoStub) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if s.existing != nil && s.existing.ID == id {
		clone := *s.existing
		return &clone, nil
	}
	return nil, ErrAPIKeyNotFound
}

func newLimitsAPIKeyService(repo *limitsAPIKeyRepoStub, security config.SecurityConfig) *APIKeyService {
	return &APIKeyService{
		apiKeyRepo: repo,
		userRepo:   &limitsUserRepoStub{},
		cfg:        &config.Config{Security: security},
	}
}

func TestAPIKeyService_Create_APIKeyLimitReached(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{count: 3}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeysPerUser: 3})

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "k"})
	require.ErrorIs(t, err, ErrAPIKeyLimitReached)
	require.Equal(t, "API_KEY_LIMIT_REACHED", infraerrors.Reason(err))
	require.Equal(t, 400, infraerrors.Code(err))
	require.Empty(t, repo.created, "no key may be created once the cap is reached")
	require.Equal(t, 1, repo.countCalls)
}

func TestAPIKeyService_Create_BelowAPIKeyLimitSucceeds(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{count: 2}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeysPerUser: 3})

	key, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "k"})
	require.NoError(t, err)
	require.NotNil(t, key)
	require.Len(t, repo.created, 1)
}

func TestAPIKeyService_Create_APIKeyLimitDisabledSkipsCount(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{count: 1_000_000}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeysPerUser: 0})

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "k"})
	require.NoError(t, err)
	require.Equal(t, 0, repo.countCalls, "limit 0 means unlimited and must not query the count")
}

func TestAPIKeyService_Create_APIKeyLimitCountErrorFailsClosed(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{countErr: errors.New("db down")}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeysPerUser: 3})

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "k"})
	require.Error(t, err)
	require.Empty(t, repo.created)
}

func TestAPIKeyService_Create_IPRulesLimit(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeyIPRules: 2})

	_, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:        "k",
		IPWhitelist: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
	})
	require.ErrorIs(t, err, ErrAPIKeyIPRulesTooMany)
	require.Empty(t, repo.created)

	_, err = svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:        "k",
		IPBlacklist: []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"},
	})
	require.ErrorIs(t, err, ErrAPIKeyIPRulesTooMany)
	require.Empty(t, repo.created)

	// 白名单与黑名单各自独立计数：各两条时允许。
	key, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{
		Name:        "k",
		IPWhitelist: []string{"10.0.0.1", "10.0.0.2"},
		IPBlacklist: []string{"192.168.0.0/24", "172.16.0.1"},
	})
	require.NoError(t, err)
	require.NotNil(t, key)
	require.Len(t, repo.created, 1)
}

func TestAPIKeyService_Update_IPRulesLimit(t *testing.T) {
	repo := &limitsAPIKeyRepoStub{existing: &APIKey{ID: 5, UserID: 7, Key: "sk-existing", Status: StatusActive}}
	svc := newLimitsAPIKeyService(repo, config.SecurityConfig{MaxAPIKeyIPRules: 1})

	tooMany := []string{"10.0.0.1", "10.0.0.2"}
	_, err := svc.Update(context.Background(), 5, 7, UpdateAPIKeyRequest{IPWhitelist: &tooMany})
	require.ErrorIs(t, err, ErrAPIKeyIPRulesTooMany)

	_, err = svc.Update(context.Background(), 5, 7, UpdateAPIKeyRequest{IPBlacklist: &tooMany})
	require.ErrorIs(t, err, ErrAPIKeyIPRulesTooMany)
}

func TestAPIKeyService_IPRulesLimitDisabledAllowsAnyCount(t *testing.T) {
	svc := newLimitsAPIKeyService(&limitsAPIKeyRepoStub{}, config.SecurityConfig{MaxAPIKeyIPRules: 0})
	rules := make([]string, 500)
	for i := range rules {
		rules[i] = "10.0.0.1"
	}
	require.NoError(t, svc.validateIPRuleLimits(rules, rules))
}
