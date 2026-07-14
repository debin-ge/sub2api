package cmd

import (
	"context"
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/sub2api/session-recovery/internal/adapter"
	"github.com/sub2api/session-recovery/internal/detector"
	"github.com/sub2api/session-recovery/internal/model"
	"github.com/sub2api/session-recovery/internal/restorer"
)

var (
	platformFlag string
	dryRunFlag   bool
	sessionFlag  string
	restoreAll   bool
	yesFlag      bool
)

var rootCmd = &cobra.Command{
	Use:   "session-recovery",
	Short: "多平台 AI Coding Agent 会话管理和恢复工具",
	Long: `支持 Claude Code、Codex、OpenCode、Gemini、OpenClaw、Hermes 等平台的会话恢复工具。

功能：
  - 自动检测已安装的平台
  - 构建会话索引
  - 展示所有会话
  - 恢复单个或全部会话`,
	RunE: runInteractive,
}

func init() {
	rootCmd.Flags().StringVar(&platformFlag, "platform", "", "指定平台 (codex, claude_code, 等)")
	rootCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "仅显示隐藏会话，不恢复")
	rootCmd.Flags().StringVar(&sessionFlag, "session", "", "恢复特定会话 (部分 ID)")
	rootCmd.Flags().BoolVar(&restoreAll, "restore-all", false, "恢复所有隐藏会话")
	rootCmd.Flags().BoolVar(&yesFlag, "yes", false, "跳过确认提示")
}

func Execute() error {
	return rootCmd.Execute()
}

func runInteractive(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// 1. 检测平台
	fmt.Println("🔍 正在检测已安装的 AI Coding Agent 平台...")
	det := detector.NewDetector()
	platforms := det.DetectPlatforms()

	if len(platforms) == 0 {
		fmt.Println("\n❌ 未检测到任何支持的平台")
		return nil
	}

	fmt.Printf("\n✅ 检测到 %d 个平台:\n", len(platforms))
	for _, p := range platforms {
		fmt.Printf("  - %s (%s)\n", p.DisplayName, p.SessionPath)
	}

	// 2. 选择平台
	var selectedPlatform *model.PlatformInfo
	if platformFlag != "" {
		// 从命令行参数指定
		for _, p := range platforms {
			if p.Name == platformFlag {
				selectedPlatform = p
				break
			}
		}
		if selectedPlatform == nil {
			return fmt.Errorf("平台 %s 未找到或未安装", platformFlag)
		}
	} else if len(platforms) == 1 {
		selectedPlatform = platforms[0]
	} else {
		// 交互式选择
		var err error
		selectedPlatform, err = selectPlatform(platforms)
		if err != nil {
			return err
		}
	}

	fmt.Printf("\n📦 已选择: %s\n", selectedPlatform.DisplayName)

	// 3. 创建适配器
	var adp adapter.Adapter
	switch selectedPlatform.Name {
	case "codex":
		adp = adapter.NewCodexAdapter()
	case "claude_code":
		adp = adapter.NewClaudeCodeAdapter()
	case "opencode":
		adp = adapter.NewOpenCodeAdapter()
	case "gemini":
		adp = adapter.NewGeminiAdapter()
	case "openclaw":
		adp = adapter.NewOpenClawAdapter()
	case "hermes":
		adp = adapter.NewHermesAdapter()
	default:
		return fmt.Errorf("平台 %s 的适配器尚未实现", selectedPlatform.Name)
	}

	// 4. 扫描会话
	fmt.Println("\n🔨 正在构建会话索引...")
	sessions, err := adp.ScanAllSessions(ctx)
	if err != nil {
		return fmt.Errorf("扫描会话失败: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("\n⚠️  未找到任何会话")
		return nil
	}

	// 5. 区分可见和隐藏的会话
	currentProvider, _ := adp.GetCurrentProvider()
	var visibleSessions, hiddenSessions []*model.Session

	for _, s := range sessions {
		if s.OriginalProvider == currentProvider || s.OriginalProvider == "" {
			s.IsVisible = true
			visibleSessions = append(visibleSessions, s)
		} else {
			s.NeedsRecovery = true
			s.CurrentProvider = currentProvider
			hiddenSessions = append(hiddenSessions, s)
		}
	}

	fmt.Printf("✅ 扫描完成:\n")
	fmt.Printf("   - 当前可见: %d 个会话\n", len(visibleSessions))
	fmt.Printf("   - 需要恢复: %d 个会话\n", len(hiddenSessions))
	fmt.Printf("   - 总计: %d 个会话\n", len(sessions))

	if len(hiddenSessions) == 0 {
		fmt.Println("\n✅ 所有会话都已可见，无需恢复")
		return nil
	}

	// 6. 干运行模式：仅显示
	if dryRunFlag {
		displayHiddenSessions(hiddenSessions)
		return nil
	}

	// 7. 恢复会话
	rst := restorer.NewRestorer(adp)

	if sessionFlag != "" {
		// 恢复特定会话
		return restoreSpecificSession(ctx, rst, hiddenSessions, sessionFlag, selectedPlatform)
	}

	if restoreAll {
		// 批量恢复所有会话
		return restoreAllSessions(ctx, rst, hiddenSessions, selectedPlatform, yesFlag)
	}

	// 8. 交互式选择
	return handleInteractive(ctx, rst, hiddenSessions, selectedPlatform)
}

func selectPlatform(platforms []*model.PlatformInfo) (*model.PlatformInfo, error) {
	items := make([]string, len(platforms))
	for i, p := range platforms {
		items[i] = fmt.Sprintf("%s (%s)", p.DisplayName, p.SessionPath)
	}

	prompt := promptui.Select{
		Label: "请选择要恢复的平台",
		Items: items,
		Size:  10,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	return platforms[index], nil
}

func displayHiddenSessions(sessions []*model.Session) {
	fmt.Println("\n📋 需要恢复的会话:")
	fmt.Println("=" + repeatString("=", 60))

	for i, s := range sessions {
		if i >= 20 {
			fmt.Printf("\n... 还有 %d 个会话未显示\n", len(sessions)-20)
			break
		}

		fmt.Printf("\n%d. [%s] %s\n", i+1, truncateID(s.ID), s.Project)
		fmt.Printf("   Provider: %s → %s\n", s.OriginalProvider, s.CurrentProvider)
		if s.FirstMessage != "" {
			fmt.Printf("   💬 %s\n", truncate(s.FirstMessage, 80))
		}
		fmt.Printf("   📅 %s | 消息数: %d\n", s.UpdatedAt.Format("2006-01-02 15:04"), s.MessageCount)
	}
}

func restoreSpecificSession(ctx context.Context, rst *restorer.Restorer, sessions []*model.Session, sessionID string, platform *model.PlatformInfo) error {
	// 查找匹配的会话
	var matched []*model.Session
	for _, s := range sessions {
		if len(s.ID) >= len(sessionID) && s.ID[:len(sessionID)] == sessionID {
			matched = append(matched, s)
		}
	}

	if len(matched) == 0 {
		return fmt.Errorf("未找到会话: %s", sessionID)
	}

	if len(matched) > 1 {
		fmt.Printf("⚠️  找到多个匹配，请使用更长的 ID:\n")
		for _, m := range matched {
			fmt.Printf("  - %s (%s)\n", truncateID(m.ID), m.Project)
		}
		return nil
	}

	session := matched[0]
	fmt.Printf("\n🔄 正在恢复会话: %s\n", truncateID(session.ID))

	if err := rst.RestoreSession(ctx, session); err != nil {
		return fmt.Errorf("恢复失败: %w", err)
	}

	fmt.Println("✅ 会话已恢复!")
	fmt.Printf("💡 请在 %s 中查看恢复的会话\n", platform.DisplayName)

	return nil
}

func restoreAllSessions(ctx context.Context, rst *restorer.Restorer, sessions []*model.Session, platform *model.PlatformInfo, skipConfirm bool) error {
	if !skipConfirm {
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("确认要恢复所有 %d 个会话吗", len(sessions)),
			IsConfirm: true,
		}

		result, err := prompt.Run()
		if err != nil || result != "y" {
			fmt.Println("❌ 操作已取消")
			return nil
		}
	}

	fmt.Printf("\n🔄 开始批量恢复 %d 个会话...\n\n", len(sessions))

	successCount := 0
	failCount := 0

	for i, s := range sessions {
		fmt.Printf("[%d/%d] 恢复: %s", i+1, len(sessions), truncateID(s.ID))

		if err := rst.RestoreSession(ctx, s); err != nil {
			fmt.Printf(" ❌ 失败: %v\n", err)
			failCount++
		} else {
			fmt.Println(" ✅")
			successCount++
		}
	}

	fmt.Println("\n" + repeatString("=", 60))
	fmt.Printf("✅ 恢复完成: 成功 %d 个, 失败 %d 个\n", successCount, failCount)
	fmt.Printf("💡 请在 %s 中查看恢复的会话\n", platform.DisplayName)

	return nil
}

func handleInteractive(ctx context.Context, rst *restorer.Restorer, sessions []*model.Session, platform *model.PlatformInfo) error {
	displayHiddenSessions(sessions)

	items := []string{
		fmt.Sprintf("恢复所有会话 (共 %d 个)", len(sessions)),
		"恢复单个会话",
		"退出",
	}

	prompt := promptui.Select{
		Label: "请选择操作",
		Items: items,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return err
	}

	switch index {
	case 0:
		return restoreAllSessions(ctx, rst, sessions, platform, false)
	case 1:
		return selectAndRestoreOne(ctx, rst, sessions, platform)
	case 2:
		fmt.Println("\n👋 再见!")
		return nil
	}

	return nil
}

func selectAndRestoreOne(ctx context.Context, rst *restorer.Restorer, sessions []*model.Session, platform *model.PlatformInfo) error {
	items := make([]string, len(sessions))
	for i, s := range sessions {
		items[i] = fmt.Sprintf("[%s] %s - %s", truncateID(s.ID), s.Project, truncate(s.FirstMessage, 50))
	}

	prompt := promptui.Select{
		Label: "选择要恢复的会话",
		Items: items,
		Size:  15,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return err
	}

	session := sessions[index]
	fmt.Printf("\n🔄 正在恢复会话: %s\n", truncateID(session.ID))

	if err := rst.RestoreSession(ctx, session); err != nil {
		return fmt.Errorf("恢复失败: %w", err)
	}

	fmt.Println("✅ 会话已恢复!")
	fmt.Printf("💡 请在 %s 中查看恢复的会话\n", platform.DisplayName)

	return nil
}

// 辅助函数
func truncateID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func repeatString(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
