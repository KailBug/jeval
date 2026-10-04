// Package codex reads explicitly selected Codex rollout JSONL snapshots.
// It never executes content or modifies the source file.
package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"jeval/engine/internal/model"
)

const MaxFileBytes = 16 * 1024 * 1024
const MaxEvents = 5000
const MaxTextBytes = 8192

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type payload struct {
	Type           string          `json:"type"`
	ID             string          `json:"id"`
	CWD            string          `json:"cwd"`
	Timestamp      string          `json:"timestamp"`
	CLIVersion     string          `json:"cli_version"`
	ForkedFromID   string          `json:"forked_from_id"`
	ParentThreadID string          `json:"parent_thread_id"`
	HistoryMode    string          `json:"history_mode"`
	TurnID         string          `json:"turn_id"`
	Item           json.RawMessage `json:"item"`
	Role           string          `json:"role"`
	Content        json.RawMessage `json:"content"`
	CallID         string          `json:"call_id"`
	Name           string          `json:"name"`
	Arguments      string          `json:"arguments"`
	Input          string          `json:"input"`
	Output         json.RawMessage `json:"output"`
	Message        string          `json:"message"`
}

func clip(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	for !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit] + "\n[内容预览已截断，请按来源行号查看原文件]", true
}

func textContent(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil && len(parts) > 0 {
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			if part.Type == "input_text" || part.Type == "output_text" || part.Type == "text" || part.Type == "Text" {
				texts = append(texts, part.Text)
			} else {
				texts = append(texts, "[未展开的内容块："+part.Type+"]")
			}
		}
		return strings.Join(texts, "\n")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	return string(raw)
}

func messageTitle(content string) string {
	title := strings.Join(strings.Fields(content), " ")
	runes := []rune(title)
	if len(runes) > 80 {
		return string(runes[:80]) + "…"
	}
	return title
}

// Paginated history can project the same message through item_completed. Match
// text within its turn, never globally: identical prompts in different turns
// are distinct messages. Scan first so either physical ordering is supported.
func messageKeys(lines [][]byte) (map[string]bool, []string) {
	keys := map[string]bool{}
	turns := make([]string, len(lines))
	turn := ""
	for i, raw := range lines {
		var row rolloutLine
		var p payload
		if json.Unmarshal(raw, &row) != nil || json.Unmarshal(row.Payload, &p) != nil {
			continue
		}
		if row.Type == "turn_context" || row.Type == "event_msg" && p.Type == "task_started" {
			turn = p.TurnID
			if turn == "" {
				turn = fmt.Sprintf("line:%d", i+1)
			}
		}
		turns[i] = turn
		if row.Type == "response_item" && p.Type == "message" {
			if p.ID != "" {
				keys["id:"+p.Role+":"+p.ID] = true
			}
			keys[messageKey(turn, p.Role, textContent(p.Content))] = true
		}
	}
	return keys, turns
}

func messageKey(turn, role, content string) string {
	return fmt.Sprintf("text:%s:%s:%x", turn, role, sha256.Sum256([]byte(content)))
}

// Read makes a bounded in-memory snapshot. Re-importing a path replaces that path's
// record; copies at different paths remain distinct, even if session IDs match.
func Read(path string) (model.Run, []model.Event, error) {
	var run model.Run
	if !filepath.IsAbs(path) || len(path) > 2048 || !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return run, nil, fmt.Errorf("请选择绝对路径下的 .jsonl 文件")
	}
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return run, nil, fmt.Errorf("无法读取所选文件：%w", err)
	}
	if len(path) > 2048 {
		return run, nil, fmt.Errorf("来源路径超过 2048 字节上限")
	}
	f, err := os.Open(path)
	if err != nil {
		return run, nil, fmt.Errorf("无法打开所选文件：%w", err)
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() {
		return run, nil, fmt.Errorf("请选择普通文件")
	}
	if stat.Size() > MaxFileBytes {
		return run, nil, fmt.Errorf("文件超过 16 MiB 导入上限")
	}
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return run, nil, fmt.Errorf("读取文件失败：%w", err)
	}
	if len(data) > MaxFileBytes {
		return run, nil, fmt.Errorf("文件超过 16 MiB 导入上限")
	}
	if !utf8.Valid(data) {
		return run, nil, fmt.Errorf("记录必须使用 UTF-8 编码")
	}
	identity := filepath.Clean(path)
	if runtime.GOOS == "windows" {
		identity = strings.ToLower(identity)
	}
	id := fmt.Sprintf("codex-%x", sha256.Sum256([]byte(identity)))
	info := &model.ImportInfo{File: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), Warnings: []model.ImportWarning{}}
	run = model.Run{ID: id, Title: filepath.Base(path), Project: "未提供项目", Source: "Codex", Status: "unknown", ImportInfo: info}
	warn := func(line int, message string) {
		info.WarningCount++
		if len(info.Warnings) < 30 {
			info.Warnings = append(info.Warnings, model.ImportWarning{Line: line, Message: message})
		}
	}
	stamp := func(value string, line int) *string {
		if value == "" {
			return nil
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			warn(line, "时间戳无效，显示为未知")
			return nil
		}
		return &value
	}
	events := make([]model.Event, 0)
	calls := map[string]string{}
	resultCalls := map[int]string{}
	hasMeta, hasTitle := false, false
	if bytes.Count(data, []byte{'\n'}) >= 50000 {
		return model.Run{}, nil, fmt.Errorf("文件超过 50000 行导入上限")
	}
	lines := bytes.Split(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), []byte{'\n'})
	canonical, turns := messageKeys(lines)
	for index, raw := range lines {
		line := index + 1
		if len(bytes.TrimSpace(raw)) == 0 {
			continue
		}
		var item rolloutLine
		var p payload
		if json.Unmarshal(raw, &item) != nil || len(item.Payload) == 0 || string(item.Payload) == "null" || json.Unmarshal(item.Payload, &p) != nil {
			warn(line, "无法解析此行（可能是写入中的半行），已跳过")
			continue
		}
		event := model.Event{ID: fmt.Sprintf("%s:%d", id, line), RunID: id,
			Evidence: model.EvidenceRef{SourceID: id, Location: path, Line: line}}
		switch item.Type {
		case "session_meta":
			if hasMeta || p.ID == "" || len(p.ID) > 256 {
				return model.Run{}, nil, fmt.Errorf("需要唯一且有效的 session_meta.id")
			}
			if p.HistoryMode != "" && p.HistoryMode != "classic" && p.HistoryMode != "paginated" {
				return model.Run{}, nil, fmt.Errorf("暂不支持此 history_mode：支持 classic / paginated rollout")
			}
			hasMeta = true
			info.SessionID = p.ID
			info.HistoryMode = p.HistoryMode
			if info.HistoryMode == "" {
				info.HistoryMode = "classic"
			}
			info.CLIVersion, _ = clip(p.CLIVersion, 128)
			info.ForkedFromID, _ = clip(p.ForkedFromID, 256)
			info.ParentThreadID, _ = clip(p.ParentThreadID, 256)
			if p.CWD != "" {
				run.Project, _ = clip(p.CWD, 512)
			}
			run.StartedAt = stamp(p.Timestamp, line)
			if run.StartedAt == nil {
				run.StartedAt = stamp(item.Timestamp, line)
			}
			continue
		case "response_item":
			switch p.Type {
			case "message":
				if p.Role != "user" && p.Role != "assistant" && p.Role != "system" && p.Role != "developer" {
					warn(line, "不支持的消息角色，已跳过")
					continue
				}
				event.Kind, event.Role, event.Title, event.Content = "message", p.Role, "消息", textContent(p.Content)
			case "function_call", "custom_tool_call":
				event.Kind, event.Role, event.Title, event.Content = "tool_call", "assistant", p.Name, p.Arguments
				if p.Type == "custom_tool_call" {
					event.Content = p.Input
				}
				if p.CallID == "" {
					warn(line, "工具调用缺少 call_id")
				} else if _, exists := calls[p.CallID]; exists {
					return model.Run{}, nil, fmt.Errorf("第 %d 行 call_id 重复，无法可靠关联工具结果", line)
				} else {
					calls[p.CallID] = event.ID
				}
			case "function_call_output", "custom_tool_call_output":
				event.Kind, event.Role, event.Title, event.Content = "tool_result", "tool", "工具结果", textContent(p.Output)
				resultCalls[len(events)] = p.CallID
			case "reasoning":
				// Internal/encrypted reasoning is not a user-visible transcript item.
				continue
			default:
				warn(line, "尚未映射的 response_item，已跳过")
				continue
			}
		case "event_msg":
			switch p.Type {
			case "user_message", "agent_message", "token_count", "thread_settings_applied":
				// Canonical message content comes from response_item, avoiding duplicates.
				continue
			case "item_completed":
				var item payload
				if json.Unmarshal(p.Item, &item) != nil {
					warn(line, "无效的 item_completed.item，已跳过")
					continue
				}
				if item.Type == "Reasoning" {
					continue
				}
				role := ""
				if item.Type == "UserMessage" {
					role = "user"
				}
				if item.Type == "AgentMessage" {
					role = "assistant"
				}
				if role == "" {
					warn(line, "尚未映射的 item_completed 类型，已跳过")
					continue
				}
				content := textContent(item.Content)
				turn := p.TurnID
				if turn == "" {
					turn = turns[index]
				}
				if item.ID != "" && canonical["id:"+role+":"+item.ID] || canonical[messageKey(turn, role, content)] {
					continue
				}
				event.Kind, event.Role, event.Title, event.Content = "message", role, "消息", content
			case "task_started", "task_complete", "turn_aborted":
				event.Kind, event.Role, event.Title, event.Content = "lifecycle", "system", "回合状态 · "+p.Type, string(item.Payload)
			case "error":
				event.Kind, event.Role, event.Title, event.Content = "error", "system", "来源错误", p.Message
			default:
				warn(line, "尚未映射的 event_msg，已跳过")
				continue
			}
		case "turn_context", "world_state", "token_usage_record":
			continue
		default:
			warn(line, "尚未映射的记录类型，已跳过")
			continue
		}
		if len(events) >= MaxEvents {
			return model.Run{}, nil, fmt.Errorf("文件超过 5000 个事件导入上限")
		}
		if event.Kind == "message" && event.Role == "user" && !hasTitle && strings.TrimSpace(event.Content) != "" {
			run.Title, hasTitle = messageTitle(event.Content), true
		}
		event.Sequence = len(events) + 1
		event.Timestamp = stamp(item.Timestamp, line)
		if event.Title == "" {
			event.Title = "工具调用"
		}
		event.Title, _ = clip(event.Title, 256)
		var truncated bool
		event.Content, truncated = clip(event.Content, MaxTextBytes)
		if truncated {
			warn(line, "长内容仅保留前 8 KiB 预览，原文仍在来源文件中")
		}
		events = append(events, event)
	}
	if !hasMeta {
		return model.Run{}, nil, fmt.Errorf("未找到 session_meta：不是受支持的 Codex rollout 文件")
	}
	for index, callID := range resultCalls {
		if parent, ok := calls[callID]; ok {
			events[index].ParentID = &parent
		} else {
			warn(events[index].Evidence.Line, "工具结果未找到对应调用")
		}
	}
	if len(events) == 0 {
		warn(1, "此文件没有可展示的消息或工具事件")
	}
	run.EventCount = len(events)
	return run, events, nil
}
