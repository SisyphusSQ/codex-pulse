package app

import "runtime"

const (
	appName       = "Codex Pulse"
	defaultLocale = "zh-CN"
)

// BootstrapInfo contains the non-sensitive metadata needed to render the
// application shell. Business and user data belong to later services.
type BootstrapInfo struct {
	Name     string `json:"name"`
	Locale   string `json:"locale"`
	Platform string `json:"platform"`
}

// Service exposes the minimal application-shell contract to generated Wails
// bindings. It intentionally has no external dependencies or mutable state.
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func (*Service) Bootstrap() BootstrapInfo {
	return BootstrapInfo{
		Name:     appName,
		Locale:   defaultLocale,
		Platform: runtime.GOOS,
	}
}
