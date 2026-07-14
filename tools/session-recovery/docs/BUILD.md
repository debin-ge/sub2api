# 编译指南

## 本地编译

### macOS

```bash
# 编译当前平台
go build -o bin/session-recovery main.go

# 或使用 Makefile
make build-mac
```

### Linux

```bash
go build -o bin/session-recovery main.go
```

### Windows

```bash
# 在 Windows 上直接编译
go build -o bin/session-recovery.exe main.go
```

## 跨平台编译

由于项目使用了 SQLite (go-sqlite3)，需要 CGO 支持，跨平台编译需要相应的交叉编译工具链。

### 方案 1：使用纯 Go SQLite 驱动（推荐）

修改为使用 `modernc.org/sqlite` 替代 `github.com/mattn/go-sqlite3`，无需 CGO：

```bash
# 1. 替换依赖
go get modernc.org/sqlite
go mod tidy

# 2. 修改导入
# 将所有文件中的
#   _ "github.com/mattn/go-sqlite3"
# 替换为
#   _ "modernc.org/sqlite"

# 3. 跨平台编译
GOOS=windows GOARCH=amd64 go build -o bin/session-recovery-windows-amd64.exe main.go
GOOS=linux GOARCH=amd64 go build -o bin/session-recovery-linux-amd64 main.go
GOOS=darwin GOARCH=amd64 go build -o bin/session-recovery-darwin-amd64 main.go
GOOS=darwin GOARCH=arm64 go build -o bin/session-recovery-darwin-arm64 main.go
```

### 方案 2：使用 Docker 交叉编译

创建 Dockerfile：

```dockerfile
FROM golang:1.21-alpine

RUN apk add --no-cache \
    gcc \
    g++ \
    musl-dev \
    mingw-w64-gcc

WORKDIR /app
COPY . .

RUN go mod download

# 编译多个平台
RUN GOOS=linux GOARCH=amd64 go build -o bin/session-recovery-linux-amd64 main.go
RUN CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
    go build -o bin/session-recovery-windows-amd64.exe main.go
```

编译：

```bash
docker build -t session-recovery-builder .
docker run --rm -v $(pwd)/bin:/app/bin session-recovery-builder
```

### 方案 3：安装 MinGW（macOS）

```bash
# 安装 MinGW
brew install mingw-w64

# 编译 Windows 版本
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC=x86_64-w64-mingw32-gcc \
  go build -o bin/session-recovery-windows-amd64.exe main.go
```

### 方案 4：分平台编译（最简单）

在各自的平台上编译：

- **Windows 用户**：在 Windows 上运行 `go build`
- **Linux 用户**：在 Linux 上运行 `go build`
- **macOS 用户**：在 macOS 上运行 `go build`

### 方案 5：使用 GitHub Actions CI/CD

创建 `.github/workflows/build.yml`：

```yaml
name: Build

on:
  push:
    tags:
      - 'v*'

jobs:
  build:
    runs-on: ${{ matrix.os }}
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
    
    steps:
    - uses: actions/checkout@v3
    
    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'
    
    - name: Build
      run: |
        go build -o bin/session-recovery${{ matrix.os == 'windows-latest' && '.exe' || '' }} main.go
    
    - name: Upload artifact
      uses: actions/upload-artifact@v3
      with:
        name: session-recovery-${{ matrix.os }}
        path: bin/*
```

## Makefile 目标

项目包含以下 Makefile 目标：

```bash
make build-mac      # 编译 macOS 版本
make build-linux    # 编译 Linux 版本（需要 Docker 或 Linux）
make build-windows  # 编译 Windows 版本（需要 MinGW 或 Docker）
make build-all      # 编译所有平台
make test           # 运行测试
make clean          # 清理构建产物
```

## 推荐方案

**开发阶段**：使用方案 1（纯 Go SQLite 驱动），最简单且无需额外工具

**生产发布**：使用方案 5（GitHub Actions），自动化构建所有平台

## 验证编译结果

```bash
# 查看编译的二进制文件
ls -lh bin/

# 检查文件类型
file bin/session-recovery*

# 运行编译的程序
./bin/session-recovery --help
```

## 常见问题

### 问题：CGO_ENABLED=1 but no C compiler found

**解决**：安装对应平台的 C 编译器（gcc/clang/mingw）

### 问题：undefined reference to `sqlite3_xxx`

**解决**：确保安装了 SQLite 开发库或使用纯 Go 驱动

### 问题：编译 Windows 版本在 macOS 上失败

**解决**：安装 `brew install mingw-w64` 或使用 Docker/GitHub Actions
