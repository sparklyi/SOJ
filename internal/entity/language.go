package entity

type Language struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Name     string `json:"name"`
	Status   *bool  `json:"status" binding:"omitempty"`
	Page     int    `json:"page" binding:"omitempty,number"`
	PageSize int    `json:"page_size" binding:"omitempty,number"`
}
