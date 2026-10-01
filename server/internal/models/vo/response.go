package vo

import (
	"net/http"

	"github.com/labstack/echo/v5"

	"github.com/SisyphusSQ/codex-pulse/server/internal/requestinfo"
	"github.com/SisyphusSQ/codex-pulse/server/utils"
)

type Response struct {
	RequestID string `json:"request_id,omitempty"`
	Code      int    `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
}

func SuccessResp(data any) Response {
	return Response{Code: http.StatusOK, Message: "success", Data: data}
}

func ErrorResp(status int, message string) Response {
	return Response{Code: status, Message: message}
}

func CommSuccResp(c *echo.Context, data any) error {
	response := SuccessResp(data)
	response.RequestID = requestinfo.ID(c.Request().Context())
	return c.JSON(http.StatusOK, response)
}

func CommErrResp(c *echo.Context, err error) error {
	status := utils.GetStatusCode(err)
	message := http.StatusText(status)
	return c.JSON(status, Response{Code: status, Message: message, RequestID: requestinfo.ID(c.Request().Context())})
}
