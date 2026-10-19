package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

const flovaSkillExistsJSON = `{"code":"invalid_request","message":"A skill with the same name and description already exists","data":null}`

func flovaSkillConflict(stub *flovaStubServer) {
	stub.on("/api/agent/v2/skill/create", func(int, *http.Request, string) *http.Response {
		return openAIVideoResponseForTest(http.StatusBadRequest, flovaSkillExistsJSON, nil)
	})
}

func TestFlovaResolveSkillCreatesWhenAbsent(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/skill/create", `{"code":0,"data":{"skill_id":"sk_new"}}`)
	id, reused, err := newFlovaTestProvider(stub).ResolveSkill(context.Background(), flovaVideoTestAccount())
	require.NoError(t, err)
	require.Equal(t, "sk_new", id)
	require.False(t, reused)
	require.Zero(t, stub.count("/api/agent/v2/skill/my/list"))
}

func TestFlovaResolveSkillReusesExistingOnConflict(t *testing.T) {
	shapes := map[string]string{
		"wrapped list": `{"code":0,"data":{"skills":[` +
			`{"skill_id":"sk_other","skill_name":"something else"},` +
			`{"skill_id":"sk_lookalike","skill_name":"sub2api relay single-shot","skill_description":"a different description"},` +
			`{"skill_id":"c9c98496adfb41e8ae55e8010123c593","skill_name":"sub2api relay single-shot","skill_description":"` + flovaSkillDescription + `"}]}}`,
		"bare array with short keys": `{"code":0,"data":[{"id":"c9c98496adfb41e8ae55e8010123c593","name":"sub2api relay single-shot"}]}`,
	}
	for name, body := range shapes {
		t.Run(name, func(t *testing.T) {
			stub := newFlovaStubServer()
			flovaSkillConflict(stub)
			stub.json("/api/agent/v2/skill/my/list", body)
			id, reused, err := newFlovaTestProvider(stub).ResolveSkill(context.Background(), flovaVideoTestAccount())
			require.NoError(t, err)
			require.True(t, reused)
			require.Equal(t, "c9c98496adfb41e8ae55e8010123c593", id)
			list := stub.lastBody("/api/agent/v2/skill/my/list")
			require.Equal(t, "created", list["view"])
			require.Equal(t, true, list["only_brief"])
		})
	}
}

func TestFlovaResolveSkillConflictWithoutMatchIsExplicit(t *testing.T) {
	stub := newFlovaStubServer()
	flovaSkillConflict(stub)
	stub.json("/api/agent/v2/skill/my/list", `{"code":0,"data":{"skills":[{"skill_id":"sk_other","skill_name":"something else"}]}}`)
	_, _, err := newFlovaTestProvider(stub).ResolveSkill(context.Background(), flovaVideoTestAccount())
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "skill_conflict", typed.Code)
	require.Contains(t, typed.Message, "set skill_id on the account manually")
}

func TestFlovaResolveSkillPassesThroughOtherErrors(t *testing.T) {
	stub := newFlovaStubServer()
	stub.on("/api/agent/v2/skill/create", func(int, *http.Request, string) *http.Response {
		return openAIVideoResponseForTest(http.StatusBadRequest, `{"code":"invalid_request","message":"skill_content is required","data":null}`, nil)
	})
	_, _, err := newFlovaTestProvider(stub).ResolveSkill(context.Background(), flovaVideoTestAccount())
	var typed *VideoProviderError
	require.ErrorAs(t, err, &typed)
	require.Equal(t, "skill_content is required", typed.Message)
	require.Zero(t, stub.count("/api/agent/v2/skill/my/list"), "only the duplicate conflict triggers a lookup")
}

func TestFlovaVideoProviderCreateReusesExistingSkill(t *testing.T) {
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	flovaSkillConflict(stub)
	stub.json("/api/agent/v2/skill/my/list", `{"code":0,"data":{"skills":[{"skill_id":"sk_existing","skill_name":"sub2api relay single-shot","skill_description":"`+flovaSkillDescription+`"}]}}`)
	stub.json("/api/agent/v2/create", `{"code":0,"data":{"project_id":"p1"}}`)
	stub.json("/api/agent/v2/chat", `{"code":0,"data":{"stream_chat_id":"s1"}}`)
	account := flovaVideoTestAccount()
	delete(account.Credentials, "skill_id")
	repo := newFlovaAccountRepoStub(account)
	provider := newFlovaTestProvider(stub)
	provider.accounts = repo
	_, err := provider.Create(context.Background(), account, VideoCreateRequest{TaskID: "t", Model: FlovaVideoModelSeedance20, Prompt: "x", Seconds: 4, Resolution: "720p"}, nil)
	require.NoError(t, err)
	require.Equal(t, []string{"/api/agent/v2/user", "/api/agent/v2/skill/create", "/api/agent/v2/skill/my/list", "/api/agent/v2/create", "/api/agent/v2/chat"}, stub.calls)
	require.Equal(t, "sk_existing", stub.lastBody("/api/agent/v2/chat")["extra_data"].(map[string]any)["skill_id"])
	require.Equal(t, "sk_existing", repo.updated.Credentials["skill_id"], "the recovered skill is saved on the account")
}
