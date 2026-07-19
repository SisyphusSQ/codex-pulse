package appserver

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestThreadListerPaginatesStateDBWithoutPersistingPreview(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	rollout := filepath.Join(home, "sessions", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(rollout), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rollout, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalRollout, err := filepath.EvalSymlinks(rollout)
	if err != nil {
		t.Fatal(err)
	}

	name := "真实标题"
	path := rollout
	next := "next-page"
	rpc := &fakeThreadRPC{responses: []threadListResult{
		{
			Data: []threadRecord{{
				ID: "thread-1", Name: &name, Preview: "private prompt must not escape",
				CWD: "/workspace/project", CreatedAt: 100, UpdatedAt: 200, RecencyAt: int64Pointer(250), Path: &path,
			}},
			NextCursor: &next,
		},
		{Data: []threadRecord{{ID: "thread-2", Preview: "another private prompt", CWD: "/workspace/other", CreatedAt: 300, UpdatedAt: 400}}},
	}}

	result, err := NewThreadLister(rpc, ThreadListerOptions{PageSize: 1}).List(context.Background(), home)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Threads) != 2 || len(rpc.calls) != 2 {
		t.Fatalf("result=%+v calls=%+v", result, rpc.calls)
	}
	for index, call := range rpc.calls {
		if call.Method != "thread/list" || !call.Params.UseStateDBOnly || call.Params.Limit != 1 ||
			call.Params.SortKey != "recency_at" || call.Params.Cursor != []string{"", next}[index] {
			t.Fatalf("call %d = %+v", index, call)
		}
	}
	if result.Threads[0].Name == nil || *result.Threads[0].Name != name ||
		result.Threads[0].RolloutPath == nil || *result.Threads[0].RolloutPath != canonicalRollout {
		t.Fatalf("first metadata = %+v", result.Threads[0])
	}
	if result.Threads[0].CreatedAtMS != 100_000 || result.Threads[0].UpdatedAtMS != 200_000 ||
		result.Threads[0].RecencyAtMS == nil || *result.Threads[0].RecencyAtMS != 250_000 {
		t.Fatalf("timestamp normalization failed: %+v", result.Threads[0])
	}
	if result.Threads[1].Name != nil || result.Threads[1].RolloutPath != nil {
		t.Fatalf("missing name/path should stay absent: %+v", result.Threads[1])
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(encoded), "private prompt") || stringContains(string(encoded), "preview") {
		t.Fatalf("metadata output leaked preview field or content: %s", encoded)
	}
}

func TestThreadListerRejectsRolloutPathsOutsideConfirmedHome(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	outside := filepath.Join(root, "outside.jsonl")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rpc := &fakeThreadRPC{responses: []threadListResult{{Data: []threadRecord{{
		ID: "thread-1", CWD: "/workspace", CreatedAt: 1, UpdatedAt: 2, Path: &outside,
	}}}}}

	result, err := NewThreadLister(rpc, ThreadListerOptions{}).List(context.Background(), home)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(result.Threads) != 1 || result.Threads[0].RolloutPath != nil {
		t.Fatalf("outside path escaped home fence: %+v", result)
	}
	if !reflect.DeepEqual(result.Diagnostics, []MetadataDiagnostic{{Code: "rollout_path_outside_home", ThreadOrdinal: 0}}) {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
	encoded, err := json.Marshal(result.Diagnostics)
	if err != nil {
		t.Fatal(err)
	}
	if stringContains(string(encoded), root) {
		t.Fatalf("diagnostic leaked filesystem path: %s", encoded)
	}
}

func TestThreadListerStopsOnRepeatedCursor(t *testing.T) {
	t.Parallel()

	repeated := "same"
	rpc := &fakeThreadRPC{responses: []threadListResult{
		{NextCursor: &repeated},
		{NextCursor: &repeated},
	}}
	_, err := NewThreadLister(rpc, ThreadListerOptions{}).List(context.Background(), t.TempDir())
	if err == nil || err.Error() != "thread/list returned a repeated cursor" {
		t.Fatalf("List() error = %v", err)
	}
}

type fakeThreadRPC struct {
	responses []threadListResult
	calls     []threadCall
}

type threadCall struct {
	Method string
	Params threadListParams
}

func (rpc *fakeThreadRPC) Call(_ context.Context, method string, params any, result any) error {
	typedParams := params.(threadListParams)
	rpc.calls = append(rpc.calls, threadCall{Method: method, Params: typedParams})
	response := rpc.responses[len(rpc.calls)-1]
	reflect.ValueOf(result).Elem().Set(reflect.ValueOf(response))
	return nil
}

func int64Pointer(value int64) *int64 {
	return &value
}

func stringContains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}
