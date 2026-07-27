package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var workerShutdownPattern = regexp.MustCompile(`(?m)^\s*worker_shutdown_timeout\s+[0-9]+[smh]?;`)

func commandAvailable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func envList(extra map[string]string) []string {
	result := os.Environ()
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+extra[key])
	}
	return result
}

func (a *app) runAttached(ctx context.Context, extraEnv map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = envList(extraEnv)
	cmd.Stdout = a.stdout
	cmd.Stderr = a.stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("执行 %s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (a *app) runCapture(ctx context.Context, extraEnv map[string]string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = envList(extraEnv)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		if message != "" {
			return stdout.String(), fmt.Errorf("执行 %s %s: %w: %s", name, strings.Join(args, " "), err, message)
		}
		return stdout.String(), fmt.Errorf("执行 %s %s: %w", name, strings.Join(args, " "), err)
	}
	return stdout.String(), nil
}

func (a *app) requireCommands(names ...string) error {
	var missing []string
	for _, name := range names {
		if !commandAvailable(name) {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少依赖: %s（安装后重试）", strings.Join(missing, ", "))
	}
	return nil
}

func (a *app) checkDocker(ctx context.Context) error {
	if err := a.requireCommands("docker"); err != nil {
		return err
	}
	if _, err := a.runCapture(ctx, nil, "docker", "compose", "version"); err != nil {
		return errors.New("docker compose v2 不可用（请安装 Docker Compose v2）")
	}
	if _, err := a.runCapture(ctx, nil, "docker", "info"); err != nil {
		return errors.New("无法访问 Docker daemon（请确认 Docker 已启动且当前用户有权限）")
	}
	return nil
}

func (a *app) checkRuntimeDependencies(ctx context.Context) error {
	if err := a.checkRoot(); err != nil {
		return err
	}
	if err := a.requireCommands("nginx"); err != nil {
		return err
	}
	return a.checkDocker(ctx)
}

func (a *app) checkDependencies(ctx context.Context) error {
	if err := a.checkRuntimeDependencies(ctx); err != nil {
		return err
	}
	a.log("检查 nginx 配置接入...")
	if _, err := a.runCapture(ctx, nil, "nginx", "-t"); err != nil {
		return errors.New("nginx -t 未通过（请先修复 nginx 配置）")
	}
	dump, err := a.runCapture(ctx, nil, "nginx", "-T")
	if err != nil {
		return errors.New("无法读取 nginx 完整配置（nginx -T 失败）")
	}
	if !strings.Contains(dump, "blue-green-managed-http-config") {
		return fmt.Errorf("nginx 未加载 %s；请在 http {} 内添加 include %s/*.conf;", filepath.Join(a.nginxDir, "http.conf"), a.nginxDir)
	}
	if !workerShutdownPattern.MatchString(dump) {
		return errors.New("nginx 缺少 worker_shutdown_timeout；请在 main 上下文设置 worker_shutdown_timeout 1200s;")
	}
	if !commandAvailable("systemd-run") {
		a.warn("systemd-run 不可用，排空回收将使用 bgdeploy 后台子进程")
	}
	a.log("依赖检查通过: Docker/Compose、nginx 与蓝绿配置均可用")
	return nil
}
