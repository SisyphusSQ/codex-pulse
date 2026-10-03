package quota_srv

import (
	"encoding/hex"
	"net/url"
	"slices"
	"uuid"

	quota_dto "github.com/SisyphusSQ/codex-pulse/server/internal/models/dto/quota_dto"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

func ParseQuery(values url.Values) (quota_dto.Query, error) {
	var q quota_dto.Query
	for key, value := range values {
		if !slices.Contains([]string{"provider", "account_key", "client_id"}, key) || len(value) != 1 || len(value[0]) > 128 {
			return q, utils.ErrBadParamInput
		}
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
