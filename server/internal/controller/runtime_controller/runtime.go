package runtime_controller

import (
	"github.com/labstack/echo/v5"

	reportingv1 "github.com/SisyphusSQ/codex-pulse/api/codexpulse/reporting/v1"
	"github.com/SisyphusSQ/codex-pulse/server/docs/sqls/schema"
	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo/runtime_vo"
	"github.com/SisyphusSQ/codex-pulse/server/internal/service/access_srv"
	"github.com/SisyphusSQ/codex-pulse/server/vars"
)

func Register(e *echo.Echo) {
	e.GET("/api/v1/version", func(c *echo.Context) error {
		if err := access_srv.RequireAdmin(apphttp.Principal(c)); err != nil {
			return err
		}
		return vo.CommSuccResp(c, runtime_vo.VersionView{Version: vars.AppVersion, Commit: vars.GitCommit, BuiltAt: vars.BuildTime, ReportingProtocol: reportingv1.Version, ThroughputCapsule: 1, Schema: schema.Version})
	})
}
