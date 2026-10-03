package reporting_do

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/v2"
)

// ObservationDigest 与接收端原来的比较语义一致：关联及接收时间不属于不可变观测。
func ObservationDigest(row QuotaObservation) (string, error) {
	row.AccountKey = nil
	row.AssociationScope = nil
	row.HistoryOrigin = ""
	row.ReceivedAtMS = 0
	body, err := json.Marshal(row)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
