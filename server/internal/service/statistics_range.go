package service

import (
	"encoding/hex"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
	"uuid"

	"github.com/SisyphusSQ/codex-pulse/server/internal/models/dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// ParseStatisticsQuery 固定日期/时区/排序和分页语义；动态 SQL 名称不来自参数。
func ParseStatisticsQuery(values url.Values, now time.Time) (q dto.StatisticsQuery, err error) {
	q.TimeZone = values.Get("time_zone")
	if q.TimeZone == "" {
		q.TimeZone = "Asia/Shanghai"
	}
	if q.TimeZone == "Local" || strings.HasPrefix(q.TimeZone, "/") {
		return q, utils.ErrBadParamInput
	}
	q.Location, err = time.LoadLocation(q.TimeZone)
	if err != nil {
		return q, utils.ErrBadParamInput
	}
	local := now.In(q.Location)
	end := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, q.Location).AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -30)
	if values.Get("start_at_ms") != "" || values.Get("end_at_ms") != "" {
		if values.Get("start_date") != "" || values.Get("end_date_exclusive") != "" {
			return q, utils.ErrBadParamInput
		}
		q.StartAtMS, err = strconv.ParseInt(values.Get("start_at_ms"), 10, 64)
		if err != nil {
			return q, utils.ErrBadParamInput
		}
		q.EndAtMS, err = strconv.ParseInt(values.Get("end_at_ms"), 10, 64)
		if err != nil {
			return q, utils.ErrBadParamInput
		}
	} else {
		if text := values.Get("start_date"); text != "" {
			start, err = time.ParseInLocation(time.DateOnly, text, q.Location)
			if err != nil {
				return q, utils.ErrBadParamInput
			}
		}
		if text := values.Get("end_date_exclusive"); text != "" {
			end, err = time.ParseInLocation(time.DateOnly, text, q.Location)
			if err != nil {
				return q, utils.ErrBadParamInput
			}
		}
		q.StartAtMS = start.UnixMilli()
		q.EndAtMS = end.UnixMilli()
	}
	if q.StartAtMS < 0 || q.EndAtMS <= q.StartAtMS || q.EndAtMS > 9007199254740991 || q.EndAtMS-q.StartAtMS > int64(3660*24*time.Hour/time.Millisecond) {
		return q, utils.ErrBadParamInput
	}
	q.Provider = values.Get("provider")
	if q.Provider != "" && !slices.Contains([]string{"codex", "cursor", "grok"}, q.Provider) {
		return q, utils.ErrBadParamInput
	}
	q.Model = values.Get("model")
	if !utf8.ValidString(q.Model) || utf8.RuneCountInString(q.Model) > 256 || strings.ContainsRune(q.Model, 0) {
		return q, utils.ErrBadParamInput
	}
	q.ClientID = values.Get("client_id")
	if q.ClientID != "" {
		if _, err := uuid.Parse(q.ClientID); err != nil {
			return q, utils.ErrBadParamInput
		}
	}
	q.ProjectID = values.Get("project_id")
	if q.ProjectID != "" && !statisticsKey(q.ProjectID) {
		return q, utils.ErrBadParamInput
	}
	q.Search = values.Get("search")
	if !utf8.ValidString(q.Search) || utf8.RuneCountInString(q.Search) > 256 || strings.ContainsRune(q.Search, 0) {
		return q, utils.ErrBadParamInput
	}
	q.Sort = values.Get("sort")
	if q.Sort == "" {
		q.Sort = "activity"
	}
	if !slices.Contains([]string{"activity", "title", "tokens", "cost", "name"}, q.Sort) {
		return q, utils.ErrBadParamInput
	}
	q.Direction = values.Get("direction")
	if q.Direction == "" {
		q.Direction = "desc"
	}
	if q.Direction != "asc" && q.Direction != "desc" {
		return q, utils.ErrBadParamInput
	}
	q.Page = 1
	q.Limit = 25
	for name, target := range map[string]*int{"page": &q.Page, "limit": &q.Limit} {
		if text := values.Get(name); text != "" {
			value, err := strconv.Atoi(text)
			if err != nil {
				return q, utils.ErrBadParamInput
			}
			*target = value
		}
	}
	if q.Page < 1 || q.Page > 100000 || q.Limit < 1 || q.Limit > 100 {
		return q, utils.ErrBadParamInput
	}
	return
}
func statisticsKey(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil
}
