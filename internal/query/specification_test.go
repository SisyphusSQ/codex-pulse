package query

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestSpecificationValidateNormalizesBoundedRequest(t *testing.T) {
	config := validSpecificationConfig()
	specification, err := NewSpecification(config)
	if err != nil {
		t.Fatalf("NewSpecification() error = %v", err)
	}

	cursor := "opaque_cursor-1"
	request := Request{
		Page: PageRequest{Cursor: &cursor},
		Sort: []SortTerm{{Field: "startedAt", Direction: SortDescending}},
		Filters: []FilterTerm{
			{Field: "projectId", Operator: FilterIn, Values: []string{"project-a", "project-b"}},
			{Field: "model", Operator: FilterIsNotNull},
		},
	}
	validated, err := specification.Validate(context.Background(), request)
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	wantSort := []SortTerm{
		{Field: "startedAt", Direction: SortDescending},
		{Field: "sessionId", Direction: SortDescending},
	}
	if validated.Page.Limit != 50 || validated.Page.Cursor == nil || *validated.Page.Cursor != cursor {
		t.Fatalf("validated page = %#v", validated.Page)
	}
	if !reflect.DeepEqual(validated.Sort, wantSort) {
		t.Fatalf("validated sort = %#v, want %#v", validated.Sort, wantSort)
	}
	wantFilters := []FilterTerm{
		{Field: "projectId", Operator: FilterIn, Values: []string{"project-a", "project-b"}},
		{Field: "model", Operator: FilterIsNotNull, Values: []string{}},
	}
	if !reflect.DeepEqual(validated.Filters, wantFilters) {
		t.Fatalf("validated filters = %#v, want %#v", validated.Filters, wantFilters)
	}

	// 测试 specification 与 validated request 都不引用调用方可变 slice。
	config.DefaultSort[0].Field = "model"
	config.SortFields[0] = "model"
	config.FilterFields[0].Operators[0] = FilterContains
	request.Sort[0].Field = "model"
	request.Filters[0].Values[0] = "changed"
	if !reflect.DeepEqual(validated.Sort, wantSort) || validated.Filters[0].Values[0] != "project-a" {
		t.Fatalf("validated request changed after caller mutation: %#v", validated)
	}

	replayed, err := specification.Validate(context.Background(), Request{
		Page: PageRequest{Cursor: &cursor},
		Sort: []SortTerm{{Field: "startedAt", Direction: SortDescending}},
		Filters: []FilterTerm{
			{Field: "projectId", Operator: FilterIn, Values: []string{"project-a", "project-b"}},
			{Field: "model", Operator: FilterIsNotNull},
		},
	})
	if err != nil || !reflect.DeepEqual(replayed, validated) {
		t.Fatalf("replayed request = %#v, %v; want %#v", replayed, err, validated)
	}
}

func TestSpecificationValidateUsesFrozenDefaultsAcrossRestart(t *testing.T) {
	first, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification(first) error = %v", err)
	}
	second, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification(second) error = %v", err)
	}

	request := Request{}
	firstResult, err := first.Validate(context.Background(), request)
	if err != nil {
		t.Fatalf("first.Validate() error = %v", err)
	}
	secondResult, err := second.Validate(context.Background(), request)
	if err != nil {
		t.Fatalf("second.Validate() error = %v", err)
	}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("restart validation drifted: first=%#v second=%#v", firstResult, secondResult)
	}
	if firstResult.Filters == nil || firstResult.Sort == nil {
		t.Fatalf("known-empty slices must be non-nil: %#v", firstResult)
	}
}

func TestSpecificationValidateRejectsInvalidRequest(t *testing.T) {
	specification, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification() error = %v", err)
	}

	tests := []struct {
		name    string
		request Request
		field   string
	}{
		{name: "negative limit", request: Request{Page: PageRequest{Limit: -1}}, field: "page.limit"},
		{name: "limit over endpoint max", request: Request{Page: PageRequest{Limit: 201}}, field: "page.limit"},
		{name: "blank cursor", request: Request{Page: PageRequest{Cursor: stringPointer("")}}, field: "page.cursor"},
		{name: "cursor with unsafe bytes", request: Request{Page: PageRequest{Cursor: stringPointer("row id")}}, field: "page.cursor"},
		{name: "oversize cursor", request: Request{Page: PageRequest{Cursor: stringPointer(strings.Repeat("a", 2049))}}, field: "page.cursor"},
		{name: "unknown sort", request: Request{Sort: []SortTerm{{Field: "sqlColumn", Direction: SortAscending}}}, field: "sort.field"},
		{name: "invalid direction", request: Request{Sort: []SortTerm{{Field: "startedAt", Direction: "sideways"}}}, field: "sort.direction"},
		{name: "duplicate sort", request: Request{Sort: []SortTerm{{Field: "startedAt", Direction: SortAscending}, {Field: "startedAt", Direction: SortDescending}}}, field: "sort.field"},
		{name: "too many sorts", request: Request{Sort: []SortTerm{{Field: "startedAt", Direction: SortAscending}, {Field: "model", Direction: SortAscending}, {Field: "projectId", Direction: SortAscending}, {Field: "sessionId", Direction: SortAscending}, {Field: "startedAt", Direction: SortDescending}}}, field: "sort"},
		{name: "unknown filter", request: Request{Filters: []FilterTerm{{Field: "rawSQL", Operator: FilterEqual, Values: []string{"x"}}}}, field: "filters.field"},
		{name: "unknown operator", request: Request{Filters: []FilterTerm{{Field: "projectId", Operator: FilterContains, Values: []string{"x"}}}}, field: "filters.operator"},
		{name: "equal missing value", request: Request{Filters: []FilterTerm{{Field: "projectId", Operator: FilterEqual}}}, field: "filters.values"},
		{name: "null with value", request: Request{Filters: []FilterTerm{{Field: "projectId", Operator: FilterIsNull, Values: []string{"x"}}}}, field: "filters.values"},
		{name: "blank filter value", request: Request{Filters: []FilterTerm{{Field: "projectId", Operator: FilterEqual, Values: []string{" "}}}}, field: "filters.values"},
		{name: "oversize filter value", request: Request{Filters: []FilterTerm{{Field: "projectId", Operator: FilterEqual, Values: []string{strings.Repeat("x", 257)}}}}, field: "filters.values"},
		{
			name: "duplicate filter",
			request: Request{Filters: []FilterTerm{
				{Field: "projectId", Operator: FilterEqual, Values: []string{"a"}},
				{Field: "projectId", Operator: FilterEqual, Values: []string{"b"}},
			}},
			field: "filters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := specification.Validate(context.Background(), test.request)
			assertValidationField(t, err, test.field)
		})
	}
}

func TestSpecificationValidateHonorsCancellation(t *testing.T) {
	specification, err := NewSpecification(validSpecificationConfig())
	if err != nil {
		t.Fatalf("NewSpecification() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = specification.Validate(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Validate(cancelled) error = %v, want context.Canceled", err)
	}
}

func TestNewSpecificationRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SpecificationConfig)
	}{
		{name: "default limit missing", mutate: func(config *SpecificationConfig) { config.DefaultLimit = 0 }},
		{name: "default over max", mutate: func(config *SpecificationConfig) { config.DefaultLimit = 201 }},
		{name: "max over hard cap", mutate: func(config *SpecificationConfig) { config.MaxLimit = 501 }},
		{name: "range over hard cap", mutate: func(config *SpecificationConfig) { config.MaxRangeDays = 3661 }},
		{name: "duplicate sort field", mutate: func(config *SpecificationConfig) { config.SortFields = append(config.SortFields, "model") }},
		{name: "invalid sort field", mutate: func(config *SpecificationConfig) { config.SortFields[0] = "db_column" }},
		{name: "tie breaker unavailable", mutate: func(config *SpecificationConfig) { config.TieBreaker.Field = "turnId" }},
		{name: "invalid tie breaker", mutate: func(config *SpecificationConfig) { config.TieBreaker.Direction = "sideways" }},
		{name: "default sort unavailable", mutate: func(config *SpecificationConfig) { config.DefaultSort[0].Field = "turnId" }},
		{name: "default tie breaker direction conflicts", mutate: func(config *SpecificationConfig) {
			config.DefaultSort = append(config.DefaultSort, SortTerm{
				Field: "sessionId", Direction: SortAscending,
			})
		}},
		{name: "default sort full without tie breaker", mutate: func(config *SpecificationConfig) {
			config.SortFields = append(config.SortFields, "turnId")
			config.DefaultSort = []SortTerm{
				{Field: "startedAt", Direction: SortDescending},
				{Field: "model", Direction: SortAscending},
				{Field: "projectId", Direction: SortAscending},
				{Field: "turnId", Direction: SortDescending},
			}
		}},
		{name: "duplicate filter field", mutate: func(config *SpecificationConfig) {
			config.FilterFields = append(config.FilterFields, config.FilterFields[0])
		}},
		{name: "filter without operators", mutate: func(config *SpecificationConfig) { config.FilterFields[0].Operators = nil }},
		{name: "duplicate filter operator", mutate: func(config *SpecificationConfig) {
			config.FilterFields[0].Operators = []FilterOperator{FilterEqual, FilterEqual}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := validSpecificationConfig()
			test.mutate(&config)
			_, err := NewSpecification(config)
			if !errors.Is(err, ErrInvalidSpecification) {
				t.Fatalf("NewSpecification() error = %v, want ErrInvalidSpecification", err)
			}
		})
	}
}

func validSpecificationConfig() SpecificationConfig {
	return SpecificationConfig{
		DefaultLimit: 50,
		MaxLimit:     200,
		MaxRangeDays: 366,
		SortFields:   []string{"startedAt", "model", "projectId", "sessionId"},
		FilterFields: []FilterField{
			{Field: "projectId", Operators: []FilterOperator{FilterEqual, FilterNotEqual, FilterIn, FilterIsNull, FilterIsNotNull}},
			{Field: "model", Operators: []FilterOperator{FilterEqual, FilterIn, FilterIsNull, FilterIsNotNull}},
		},
		DefaultSort: []SortTerm{{Field: "startedAt", Direction: SortDescending}},
		TieBreaker:  SortTerm{Field: "sessionId", Direction: SortDescending},
	}
}

func assertValidationField(t testing.TB, err error, field string) {
	t.Helper()
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
	var failure *Failure
	if !errors.As(err, &failure) || failure.Field() != field {
		t.Fatalf("error = %#v, want validation field %q", err, field)
	}
}

func stringPointer(value string) *string {
	return &value
}
