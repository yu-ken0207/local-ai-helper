package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/yu-ken0207/local-ai-helper/internal/config"
	"github.com/yu-ken0207/local-ai-helper/internal/engine"
)

// 從手機下載模型。下載在小幫手這邊跑（不綁著手機的連線），手機鎖螢幕、App 關掉都不會中斷；
// 手機回來再問進度就好。

// Download 一個下載工作
type Download struct {
	ID     string `json:"id"`
	Model  string `json:"model"`
	Engine string `json:"engine"` // ollama、lmstudio
	// Status downloading、done、failed、cancelled
	Status    string `json:"status"`
	Completed int64  `json:"completed"`
	Total     int64  `json:"total"`
	// Code 失敗的原因代碼（not_found：沒有這個模型；engine_error：其他）
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
	// Cancellable 能不能取消（LM Studio 沒有取消下載的 API）
	Cancellable bool       `json:"cancellable"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`

	cancel context.CancelFunc
}

type downloads struct {
	mu   sync.Mutex
	jobs []*Download
}

// keepFinished 下載完、失敗的工作留多久讓手機看到結果
const keepFinished = time.Hour

// pollEvery LM Studio 下載進度多久問一次
var pollEvery = time.Second

// list 目前的工作（複製一份，順便清掉太舊的）
func (d *downloads) list() []Download {
	d.mu.Lock()
	defer d.mu.Unlock()
	kept := d.jobs[:0]
	out := []Download{}
	for _, j := range d.jobs {
		if j.FinishedAt != nil && time.Since(*j.FinishedAt) > keepFinished {
			continue
		}
		kept = append(kept, j)
		out = append(out, *j)
	}
	d.jobs = kept
	return out
}

func (d *downloads) update(j *Download, fn func(j *Download)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	fn(j)
}

func (d *downloads) finish(j *Download, err error) {
	d.update(j, func(j *Download) {
		now := time.Now()
		j.FinishedAt = &now
		j.cancel = nil
		switch {
		case err == nil:
			j.Status = "done"
			if j.Total > 0 {
				j.Completed = j.Total
			}
		case errors.Is(err, context.Canceled):
			j.Status = "cancelled"
		default:
			j.Status, j.Error, j.Code = "failed", err.Error(), "engine_error"
			if notFound(err.Error()) {
				j.Code = "not_found"
			}
		}
	})
}

// notFound 引擎說沒有這個模型（名稱打錯）
func notFound(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "file does not exist") || strings.Contains(msg, "not found") || strings.Contains(msg, "no model")
}

// downloadEngines 這台電腦上可以下載模型的引擎（Ollama 優先）
func (s *Server) downloadEngines(engines []engine.Info) []string {
	out := []string{}
	for _, e := range engines {
		if e.OK && (e.Kind == engine.KindOllama || e.Kind == engine.KindLMStudio) {
			out = append(out, e.Kind)
		}
	}
	return out
}

func (s *Server) listDownloads(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"downloads": s.dl.list()})
}

func (s *Server) startDownload(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string `json:"model"`
		Engine string `json:"engine"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid body")
		return
	}
	body.Model = strings.TrimSpace(body.Model)
	if body.Model == "" || len(body.Model) > 300 {
		writeError(w, http.StatusBadRequest, "bad_request", "model name is required")
		return
	}
	available := s.downloadEngines(s.Engines(r.Context()))
	if body.Engine == "" && len(available) > 0 {
		body.Engine = available[0]
	}
	ok := false
	for _, e := range available {
		ok = ok || e == body.Engine
	}
	if !ok {
		writeError(w, http.StatusConflict, "no_download_engine", "no engine on this computer can download models")
		return
	}

	s.dl.mu.Lock()
	for _, j := range s.dl.jobs {
		if j.Model == body.Model && j.Engine == body.Engine && j.Status == "downloading" {
			cp := *j
			s.dl.mu.Unlock()
			writeJSON(w, http.StatusOK, cp)
			return
		}
	}
	// 下載不跟著手機的連線走：手機斷線也繼續
	ctx, cancel := context.WithCancel(context.Background())
	j := &Download{ID: config.NewID(6), Model: body.Model, Engine: body.Engine, Status: "downloading", StartedAt: time.Now(), cancel: cancel, Cancellable: body.Engine == engine.KindOllama}
	s.dl.jobs = append(s.dl.jobs, j)
	cp := *j
	s.dl.mu.Unlock()

	go func() {
		defer cancel()
		var err error
		if j.Engine == engine.KindOllama {
			err = s.pullOllama(ctx, j)
		} else {
			err = s.pullLMStudio(ctx, j)
		}
		s.dl.finish(j, err)
	}()
	writeJSON(w, http.StatusAccepted, cp)
}

// stopDownload 下載中的取消；已經結束的從清單拿掉
func (s *Server) stopDownload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s.dl.mu.Lock()
	defer s.dl.mu.Unlock()
	for i, j := range s.dl.jobs {
		if j.ID != id {
			continue
		}
		if j.Status == "downloading" {
			if !j.Cancellable || j.cancel == nil {
				writeError(w, http.StatusNotImplemented, "unsupported", "this engine cannot cancel a download")
				return
			}
			j.cancel()
		} else {
			s.dl.jobs = append(s.dl.jobs[:i], s.dl.jobs[i+1:]...)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "no such download")
}

// pullOllama 用 Ollama 的 /api/pull 下載；每一層各自回報進度，加起來就是總進度
func (s *Server) pullOllama(ctx context.Context, j *Download) error {
	data, _ := json.Marshal(map[string]any{"model": j.Model, "stream": true})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(s.OllamaURL, "/")+"/api/pull", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("ollama returned %s", resp.Status)
	}
	type layer struct{ total, completed int64 }
	layers := map[string]layer{}
	order := []string{}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var line struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
			Error     string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		if line.Error != "" {
			return errors.New(line.Error)
		}
		if line.Digest != "" && line.Total > 0 {
			if _, seen := layers[line.Digest]; !seen {
				order = append(order, line.Digest)
			}
			layers[line.Digest] = layer{line.Total, line.Completed}
			var total, completed int64
			for _, d := range order {
				total += layers[d].total
				completed += layers[d].completed
			}
			s.dl.update(j, func(j *Download) { j.Total, j.Completed = total, completed })
		}
		if line.Status == "success" {
			return nil
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.New("download ended before it finished")
}

// pullLMStudio 叫 LM Studio 下載，每秒問一次進度
func (s *Server) pullLMStudio(ctx context.Context, j *Download) error {
	if s.Registry == nil || s.Registry.LMStudio == nil {
		return errors.New("LM Studio is not available")
	}
	lm := s.Registry.LMStudio
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	st, err := lm.StartDownload(c, j.Model)
	cancel()
	if err != nil {
		return err
	}
	if st.Status == "already_downloaded" {
		return nil
	}
	s.dl.update(j, func(j *Download) { j.Total = st.Total })
	failures := 0
	for {
		switch st.Status {
		case "completed":
			return nil
		case "failed":
			return errors.New("LM Studio could not download this model")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollEvery):
		}
		c, cancel := context.WithTimeout(ctx, 10*time.Second)
		next, err := lm.DownloadProgress(c, st.JobID)
		cancel()
		if err != nil {
			// LM Studio 偶爾忙不過來：連續失敗才放棄
			if failures++; failures >= 30 {
				return err
			}
			continue
		}
		failures = 0
		next.JobID = st.JobID
		st = next
		s.dl.update(j, func(j *Download) {
			if st.Total > 0 {
				j.Total = st.Total
			}
			j.Completed = st.Downloaded
		})
	}
}
