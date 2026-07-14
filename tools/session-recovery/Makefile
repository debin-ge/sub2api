.PHONY: build-mac build-linux build-windows build-all test clean help

# 默认目标
all: build-mac

# macOS 构建
build-mac:
	@echo "Building for macOS (amd64)..."
	GOOS=darwin GOARCH=amd64 go build -o bin/session-recovery-darwin-amd64 main.go
	@echo "Building for macOS (arm64)..."
	GOOS=darwin GOARCH=arm64 go build -o bin/session-recovery-darwin-arm64 main.go
	@echo "✅ macOS builds complete"

# Linux 构建
build-linux:
	@echo "Building for Linux (amd64)..."
	GOOS=linux GOARCH=amd64 go build -o bin/session-recovery-linux-amd64 main.go
	@echo "✅ Linux build complete"

# Windows 构建
build-windows:
	@echo "Building for Windows (amd64)..."
	GOOS=windows GOARCH=amd64 go build -o bin/session-recovery-windows-amd64.exe main.go
	@echo "✅ Windows build complete"

# 所有平台构建
build-all: build-mac build-linux build-windows
	@echo "✅ All platform builds complete"
	@ls -lh bin/

# 本地开发构建
build:
	@echo "Building for current platform..."
	go build -o bin/session-recovery main.go
	@echo "✅ Build complete"

# 运行测试
test:
	@echo "Running tests..."
	go test ./... -v

# 代码检查
lint:
	@echo "Running linter..."
	golangci-lint run

# 清理构建产物
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	@echo "✅ Clean complete"

# 显示帮助
help:
	@echo "Session Recovery Tool - Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  make build          - Build for current platform"
	@echo "  make build-mac      - Build for macOS (amd64 + arm64)"
	@echo "  make build-linux    - Build for Linux (amd64)"
	@echo "  make build-windows  - Build for Windows (amd64)"
	@echo "  make build-all      - Build for all platforms"
	@echo "  make test           - Run tests"
	@echo "  make lint           - Run linter"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make help           - Show this help"
