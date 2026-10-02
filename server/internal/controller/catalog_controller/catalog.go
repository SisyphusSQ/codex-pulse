package catalog_controller

import (
	"github.com/labstack/echo/v5"

	apphttp "github.com/SisyphusSQ/codex-pulse/server/internal/http"
	"github.com/SisyphusSQ/codex-pulse/server/internal/models/vo"
	catalog_srv "github.com/SisyphusSQ/codex-pulse/server/internal/service/catalog_srv"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Catalog struct{ service *catalog_srv.Catalog }

func NewCatalog(service *catalog_srv.Catalog) *Catalog { return &Catalog{service: service} }
func (s *Catalog) Register(e *echo.Echo)               { e.GET("/api/v1/catalog", s.Current) }
func (s *Catalog) Current(c *echo.Context) error {
	if len(c.Request().URL.Query()) != 0 {
		return utils.ErrBadParamInput
	}
	out, err := s.service.Current(c.Request().Context(), apphttp.Principal(c))
	if err != nil {
		return err
	}
	return vo.CommSuccResp(c, out)
}
