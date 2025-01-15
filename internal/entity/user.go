package entity

type Register struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=20"`
	Code     string `json:"code" binding:"required,len=6"`
}
type LoginByEmail struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=20"`
	Code     string `json:"code" binding:"required,len=6"`
}
type LoginByPassword struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=20"`
}
