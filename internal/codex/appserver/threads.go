package appserver

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	defaultThreadPageSize = 100
	maxThreadNameBytes    = 4096
)

type RPC interface {
	Call(ctx context.Context, method string, params any, result any) error
}

type ThreadListerOptions struct {
	PageSize int
}

type ThreadLister struct {
	rpc      RPC
	pageSize int
}

type ThreadMetadata struct {
	SessionID   string  `json:"session_id"`
	Name        *string `json:"name,omitempty"`
	CWD         string  `json:"cwd"`
	CreatedAtMS int64   `json:"created_at_ms"`
	UpdatedAtMS int64   `json:"updated_at_ms"`
	RecencyAtMS *int64  `json:"recency_at_ms,omitempty"`
	RolloutPath *string `json:"rollout_path,omitempty"`
}

type MetadataDiagnostic struct {
	Code          string `json:"code"`
	ThreadOrdinal int    `json:"thread_ordinal"`
}

type ThreadList struct {
	Threads     []ThreadMetadata     `json:"threads"`
	Diagnostics []MetadataDiagnostic `json:"diagnostics,omitempty"`
}

type threadListParams struct {
	Cursor         string `json:"cursor,omitempty"`
	Limit          int    `json:"limit"`
	SortKey        string `json:"sortKey"`
	UseStateDBOnly bool   `json:"useStateDbOnly"`
}

type threadListResult struct {
	Data       []threadRecord `json:"data"`
	NextCursor *string        `json:"nextCursor"`
}

type threadRecord struct {
	ID        string  `json:"id"`
	Name      *string `json:"name"`
	Preview   string  `json:"preview"`
	CWD       string  `json:"cwd"`
	CreatedAt int64   `json:"createdAt"`
	UpdatedAt int64   `json:"updatedAt"`
	RecencyAt *int64  `json:"recencyAt"`
	Path      *string `json:"path"`
}

func NewThreadLister(rpc RPC, options ThreadListerOptions) *ThreadLister {
	pageSize := options.PageSize
	if pageSize <= 0 {
		pageSize = defaultThreadPageSize
	}
	return &ThreadLister{rpc: rpc, pageSize: pageSize}
}

func (lister *ThreadLister) List(ctx context.Context, confirmedHome string) (ThreadList, error) {
	if lister == nil || lister.rpc == nil || lister.pageSize <= 0 {
		return ThreadList{}, errors.New("invalid thread lister")
	}
	canonicalHome, err := filepath.EvalSymlinks(confirmedHome)
	if err != nil {
		return ThreadList{}, fmt.Errorf("confirm Codex Home: %w", err)
	}
	canonicalHome, err = filepath.Abs(canonicalHome)
	if err != nil {
		return ThreadList{}, fmt.Errorf("canonicalize Codex Home: %w", err)
	}

	var output ThreadList
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		if err := ctx.Err(); err != nil {
			return ThreadList{}, err
		}
		var response threadListResult
		err := lister.rpc.Call(ctx, "thread/list", threadListParams{
			Cursor: cursor, Limit: lister.pageSize, SortKey: "recency_at", UseStateDBOnly: true,
		}, &response)
		if err != nil {
			return ThreadList{}, fmt.Errorf("list App Server threads: %w", err)
		}
		for _, thread := range response.Data {
			ordinal := len(output.Threads)
			metadata, diagnostics, ok := normalizeThread(thread, canonicalHome, ordinal)
			output.Diagnostics = append(output.Diagnostics, diagnostics...)
			if ok {
				output.Threads = append(output.Threads, metadata)
			}
		}
		if response.NextCursor == nil || *response.NextCursor == "" {
			return output, nil
		}
		if _, exists := seenCursors[*response.NextCursor]; exists {
			return ThreadList{}, errors.New("thread/list returned a repeated cursor")
		}
		seenCursors[*response.NextCursor] = struct{}{}
		cursor = *response.NextCursor
	}
}

func normalizeThread(thread threadRecord, canonicalHome string, ordinal int) (ThreadMetadata, []MetadataDiagnostic, bool) {
	if thread.ID == "" || len(thread.ID) > 4096 || !utf8.ValidString(thread.ID) ||
		thread.CreatedAt <= 0 || thread.UpdatedAt <= 0 ||
		thread.CreatedAt > math.MaxInt64/1000 || thread.UpdatedAt > math.MaxInt64/1000 {
		return ThreadMetadata{}, []MetadataDiagnostic{{Code: "invalid_thread_metadata", ThreadOrdinal: ordinal}}, false
	}
	metadata := ThreadMetadata{
		SessionID:   thread.ID,
		CWD:         thread.CWD,
		CreatedAtMS: thread.CreatedAt * 1000,
		UpdatedAtMS: thread.UpdatedAt * 1000,
	}
	var diagnostics []MetadataDiagnostic
	if thread.Name != nil {
		trimmed := strings.TrimSpace(*thread.Name)
		if trimmed != "" && len(trimmed) <= maxThreadNameBytes && utf8.ValidString(trimmed) {
			metadata.Name = &trimmed
		} else {
			diagnostics = append(diagnostics, MetadataDiagnostic{Code: "invalid_thread_name", ThreadOrdinal: ordinal})
		}
	}
	if thread.RecencyAt != nil {
		if *thread.RecencyAt > 0 && *thread.RecencyAt <= math.MaxInt64/1000 {
			value := *thread.RecencyAt * 1000
			metadata.RecencyAtMS = &value
		} else {
			diagnostics = append(diagnostics, MetadataDiagnostic{Code: "invalid_thread_recency", ThreadOrdinal: ordinal})
		}
	}
	if thread.Path != nil && *thread.Path != "" {
		path, code := validatedRolloutPath(canonicalHome, *thread.Path)
		if code == "" {
			metadata.RolloutPath = &path
		} else {
			diagnostics = append(diagnostics, MetadataDiagnostic{Code: code, ThreadOrdinal: ordinal})
		}
	}
	return metadata, diagnostics, true
}

func validatedRolloutPath(canonicalHome, candidate string) (string, string) {
	if !filepath.IsAbs(candidate) || filepath.Ext(candidate) != ".jsonl" {
		return "", "rollout_path_invalid"
	}
	canonicalCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "rollout_path_unavailable"
	}
	canonicalCandidate, err = filepath.Abs(canonicalCandidate)
	if err != nil {
		return "", "rollout_path_invalid"
	}
	relative, err := filepath.Rel(canonicalHome, canonicalCandidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "rollout_path_outside_home"
	}
	return canonicalCandidate, ""
}
