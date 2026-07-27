package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type app struct {
	root            string
	registryFile    string
	envsDir         string
	stacksDir       string
	nginxDir        string
	nginxUpstreams  string
	nginxSites      string
	nginxSnippetDir string
	executable      string
	stdout          io.Writer
	stderr          io.Writer
	euid            func() int
	requireRoot     bool
	now             func() time.Time
}

func newApp(root, nginxDir, nginxSnippetDir string, stdout, stderr io.Writer) (*app, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("定位当前可执行文件: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, fmt.Errorf("解析可执行文件路径: %w", err)
	}

	if root == "" {
		root = os.Getenv("S2A_ROOT")
	}
	if root == "" {
		root = filepath.Dir(executable)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析部署根目录: %w", err)
	}

	if nginxDir == "" {
		nginxDir = os.Getenv("S2A_NGINX_DIR")
	}
	if nginxDir == "" {
		nginxDir = "/etc/nginx/sub2api"
	}
	if nginxSnippetDir == "" {
		nginxSnippetDir = os.Getenv("S2A_NGINX_SNIPPET_DIR")
	}
	if nginxSnippetDir == "" {
		nginxSnippetDir = "/etc/nginx/snippets"
	}

	return &app{
		root:            filepath.Clean(root),
		registryFile:    filepath.Join(root, "registry", "sites.yaml"),
		envsDir:         filepath.Join(root, "registry", "envs"),
		stacksDir:       filepath.Join(root, "stacks"),
		nginxDir:        filepath.Clean(nginxDir),
		nginxUpstreams:  filepath.Join(nginxDir, "upstreams"),
		nginxSites:      filepath.Join(nginxDir, "sites"),
		nginxSnippetDir: filepath.Clean(nginxSnippetDir),
		executable:      executable,
		stdout:          stdout,
		stderr:          stderr,
		euid:            os.Geteuid,
		requireRoot:     true,
		now:             time.Now,
	}, nil
}

func (a *app) log(format string, args ...any) {
	fmt.Fprintf(a.stdout, "[%s] %s\n", a.now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.stderr, "[%s] WARN: %s\n", a.now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

func (a *app) checkRoot() error {
	if !a.requireRoot || a.euid() == 0 {
		return nil
	}
	return fmt.Errorf("权限不足: 请进入 %s 后使用 sudo ./s2a 执行", a.root)
}

func (a *app) stackDir(slug string) string {
	return filepath.Join(a.stacksDir, slug)
}

func (a *app) upstreamPath(slug string) string {
	return filepath.Join(a.nginxUpstreams, slug+".conf")
}

func (a *app) statePath(slug string) string {
	return filepath.Join(a.stackDir(slug), "STATE")
}

func (a *app) drainPIDFile(slug, slot string) string {
	return filepath.Join(a.stackDir(slug), "drain-"+slot+".pid")
}
