package apperror

import (
	"errors"
	"net/http"
)

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
	Details    any
}

func New(code, message string, httpStatus int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

// WithDetails returns a copy of the error carrying structured details. The
// receiver is left untouched so shared sentinel errors stay immutable.
func (e *Error) WithDetails(details any) *Error {
	if e == nil {
		return nil
	}
	clone := *e
	clone.Details = details
	return &clone
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

// TooManyRequests is for per-caller limits -- one caller exhausting its own
// budget -- as opposed to ServiceUnavailable, which means the service as a
// whole cannot take the work right now. Clients are expected to tell those
// apart: "you are running too many things" is not "the site is down".
func TooManyRequests(code, message string) *Error {
	return New(code, message, http.StatusTooManyRequests)
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
