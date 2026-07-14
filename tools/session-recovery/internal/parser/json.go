package parser

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/sub2api/session-recovery/internal/model"
)

// JSONParser JSON 格式解析器
type JSONParser struct{}

// NewJSONParser 创建 JSON 解析器
func NewJSONParser() *JSONParser {
	return &JSONParser{}
}

// ParseFile 解析 JSON 文件
func (p *JSONParser) ParseFile(ctx context.Context, path string) (*model.Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, err
	}

	session := &model.Session{
		FilePath:  path,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// 提取会话 ID
	if id, ok := sessionData["id"].(string); ok {
		session.ID = id
	} else if id, ok := sessionData["session_id"].(string); ok {
		session.ID = id
	}

	// 提取 Provider
	if provider, ok := sessionData["provider"].(string); ok {
		session.OriginalProvider = provider
	}

	// 提取项目信息
	if project, ok := sessionData["project"].(string); ok {
		session.Project = project
	}

	// 提取分支信息
	if branch, ok := sessionData["branch"].(string); ok {
		session.Branch = branch
	}

	// 提取消息
	if messages, ok := sessionData["messages"].([]interface{}); ok {
		session.MessageCount = len(messages)

		// 获取第一条用户消息
		for _, msg := range messages {
			if msgMap, ok := msg.(map[string]interface{}); ok {
				if role, ok := msgMap["role"].(string); ok && role == "user" {
					if content, ok := msgMap["content"].(string); ok {
						session.FirstMessage = content
						if len(content) > 200 {
							session.FirstMessage = content[:200]
						}
						break
					}
				}
			}
		}
	}

	// 提取时间戳
	if createdAt, ok := sessionData["created_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			session.CreatedAt = t
		}
	}

	if updatedAt, ok := sessionData["updated_at"].(string); ok {
		if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
			session.UpdatedAt = t
		}
	}

	return session, nil
}
