package entity

import "time"

type SendEmailCode struct {
	Email string `json:"email" binding:"required,email"`
}
type SendEmail struct {
	Recipients []string   `json:"recipients" binding:"required,email"`
	Subject    string     `json:"subject" binding:"required"`
	Body       string     `json:"body" binding:"required"`
	SendTime   *time.Time `json:"delay" binding:"omitempty"`
}
