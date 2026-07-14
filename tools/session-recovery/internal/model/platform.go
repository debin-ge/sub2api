package model

// PlatformInfo 平台信息
type PlatformInfo struct {
	Name         string // 平台名称
	DisplayName  string // 显示名称
	SessionPath  string // 会话路径
	ConfigPath   string // 配置路径
	CLICommand   string // CLI 命令
	IsInstalled  bool   // 是否已安装
	VisibleCount int    // 可见会话数量
	HiddenCount  int    // 隐藏会话数量
}
