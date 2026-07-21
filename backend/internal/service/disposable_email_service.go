package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

const (
	// 开源临时邮箱黑名单 GitHub 地址
	disposableEmailDomainsURL = "https://raw.githubusercontent.com/disposable/disposable-email-domains/master/domains.txt"

	// TTL for the blacklist cache (7 days)
	disposableDomainsCacheTTL = 7 * 24 * time.Hour

	// 刷新黑名单时分布式锁的过期时间（避免死锁）
	disposableRefreshLockTTL = 10 * time.Minute
)

// DisposableEmailCache 临时邮箱黑名单的存储操作，由 repository 层基于 Redis 实现。
type DisposableEmailCache interface {
	// ReplaceDomains 用新的域名集合替换黑名单并设置过期时间
	ReplaceDomains(ctx context.Context, domains []string, ttl time.Duration) error
	// IsDisposableDomain 判断域名是否在黑名单中
	IsDisposableDomain(ctx context.Context, domain string) (bool, error)
	// DomainCount 返回黑名单中的域名数量
	DomainCount(ctx context.Context) (int64, error)
	// DomainsTTL 返回黑名单的剩余过期时间
	DomainsTTL(ctx context.Context) (time.Duration, error)
	// AcquireRefreshLock 尝试获取刷新黑名单的分布式锁
	AcquireRefreshLock(ctx context.Context, ttl time.Duration) (bool, error)
	// ReleaseRefreshLock 释放刷新锁
	ReleaseRefreshLock(ctx context.Context)
}

// DisposableEmailService 临时邮箱检测服务
type DisposableEmailService struct {
	cache DisposableEmailCache
}

// NewDisposableEmailService 创建临时邮箱检测服务
func NewDisposableEmailService(cache DisposableEmailCache) *DisposableEmailService {
	return &DisposableEmailService{
		cache: cache,
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
	defer func() { _ = resp.Body.Close() }()

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

	if err := s.cache.ReplaceDomains(ctx, domains, disposableDomainsCacheTTL); err != nil {
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

	// 检查黑名单中是否存在该域名
	exists, err := s.cache.IsDisposableDomain(ctx, domain)
	if err != nil {
		// 如果 Redis 出错，fail-open（不阻止注册）
		return false, nil
	}

	return exists, nil
}

// GetBlacklistCount 获取黑名单中的域名数量
func (s *DisposableEmailService) GetBlacklistCount(ctx context.Context) (int64, error) {
	return s.cache.DomainCount(ctx)
}

// GetBlacklistTTL 获取黑名单的剩余过期时间
func (s *DisposableEmailService) GetBlacklistTTL(ctx context.Context) (time.Duration, error) {
	return s.cache.DomainsTTL(ctx)
}

// StartBackgroundRefresh 启动后台任务：启动时加载黑名单，并每天刷新一次。
// 使用 Redis 分布式锁避免多实例重复下载。
func (s *DisposableEmailService) StartBackgroundRefresh() {
	if s == nil || s.cache == nil {
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

	// 尝试获取分布式锁（避免多实例同时下载）
	acquired, err := s.cache.AcquireRefreshLock(ctx, disposableRefreshLockTTL)
	if err != nil || !acquired {
		// 未获取到锁，其他实例正在刷新
		return
	}
	defer s.cache.ReleaseRefreshLock(ctx)

	count, err := s.LoadBlacklistToRedis(ctx)
	if err != nil {
		logger.LegacyPrintf("service.disposable_email", "[DisposableEmail] Failed to load blacklist: %v", err)
		return
	}
	logger.LegacyPrintf("service.disposable_email", "[DisposableEmail] Loaded %d disposable email domains to Redis", count)
}
