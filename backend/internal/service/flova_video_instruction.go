package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// flovaPlan 是校验后的提交计划：选定档案与归一化后的比例 / 分辨率 / 时长。
type flovaPlan struct {
	profile    *flovaModelProfile
	ratio      string
	resolution string
	seconds    int
}

// flovaFile 是上传到 Flova 存储后的文件引用（/chat 的 files[] 条目）。
type flovaFile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// flovaEscapePrompt 去掉会破坏定界符与文件标记语法的片段：<<< / >>> 定界符、反引号。
// 换行保留，Agent 读得懂多行提示词。
func flovaEscapePrompt(prompt string) string {
	prompt = strings.ReplaceAll(prompt, "\r\n", "\n")
	prompt = strings.ReplaceAll(prompt, ">>>", "> > >")
	prompt = strings.ReplaceAll(prompt, "<<<", "< < <")
	prompt = strings.ReplaceAll(prompt, "`", "'")
	return strings.TrimSpace(prompt)
}

// flovaRenderInstruction 渲染发给 Agent 的受控指令。返回指令正文与其 sha256（审计用）。
//
// 结构与实测成功的 PoC 指令一致：文件标记在最前面（CLI 的 --file-from 行为），随后是
// 模型 / 比例 / 分辨率 / 时长、参考说明、定界的原文提示词、本轮栅栏。
func flovaRenderInstruction(plan *flovaPlan, prompt string, files []flovaFile) (string, string) {
	var b strings.Builder
	for _, file := range files {
		marker, _ := json.Marshal(map[string]string{"type": "uploaded", "resource_name": file.Name})
		_, _ = b.WriteString("`")
		_, _ = b.Write(marker)
		_, _ = b.WriteString("` ")
	}
	fmt.Fprintf(&b, "Generate exactly one video. Model: %s. Aspect ratio: %s. Resolution: %s. Duration: %d seconds.\n",
		plan.profile.DisplayName, plan.ratio, plan.resolution, plan.seconds)
	if len(files) > 0 {
		_, _ = b.WriteString("Reference image: use the attached uploaded image as the first frame / visual reference only.\n")
	}
	fmt.Fprintf(&b, "Prompt (use verbatim, do not rewrite): <<<%s>>>\n", flovaEscapePrompt(prompt))
	_, _ = b.WriteString("Rules for this round: produce exactly one video asset and nothing else. Do not write a script, storyboard, voiceover, music, timeline or export. ")
	_, _ = b.WriteString("Do not generate images unless strictly required as the first frame of this single video. ")
	_, _ = b.WriteString("Use the model, aspect ratio, resolution and duration exactly as given; if the model is unavailable to this account, stop and report the reason instead of substituting another model.")
	content := b.String()
	sum := sha256.Sum256([]byte(content))
	return content, hex.EncodeToString(sum[:])
}
