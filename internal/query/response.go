package query

// NewResponseMeta 验证 response 状态和分页不变量，并复制 cursor/issue 输入。
func NewResponseMeta(
	status ResponseStatus,
	page *PageInfo,
	issueCodes []ErrorCode,
) (ResponseMeta, error) {
	if !validResponseStatus(status) {
		return ResponseMeta{}, validationFailure("response.status")
	}
	if status == ResponseComplete && len(issueCodes) != 0 ||
		status != ResponseComplete && len(issueCodes) == 0 {
		return ResponseMeta{}, validationFailure("response.issues")
	}
	issues := make([]Issue, 0, len(issueCodes))
	seen := make(map[ErrorCode]struct{}, len(issueCodes))
	for _, code := range issueCodes {
		if code != ErrorPartial && code != ErrorUnavailable {
			return ResponseMeta{}, validationFailure("response.issues")
		}
		if status == ResponseUnavailable && code != ErrorUnavailable {
			return ResponseMeta{}, validationFailure("response.issues")
		}
		if _, exists := seen[code]; exists {
			return ResponseMeta{}, validationFailure("response.issues")
		}
		seen[code] = struct{}{}
		detail := errorDetail(code)
		issues = append(issues, Issue{
			Code: detail.Code, MessageKey: detail.MessageKey, Retryable: detail.Retryable,
		})
	}
	validatedPage, err := validatePageInfo(page)
	if err != nil {
		return ResponseMeta{}, err
	}
	return ResponseMeta{
		Version: ContractVersion, Status: status, Page: validatedPage, Issues: issues,
	}, nil
}

func validatePageInfo(page *PageInfo) (*PageInfo, error) {
	if page == nil {
		return nil, nil
	}
	if page.Limit < 1 || page.Limit > HardMaxPageLimit {
		return nil, validationFailure("response.page.limit")
	}
	if page.HasMore && page.NextCursor == nil {
		return nil, validationFailure("response.page.nextCursor")
	}
	if !page.HasMore && page.NextCursor != nil {
		return nil, validationFailure("response.page.hasMore")
	}
	validated := &PageInfo{Limit: page.Limit, HasMore: page.HasMore}
	if page.NextCursor != nil {
		if !validCursor(*page.NextCursor) {
			return nil, validationFailure("response.page.nextCursor")
		}
		cursor := *page.NextCursor
		validated.NextCursor = &cursor
	}
	return validated, nil
}

func validResponseStatus(value ResponseStatus) bool {
	return value == ResponseComplete || value == ResponsePartial || value == ResponseUnavailable
}
