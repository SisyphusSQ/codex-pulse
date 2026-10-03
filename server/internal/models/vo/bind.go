package vo

import (
	"github.com/labstack/echo/v5"

	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

// BindAndValidate 统一请求解析与校验的失败语义，具体模型保留自己的规则。
func BindAndValidate(c *echo.Context, request interface{ Validate() error }) error {
	if err := c.Bind(request); err != nil {
		return utils.ErrBadParamInput
	}
	return request.Validate()
}
