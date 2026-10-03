package utils

import (
	"errors"
	"net/http"
)

var (
	ErrUnauthorized        = errors.New("unauthorized")
	ErrForbidden           = errors.New("forbidden")
	ErrInternalServerError = errors.New("internal server error")
	ErrNotFound            = errors.New("not found")
	ErrConflict            = errors.New("resource already exists")
	ErrRequestBudget       = errors.New("request budget exceeded")
	ErrBadParamInput       = errors.New("invalid request parameters")
)

func GetStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}

	switch {
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrForbidden):
		return http.StatusForbidden
	case errors.Is(err, ErrBadParamInput):
		return http.StatusBadRequest
	case errors.Is(err, ErrRequestBudget):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, ErrInternalServerError):
		return http.StatusInternalServerError
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
