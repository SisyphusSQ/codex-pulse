package statistics_controller

import (
	"github.com/labstack/echo/v5"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
)

func (s *Statistics) Totals(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Totals(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}

func (s *Statistics) Annual(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Annual(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Activity(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Activity(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Top(c *echo.Context) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Top(c.Request().Context(), apphttp.Principal(c), q)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
func (s *Statistics) Providers(c *echo.Context) error { return s.breakdown(c, false) }
func (s *Statistics) Models(c *echo.Context) error    { return s.breakdown(c, true) }
func (s *Statistics) breakdown(c *echo.Context, models bool) error {
	q, err := statisticsQuery(c)
	if err != nil {
		return err
	}
	out, err := s.service.Breakdown(c.Request().Context(), apphttp.Principal(c), q, models)
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
