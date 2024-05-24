package controllers

import (
	"fmt"
	"net/http"
)

type ErrorHandler struct {
}

func NewErrorUtils() *ErrorHandler {
	return &ErrorHandler{}
}

var (
	// ErrInternalServerError return
	ErrInternalServerError = fmt.Errorf("internal server Error")
	// ErrNotFound return
	ErrNotFound = fmt.Errorf("your requested data is not found")
	// ErrConflict return
	ErrConflict = fmt.Errorf("data already exist")
	// ErrBadParamInput return
	ErrBadRequest = fmt.Errorf("bad request")
	// ErrAuthFailed return
	ErrAuthFailed = fmt.Errorf("access denied")
	// ErrPermission return
	ErrPermission = fmt.Errorf("permission is required to perform this action")
	// ErrMaximumSize return
	ErrMaximumSize = fmt.Errorf("maximum size reached")
	// ErrProhibitedFileType return
	ErrProhibitedFileType = fmt.Errorf("prohibited file type")
	// ErrParseFailed return
	ErrParseFailed = fmt.Errorf("failed to parse data")
	// ErrNoChanges return
	ErrNoChanges = fmt.Errorf("no changes has been made")
	// ErrStatusUnprocessableEntity
	ErrStatusUnprocessableEntity = fmt.Errorf("unprocessable entity")
)

// GetHttpStatusCode to return Status code
func (utils *ErrorHandler) GetHttpStatusCode(err error) int {
	if err == nil {
		return http.StatusOK
	}
	switch err {
	case ErrInternalServerError:
		return http.StatusInternalServerError
	case ErrNotFound:
		return http.StatusNotFound
	case ErrAuthFailed:
		return http.StatusUnauthorized
	case ErrPermission:
		return http.StatusUnauthorized
	case ErrConflict:
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}
