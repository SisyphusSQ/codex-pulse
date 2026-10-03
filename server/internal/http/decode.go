package http

import (
	"encoding/json/v2"
	"io"
	"mime"
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// DecodeJSON 拒绝未知、重复字段及超预算正文，不回显原始解析错误。
func DecodeJSON(c *echo.Context, target any, maximum int64) error {
	contentType, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return echo.ErrUnsupportedMediaType
	}
	bytes, err := io.ReadAll(io.LimitReader(c.Request().Body, maximum+1))
	if err != nil {
		return utils.ErrBadParamInput
	}
	if int64(len(bytes)) > maximum {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, http.StatusText(http.StatusRequestEntityTooLarge))
	}
	if err := json.Unmarshal(bytes, target, json.RejectUnknownMembers(true)); err != nil {
		return utils.ErrBadParamInput
	}
	return nil
}
