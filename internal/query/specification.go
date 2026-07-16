package query

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

type filterRule struct {
	operators map[FilterOperator]struct{}
}

// Specification 是构造后不再变化的 endpoint query allowlist。
type Specification struct {
	defaultLimit int
	maxLimit     int
	maxRangeDays int
	sortFields   map[string]struct{}
	filterFields map[string]filterRule
	defaultSort  []SortTerm
	tieBreaker   SortTerm
}

// NewSpecification 验证并深复制 endpoint 配置，阻止调用方后续 mutation 漂移 contract。
func NewSpecification(config SpecificationConfig) (Specification, error) {
	if config.MaxLimit < 1 || config.MaxLimit > HardMaxPageLimit ||
		config.DefaultLimit < 1 || config.DefaultLimit > config.MaxLimit {
		return Specification{}, fmt.Errorf("%w: page limits", ErrInvalidSpecification)
	}
	if config.MaxRangeDays < 1 || config.MaxRangeDays > HardMaxRangeDays {
		return Specification{}, fmt.Errorf("%w: date range limit", ErrInvalidSpecification)
	}

	sortFields := make(map[string]struct{}, len(config.SortFields))
	for _, field := range config.SortFields {
		if !validField(field) {
			return Specification{}, fmt.Errorf("%w: sort field", ErrInvalidSpecification)
		}
		if _, exists := sortFields[field]; exists {
			return Specification{}, fmt.Errorf("%w: duplicate sort field", ErrInvalidSpecification)
		}
		sortFields[field] = struct{}{}
	}
	if len(sortFields) == 0 || !validSortTerm(config.TieBreaker, sortFields) {
		return Specification{}, fmt.Errorf("%w: tie breaker", ErrInvalidSpecification)
	}

	defaultSort, err := validateConfiguredSort(config.DefaultSort, sortFields, config.TieBreaker)
	if err != nil {
		return Specification{}, err
	}

	filterFields := make(map[string]filterRule, len(config.FilterFields))
	for _, configured := range config.FilterFields {
		if !validField(configured.Field) || len(configured.Operators) == 0 {
			return Specification{}, fmt.Errorf("%w: filter field", ErrInvalidSpecification)
		}
		if _, exists := filterFields[configured.Field]; exists {
			return Specification{}, fmt.Errorf("%w: duplicate filter field", ErrInvalidSpecification)
		}
		operators := make(map[FilterOperator]struct{}, len(configured.Operators))
		for _, operator := range configured.Operators {
			if !validFilterOperator(operator) {
				return Specification{}, fmt.Errorf("%w: filter operator", ErrInvalidSpecification)
			}
			if _, exists := operators[operator]; exists {
				return Specification{}, fmt.Errorf("%w: duplicate filter operator", ErrInvalidSpecification)
			}
			operators[operator] = struct{}{}
		}
		filterFields[configured.Field] = filterRule{operators: operators}
	}

	return Specification{
		defaultLimit: config.DefaultLimit,
		maxLimit:     config.MaxLimit,
		maxRangeDays: config.MaxRangeDays,
		sortFields:   sortFields,
		filterFields: filterFields,
		defaultSort:  defaultSort,
		tieBreaker:   config.TieBreaker,
	}, nil
}

// Validate 归一化 request，并保证返回值不引用调用方可变 slice 或 cursor 指针。
func (spec Specification) Validate(ctx context.Context, request Request) (ValidatedRequest, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ValidatedRequest{}, err
	}
	if spec.defaultLimit < 1 || len(spec.sortFields) == 0 {
		return ValidatedRequest{}, fmt.Errorf("%w: uninitialized", ErrInvalidSpecification)
	}

	page, err := spec.validatePage(request.Page)
	if err != nil {
		return ValidatedRequest{}, err
	}
	sortTerms, err := spec.validateSort(request.Sort)
	if err != nil {
		return ValidatedRequest{}, err
	}
	filters, err := spec.validateFilters(ctx, request.Filters)
	if err != nil {
		return ValidatedRequest{}, err
	}
	var timeRange *UTCTimeRange
	if request.TimeRange != nil {
		timeRange, err = normalizeLocalDateRange(*request.TimeRange, spec.maxRangeDays)
		if err != nil {
			return ValidatedRequest{}, err
		}
	}

	return ValidatedRequest{
		Page: page, Sort: sortTerms, Filters: filters, TimeRange: timeRange,
	}, nil
}

func (spec Specification) validatePage(page PageRequest) (PageRequest, error) {
	limit := page.Limit
	if limit == 0 {
		limit = spec.defaultLimit
	}
	if limit < 1 || limit > spec.maxLimit {
		return PageRequest{}, validationFailure("page.limit")
	}
	validated := PageRequest{Limit: limit}
	if page.Cursor == nil {
		return validated, nil
	}
	if !validCursor(*page.Cursor) {
		return PageRequest{}, validationFailure("page.cursor")
	}
	cursor := *page.Cursor
	validated.Cursor = &cursor
	return validated, nil
}

func (spec Specification) validateSort(input []SortTerm) ([]SortTerm, error) {
	terms := input
	if len(terms) == 0 {
		terms = spec.defaultSort
	}
	if len(terms) > maxSortTerms {
		return nil, validationFailure("sort")
	}
	validated := make([]SortTerm, 0, len(terms)+1)
	seen := make(map[string]struct{}, len(terms)+1)
	for _, term := range terms {
		if _, allowed := spec.sortFields[term.Field]; !allowed {
			return nil, validationFailure("sort.field")
		}
		if !validSortDirection(term.Direction) {
			return nil, validationFailure("sort.direction")
		}
		if _, exists := seen[term.Field]; exists {
			return nil, validationFailure("sort.field")
		}
		seen[term.Field] = struct{}{}
		validated = append(validated, term)
	}
	if _, exists := seen[spec.tieBreaker.Field]; !exists {
		if len(validated) == maxSortTerms {
			return nil, validationFailure("sort")
		}
		validated = append(validated, spec.tieBreaker)
	}
	return validated, nil
}

func (spec Specification) validateFilters(ctx context.Context, input []FilterTerm) ([]FilterTerm, error) {
	if len(input) > maxFilterTerms {
		return nil, validationFailure("filters")
	}
	validated := make([]FilterTerm, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, filter := range input {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rule, allowed := spec.filterFields[filter.Field]
		if !allowed {
			return nil, validationFailure("filters.field")
		}
		if _, allowed = rule.operators[filter.Operator]; !allowed {
			return nil, validationFailure("filters.operator")
		}
		key := filter.Field + "\x00" + string(filter.Operator)
		if _, exists := seen[key]; exists {
			return nil, validationFailure("filters")
		}
		seen[key] = struct{}{}
		if !validFilterArity(filter.Operator, len(filter.Values)) {
			return nil, validationFailure("filters.values")
		}
		values := make([]string, len(filter.Values))
		for index, value := range filter.Values {
			if !validFilterValue(value) {
				return nil, validationFailure("filters.values")
			}
			values[index] = value
		}
		validated = append(validated, FilterTerm{
			Field: filter.Field, Operator: filter.Operator, Values: values,
		})
	}
	return validated, nil
}

func validateConfiguredSort(
	input []SortTerm,
	allowed map[string]struct{},
	tieBreaker SortTerm,
) ([]SortTerm, error) {
	if len(input) == 0 || len(input) > maxSortTerms {
		return nil, fmt.Errorf("%w: default sort", ErrInvalidSpecification)
	}
	result := make([]SortTerm, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	hasTieBreaker := false
	for _, term := range input {
		if !validSortTerm(term, allowed) {
			return nil, fmt.Errorf("%w: default sort", ErrInvalidSpecification)
		}
		if _, exists := seen[term.Field]; exists {
			return nil, fmt.Errorf("%w: duplicate default sort", ErrInvalidSpecification)
		}
		seen[term.Field] = struct{}{}
		if term.Field == tieBreaker.Field {
			if term.Direction != tieBreaker.Direction {
				return nil, fmt.Errorf("%w: default tie breaker", ErrInvalidSpecification)
			}
			hasTieBreaker = true
		}
		result = append(result, term)
	}
	if !hasTieBreaker {
		if len(result) == maxSortTerms {
			return nil, fmt.Errorf("%w: default sort lacks tie breaker", ErrInvalidSpecification)
		}
		result = append(result, tieBreaker)
	}
	return result, nil
}

func validSortTerm(term SortTerm, allowed map[string]struct{}) bool {
	_, exists := allowed[term.Field]
	return exists && validSortDirection(term.Direction)
}

func validSortDirection(value SortDirection) bool {
	return value == SortAscending || value == SortDescending
}

func validFilterOperator(value FilterOperator) bool {
	switch value {
	case FilterEqual, FilterNotEqual, FilterIn, FilterGreaterThan, FilterAtLeast,
		FilterLessThan, FilterAtMost, FilterContains, FilterIsNull, FilterIsNotNull:
		return true
	default:
		return false
	}
}

func validFilterArity(operator FilterOperator, count int) bool {
	switch operator {
	case FilterIsNull, FilterIsNotNull:
		return count == 0
	case FilterIn:
		return count >= 1 && count <= maxFilterValues
	default:
		return count == 1
	}
}

func validField(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for index := 1; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func validCursor(value string) bool {
	if len(value) < 1 || len(value) > maxCursorLength {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func validFilterValue(value string) bool {
	return len(value) >= 1 && len(value) <= maxFilterValueSize &&
		utf8.ValidString(value) && strings.TrimSpace(value) != ""
}
