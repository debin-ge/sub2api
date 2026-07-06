package service

import (
	"context"
	"reflect"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent"
)

type benchmarkTargetValidationRepoStub struct {
	BenchmarkRepository
	createTargetCalled bool
	updateTargetCalled bool
	createRunCalled    bool
	createRunInput     *BenchmarkCreateRunInput
	createTargetInput  *BenchmarkTargetInput
	enabledTargets     []*ent.BenchmarkTarget
	enabledTasks       []*ent.BenchmarkTask
}

func (s *benchmarkTargetValidationRepoStub) CreateTarget(ctx context.Context, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	s.createTargetCalled = true
	s.createTargetInput = &input
	target := &ent.BenchmarkTarget{
		ID:                  10,
		ModelName:           input.ModelName,
		ChannelID:           input.ChannelID,
		Enabled:             input.Enabled,
		ChannelNameSnapshot: benchmarkValidationStringPtr(input.ChannelNameSnapshot),
	}
	return target, nil
}

func (s *benchmarkTargetValidationRepoStub) UpdateTarget(ctx context.Context, id int64, input BenchmarkTargetInput) (*ent.BenchmarkTarget, error) {
	s.updateTargetCalled = true
	target := &ent.BenchmarkTarget{
		ID:                  id,
		ModelName:           input.ModelName,
		ChannelID:           input.ChannelID,
		Enabled:             input.Enabled,
		ChannelNameSnapshot: benchmarkValidationStringPtr(input.ChannelNameSnapshot),
	}
	return target, nil
}

func benchmarkValidationStringPtr(value string) *string {
	return &value
}

func (s *benchmarkTargetValidationRepoStub) ListEnabledTargets(ctx context.Context) ([]*ent.BenchmarkTarget, error) {
	return s.enabledTargets, nil
}

func (s *benchmarkTargetValidationRepoStub) ListTargetsByIDs(ctx context.Context, ids []int64) ([]*ent.BenchmarkTarget, error) {
	byID := make(map[int64]*ent.BenchmarkTarget, len(s.enabledTargets))
	for _, target := range s.enabledTargets {
		byID[target.ID] = target
	}
	out := make([]*ent.BenchmarkTarget, 0, len(ids))
	for _, id := range ids {
		if target := byID[id]; target != nil {
			out = append(out, target)
		}
	}
	return out, nil
}

func (s *benchmarkTargetValidationRepoStub) ListEnabledTasks(ctx context.Context) ([]*ent.BenchmarkTask, error) {
	return s.enabledTasks, nil
}

func (s *benchmarkTargetValidationRepoStub) CreateRunWithSnapshots(ctx context.Context, input BenchmarkCreateRunInput) (*ent.BenchmarkRun, error) {
	s.createRunCalled = true
	s.createRunInput = &input
	return &ent.BenchmarkRun{ID: 99, Status: input.Status}, nil
}

type benchmarkTargetChannelResolverStub struct {
	channels         map[int64]*Channel
	groups           map[int64]*Group
	platforms        map[int64]string
	channelIDByGroup map[int64]int64
	createdChannels  []*Channel
	nextChannelID    int64
}

func (s *benchmarkTargetChannelResolverStub) GetByID(ctx context.Context, id int64) (*Channel, error) {
	if s.channels == nil {
		return nil, nil
	}
	return s.channels[id], nil
}

func (s *benchmarkTargetChannelResolverStub) GetGroupPlatforms(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
	got := make(map[int64]string, len(groupIDs))
	for _, groupID := range groupIDs {
		if platform, ok := s.platforms[groupID]; ok {
			got[groupID] = platform
		}
	}
	return got, nil
}

func (s *benchmarkTargetChannelResolverStub) GetGroupByID(ctx context.Context, id int64) (*Group, error) {
	if s.groups == nil {
		return nil, nil
	}
	return s.groups[id], nil
}

func (s *benchmarkTargetChannelResolverStub) GetChannelIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	if s.channelIDByGroup == nil {
		return 0, nil
	}
	return s.channelIDByGroup[groupID], nil
}

func (s *benchmarkTargetChannelResolverStub) Create(ctx context.Context, channel *Channel) error {
	if s.nextChannelID <= 0 {
		s.nextChannelID = 100
	}
	created := channel.Clone()
	created.ID = s.nextChannelID
	channel.ID = created.ID
	s.nextChannelID++
	s.createdChannels = append(s.createdChannels, created)
	if s.channels == nil {
		s.channels = map[int64]*Channel{}
	}
	s.channels[created.ID] = created
	if s.channelIDByGroup == nil {
		s.channelIDByGroup = map[int64]int64{}
	}
	for _, groupID := range created.GroupIDs {
		s.channelIDByGroup[groupID] = created.ID
	}
	return nil
}

func TestBenchmarkServiceCreateTargetFromGroupUsesExistingChannel(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		groups: map[int64]*Group{
			30: {ID: 30, Name: "OpenAI subscription", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		},
		channels: map[int64]*Channel{
			3: {ID: 3, Name: "Existing Channel", GroupIDs: []int64{30}},
		},
		channelIDByGroup: map[int64]int64{30: 3},
		platforms:        map[int64]string{30: PlatformOpenAI},
	})

	target, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		GroupID:   30,
		Enabled:   true,
	})

	if err != nil {
		t.Fatalf("CreateTarget returned error: %v", err)
	}
	if target.ChannelID != 3 {
		t.Fatalf("target.ChannelID = %d, want 3", target.ChannelID)
	}
	if repo.createTargetInput == nil || repo.createTargetInput.ChannelID != 3 {
		t.Fatalf("CreateTarget input = %#v, want channel_id 3", repo.createTargetInput)
	}
	if target.ChannelNameSnapshot == nil || *target.ChannelNameSnapshot != "Existing Channel" {
		t.Fatalf("ChannelNameSnapshot = %#v, want Existing Channel", target.ChannelNameSnapshot)
	}
}

func TestBenchmarkServiceCreateTargetFromGroupCreatesBenchmarkChannel(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	resolver := &benchmarkTargetChannelResolverStub{
		groups: map[int64]*Group{
			31: {ID: 31, Name: "OpenAI standard", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard},
		},
		platforms:     map[int64]string{31: PlatformOpenAI},
		nextChannelID: 9,
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(resolver)

	target, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1-mini",
		GroupID:   31,
		Enabled:   true,
	})

	if err != nil {
		t.Fatalf("CreateTarget returned error: %v", err)
	}
	if target.ChannelID != 9 {
		t.Fatalf("target.ChannelID = %d, want created channel 9", target.ChannelID)
	}
	if len(resolver.createdChannels) != 1 {
		t.Fatalf("created channels = %d, want 1", len(resolver.createdChannels))
	}
	created := resolver.createdChannels[0]
	if created.Name != "Benchmark Group 31 #31" || created.Status != StatusActive {
		t.Fatalf("created channel = %#v, want active Benchmark Group 31 #31", created)
	}
	if !reflect.DeepEqual(created.GroupIDs, []int64{31}) {
		t.Fatalf("created channel groups = %#v, want [31]", created.GroupIDs)
	}
}

func TestBenchmarkServiceCreateTargetFromGroupRejectsUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	resolver := &benchmarkTargetChannelResolverStub{
		groups: map[int64]*Group{
			20: {ID: 20, Name: "Anthropic subscription", Platform: PlatformAnthropic, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		},
		platforms: map[int64]string{20: PlatformAnthropic},
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(resolver)

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "claude-sonnet-4",
		GroupID:   20,
		Enabled:   true,
	})

	if err == nil || err.Error() != "benchmark target group 20 platform anthropic is not supported by standard radar execution" {
		t.Fatalf("CreateTarget error = %v, want unsupported group platform", err)
	}
	if repo.createTargetCalled {
		t.Fatal("CreateTarget persisted an unsupported group target")
	}
	if len(resolver.createdChannels) != 0 {
		t.Fatalf("created channels = %d, want 0", len(resolver.createdChannels))
	}
}

func TestBenchmarkServiceCreateTargetRejectsChannelWithoutGroups(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			1: {ID: 1, Name: "Ungrouped", GroupIDs: nil},
		},
	})

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		ChannelID: 1,
		Enabled:   true,
	})

	if err == nil || err.Error() != "benchmark target channel 1 has no groups" {
		t.Fatalf("CreateTarget error = %v, want benchmark target channel 1 has no groups", err)
	}
	if repo.createTargetCalled {
		t.Fatal("CreateTarget persisted an invalid benchmark target")
	}
}

func TestBenchmarkServiceCreateTargetRejectsChannelWithoutOpenAIGroup(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			2: {ID: 2, Name: "Anthropic only", GroupIDs: []int64{20}},
		},
		platforms: map[int64]string{20: PlatformAnthropic},
	})

	_, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		ChannelID: 2,
		Enabled:   true,
	})

	if err == nil || err.Error() != "benchmark target channel 2 has no openai-compatible group" {
		t.Fatalf("CreateTarget error = %v, want benchmark target channel 2 has no openai-compatible group", err)
	}
	if repo.createTargetCalled {
		t.Fatal("CreateTarget persisted an invalid benchmark target")
	}
}

func TestBenchmarkServiceCreateTargetAllowsOpenAIChannelAndFillsSnapshot(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			3: {ID: 3, Name: "OpenAI Channel", GroupIDs: []int64{30, 31}},
		},
		platforms: map[int64]string{30: PlatformAnthropic, 31: PlatformOpenAI},
	})

	target, err := svc.CreateTarget(context.Background(), BenchmarkTargetInput{
		ModelName: "gpt-4.1",
		ChannelID: 3,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("CreateTarget returned error: %v", err)
	}
	if target.ChannelNameSnapshot == nil || *target.ChannelNameSnapshot != "OpenAI Channel" {
		t.Fatalf("ChannelNameSnapshot = %#v, want OpenAI Channel", target.ChannelNameSnapshot)
	}
}

func TestBenchmarkServiceCreateRunRejectsSelectedTargetWithInvalidChannel(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{
		enabledTargets: []*ent.BenchmarkTarget{
			{ID: 11, ModelName: "gpt-4.1", ChannelID: 1, Enabled: true},
		},
		enabledTasks: []*ent.BenchmarkTask{
			{ID: 21, Title: "Task", Type: "reasoning", Prompt: "Return OK", VerifierType: "normalized_match", Enabled: true},
		},
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			1: {ID: 1, Name: "Ungrouped", GroupIDs: nil},
		},
	})

	_, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{})

	if err == nil || err.Error() != "benchmark target channel 1 has no groups" {
		t.Fatalf("CreateRun error = %v, want benchmark target channel 1 has no groups", err)
	}
	if repo.createRunCalled {
		t.Fatal("CreateRun created a run for an invalid benchmark target")
	}
}

func TestBenchmarkServiceCreateRunAllowsTargetsWithOpenAIChannels(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{
		enabledTargets: []*ent.BenchmarkTarget{
			{ID: 11, ModelName: "gpt-4.1", ChannelID: 3, Enabled: true},
		},
		enabledTasks: []*ent.BenchmarkTask{
			{ID: 21, Title: "Task", Type: "reasoning", Prompt: "Return OK", VerifierType: "normalized_match", Enabled: true},
		},
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			3: {ID: 3, Name: "OpenAI Channel", GroupIDs: []int64{30}},
		},
		platforms: map[int64]string{30: PlatformOpenAI},
	})

	run, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{})
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	if run.ID != 99 || !repo.createRunCalled {
		t.Fatalf("CreateRun did not create expected run: run=%#v called=%v", run, repo.createRunCalled)
	}
}

func TestBenchmarkServicePreviewRunRejectsSelectedTargetWithInvalidChannel(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{
		enabledTargets: []*ent.BenchmarkTarget{
			{ID: 11, ModelName: "gpt-4.1", ChannelID: 1, Enabled: true},
		},
		enabledTasks: []*ent.BenchmarkTask{
			{ID: 21, Title: "Task", Type: "reasoning", Prompt: "Return OK", VerifierType: "normalized_match", Enabled: true},
		},
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			1: {ID: 1, Name: "Ungrouped", GroupIDs: nil},
		},
	})

	_, err := svc.PreviewRun(context.Background(), nil, 0)

	if err == nil || err.Error() != "benchmark target channel 1 has no groups" {
		t.Fatalf("PreviewRun error = %v, want benchmark target channel 1 has no groups", err)
	}
}

func TestBenchmarkServiceRunTargetValidationPreservesSelectionOrder(t *testing.T) {
	t.Parallel()

	repo := &benchmarkTargetValidationRepoStub{
		enabledTargets: []*ent.BenchmarkTarget{
			{ID: 11, ModelName: "model-a", ChannelID: 3, Enabled: true},
			{ID: 22, ModelName: "model-b", ChannelID: 4, Enabled: true},
		},
		enabledTasks: []*ent.BenchmarkTask{
			{ID: 21, Title: "Task", Type: "reasoning", Prompt: "Return OK", VerifierType: "normalized_match", Enabled: true},
		},
	}
	svc := NewBenchmarkService(repo)
	svc.SetChannelResolver(&benchmarkTargetChannelResolverStub{
		channels: map[int64]*Channel{
			3: {ID: 3, Name: "OpenAI A", GroupIDs: []int64{30}},
			4: {ID: 4, Name: "OpenAI B", GroupIDs: []int64{40}},
		},
		platforms: map[int64]string{30: PlatformOpenAI, 40: PlatformOpenAI},
	})

	_, err := svc.CreateRun(context.Background(), BenchmarkCreateRunRequest{TargetIDs: []int64{22, 11}})
	if err != nil {
		t.Fatalf("CreateRun returned error: %v", err)
	}
	got := benchmarkRunTargetIDsFromInput(repo.createRunInput.Targets)
	if !reflect.DeepEqual(got, []int64{22, 11}) {
		t.Fatalf("snapshot target ids = %#v, want selected order [22 11]", got)
	}
}
