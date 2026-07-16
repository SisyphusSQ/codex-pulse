package query

import (
	"reflect"
	"testing"
)

func TestNewResponseMetaDistinguishesCompletePartialAndUnavailable(t *testing.T) {
	complete, err := NewResponseMeta(ResponseComplete, &PageInfo{Limit: 50}, nil)
	if err != nil {
		t.Fatalf("NewResponseMeta(complete) error = %v", err)
	}
	if complete.Version != ContractVersion || complete.Status != ResponseComplete ||
		complete.Issues == nil || len(complete.Issues) != 0 || complete.Page == nil || complete.Page.Limit != 50 {
		t.Fatalf("complete meta = %#v", complete)
	}

	nextCursor := "next_cursor-1"
	partial, err := NewResponseMeta(ResponsePartial, &PageInfo{
		Limit: 50, HasMore: true, NextCursor: &nextCursor,
	}, []ErrorCode{ErrorPartial})
	if err != nil {
		t.Fatalf("NewResponseMeta(partial) error = %v", err)
	}
	wantPartialIssues := []Issue{{
		Code: ErrorPartial, MessageKey: "query.error.partial", Retryable: true,
	}}
	if partial.Status != ResponsePartial || !reflect.DeepEqual(partial.Issues, wantPartialIssues) ||
		partial.Page == nil || !partial.Page.HasMore || partial.Page.NextCursor == nil ||
		*partial.Page.NextCursor != nextCursor {
		t.Fatalf("partial meta = %#v", partial)
	}

	unavailable, err := NewResponseMeta(ResponseUnavailable, nil, []ErrorCode{ErrorUnavailable})
	if err != nil {
		t.Fatalf("NewResponseMeta(unavailable) error = %v", err)
	}
	if unavailable.Status != ResponseUnavailable || len(unavailable.Issues) != 1 || unavailable.Page != nil {
		t.Fatalf("unavailable meta = %#v", unavailable)
	}

	nextCursor = "changed"
	wantCursor := "next_cursor-1"
	if partial.Page.NextCursor == nil || *partial.Page.NextCursor != wantCursor {
		t.Fatalf("response meta retained caller cursor pointer: %#v", partial.Page)
	}
}

func TestNewResponseMetaRejectsInconsistentState(t *testing.T) {
	tests := []struct {
		name   string
		status ResponseStatus
		page   *PageInfo
		issues []ErrorCode
		field  string
	}{
		{name: "invalid status", status: "loading", field: "response.status"},
		{name: "complete with issue", status: ResponseComplete, issues: []ErrorCode{ErrorPartial}, field: "response.issues"},
		{name: "partial without issue", status: ResponsePartial, field: "response.issues"},
		{name: "unavailable without issue", status: ResponseUnavailable, field: "response.issues"},
		{name: "partial with fatal issue", status: ResponsePartial, issues: []ErrorCode{ErrorInternal}, field: "response.issues"},
		{name: "unavailable with partial only", status: ResponseUnavailable, issues: []ErrorCode{ErrorPartial}, field: "response.issues"},
		{name: "duplicate issue", status: ResponsePartial, issues: []ErrorCode{ErrorPartial, ErrorPartial}, field: "response.issues"},
		{name: "page missing limit", status: ResponseComplete, page: &PageInfo{}, field: "response.page.limit"},
		{name: "page over hard max", status: ResponseComplete, page: &PageInfo{Limit: 501}, field: "response.page.limit"},
		{name: "has more without cursor", status: ResponseComplete, page: &PageInfo{Limit: 50, HasMore: true}, field: "response.page.nextCursor"},
		{name: "cursor without has more", status: ResponseComplete, page: &PageInfo{Limit: 50, NextCursor: stringPointer("next")}, field: "response.page.hasMore"},
		{name: "unsafe cursor", status: ResponseComplete, page: &PageInfo{Limit: 50, HasMore: true, NextCursor: stringPointer("row id")}, field: "response.page.nextCursor"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewResponseMeta(test.status, test.page, test.issues)
			assertValidationField(t, err, test.field)
		})
	}
}

func TestNewResponseMetaIsDeterministicAcrossRestart(t *testing.T) {
	first, err := NewResponseMeta(ResponsePartial, nil, []ErrorCode{ErrorPartial, ErrorUnavailable})
	if err != nil {
		t.Fatalf("NewResponseMeta(first) error = %v", err)
	}
	second, err := NewResponseMeta(ResponsePartial, nil, []ErrorCode{ErrorPartial, ErrorUnavailable})
	if err != nil {
		t.Fatalf("NewResponseMeta(second) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("response meta drifted: first=%#v second=%#v", first, second)
	}
}
