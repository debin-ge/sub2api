package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGrokExecutionFractionalEditAndExtendedOutput(t *testing.T) {
	for _, op := range []string{VideoOperationEdit, VideoOperationExtend} {
		t.Run(op, func(t *testing.T) {
			req := VideoCreateRequest{Model: "grok-imagine-video", Operation: op, Grok: &GrokVideoOptions{Source: grokVerifiedSourceForTest(8.25)}}
			if op == VideoOperationExtend {
				req.Seconds = 10
			}
			r, spec, err := resolveVideoExecutionSpec(req, 12, VideoProviderGrok, nil)
			require.NoError(t, err)
			billable := 8.25
			output := 8.25
			if op == VideoOperationExtend {
				billable = 10
				output = 18.25
			}
			require.Equal(t, billable, spec.BillableSeconds)
			require.Equal(t, "720p", r.Resolution)
			attrs := VideoPricingAttributes{Seconds: r.Seconds, BillableSeconds: spec.BillableSeconds, MaximumOutputSeconds: r.Seconds}
			units, err := videoPricingUnits(VideoBillingUnitSecond, attrs)
			require.NoError(t, err)
			require.Equal(t, billable, units)
			unit := VideoBillingUnitSecond
			accountID := int64(12)
			hash, err := HashVideoRequest(spec)
			require.NoError(t, err)
			task := &VideoTask{Provider: VideoProviderGrok, AccountID: &accountID, Operation: op, UpstreamModel: r.Model, BillingUnit: &unit, PriceSnapshot: map[string]any{"unit_price": 0.1, "customer_multiplier": 1.0}, RequestAttributes: map[string]any{"seconds": billable, "source_output_seconds": spec.SourceSeconds, "execution_spec": spec, "execution_spec_version": 2, "execution_spec_hash": hash}}
			provider := &ProviderVideoTask{Metadata: map[string]any{"seconds": output}}
			require.NoError(t, videoCheckObservedSpecification(task, provider.Metadata))
			actual, cost, err := videoActualCost(task, provider)
			require.NoError(t, err)
			require.Equal(t, billable, actual)
			require.InDelta(t, 0.1*billable, cost, 1e-8)
		})
	}
}
func TestGrokExecutionRequestHashIncludesNativeReferences(t *testing.T) {
	req := VideoSubmitRequest{Operation: VideoOperationGenerate, Model: "grok-imagine-video-1.5", Prompt: "test", Grok: &GrokVideoOptions{VoiceIDs: []string{"eve"}}}
	a, e := videoClientRequestHash(req)
	require.NoError(t, e)
	req.Grok.VoiceIDs = []string{"leo"}
	b, e := videoClientRequestHash(req)
	require.NoError(t, e)
	require.NotEqual(t, a, b)
	req.Grok.Keyframes = []GrokVideoKeyframe{{TimestampSeconds: 3, Image: VideoContentItem{Type: VideoContentTypeImage, URL: "https://example.com/a.png"}}}
	c, e := videoClientRequestHash(req)
	require.NoError(t, e)
	require.NotEqual(t, b, c)
	req.Grok.Keyframes[0].TimestampSeconds = 4
	d, e := videoClientRequestHash(req)
	require.NoError(t, e)
	require.NotEqual(t, c, d)
}

func TestGrokPricingInputTypeDoesNotDependOnTransport(t *testing.T) {
	reference := VideoContentItem{Type: VideoContentTypeImage, Role: VideoInputRoleFirstFrame, URL: "https://example.com/image.png"}
	request := VideoSubmitRequest{Operation: VideoOperationGenerate, Content: []VideoContentItem{reference}, Grok: &GrokVideoOptions{}}
	expected := videoPricingInputType(request)
	request.Content[0].URL = ""
	input := VideoInput{VideoInputManifestEntry: VideoInputManifestEntry{Role: VideoInputRoleFirstFrame}}
	request.Content[0].Input = &input
	request.Inputs = []VideoInput{input}
	require.Equal(t, string(VideoInputRoleReferenceImage), expected)
	require.Equal(t, expected, videoPricingInputType(request))
}
