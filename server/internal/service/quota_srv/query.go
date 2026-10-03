package quota_srv

import (
	"encoding/hex"
	"net/url"
	"slices"
	"strconv"
	"uuid"

	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func ParseQuery(values url.Values) (quota_dto.Query, error) {
	var q quota_dto.Query
	for key, value := range values {
		if !slices.Contains([]string{"provider", "account_key", "client_id", "window_key", "view", "page", "limit"}, key) || len(value) != 1 || len(value[0]) > 128 {
			return q, utils.ErrBadParamInput
		}
	}
	q.WindowKey, q.View = values.Get("window_key"), values.Get("view")
	if q.WindowKey != "" {
		b, err := hex.DecodeString(q.WindowKey)
		if err != nil || len(b) != 32 {
			return q, utils.ErrBadParamInput
		}
	}
	if !slices.Contains([]string{"", "summary", "evidence"}, q.View) {
		return q, utils.ErrBadParamInput
	}
	q.Page, q.Limit = 1, 20
	for key, target := range map[string]*int{"page": &q.Page, "limit": &q.Limit} {
		if v := values.Get(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return q, utils.ErrBadParamInput
			}
			*target = n
		}
	}
	if q.Page > 100000 || q.Limit > 100 {
		return q, utils.ErrBadParamInput
	}
	if q.View == "evidence" && q.WindowKey == "" {
		return q, utils.ErrBadParamInput
	}
	q.Provider, q.AccountKey, q.ClientID = values.Get("provider"), values.Get("account_key"), values.Get("client_id")
	if q.Provider != "" && !slices.Contains([]string{"codex", "cursor", "grok"}, q.Provider) {
		return q, utils.ErrBadParamInput
	}
	if q.AccountKey != "" {
		if b, err := hex.DecodeString(q.AccountKey); err != nil || len(b) != 32 {
			return q, utils.ErrBadParamInput
		}
	}
	if q.ClientID != "" {
		id, err := uuid.Parse(q.ClientID)
		if err != nil || id.String() != q.ClientID {
			return q, utils.ErrBadParamInput
		}
	}
	return q, nil
}
