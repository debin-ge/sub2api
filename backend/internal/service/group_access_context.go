package service

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// NewGroupAccessProfileFromUser builds a detached authorization profile. The
// slice copy prevents downstream code from mutating the cached User snapshot.
func NewGroupAccessProfileFromUser(user *User) *GroupAccessProfile {
	if user == nil || user.ID <= 0 {
		return nil
	}
	return &GroupAccessProfile{
		UserID:         user.ID,
		IsVIP:          user.IsVIP,
		VIPAccessState: user.AccessState(),
		AllowedGroups:  append([]int64(nil), user.AllowedGroups...),
	}
}

func WithGroupAccessProfile(ctx context.Context, profile *GroupAccessProfile) context.Context {
	if ctx == nil || profile == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.GroupAccessProfile, profile)
}

func GroupAccessProfileFromContext(ctx context.Context) (*GroupAccessProfile, bool) {
	if ctx == nil {
		return nil, false
	}
	profile, ok := ctx.Value(ctxkey.GroupAccessProfile).(*GroupAccessProfile)
	return profile, ok && profile != nil && profile.UserID > 0
}
