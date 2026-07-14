package store

const listSourceFilesBySessionStateQuery = `
	SELECT source_file_id, provider, session_id, current_path, device_id, inode,
		size_bytes, mtime_ns, parsed_offset, parser_version, active_generation,
		state, last_scanned_at_ms, last_error_class, updated_at_ms
	FROM source_files
	WHERE session_id = ? AND state = ?
	ORDER BY last_scanned_at_ms, source_file_id
	LIMIT ?`

const listDueSourcesQuery = `
	SELECT source_instance_id, source_type, scope_key, last_attempt_at_ms,
		last_success_at_ms, next_due_at_ms, consecutive_failures,
		last_error_class, freshness_state, cursor_version, updated_at_ms
	FROM source_state
	WHERE next_due_at_ms <= ?
	ORDER BY next_due_at_ms, source_instance_id
	LIMIT ?`

const listSourceAttemptsQuery = `
	SELECT request_id, source_instance_id, started_at_ms, finished_at_ms,
		outcome, http_status, error_class, payload_sha256
	FROM source_attempts
	WHERE source_instance_id = ?
	ORDER BY started_at_ms DESC, request_id DESC
	LIMIT ?`

const jobRunsQueryPrefix = `
	SELECT job_id, job_type, requested_by, priority, state, phase, source_file_id,
		resume_of_job_id, created_at_ms, started_at_ms, finished_at_ms,
		progress_current, progress_total, resume_generation, resume_offset,
		error_class, updated_at_ms
	FROM job_runs WHERE 1 = 1`

func buildJobRunsQuery(filter JobRunFilter, limit int) (string, []any) {
	query := jobRunsQueryPrefix
	arguments := make([]any, 0, 3)
	if filter.State != nil {
		query += ` AND state = ?`
		arguments = append(arguments, string(*filter.State))
	}
	if filter.SourceFileID != nil {
		query += ` AND source_file_id = ?`
		arguments = append(arguments, *filter.SourceFileID)
	}
	if filter.State == nil && filter.SourceFileID != nil {
		query += ` ORDER BY created_at_ms DESC, job_id DESC LIMIT ?`
	} else {
		query += ` ORDER BY updated_at_ms, priority DESC, job_id LIMIT ?`
	}
	arguments = append(arguments, limit)
	return query, arguments
}

const healthEventsQueryPrefix = `
	SELECT event_id, fingerprint, domain, severity, code, source_file_id, job_id,
		error_class, first_seen_at_ms, last_seen_at_ms, resolved_at_ms,
		occurrence_count, updated_at_ms
	FROM health_events WHERE 1 = 1`

func buildHealthEventsQuery(filter HealthEventFilter, limit int) (string, []any) {
	query := healthEventsQueryPrefix
	arguments := make([]any, 0, 5)
	if filter.Active != nil {
		if *filter.Active {
			query += ` AND resolved_at_ms IS NULL`
		} else {
			query += ` AND resolved_at_ms IS NOT NULL`
		}
	}
	if filter.Severity != nil {
		query += ` AND severity = ?`
		arguments = append(arguments, string(*filter.Severity))
	}
	if filter.SourceFileID != nil {
		query += ` AND source_file_id = ?`
		arguments = append(arguments, *filter.SourceFileID)
	}
	if filter.JobID != nil {
		query += ` AND job_id = ?`
		arguments = append(arguments, *filter.JobID)
	}
	query += ` ORDER BY last_seen_at_ms DESC, event_id LIMIT ?`
	arguments = append(arguments, limit)
	return query, arguments
}

const effectivePricingVersionQuery = `
	SELECT current.pricing_version,
		(
			SELECT MIN(next.effective_from_ms)
			FROM pricing_versions AS next
			WHERE next.source = current.source
			  AND next.currency = current.currency
			  AND next.effective_from_ms > current.effective_from_ms
		) AS effective_to_ms
	FROM pricing_versions AS current
	WHERE current.source = ? AND current.currency = ? AND current.effective_from_ms <= ?
	ORDER BY current.effective_from_ms DESC
	LIMIT 1`

const pricingVersionModelsQuery = `
	SELECT match_kind, model_pattern, priority,
		input_micros_per_million, cached_input_micros_per_million,
		output_micros_per_million
	FROM model_prices
	WHERE pricing_version = ?
	ORDER BY priority DESC, match_kind, model_pattern`
