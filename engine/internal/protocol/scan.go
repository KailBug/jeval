package protocol

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"jeval/engine/internal/adapters/codex"
	"jeval/engine/internal/model"
)

const maxScanEntries = 10000
const maxScanFiles = 200
const maxScanDepth = 32

var errScanLimit = errors.New("扫描达到条目、文件或目录深度上限，请选择更小的目录")

type ScanIssue struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type ScanStatus struct {
	ID         string      `json:"id"`
	Root       string      `json:"root"`
	State      string      `json:"state"`
	Phase      string      `json:"phase"`
	Ready      int         `json:"ready"`
	Visited    int         `json:"visited"`
	Discovered int         `json:"discovered"`
	Imported   int         `json:"imported"`
	Updated    int         `json:"updated"`
	Failed     int         `json:"failed"`
	Skipped    int         `json:"skipped"`
	Issues     []ScanIssue `json:"issues"`
	Message    string      `json:"message"`
}

type ScanCandidate struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Project      string  `json:"project"`
	Path         string  `json:"path"`
	StartedAt    *string `json:"startedAt"`
	EventCount   int     `json:"eventCount"`
	WarningCount int     `json:"warningCount"`
	Preview      string  `json:"preview"`
	Existing     bool    `json:"existing"`
	Imported     bool    `json:"imported"`
	Error        string  `json:"error"`
	sha256       string
}

// Only one scan is active. Parsing runs outside mu; publication and queries share
// the lock so readers see either the old or complete new per-file snapshot.
type scanService struct {
	mu         sync.Mutex
	record     model.Record
	scan       *ScanStatus
	cancel     context.CancelFunc
	done       chan struct{}
	candidates []ScanCandidate
}

func (s *scanService) snapshot() ScanStatus {
	status := *s.scan
	status.Issues = append([]ScanIssue{}, status.Issues...)
	return status
}

func (s *scanService) active() bool {
	return s.scan != nil && (s.scan.State == "running" || s.scan.State == "cancelling")
}

func (s *scanService) dispatch(req Request) Response {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.ID == "" || len(req.ID) > 128 || req.Type != "request" || req.Version != Version {
		return dispatchRecord(req, &s.record)
	}
	res := Response{Type: "response", Version: Version, ID: req.ID}
	switch req.Method {
	case "codex.scan.start":
		if s.active() {
			return failure(req.ID, "SCAN_BUSY", "请等待当前扫描完成或取消扫描")
		}
		var params struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(req.Params, &params) != nil || !filepath.IsAbs(params.Path) || len(params.Path) > 2048 {
			return failure(req.ID, "INVALID_PARAMS", "请选择绝对路径下的目录（最多 2048 字节）")
		}
		root, err := filepath.EvalSymlinks(params.Path)
		if err != nil || len(root) > 2048 {
			return failure(req.ID, "SCAN_FAILED", "无法读取所选目录")
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			return failure(req.ID, "SCAN_FAILED", "请选择可读取的目录")
		}
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			return failure(req.ID, "SCAN_FAILED", "无法创建扫描 ID")
		}
		s.scan = &ScanStatus{ID: fmt.Sprintf("%x", id), Root: root, State: "running", Phase: "discovery", Issues: []ScanIssue{}, Message: "正在发现记录，尚未导入"}
		s.candidates = nil
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.done = make(chan struct{})
		res.Result = s.snapshot()
		go func(done chan struct{}) {
			defer cancel()
			s.run(ctx, root, done)
		}(s.done)
		return res
	case "codex.scan.status", "codex.scan.cancel", "codex.scan.candidates", "codex.scan.import":
		var params struct {
			ID     string   `json:"id"`
			IDs    []string `json:"ids"`
			Offset int      `json:"offset"`
			Limit  *int     `json:"limit"`
		}
		if json.Unmarshal(req.Params, &params) != nil || params.ID == "" || len(params.ID) > 128 {
			return failure(req.ID, "INVALID_PARAMS", "需要扫描 ID")
		}
		if s.scan == nil || s.scan.ID != params.ID {
			return failure(req.ID, "NOT_FOUND", "扫描不存在或引擎已重启")
		}
		if req.Method == "codex.scan.candidates" {
			limit := 50
			if params.Limit != nil {
				limit = *params.Limit
			}
			if params.Offset < 0 || params.Offset > 1000000000 || limit < 1 || limit > 100 {
				return failure(req.ID, "INVALID_PARAMS", "分页 offset 或 limit 无效")
			}
			items := append([]ScanCandidate{}, s.candidates...)
			for i := range items {
				for _, run := range s.record.Runs {
					if run.ID == items[i].ID {
						items[i].Existing = true
						break
					}
				}
			}
			res.Result = paginate(items, params.Offset, limit)
			return res
		}
		if req.Method == "codex.scan.import" {
			if s.active() {
				return failure(req.ID, "SCAN_BUSY", "请等待当前操作完成")
			}
			if len(params.IDs) == 0 || len(params.IDs) > maxScanFiles {
				return failure(req.ID, "INVALID_PARAMS", "请选择要导入的记录")
			}
			selection := make([]int, 0, len(params.IDs))
			seen := map[string]bool{}
			for _, id := range params.IDs {
				index := -1
				for i, candidate := range s.candidates {
					if candidate.ID == id && !candidate.Imported {
						index = i
						break
					}
				}
				if index < 0 || seen[id] {
					return failure(req.ID, "INVALID_PARAMS", "包含无效、重复或已导入的候选记录，请刷新选择")
				}
				seen[id] = true
				selection = append(selection, index)
			}
			files, events := 0, 0
			for _, run := range s.record.Runs {
				if !seen[run.ID] {
					events += run.EventCount
					if !run.Demo {
						files++
					}
				}
			}
			for _, index := range selection {
				files++
				events += s.candidates[index].EventCount
			}
			if files > 20 || events > 50000 {
				return failure(req.ID, "IMPORT_LIMIT", "所选记录超出本次启动 20 个文件或 50000 个事件的上限，请减少选择；本次未导入任何记录")
			}
			s.scan.Phase, s.scan.State, s.scan.Message = "import", "running", "正在导入所选记录"
			s.scan.Imported, s.scan.Updated, s.scan.Failed = 0, 0, 0
			s.scan.Issues = []ScanIssue{}
			ctx, cancel := context.WithCancel(context.Background())
			s.cancel, s.done = cancel, make(chan struct{})
			res.Result = s.snapshot()
			go func(done chan struct{}) { defer cancel(); s.importSelected(ctx, selection, done) }(s.done)
			return res
		}
		if req.Method == "codex.scan.cancel" && s.active() {
			s.cancel()
			s.scan.State = "cancelling"
			s.scan.Message = "正在取消当前操作"
		}
		res.Result = s.snapshot()
		return res
	case "codex.import":
		if s.active() {
			return failure(req.ID, "SCAN_BUSY", "请等待当前扫描完成或取消扫描")
		}
	case "shutdown":
		if s.active() {
			s.cancel()
		}
	}
	return dispatchRecord(req, &s.record)
}

func (s *scanService) close() {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}

func (s *scanService) issue(path string, err error) {
	s.scan.Failed++
	if len(s.scan.Issues) < 30 {
		// Bound filesystem diagnostics as well as filenames on the wire.
		runes := []rune(err.Error())
		if len(runes) > 512 {
			runes = append(runes[:512], '…')
		}
		p := []rune(path)
		if len(p) > 2048 {
			p = append(p[:2048], '…')
		}
		s.scan.Issues = append(s.scan.Issues, ScanIssue{Path: string(p), Message: string(runes)})
	}
}

func (s *scanService) run(ctx context.Context, root string, done chan struct{}) {
	defer close(done)
	err := s.walk(ctx, root, 0)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case ctx.Err() != nil:
		s.scan.State, s.scan.Message = "cancelled", "扫描已取消；可查看已发现的记录"
	case err != nil:
		s.scan.State, s.scan.Message = "limited", err.Error()
	default:
		s.scan.State, s.scan.Message = "completed", "扫描完成，请选择要导入的记录"
		if s.scan.Failed > 0 {
			s.scan.Message = "扫描完成，部分文件或目录读取失败"
		}
	}
}

// Read directories in bounded batches rather than materializing the entire tree.
// Symlinks (including Windows junctions) are skipped, so scans don't follow links
// into other directories. This is a local snapshot, not a filesystem transaction.
func (s *scanService) walk(ctx context.Context, path string, depth int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := os.Open(path)
	if err != nil {
		s.mu.Lock()
		s.issue(path, err)
		s.mu.Unlock()
		return nil
	}
	defer dir.Close()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := dir.ReadDir(64)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			s.mu.Lock()
			if s.scan.Visited >= maxScanEntries {
				s.mu.Unlock()
				return errScanLimit
			}
			s.scan.Visited++
			s.mu.Unlock()
			child := filepath.Join(path, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() && !entry.Type().IsRegular() {
				s.mu.Lock()
				s.scan.Skipped++
				s.mu.Unlock()
				continue
			}
			if len(child) > 2048 {
				s.mu.Lock()
				s.issue(child, errors.New("来源路径超过 2048 字节上限"))
				s.mu.Unlock()
				continue
			}
			if entry.IsDir() {
				if depth >= maxScanDepth {
					return errScanLimit
				}
				if err := s.walk(ctx, child, depth+1); err != nil {
					return err
				}
				continue
			}
			if !strings.EqualFold(filepath.Ext(child), ".jsonl") {
				continue
			}
			s.mu.Lock()
			if s.scan.Discovered >= maxScanFiles {
				s.mu.Unlock()
				return errScanLimit
			}
			s.scan.Discovered++
			s.mu.Unlock()
			run, events, err := codex.ReadContext(ctx, child)
			s.mu.Lock()
			// Cancel acknowledgements and publication serialize on mu. No file
			// can be committed after a cancellation has been acknowledged.
			if ctx.Err() != nil {
				s.mu.Unlock()
				return ctx.Err()
			}
			if err != nil {
				s.issue(child, err)
				s.mu.Unlock()
				continue
			}
			preview := ""
			for _, event := range events {
				if event.Kind == "message" {
					preview = event.Content
					break
				}
			}
			runes := []rune(preview)
			if len(runes) > 500 {
				preview = string(runes[:500]) + "…"
			}
			s.candidates = append(s.candidates, ScanCandidate{ID: run.ID, Title: run.Title, Project: run.Project, Path: run.ImportInfo.File, StartedAt: run.StartedAt, EventCount: run.EventCount, WarningCount: run.ImportInfo.WarningCount, Preview: preview, sha256: run.ImportInfo.SHA256})
			s.scan.Ready = len(s.candidates)
			s.mu.Unlock()
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			s.mu.Lock()
			s.issue(path, readErr)
			s.mu.Unlock()
			return nil
		}
	}
}

// Discovery retains only summaries. Selected files are re-read and checked
// against the discovered digest; changes require another scan and explicit choice.
func (s *scanService) importSelected(ctx context.Context, selection []int, done chan struct{}) {
	defer close(done)
	for _, index := range selection {
		if ctx.Err() != nil {
			break
		}
		s.mu.Lock()
		candidate := s.candidates[index]
		s.mu.Unlock()
		run, events, err := codex.ReadContext(ctx, candidate.Path)
		if err == nil && (run.ID != candidate.ID || run.ImportInfo.SHA256 != candidate.sha256) {
			err = errors.New("文件在扫描后发生变化，请重新扫描并确认")
		}
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			break
		}
		replaced := false
		if err == nil {
			replaced, err = replaceRun(&s.record, run, events)
		}
		if err != nil {
			s.issue(candidate.Path, err)
			s.candidates[index].Error = s.scan.Issues[len(s.scan.Issues)-1].Message
		} else {
			s.candidates[index].Imported, s.candidates[index].Error = true, ""
			if replaced {
				s.scan.Updated++
			} else {
				s.scan.Imported++
			}
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scan.State, s.scan.Message = "completed", "所选记录导入完成"
	if s.scan.Failed > 0 {
		s.scan.Message = "导入结束，部分所选记录未能导入"
	}
	if ctx.Err() != nil {
		s.scan.State, s.scan.Message = "cancelled", "导入已取消；已完成的记录保留"
	}
}
