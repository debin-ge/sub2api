package detector

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sub2api/session-recovery/internal/model"
)

// Detector 平台检测器
type Detector struct{}

// NewDetector 创建平台检测器
func NewDetector() *Detector {
	return &Detector{}
}

// DetectPlatforms 检测所有可用平台
func (d *Detector) DetectPlatforms() []*model.PlatformInfo {
	platforms := d.getPlatformConfigs()
	var detected []*model.PlatformInfo

	for _, p := range platforms {
		if d.pathExists(p.SessionPath) && d.commandExists(p.CLICommand) {
			p.IsInstalled = true
			detected = append(detected, p)
		}
	}

	return detected
}

// getPlatformConfigs 返回所有平台的配置
func (d *Detector) getPlatformConfigs() []*model.PlatformInfo {
	homeDir, _ := os.UserHomeDir()

	return []*model.PlatformInfo{
		{
			Name:        "codex",
			DisplayName: "Codex",
			SessionPath: filepath.Join(homeDir, ".codex", "sessions"),
			ConfigPath:  filepath.Join(homeDir, ".codex", "config.json"),
			CLICommand:  "", // Codex 可能没有 CLI 命令，只检查目录
		},
		{
			Name:        "claude_code",
			DisplayName: "Claude Code",
			SessionPath: filepath.Join(homeDir, ".claude", "projects"),
			ConfigPath:  filepath.Join(homeDir, ".claude", "config.json"),
			CLICommand:  "claude",
		},
		{
			Name:        "opencode",
			DisplayName: "OpenCode",
			SessionPath: filepath.Join(homeDir, ".local", "share", "opencode", "opencode.db"),
			ConfigPath:  filepath.Join(homeDir, ".config", "opencode"),
			CLICommand:  "opencode",
		},
		{
			Name:        "gemini",
			DisplayName: "Gemini CLI",
			SessionPath: filepath.Join(homeDir, ".gemini", "tmp"),
			ConfigPath:  filepath.Join(homeDir, ".gemini", "config"),
			CLICommand:  "gemini",
		},
		{
			Name:        "openclaw",
			DisplayName: "OpenClaw",
			SessionPath: filepath.Join(homeDir, ".openclaw", "agents", "main", "sessions"),
			ConfigPath:  filepath.Join(homeDir, ".openclaw", "config.json"),
			CLICommand:  "openclaw",
		},
		{
			Name:        "hermes",
			DisplayName: "Hermes",
			SessionPath: filepath.Join(homeDir, ".hermes", "state.db"),
			ConfigPath:  filepath.Join(homeDir, ".hermes", "config"),
			CLICommand:  "hermes",
		},
	}
}

// pathExists 检查路径是否存在
func (d *Detector) pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// commandExists 检查命令是否存在
func (d *Detector) commandExists(command string) bool {
	if command == "" {
		return true // 空命令名视为不需要 CLI，只检查路径
	}
	_, err := exec.LookPath(command)
	return err == nil
}
