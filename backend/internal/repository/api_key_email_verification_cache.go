package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	apiKeyEmailCodePrefix  = "api_key_email_verify:code:"
	apiKeyEmailRatePrefix  = "api_key_email_verify:rate:"
	apiKeyEmailProofPrefix = "api_key_email_verify:proof:"
)

type apiKeyEmailVerificationCache struct{ rdb *redis.Client }

func NewAPIKeyEmailVerificationCache(rdb *redis.Client) service.APIKeyEmailVerificationCache {
	return &apiKeyEmailVerificationCache{rdb: rdb}
}

func apiKeyEmailCodeKey(userID int64, email string) string {
	return fmt.Sprintf("%s%d:%s", apiKeyEmailCodePrefix, userID, strings.ToLower(strings.TrimSpace(email)))
}

func (c *apiKeyEmailVerificationCache) GetCode(ctx context.Context, userID int64, email string) (*service.VerificationCodeData, error) {
	value, err := c.rdb.Get(ctx, apiKeyEmailCodeKey(userID, email)).Bytes()
	if err != nil {
		return nil, err
	}
	var data service.VerificationCodeData
	if err := json.Unmarshal(value, &data); err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *apiKeyEmailVerificationCache) SetCode(ctx context.Context, userID int64, email string, data *service.VerificationCodeData, ttl time.Duration) error {
	value, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, apiKeyEmailCodeKey(userID, email), value, ttl).Err()
}

func (c *apiKeyEmailVerificationCache) DeleteCode(ctx context.Context, userID int64, email string) error {
	return c.rdb.Del(ctx, apiKeyEmailCodeKey(userID, email)).Err()
}

func (c *apiKeyEmailVerificationCache) IncrementSendCount(ctx context.Context, userID int64, ttl time.Duration) (int64, error) {
	key := fmt.Sprintf("%s%d", apiKeyEmailRatePrefix, userID)
	return c.rdb.Eval(ctx, `
		local count = redis.call('INCR', KEYS[1])
		if count == 1 then
			redis.call('PEXPIRE', KEYS[1], ARGV[1])
		end
		return count
	`, []string{key}, ttl.Milliseconds()).Int64()
}

func (c *apiKeyEmailVerificationCache) SetProof(ctx context.Context, token string, proof service.APIKeyEmailVerificationProof, ttl time.Duration) error {
	value, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, apiKeyEmailProofPrefix+token, value, ttl).Err()
}

func (c *apiKeyEmailVerificationCache) GetProof(ctx context.Context, token string) (*service.APIKeyEmailVerificationProof, error) {
	value, err := c.rdb.Get(ctx, apiKeyEmailProofPrefix+token).Bytes()
	if err != nil {
		return nil, err
	}
	var proof service.APIKeyEmailVerificationProof
	if err := json.Unmarshal(value, &proof); err != nil {
		return nil, err
	}
	return &proof, nil
}
