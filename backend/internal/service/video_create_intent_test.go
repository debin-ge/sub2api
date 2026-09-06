package service

import (
	"encoding/json"
	"net/http"

	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoCreateIntentRequestClassification(t *testing.T) {
	for _, prefix := range []string{"", "/v1"} {
		for _, path := range []string{"/videos", "/videos/edits", "/videos/extensions", "/videos/characters"} {
			for _, mediaType := range []string{"application/json", "application/json; charset=utf-8"} {
				require.True(t, IsIdempotentJSONVideoCreate(http.MethodPost, prefix+path, mediaType, " key "))
				require.False(t, IsIdempotentJSONVideoCreate(http.MethodGet, prefix+path, mediaType, "key"))
				for _, key := range []string{"", " \t "} {
					require.False(t, IsIdempotentJSONVideoCreate(http.MethodPost, prefix+path, mediaType, key))
				}
			}
			for _, mediaType := range []string{"", "application/json-untrusted", "text/plain", "multipart/form-data; boundary=test"} {
				require.False(t, IsIdempotentJSONVideoCreate(http.MethodPost, prefix+path, mediaType, "key"))
			}
		}
	}
	for _, path := range []string{"/videos/generations", "/v1/videos/generations", "/v11/videos", "/v1/videos/", "/videos/generations/id", "/videos/characters/id", "/images/generations"} {
		require.False(t, IsIdempotentJSONVideoCreate(http.MethodPost, path, "application/json", "key"))
	}
}

func TestVideoCreateIntentCanonicalHashPreservesClientMeaning(t *testing.T) {
	first, err := CanonicalVideoCreateRequestHash([]byte(`{"model":"alias","options":{"seed":9007199254740993},"prompt":"test"}`))
	require.NoError(t, err)
	reordered, err := CanonicalVideoCreateRequestHash([]byte(`{ "prompt":"test", "options":{ "seed":9007199254740993 }, "model":"alias" }`))
	require.NoError(t, err)
	require.Equal(t, first, reordered)
	different, err := CanonicalVideoCreateRequestHash([]byte(`{"model":"alias","options":{"seed":9007199254740992},"prompt":"test"}`))
	require.NoError(t, err)
	require.NotEqual(t, first, different)
	for _, body := range []string{`null`, `[]`, `{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `{"x":NaN}`} {
		_, err := CanonicalVideoCreateRequestHash([]byte(body))
		require.ErrorIs(t, err, ErrVideoInvalidRequest)
	}
}

func TestVideoCreateIntentGuardAndStorageAreNotPublicJSON(t *testing.T) {
	intent := &VideoCreateIntent{ID: 3, State: VideoCreateIntentPrepared, LeaseOwner: "private-worker-owner"}
	body, err := json.Marshal(intent)
	require.NoError(t, err)
	require.NotContains(t, string(body), "private")
	guard, err := json.Marshal(VideoCreateIntentGuard{ID: 3, IdempotencyKey: "private-key", LeaseOwner: "private-owner"})
	require.NoError(t, err)
	require.NotContains(t, string(guard), "private")
	var decoded VideoCreateIntent
	require.NoError(t, json.Unmarshal([]byte(`{"id":3,"lease_owner":"private-worker-owner"}`), &decoded))
	require.Equal(t, "private-worker-owner", decoded.LeaseOwner)
}
