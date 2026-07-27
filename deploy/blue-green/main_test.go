package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testEnvironment struct {
	root       string
	nginxDir   string
	snippetDir string
	stateDir   string
	flagsDir   string
	logFile    string
	certFile   string
	keyFile    string
	stdout     *bytes.Buffer
	stderr     *bytes.Buffer
	app        *app
}

func newTestEnvironment(t *testing.T) *testEnvironment {
	t.Helper()
	temp := t.TempDir()
	binDir := filepath.Join(temp, "bin")
	stateDir := filepath.Join(temp, "fake-state")
	flagsDir := filepath.Join(temp, "fake-flags")
	for _, dir := range []string{binDir, stateDir, flagsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	logFile := filepath.Join(temp, "commands.log")
	writeExecutable(t, filepath.Join(binDir, "docker"), `#!/bin/bash
set -eu
printf 'docker %s\n' "$*" >> "$BG_TEST_LOG"
if [ "${1:-}" = compose ]; then
  shift
  project=""
  while [ "$#" -gt 0 ]; do
    case "$1" in
      -p) project="$2"; shift 2 ;;
      --project-directory|-f) shift 2 ;;
      *) break ;;
    esac
  done
  command="${1:-}"
  case "$command" in
    version) exit 0 ;;
    up) touch "$BG_TEST_STATE/running-$project"; exit 0 ;;
    ps)
      [ ! -f "$BG_TEST_STATE/running-$project" ] || printf 'cid-%s\n' "$project"
      exit 0 ;;
    down) rm -f "$BG_TEST_STATE/running-$project"; exit 0 ;;
    logs) printf 'fake logs for %s\n' "$project"; exit 0 ;;
  esac
  exit 0
fi
case "${1:-}" in
  info)
    [ ! -f "$BG_TEST_FLAGS/docker-info-fail" ]
    ;;
  network)
    name="${3:-}"
    case "${2:-}" in
      inspect) [ -f "$BG_TEST_STATE/network-$name" ] ;;
      create) touch "$BG_TEST_STATE/network-$name" ;;
    esac
    ;;
  ps)
    project=""
    while [ "$#" -gt 0 ]; do
      if [ "$1" = --filter ]; then project="${2##*=}"; shift 2; else shift; fi
    done
    [ ! -f "$BG_TEST_STATE/running-$project" ] || printf 'Up 1 minute (fake/image:latest)\n'
    ;;
esac
`)
	writeExecutable(t, filepath.Join(binDir, "nginx"), `#!/bin/bash
set -eu
printf 'nginx %s\n' "$*" >> "$BG_TEST_LOG"
if [ "${1:-}" = -t ] && [ -f "$BG_TEST_FLAGS/nginx-test-fail" ]; then exit 1; fi
if [ "${1:-}" = -T ]; then
  [ -f "$BG_TEST_FLAGS/nginx-include-missing" ] || printf '# blue-green-managed-http-config\n'
  printf 'worker_shutdown_timeout 1200s;\n'
fi
`)
	writeExecutable(t, filepath.Join(binDir, "systemd-run"), `#!/bin/bash
set -eu
printf 'systemd-run %s\n' "$*" >> "$BG_TEST_LOG"
unit=""
for value in "$@"; do case "$value" in --unit=*) unit="${value#--unit=}" ;; esac; done
touch "$BG_TEST_STATE/timer-$unit"
`)
	writeExecutable(t, filepath.Join(binDir, "systemctl"), `#!/bin/bash
set -eu
printf 'systemctl %s\n' "$*" >> "$BG_TEST_LOG"
case "${1:-}" in
  stop)
    unit="${2%.timer}"; unit="${unit%.service}"
    if [ -f "$BG_TEST_STATE/timer-$unit" ]; then
      rm -f "$BG_TEST_STATE/timer-$unit"
      exit 0
    fi
    exit 1
    ;;
  list-timers)
    for file in "$BG_TEST_STATE"/timer-*; do
      [ ! -f "$file" ] || printf '%s.timer fake-pending\n' "${file##*/timer-}"
    done
    ;;
esac
`)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BG_TEST_STATE", stateDir)
	t.Setenv("BG_TEST_FLAGS", flagsDir)
	t.Setenv("BG_TEST_LOG", logFile)
	t.Setenv("BGDEPLOY_CONFIG", "")
	t.Setenv("BGDEPLOY_ROOT", "")
	t.Setenv("BGDEPLOY_NGINX_DIR", "")
	t.Setenv("BGDEPLOY_NGINX_SNIPPET_DIR", "")

	root := filepath.Join(temp, "srv")
	nginxDir := filepath.Join(temp, "nginx", "blue-green")
	snippetDir := filepath.Join(temp, "nginx", "snippets")
	certFile := filepath.Join(temp, "tls", "cert.pem")
	keyFile := filepath.Join(temp, "tls", "key.pem")
	if err := os.MkdirAll(filepath.Dir(certFile), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{certFile, keyFile} {
		if err := os.WriteFile(file, []byte("test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	testApp, err := newApp(root, nginxDir, snippetDir, stdout, stderr)
	if err != nil {
		t.Fatal(err)
	}
	testApp.euid = func() int { return 0 }
	testApp.now = func() time.Time { return time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC) }
	return &testEnvironment{
		root: root, nginxDir: nginxDir, snippetDir: snippetDir,
		stateDir: stateDir, flagsDir: flagsDir, logFile: logFile,
		certFile: certFile, keyFile: keyFile,
		stdout: stdout, stderr: stderr, app: testApp,
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func (environment *testEnvironment) writeSites(t *testing.T, portBase int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(environment.root, "registry", "envs"), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`defaults:
  image_repo: registry.example.com/application
  bind_host: 127.0.0.1
  drain_seconds: 60
  health_timeout_seconds: 2
  health_interval_seconds: 1
  tz: UTC
stacks:
  - slug: api-test
    domain: test.example.com
    port_base: %d
    image_tag: v1.0.0
    tls:
      cert: %s
      key: %s
`, portBase, environment.certFile, environment.keyFile)
	if err := os.WriteFile(filepath.Join(environment.root, "registry", "sites.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapDoesNotOverwriteConfiguration(t *testing.T) {
	environment := newTestEnvironment(t)
	if err := environment.app.bootstrap(); err != nil {
		t.Fatal(err)
	}
	sitesPath := filepath.Join(environment.root, "registry", "sites.yaml")
	if err := os.WriteFile(sitesPath, []byte("custom: true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := environment.app.bootstrap(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(sitesPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "custom: true\n" {
		t.Fatalf("bootstrap overwrote sites.yaml: %q", content)
	}
	for _, path := range []string{
		filepath.Join(environment.root, "registry", "envs"),
		filepath.Join(environment.root, "stacks"),
		filepath.Join(environment.root, "env.example"),
		filepath.Join(environment.root, "runtime.yaml"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("bootstrap did not create %s: %v", path, err)
		}
	}
}

func TestRuntimeConfigurationPrecedence(t *testing.T) {
	temp := t.TempDir()
	configPath := filepath.Join(temp, "runtime.yaml")
	configRoot := filepath.Join(temp, "from-config")
	configNginx := filepath.Join(temp, "nginx-from-config")
	configSnippet := filepath.Join(temp, "snippet-from-config")
	content := fmt.Sprintf("root: %s\nnginx_dir: %s\nnginx_snippet_dir: %s\n", configRoot, configNginx, configSnippet)
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	environmentRoot := filepath.Join(temp, "from-environment")
	flagRoot := filepath.Join(temp, "from-flag")
	environmentSnippet := filepath.Join(temp, "snippet-from-environment")
	t.Setenv("BGDEPLOY_ROOT", environmentRoot)
	t.Setenv("BGDEPLOY_NGINX_SNIPPET_DIR", environmentSnippet)

	configured, err := newAppWithConfig(configPath, flagRoot, "", "", io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if configured.root != flagRoot {
		t.Fatalf("root = %s, want flag value %s", configured.root, flagRoot)
	}
	if configured.nginxDir != configNginx {
		t.Fatalf("nginxDir = %s, want config value %s", configured.nginxDir, configNginx)
	}
	if configured.nginxSnippetDir != environmentSnippet {
		t.Fatalf("nginxSnippetDir = %s, want environment value %s", configured.nginxSnippetDir, environmentSnippet)
	}
}

func TestWriteCommandsRequireRoot(t *testing.T) {
	environment := newTestEnvironment(t)
	environment.app.euid = func() int { return 1000 }
	err := environment.app.bootstrap()
	if err == nil || !strings.Contains(err.Error(), "权限不足") {
		t.Fatalf("bootstrap error = %v, want permission failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(environment.root, "registry")); !os.IsNotExist(statErr) {
		t.Fatalf("bootstrap mutated files without root: %v", statErr)
	}
}

func TestRenderAndInitUseEmbeddedAssets(t *testing.T) {
	environment := newTestEnvironment(t)
	environment.writeSites(t, 28080)
	envPath := filepath.Join(environment.root, "registry", "envs", "api-test.env")
	if err := os.WriteFile(envPath, []byte("POSTGRES_PASSWORD=test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := environment.app.render(context.Background()); err != nil {
		t.Fatal(err)
	}
	expected := []string{
		filepath.Join(environment.root, "stacks", "api-test", "compose.data.yml"),
		filepath.Join(environment.root, "stacks", "api-test", "compose.app.yml"),
		filepath.Join(environment.nginxDir, "http.conf"),
		filepath.Join(environment.nginxDir, "sites", "api-test.conf"),
		filepath.Join(environment.nginxDir, "upstreams", "api-test.conf"),
		filepath.Join(environment.snippetDir, "blue-green-proxy.conf"),
	}
	for _, path := range expected {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("missing rendered file %s: %v", path, err)
		}
	}

	upstreamPath := environment.app.upstreamPath("api-test")
	if err := os.WriteFile(upstreamPath, []byte("upstream api_test { server 127.0.0.1:28081; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := environment.app.render(context.Background()); err != nil {
		t.Fatal(err)
	}
	upstream, _ := os.ReadFile(upstreamPath)
	if !bytes.Contains(upstream, []byte(":28081")) {
		t.Fatalf("render overwrote active upstream: %s", upstream)
	}

	if err := environment.app.initStack(context.Background(), "api-test"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("env mode = %o, want 600", info.Mode().Perm())
	}
	link := filepath.Join(environment.root, "stacks", "api-test", ".env")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatal(err)
	}
	if target != envPath {
		t.Fatalf(".env target = %s, want %s", target, envPath)
	}
	if _, err := os.Stat(filepath.Join(environment.stateDir, "network-api-test-net")); err != nil {
		t.Fatalf("network was not created: %v", err)
	}
}

func TestInitChecksDependenciesBeforeMutation(t *testing.T) {
	environment := newTestEnvironment(t)
	environment.writeSites(t, 28100)
	if err := environment.app.render(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(environment.flagsDir, "nginx-include-missing"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := environment.app.initStack(context.Background(), "api-test")
	if err == nil || !strings.Contains(err.Error(), "nginx 未加载") {
		t.Fatalf("init error = %v, want nginx include failure", err)
	}
	if _, statErr := os.Stat(filepath.Join(environment.root, "stacks", "api-test", "data")); !os.IsNotExist(statErr) {
		t.Fatalf("init mutated stack before dependency check: %v", statErr)
	}
}

func TestDeployRollbackAndSafetyGates(t *testing.T) {
	blueListener, greenListener, portBase := listenOnConsecutivePorts(t)
	var blueBody atomic.Value
	var greenBody atomic.Value
	blueBody.Store(`{"status":"ok","version":"1.0.0","slot":"blue"}`)
	greenBody.Store(`{"status":"ok","version":"1.1.0","slot":"green"}`)
	serveHealth(t, blueListener, &blueBody)
	serveHealth(t, greenListener, &greenBody)

	environment := newTestEnvironment(t)
	environment.writeSites(t, portBase)
	envPath := filepath.Join(environment.root, "registry", "envs", "api-test.env")
	if err := os.WriteFile(envPath, []byte("POSTGRES_PASSWORD=test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := environment.app.render(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := environment.app.deploy(context.Background(), "api-test", "v1.0.0"); err != nil {
		t.Fatalf("first deploy: %v\nstdout=%s\nstderr=%s", err, environment.stdout, environment.stderr)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase)
	state, err := environment.app.readState("api-test")
	if err != nil || state.Slot != slotBlue || state.PrevSlot != "" {
		t.Fatalf("first state = %+v, err=%v", state, err)
	}

	if err := environment.app.deploy(context.Background(), "api-test", "v1.1.0"); err != nil {
		t.Fatalf("blue-green deploy: %v", err)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase+1)
	state, _ = environment.app.readState("api-test")
	if state.Slot != slotGreen || state.PrevSlot != slotBlue || state.PrevTag != "v1.0.0" {
		t.Fatalf("second state = %+v", state)
	}

	if err := environment.app.rollback(context.Background(), "api-test"); err != nil {
		t.Fatalf("fast rollback: %v", err)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase)

	greenBody.Store(`{"status":"ok","version":"1.2.0"}`)
	err = environment.app.deploy(context.Background(), "api-test", "v1.2.0")
	if err == nil || !strings.Contains(err.Error(), "未返回 slot") {
		t.Fatalf("deploy error = %v, want missing slot failure", err)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase)

	greenBody.Store(`{"status":"ok","version":"1.2.0","slot":"green"}`)
	if err := os.WriteFile(filepath.Join(environment.flagsDir, "nginx-test-fail"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err = environment.app.deploy(context.Background(), "api-test", "v1.2.0")
	if err == nil || !strings.Contains(err.Error(), "nginx -t 校验失败") {
		t.Fatalf("deploy error = %v, want nginx test failure", err)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase)
	if _, err := os.Stat(environment.app.upstreamPath("api-test") + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("upstream backup was not cleaned up: %v", err)
	}
	if err := os.Remove(filepath.Join(environment.flagsDir, "nginx-test-fail")); err != nil {
		t.Fatal(err)
	}

	err = environment.app.teardown(context.Background(), "api-test", slotBlue)
	if err == nil || !strings.Contains(err.Error(), "拒绝回收") {
		t.Fatalf("teardown current slot error = %v", err)
	}
	if err := environment.app.teardown(context.Background(), "api-test", slotGreen); err != nil {
		t.Fatalf("teardown inactive slot: %v", err)
	}
	greenBody.Store(`{"status":"ok","version":"1.1.0","slot":"green"}`)
	if err := environment.app.rollback(context.Background(), "api-test"); err != nil {
		t.Fatalf("fallback rollback: %v", err)
	}
	assertUpstreamPort(t, environment.app.upstreamPath("api-test"), portBase+1)
}

func listenOnConsecutivePorts(t *testing.T) (net.Listener, net.Listener, int) {
	t.Helper()
	for port := 30000; port < 60000; port++ {
		first, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		second, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port+1))
		if err == nil {
			t.Cleanup(func() {
				_ = first.Close()
				_ = second.Close()
			})
			return first, second, port
		}
		_ = first.Close()
	}
	t.Fatal("could not reserve consecutive ports")
	return nil, nil, 0
}

func serveHealth(t *testing.T, listener net.Listener, body *atomic.Value) {
	t.Helper()
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(writer, body.Load().(string))
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
	})
}

func assertUpstreamPort(t *testing.T, path string, port int) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), fmt.Sprintf("server 127.0.0.1:%d", port)) {
		t.Fatalf("upstream = %s, want port %d", content, port)
	}
}
