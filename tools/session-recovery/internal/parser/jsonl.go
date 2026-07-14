package parser

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/sub2api/session-recovery/internal/model"
)

// JSONLParser JSONL 格式解析器
type JSONLParser struct{}

// NewJSONLParser 创建 JSONL 解析器
func NewJSONLParser() *JSONLParser {
	return &JSONLParser{}
}

// ParseFile 解析 JSONL 文件
func (p *JSONLParser) ParseFile(ctx context.Context, path string) (*model.Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	session := &model.Session{
		FilePath:  path,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	scanner := bufio.NewScanner(file)
	messageCount := 0
	var firstUserMessage string

	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		// 提取会话 ID
		if id, ok := entry["session_id"].(string); ok && session.ID == "" {
			session.ID = id
		}

		// 提取 Provider
		if provider, ok := entry["provider"].(string); ok && session.OriginalProvider == "" {
			session.OriginalProvider = provider
		}

		// 提取项目信息
		if project, ok := entry["project"].(string); ok && session.Project == "" {
			session.Project = project
		}

		// 提取分支信息
		if branch, ok := entry["branch"].(string); ok && session.Branch == "" {
			session.Branch = branch
		}

		// 提取消息
		if msgType, ok := entry["type"].(string); ok && msgType == "message" {
			if msg, ok := entry["message"].(map[string]interface{}); ok {
				if role, ok := msg["role"].(string); ok {
					messageCount++

					// 获取第一条用户消息
					if role == "user" && firstUserMessage == "" {
						if content, ok := msg["content"].(string); ok {
							firstUserMessage = content
						}
					}
				}
			}
		}

		// 提取时间戳
		if timestamp, ok := entry["timestamp"].(string); ok {
			if t, err := time.Parse(time.RFC3339, timestamp); err == nil {
				if session.CreatedAt.After(t) {
					session.CreatedAt = t
				}
				if session.UpdatedAt.Before(t) {
					session.UpdatedAt = t
				}
			}
		}
	}

	session.MessageCount = messageCount
	session.FirstMessage = firstUserMessage
	if len(firstUserMessage) > 200 {
		session.FirstMessage = firstUserMessage[:200]
	}

	return session, scanner.Err()
}
