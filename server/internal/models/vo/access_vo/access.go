package access_vo

type PairRequest struct {
	Code string `json:"code"`
	Mode string `json:"mode"`
}

type CreatePairingRequest struct {
	Purpose string `json:"purpose"`
	Name    string `json:"name"`
}

// PairingView 仅在受保护的签发响应中显示一次配对材料。
type PairingView struct {
	Code        string `json:"code"`
	Purpose     string `json:"purpose"`
	ExpiresAtMS int64  `json:"expires_at_ms"`
}

type SessionView struct {
	ClientID    string `json:"client_id"`
	Name        string `json:"name"`
	Purpose     string `json:"purpose"`
	CSRF        string `json:"csrf,omitempty"`
	ExpiresAtMS *int64 `json:"expires_at_ms"`
}

type CollectorPairView struct {
	ClientID        string `json:"client_id"`
	Credential      string `json:"credential"`
	ProtocolVersion int    `json:"protocol_version"`
}

type ClientView struct {
	ID               string `json:"id"`
	Purpose          string `json:"purpose"`
	Name             string `json:"name"`
	CreatedAtMS      int64  `json:"created_at_ms"`
	ExpiresAtMS      *int64 `json:"expires_at_ms"`
	RevokedAtMS      *int64 `json:"revoked_at_ms"`
	LastReceivedAtMS *int64 `json:"last_received_at_ms"`
}

type ClientListView struct {
	Clients []ClientView `json:"clients"`
}
