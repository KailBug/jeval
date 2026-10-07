package protocol

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
)

type UpdateStatus struct {
	ID      string              `json:"id"`
	RunID   string              `json:"runId"`
	State   string              `json:"state"`
	Phase   string              `json:"phase"`
	Message string              `json:"message"`
	Run     *model.Run          `json:"run,omitempty"`
	Report  *codex.UpdateReport `json:"report,omitempty"`
}

func (s *scanService) updateActive() bool {
	return s.update != nil && (s.update.State == "running" || s.update.State == "cancelling")
}

// Called under mu. Cancellation and publication use that same lock: after a
// cancel response is accepted no new snapshot can be committed by this job.
func (s *scanService) dispatchUpdate(req Request) Response {
	var params struct {
		RunID string `json:"runId"`
		ID    string `json:"id"`
	}
	if json.Unmarshal(req.Params, &params) != nil {
		return failure(req.ID, "INVALID_PARAMS", "需要更新任务参数")
	}
	res := Response{Type: "response", Version: Version, ID: req.ID}
	if req.Method != "codex.update.start" {
		if params.ID == "" || len(params.ID) > 128 {
			return failure(req.ID, "INVALID_PARAMS", "需要更新任务 ID")
		}
		if s.update == nil || s.update.ID != params.ID {
			return failure(req.ID, "NOT_FOUND", "更新任务不存在或引擎已重启")
		}
		if req.Method == "codex.update.cancel" && s.updateActive() {
			s.cancel()
			s.update.State, s.update.Message = "cancelling", "正在取消更新；已保存的快照仍可浏览"
		}
		res.Result = *s.update
		return res
	}
	if params.RunID == "" || len(params.RunID) > 128 {
		return failure(req.ID, "INVALID_PARAMS", "需要已登记记录 ID")
	}
	if s.active() {
		return failure(req.ID, "SCAN_BUSY", "请等待当前扫描或更新完成，或取消操作")
	}
	var selected *model.Run
	for i := range s.record.Runs {
		if s.record.Runs[i].ID == params.RunID {
			selected = &s.record.Runs[i]
			break
		}
	}
	if selected == nil || selected.Demo || selected.Source != "Codex" || selected.ImportInfo == nil {
		return failure(req.ID, "NOT_FOUND", "已登记 Codex 记录不存在")
	}
	if selected.ReadOnly {
		return failure(req.ID, "IMPORT_FAILED", "交换记录只读，请显式选择来源文件后重新导入")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return failure(req.ID, "UPDATE_FAILED", "无法创建更新任务")
	}
	s.update = &UpdateStatus{ID: fmt.Sprintf("%x", token), RunID: params.RunID, State: "running", Phase: "reading", Message: "正在核对来源；已保存的快照仍可浏览"}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	res.Result = *s.update
	go func(run model.Run, done chan struct{}) { defer cancel(); defer close(done); s.runUpdate(ctx, run) }(*selected, s.done)
	return res
}

func (s *scanService) updateBase(ctx context.Context, id string) (*codex.Checkpoint, *model.Record, error) {
	if s.store == nil {
		return nil, nil, nil
	}
	cp, err := s.store.Checkpoint(ctx, id)
	if err != nil || cp == nil {
		return cp, nil, err
	}
	record, err := s.store.Current(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	_, err = s.store.RestoreContents(ctx, &record)
	if err != nil {
		return nil, nil, err
	}
	return cp, &record, nil
}

func (s *scanService) runUpdate(ctx context.Context, previous model.Run) {
	cp, saved, err := s.updateBase(ctx, previous.ID)
	var run model.Run
	var events []model.Event
	var next *codex.Checkpoint
	var report codex.UpdateReport
	if err == nil {
		run, events, next, report, err = codex.ReadUpdate(ctx, previous.ImportInfo.File, cp, saved, func(phase string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.update.State == "running" {
				s.update.Phase = phase
				if phase == "parsing" {
					s.update.Message = "正在处理更新；已保存的快照仍可浏览"
				}
			}
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		s.update.State, s.update.Message = "cancelled", "更新已取消；已保存的快照保留"
		return
	}
	if err == nil && run.ID != previous.ID {
		err = fmt.Errorf("来源路径身份发生变化，请重新选择文件")
	}
	if err == nil {
		s.update.Phase = "saving"
		_, err = s.publishRun(ctx, run, events, next)
	}
	if err != nil {
		s.update.State = "failed"
		// Bound diagnostics within the protocol's response budget.
		message := []rune(err.Error())
		if len(message) > 512 {
			message = message[:512]
		}
		s.update.Message = string(message) + "；已保存的快照保留"
		return
	}
	s.update.State, s.update.Phase = "completed", "done"
	s.update.Message = fmt.Sprintf("已更新记录 · %d 个事件", run.EventCount)
	if report.Mode == "unchanged" {
		s.update.Message = fmt.Sprintf("来源未变化 · %d 个事件", run.EventCount)
	}
	s.update.Run, s.update.Report = &run, &report
}
