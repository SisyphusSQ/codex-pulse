package subscription_controller

import (
	"github.com/labstack/echo/v5"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	subscription_vo "github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/subscription_vo"
	subscription_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/subscription_srv"
)

type Subscription struct {
	service *subscription_srv.Subscription
}

func NewSubscription(service *subscription_srv.Subscription) *Subscription {
	return &Subscription{service: service}
}
func (s *Subscription) Register(e *echo.Echo) {
	e.GET("/api/v1/accounts/:id/subscription", s.Get)
	e.POST("/api/v1/accounts/:id/subscription", s.Update)
}
func (s *Subscription) Get(c *echo.Context) error {
	out, err := s.service.Get(c.Request().Context(), apphttp.Principal(c), c.Param("id"))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Subscription) Update(c *echo.Context) error {
	var u subscription_vo.Update
	if err := apphttp.DecodeJSON(c, &u, 8192); err != nil {
		return err
	}
	out, err := s.service.Update(c.Request().Context(), apphttp.Principal(c), c.Param("id"), u)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
