package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	// 开源临时邮箱黑名单 GitHub 地址
	disposableEmailDomainsURL = "https://raw.githubusercontent.com/disposable/disposable-email-domains/master/domains.txt"

	// Redis key for disposable email domains
	redisKeyDisposableDomains = "disposable_email:domains"

	// TTL for the blacklist in Redis (7 days)
	disposableDomainsRedisTTL = 7 * 24 * time.Hour
)

// DisposableEmailService 临时邮箱检测服务
type DisposableEmailService struct {
	redis *redis.Client
}

// NewDisposableEmailService 创建临时邮箱检测服务
func NewDisposableEmailService(redisClient *redis.Client) *DisposableEmailService {
	return &DisposableEmailService{
		redis: redisClient,
	}
}

// LoadBlacklistToRedis 从 GitHub 加载临时邮箱黑名单到 Redis
// 返回加载的域名数量和错误
func (s *DisposableEmailService) LoadBlacklistToRedis(ctx context.Context) (int, error) {
	// 下载黑名单文件
	resp, err := http.Get(disposableEmailDomainsURL)
	if err != nil {
		return 0, fmt.Errorf("failed to download blacklist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("failed to download blacklist: HTTP %d", resp.StatusCode)
	}

	// 读取文件内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read blacklist: %w", err)
	}

	// 解析域名列表（每行一个域名）
	lines := strings.Split(string(body), "\n")
	domains := make([]string, 0, len(lines))

	for _, line := range lines {
		domain := strings.TrimSpace(line)
		// 跳过空行和注释
		if domain == "" || strings.HasPrefix(domain, "#") {
			continue
		}
		domains = append(domains, strings.ToLower(domain))
	}

	if len(domains) == 0 {
		return 0, fmt.Errorf("no domains found in blacklist")
	}

	// 使用 Redis Pipeline 批量写入
	pipe := s.redis.Pipeline()

	// 删除旧数据
	pipe.Del(ctx, redisKeyDisposableDomains)

	// 批量添加到 Redis Set
	// 分批处理，每次1000个，避免单次命令过大
	batchSize := 1000
	for i := 0; i < len(domains); i += batchSize {
		end := i + batchSize
		if end > len(domains) {
			end = len(domains)
		}
		batch := domains[i:end]

		// 将 []string 转换为 []interface{}
		members := make([]interface{}, len(batch))
		for j, domain := range batch {
			members[j] = domain
		}

		pipe.SAdd(ctx, redisKeyDisposableDomains, members...)
	}

	// 设置过期时间
	pipe.Expire(ctx, redisKeyDisposableDomains, disposableDomainsRedisTTL)

	// 执行 Pipeline
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("failed to save blacklist to Redis: %w", err)
	}

	return len(domains), nil
}

// IsDisposableEmail 检查邮箱是否为临时邮箱
func (s *DisposableEmailService) IsDisposableEmail(ctx context.Context, email string) (bool, error) {
	// 提取域名
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return false, fmt.Errorf("invalid email format")
	}

	domain := strings.ToLower(strings.TrimSpace(parts[1]))

	// 检查 Redis Set 中是否存在该域名
	exists, err := s.redis.SIsMember(ctx, redisKeyDisposableDomains, domain).Result()
	if err != nil {
		// 如果 Redis 出错，fail-open（不阻止注册）
		return false, nil
	}

	return exists, nil
}

// GetBlacklistCount 获取黑名单中的域名数量
func (s *DisposableEmailService) GetBlacklistCount(ctx context.Context) (int64, error) {
	count, err := s.redis.SCard(ctx, redisKeyDisposableDomains).Result()
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetBlacklistTTL 获取黑名单的剩余过期时间
func (s *DisposableEmailService) GetBlacklistTTL(ctx context.Context) (time.Duration, error) {
	ttl, err := s.redis.TTL(ctx, redisKeyDisposableDomains).Result()
	if err != nil {
		return 0, err
	}
	return ttl, nil
}

// StartBackgroundRefresh 启动后台任务：启动时加载黑名单，并每天刷新一次。
// 使用 Redis 分布式锁避免多实例重复下载。
func (s *DisposableEmailService) StartBackgroundRefresh() {
	if s == nil || s.redis == nil {
		return
	}
	go func() {
		// 启动时延迟几秒，避免与其他启动任务竞争
		time.Sleep(5 * time.Second)
		s.refreshIfNeeded()

		// 每天刷新一次
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			s.refreshIfNeeded()
		}
	}()
}

// refreshIfNeeded 在需要时刷新黑名单（黑名单不存在或即将过期时）。
// 使用 Redis SETNX 锁避免多实例同时下载。
func (s *DisposableEmailService) refreshIfNeeded() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 检查黑名单是否已存在且未即将过期
	ttl, err := s.GetBlacklistTTL(ctx)
	if err == nil && ttl > 24*time.Hour {
		// 黑名单充足，无需刷新
		return
	}

	// 尝试获取分布式锁（10分钟过期，避免死锁）
	lockKey := redisKeyDisposableDomains + ":refresh_lock"
	acquired, err := s.redis.SetNX(ctx, lockKey, "1", 10*time.Minute).Result()
	if err != nil || !acquired {
		// 未获取到锁，其他实例正在刷新
		return
	}
	defer s.redis.Del(ctx, lockKey)

	count, err := s.LoadBlacklistToRedis(ctx)
	if err != nil {
		logger.LegacyPrintf("service.disposable_email", "[DisposableEmail] Failed to load blacklist: %v", err)
		return
	}
	logger.LegacyPrintf("service.disposable_email", "[DisposableEmail] Loaded %d disposable email domains to Redis", count)
}
