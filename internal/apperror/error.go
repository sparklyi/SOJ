package apperror

import (
	"errors"
	"net/http"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func New(code, message string, httpStatus int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func BadRequest(code, message string) *Error {
	return New(code, message, http.StatusBadRequest)
}

func Unauthorized(code, message string) *Error {
	return New(code, message, http.StatusUnauthorized)
}

func Forbidden(code, message string) *Error {
	return New(code, message, http.StatusForbidden)
}

func NotFound(code, message string) *Error {
	return New(code, message, http.StatusNotFound)
}

func Conflict(code, message string) *Error {
	return New(code, message, http.StatusConflict)
}

func Unprocessable(code, message string) *Error {
	return New(code, message, http.StatusUnprocessableEntity)
}

func ServiceUnavailable(message string) *Error {
	return New("service_unavailable", message, http.StatusServiceUnavailable)
}

func Internal() *Error {
	return New("internal_error", "internal server error", http.StatusInternalServerError)
}

func From(err error) (*Error, bool) {
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}
