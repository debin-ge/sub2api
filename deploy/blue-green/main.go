package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// Version is injected by -ldflags "-X main.Version=...".
var Version = "dev"

func main() {
	if err := runCLI(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		os.Exit(1)
	}
}

func runCLI(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("bgdeploy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configFile := fs.String("config", "", "主机级运行配置文件（默认当前目录/runtime.yaml）")
	root := fs.String("root", "", "部署根目录（默认当前工作目录）")
	nginxDir := fs.String("nginx-dir", "", "nginx 蓝绿配置目录")
	nginxSnippetDir := fs.String("nginx-snippet-dir", "", "nginx snippet 目录")
	if err := fs.Parse(args); err != nil {
		return err
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printUsage(stdout)
		return errors.New("缺少命令")
	}
	command, commandArgs := rest[0], rest[1:]
	if command == "help" || command == "-h" || command == "--help" {
		printUsage(stdout)
		return nil
	}
	if command == "version" {
		fmt.Fprintf(stdout, "bgdeploy %s\n", Version)
		return nil
	}

	app, err := newAppWithConfig(*configFile, *root, *nginxDir, *nginxSnippetDir, stdout, stderr)
	if err != nil {
		return err
	}

	switch command {
	case "bootstrap":
		if len(commandArgs) != 0 {
			return usageError("bootstrap")
		}
		return app.bootstrap()
	case "check":
		if len(commandArgs) != 0 {
			return usageError("check")
		}
		app.requireRoot = true
		return app.checkDependencies(ctx)
	case "render":
		if len(commandArgs) != 0 {
			return usageError("render")
		}
		return app.render(ctx)
	case "init":
		if len(commandArgs) != 1 {
			return usageError("init <slug>")
		}
		return app.initStack(ctx, commandArgs[0])
	case "deploy":
		if len(commandArgs) < 1 || len(commandArgs) > 2 {
			return usageError("deploy <slug> [image-tag]")
		}
		tag := ""
		if len(commandArgs) == 2 {
			tag = commandArgs[1]
		}
		return app.deploy(ctx, commandArgs[0], tag)
	case "rollback":
		if len(commandArgs) != 1 {
			return usageError("rollback <slug>")
		}
		return app.rollback(ctx, commandArgs[0])
	case "status":
		if len(commandArgs) > 1 {
			return usageError("status [slug]")
		}
		slug := ""
		if len(commandArgs) == 1 {
			slug = commandArgs[0]
		}
		return app.status(ctx, slug)
	case "teardown":
		if len(commandArgs) != 2 {
			return usageError("teardown <slug> <blue|green>")
		}
		return app.teardown(ctx, commandArgs[0], commandArgs[1])
	case "__drain":
		if len(commandArgs) != 3 {
			return usageError("__drain <seconds> <slug> <blue|green>")
		}
		seconds, err := strconv.Atoi(commandArgs[0])
		if err != nil || seconds < 0 {
			return fmt.Errorf("非法排空秒数: %q", commandArgs[0])
		}
		time.Sleep(time.Duration(seconds) * time.Second)
		err = app.teardown(ctx, commandArgs[1], commandArgs[2])
		_ = os.Remove(app.drainPIDFile(commandArgs[1], commandArgs[2]))
		return err
	default:
		printUsage(stderr)
		return fmt.Errorf("未知命令: %s", command)
	}
}

func usageError(usage string) error {
	return fmt.Errorf("用法: bgdeploy %s", usage)
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`
通用蓝绿部署 CLI

用法:
  bgdeploy [全局参数] <命令> [参数]

命令:
  bootstrap                         创建 runtime、registry、envs、stacks
  check                             检查权限、Docker/Compose 与 nginx 接入
  render                            从 sites.yaml 渲染 compose/nginx 配置
  init <slug>                       初始化 network 与 PostgreSQL/Redis
  deploy <slug> [image-tag]         蓝绿发布
  rollback <slug>                   快速或降级回滚
  status [slug]                     查询全部或指定站点状态
  teardown <slug> <blue|green>      安全回收非生效 slot
  version                           输出版本

全局参数:
  --config <path>                   默认 ./runtime.yaml
  --root <path>                     默认当前工作目录
  --nginx-dir <path>                默认 /etc/nginx/sites
  --nginx-snippet-dir <path>        默认 /etc/nginx/sites/snippets

环境变量:
  BGDEPLOY_CONFIG
  BGDEPLOY_ROOT
  BGDEPLOY_NGINX_DIR
  BGDEPLOY_NGINX_SNIPPET_DIR

优先级:
  命令行参数 > 环境变量 > runtime.yaml > 内置默认值

典型操作:
  sudo ./bgdeploy bootstrap
  sudo ./bgdeploy render
  sudo ./bgdeploy init api-staging
  sudo ./bgdeploy deploy api-staging v1.6.8
  ./bgdeploy status api-staging
  sudo ./bgdeploy rollback api-staging
`))
}
