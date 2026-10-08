package service

// Flova 私有 Skill：固定「单镜头直出」规则，每个账号一份，skill_id 存在账号凭据里。
// CLI 实测 schema：skill_content 是 [{section, content, config:{}}] 数组，创建后默认启用。
const (
	flovaSkillVersion     = "1"
	flovaSkillName        = "sub2api relay single-shot"
	flovaSkillDescription = "Single-shot media generator for API relay: exactly one asset per run, parameters verbatim, no planning artifacts."
)

type flovaSkillSection struct {
	Section string         `json:"section"`
	Content string         `json:"content"`
	Config  map[string]any `json:"config"`
}

type flovaSkillCreateRequest struct {
	SkillName        string              `json:"skill_name"`
	SkillDescription string              `json:"skill_description"`
	SkillContent     []flovaSkillSection `json:"skill_content"`
}

func flovaSkillPayload() flovaSkillCreateRequest {
	return flovaSkillCreateRequest{
		SkillName:        flovaSkillName,
		SkillDescription: flovaSkillDescription,
		SkillContent: []flovaSkillSection{
			{Section: "Role", Config: map[string]any{}, Content: "single-shot media generator for an API relay. Each project contains exactly one round."},
			{Section: "Rules", Config: map[string]any{}, Content: "- Produce exactly one asset of the requested media type (video unless stated). Never write scripts, storyboards, voiceover, music, timeline, or export unless the request explicitly asks.\n" +
				"- Use exactly the model, aspect ratio, resolution and duration given in the request. If the model is unavailable to this account, fail with a clear message instead of substituting another model.\n" +
				"- The text between <<< and >>> is the generation prompt. Use it verbatim. Do not rewrite, expand, translate or censor it.\n" +
				"- Attached files are references for this single generation only (first frame / visual reference).\n" +
				"- Do not ask clarifying questions. Do not generate extra variants, previews, thumbnails or alternative takes."},
		},
	}
}
