package entity

import "time"

// SendEmailCode 邮件验证码发送
type SendEmailCode struct {
	Email     string `json:"email" binding:"required,email"`
	CaptchaID string `json:"captcha_id" binding:"required"`
	Captcha   string `json:"captcha" binding:"required,len=6"`
}

// SendEmail 邮件发送
type SendEmail struct {
	Recipients []string   `json:"recipients" binding:"required,email"`
	Subject    string     `json:"subject" binding:"required"`
	Body       string     `json:"body" binding:"required"`
	SendTime   *time.Time `json:"delay" binding:"omitempty"`
}
