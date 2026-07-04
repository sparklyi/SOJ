package httpapi

import (
	"context"
	"errors"
	"net/http"

	"SOJ/internal/apperror"

	"github.com/gin-gonic/gin"
)

type Envelope struct {
	Data      any        `json:"data"`
	Error     *ErrorBody `json:"error"`
	RequestID string     `json:"request_id"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Data: data, Error: nil, RequestID: requestID(c)})
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, Envelope{Data: data, Error: nil, RequestID: requestID(c)})
}

func Accepted(c *gin.Context, data any) {
	c.JSON(http.StatusAccepted, Envelope{Data: data, Error: nil, RequestID: requestID(c)})
}

func AcceptedEmpty(c *gin.Context) {
	c.Status(http.StatusAccepted)
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func Error(c *gin.Context, err error) {
	RenderError(c, err)
}

func RenderError(c *gin.Context, err error) {
	appErr, ok := apperror.From(err)
	if !ok {
		if errors.Is(err, context.Canceled) {
			appErr = apperror.New("request_canceled", "request canceled", http.StatusRequestTimeout)
		} else {
			appErr = apperror.Internal()
		}
	}

	c.JSON(appErr.HTTPStatus, Envelope{
		Data: nil,
		Error: &ErrorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
		},
		RequestID: requestID(c),
	})
}

func requestID(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(ContextRequestID); ok {
		if requestID, ok := value.(string); ok {
			return requestID
		}
	}
	return c.GetHeader(HeaderRequestID)
}
