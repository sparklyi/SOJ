package entity

type Apply struct {
	ID        int    `json:"id,omitempty"`
	ContestID uint   `json:"contest_id,omitempty" binding:"required,number"`
	Name      string `json:"name,omitempty" binding:"required,min=1"`
	Email     string `json:"email,omitempty" binding:"required,email"`
	Code      string `json:"code,omitempty" binding:"omitempty,len=6"`
}

type ApplyList struct {
	ID        int    `json:"id,omitempty"`
	UserID    uint   `json:"user_id,omitempty"`
	ContestID uint   `json:"contest_id,omitempty" binding:"omitempty,number"`
	Name      string `json:"name,omitempty" binding:"omitempty"`
	Email     string `json:"email,omitempty" binding:"omitempty,email"`
	Page      int    `json:"page,omitempty" binding:"omitempty,number"`
	PageSize  int    `json:"page_size,omitempty" binding:"omitempty,number"`
}

type ApplyCheck struct {
	ContestID uint `json:"contest_id" binding:"required,number"`
	UserID    uint `json:"user_id" binding:"required,number"`
}
