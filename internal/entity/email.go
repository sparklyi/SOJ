package entity

type SendEmailCode struct {
	Email string `json:"email" binding:"required,email"`
}
