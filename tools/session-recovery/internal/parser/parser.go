package parser

import (
	"context"

	"github.com/sub2api/session-recovery/internal/model"
)

// Parser 解析器接口
type Parser interface {
	// ParseFile 解析文件并返回会话信息
	ParseFile(ctx context.Context, path string) (*model.Session, error)
}
