package response

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

func response(c *gin.Context, code int, message string, data interface{}) {
	c.JSON(code, gin.H{
		"code":    code,
		"message": message,
		"data":    data,
	})
}

// success 成功
func success(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusOK, message, data)
}

// SuccessNoContent 操作成功,无数据返回
func SuccessNoContent(c *gin.Context) {
	success(c, "success", map[string]interface{}{})
}

// SuccessWithData 操作成功, 返回数据
func SuccessWithData(c *gin.Context, data interface{}) {
	success(c, "success", data)
}

// SuccessWithMsg 操作成功, 返回信息
func SuccessWithMsg(c *gin.Context, msg string) {
	success(c, msg, map[string]interface{}{})
}

// SuccessWithDataAndMsg 操作成功, 返回数据和信息
func SuccessWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	success(c, msg, data)
}

// internalError 内部错误
func internalError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusInternalServerError, message, data)
}

// tooManyError 请求频繁
func tooManyError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusTooManyRequests, message, data)
}

// InternalErrorNoContent 内部错误, 无数据返回
func InternalErrorNoContent(c *gin.Context) {
	internalError(c, "internal error", map[string]interface{}{})
}

// InternalErrorWithData 内部错误, 返回数据
func InternalErrorWithData(c *gin.Context, data interface{}) {
	internalError(c, "internal error", data)
}

// InternalErrorWithMsg 内部错误, 返回信息
func InternalErrorWithMsg(c *gin.Context, msg string) {
	internalError(c, msg, map[string]interface{}{})
}

// InternalErrorWithDataAndMsg 内部错误, 返回数据和信息
func InternalErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	internalError(c, msg, data)
}

// badRequestError 参数错误
func badRequestError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusBadRequest, message, data)
}

// BadRequestErrorNoContent 参数错误, 无数据返回
func BadRequestErrorNoContent(c *gin.Context) {
	badRequestError(c, "bad request", map[string]interface{}{})
}

// BadRequestErrorWithData 参数错误, 返回数据
func BadRequestErrorWithData(c *gin.Context, data interface{}) {
	badRequestError(c, "bad request", data)
}

// BadRequestErrorWithMsg 参数错误, 返回信息
func BadRequestErrorWithMsg(c *gin.Context, msg string) {
	badRequestError(c, msg, map[string]interface{}{})
}

// BadRequestErrorWithDataAndMsg 参数错误, 返回数据和信息
func BadRequestErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	badRequestError(c, msg, data)
}

// notFoundError 未找到
func notFoundError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusNotFound, message, data)
}

// NotFoundErrorNoContent 未找到, 无数据返回
func NotFoundErrorNoContent(c *gin.Context) {
	notFoundError(c, "not found", map[string]interface{}{})
}

// NotFoundErrorWithData 未找到, 返回数据
func NotFoundErrorWithData(c *gin.Context, data interface{}) {
	notFoundError(c, "not found", data)
}

// NotFoundErrorWithMsg 未找到, 返回信息
func NotFoundErrorWithMsg(c *gin.Context, msg string) {
	notFoundError(c, msg, map[string]interface{}{})
}

// NotFoundErrorWithDataAndMsg 未找到, 返回数据和信息
func NotFoundErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	notFoundError(c, msg, data)
}

// methodNotAllowedError 越权
func methodNotAllowedError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusMethodNotAllowed, message, data)
}

// MethodNotAllowedErrorNoContent 越权, 无数据返回
func MethodNotAllowedErrorNoContent(c *gin.Context) {
	methodNotAllowedError(c, "method not allowed", map[string]interface{}{})
}

// MethodNotAllowedErrorWithData 越权, 返回数据
func MethodNotAllowedErrorWithData(c *gin.Context, data interface{}) {
	methodNotAllowedError(c, "method not allowed", data)
}

// MethodNotAllowedErrorWithMsg 越权, 返回信息
func MethodNotAllowedErrorWithMsg(c *gin.Context, msg string) {
	methodNotAllowedError(c, msg, map[string]interface{}{})
}

// MethodNotAllowedErrorWithDataAndMsg 越权, 返回数据和信息
func MethodNotAllowedErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	methodNotAllowedError(c, msg, data)
}

// unauthorizedError 无权
func unauthorizedError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusUnauthorized, message, data)
}

// UnauthorizedErrorNoContent 无权, 无数据返回
func UnauthorizedErrorNoContent(c *gin.Context) {
	unauthorizedError(c, "unauthorized", map[string]interface{}{})
}

// UnauthorizedErrorWithData 无权, 返回数据
func UnauthorizedErrorWithData(c *gin.Context, data interface{}) {
	unauthorizedError(c, "unauthorized", data)
}

// UnauthorizedErrorWithMsg 无权, 返回信息
func UnauthorizedErrorWithMsg(c *gin.Context, msg string) {
	unauthorizedError(c, msg, map[string]interface{}{})
}

// UnauthorizedErrorWithDataAndMsg 无权, 返回数据和信息
func UnauthorizedErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	unauthorizedError(c, msg, data)
}

// forbiddenError 禁止
func forbiddenError(c *gin.Context, message string, data interface{}) {
	response(c, http.StatusForbidden, message, data)
}

// ForbiddenErrorNoContent 禁止, 无数据返回
func ForbiddenErrorNoContent(c *gin.Context) {
	forbiddenError(c, "forbidden", map[string]interface{}{})
}

// ForbiddenErrorWithData 禁止, 返回数据
func ForbiddenErrorWithData(c *gin.Context, data interface{}) {
	forbiddenError(c, "forbidden", data)
}

// ForbiddenErrorWithMsg 禁止, 返回信息
func ForbiddenErrorWithMsg(c *gin.Context, msg string) {
	forbiddenError(c, msg, map[string]interface{}{})
}

// ForbiddenErrorWithDataAndMsg 禁止, 返回数据和信息
func ForbiddenErrorWithDataAndMsg(c *gin.Context, data interface{}, msg string) {
	forbiddenError(c, msg, data)
}

// TooManyErrorAndMsg 频繁, 返回信息
func TooManyErrorAndMsg(c *gin.Context, msg string) {
	tooManyError(c, msg, map[string]interface{}{})
}
