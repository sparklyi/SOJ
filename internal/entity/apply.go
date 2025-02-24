package entity

type Apply struct {
	ID        int    `json:"id,omitempty"`
	ContestID uint   `json:"contest_id,omitempty" binding:"required,number"`
	Name      string `json:"name,omitempty" binding:"required,min=1"`
}

type ApplyList struct {
	ID        int    `json:"id,omitempty"`
	UserID    uint   `json:"user_id,omitempty"`
	ContestID uint   `json:"contest_id,omitempty" binding:"omitempty,number"`
	Name      string `json:"name,omitempty" binding:"omitempty"`
	Page      int    `json:"page,omitempty" binding:"omitempty,number"`
	PageSize  int    `json:"page_size,omitempty" binding:"omitempty,number"`
}
