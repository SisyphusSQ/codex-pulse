package quota_controller

import (
	"github.com/labstack/echo/v5"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	quota_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/quota_srv"
)

type Quota struct{ service *quota_srv.Quota }

func NewQuota(service *quota_srv.Quota) *Quota { return &Quota{service: service} }
func (s *Quota) Register(e *echo.Echo)         { e.GET("/api/v1/quotas", s.Current) }
func (s *Quota) Current(c *echo.Context) error {
	q, err := quota_srv.ParseQuery(c.Request().URL.Query())
	if err != nil {
		return err
	}
	out, err := s.service.Current(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
