package entity

type Register struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=20"`
	Code     string `json:"code" binding:"required,len=6"`
}
type LoginByEmail struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required,len=6"`
}
type LoginByPassword struct {
	Email     string `json:"email" binding:"required,email"`
	Password  string `json:"password" binding:"required,min=8,max=20"`
	CaptchaID string `json:"captcha_id" binding:"required"`
	Captcha   string `json:"captcha" binding:"required,len=6"`
}

type UserInfo struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Username string `json:"username" binding:"omitempty"`
	Email    string `json:"email" binding:"omitempty,email"`
	Tel      string `json:"tel" binding:"omitempty"`
	Role     int    `json:"role" binding:"omitempty,oneof=-1 1 2 3"`
	Page     int    `json:"page" binding:"omitempty,number,min=1"`
	PageSize int    `json:"page_size" binding:"omitempty,number,min=20,max=100"`
}
