package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func slugUnderscore(slug string) string {
	return strings.ReplaceAll(slug, "-", "_")
}

func (a *app) render(ctx context.Context) error {
	if err := a.checkRoot(); err != nil {
		return err
	}
	if err := a.requireCommands("nginx"); err != nil {
		return err
	}

	sites, err := a.loadSites()
	if err != nil {
		return err
	}
	a.log("清单校验通过: %s", a.registryFile)

	for _, dir := range []string{a.stacksDir, a.nginxDir, a.nginxUpstreams, a.nginxSites, a.nginxSnippetDir} {
		if err := ensureDir(dir, 0o755); err != nil {
			return err
		}
	}

	httpConfig, err := renderAsset("templates/nginx-http.conf.tmpl", map[string]string{
		"NGINX_S2A_DIR": a.nginxDir,
	})
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(a.nginxDir, "http.conf"), httpConfig, 0o644); err != nil {
		return err
	}

	for _, site := range sortedSites(sites) {
		if err := a.renderSite(site); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(a.envsDir, site.Slug+".env")); err != nil {
			a.warn("%s 的密钥文件缺失: %s（init 前需创建）", site.Slug, filepath.Join(a.envsDir, site.Slug+".env"))
		}
	}

	snippet, err := readAsset("snippets/sub2api-proxy.conf")
	if err != nil {
		return err
	}
	snippetPath := filepath.Join(a.nginxSnippetDir, "sub2api-proxy.conf")
	if existing, readErr := os.ReadFile(snippetPath); readErr == nil && !bytes.Equal(existing, snippet) {
		a.warn("覆盖已漂移的 %s（以内置版本为准）", snippetPath)
	}
	if err := atomicWrite(snippetPath, snippet, 0o644); err != nil {
		return err
	}

	if err := a.runAttached(ctx, nil, "nginx", "-t"); err != nil {
		return errorsWithMessage(err, "nginx -t 校验失败，未执行 reload；请检查 nginx.conf include 与渲染产物")
	}
	if err := a.runAttached(ctx, nil, "nginx", "-s", "reload"); err != nil {
		return errorsWithMessage(err, "nginx reload 失败")
	}
	a.log("全部产物渲染完成，nginx 已 reload")
	return nil
}

func (a *app) renderSite(site resolvedSite) error {
	stackDir := a.stackDir(site.Slug)
	if err := ensureDir(stackDir, 0o755); err != nil {
		return err
	}

	dataCompose, err := renderAsset("templates/compose.data.yml.tmpl", map[string]string{
		"SLUG": site.Slug,
		"TZ":   site.TZ,
	})
	if err != nil {
		return err
	}
	appCompose, err := renderAsset("templates/compose.app.yml.tmpl", map[string]string{
		"SLUG":          site.Slug,
		"IMAGE_REPO":    site.ImageRepo,
		"BIND_HOST":     site.BindHost,
		"DRAIN_SECONDS": fmt.Sprintf("%d", site.DrainSeconds),
		"TZ":            site.TZ,
	})
	if err != nil {
		return err
	}
	siteConfig, err := renderAsset("templates/nginx-site.conf.tmpl", map[string]string{
		"DOMAIN":                site.Domain,
		"SLUG":                  site.Slug,
		"SLUG_US":               slugUnderscore(site.Slug),
		"TLS_CERT":              site.TLSCert,
		"TLS_KEY":               site.TLSKey,
		"NGINX_SNIPPET_DIR":     a.nginxSnippetDir,
		"CLIENT_MAX_BODY_SIZE":  site.ClientMaxBodySize,
		"PROXY_CONNECT_TIMEOUT": site.ProxyConnectTimeout,
		"PROXY_SEND_TIMEOUT":    site.ProxySendTimeout,
		"PROXY_READ_TIMEOUT":    site.ProxyReadTimeout,
	})
	if err != nil {
		return err
	}

	for path, content := range map[string][]byte{
		filepath.Join(stackDir, "compose.data.yml"):    dataCompose,
		filepath.Join(stackDir, "compose.app.yml"):     appCompose,
		filepath.Join(a.nginxSites, site.Slug+".conf"): siteConfig,
	} {
		if err := atomicWrite(path, content, 0o644); err != nil {
			return err
		}
	}

	upstream, err := a.renderUpstream(site, slotBlue, site.ImageTag)
	if err != nil {
		return err
	}
	created, err := writeIfMissing(a.upstreamPath(site.Slug), upstream, 0o644)
	if err != nil {
		return err
	}
	if created {
		a.log("%s: 初始化 upstream → blue:%d", site.Slug, site.PortBase)
	}
	a.log("%s: compose + nginx site 渲染完成", site.Slug)
	return nil
}

func errorsWithMessage(err error, message string) error {
	return fmt.Errorf("%s: %w", message, err)
}
