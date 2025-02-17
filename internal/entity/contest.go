package entity

import "time"

type ProblemProfile struct {
	ID   int    `json:"id" binding:"required,number" bson:"id"`
	Name string `json:"name" binding:"required" bson:"name"`
}

type Contest struct {
	ID          int              `json:"id" binding:"omitempty,number" bson:"id"`
	Name        string           `json:"name" binding:"required" bson:"name"`
	Tag         int              `json:"tag" binding:"required,number,min=1" bson:"tag"`
	Type        int              `json:"type" binding:"required,number,min=1" bson:"type"`
	Sponsor     string           `json:"sponsor" binding:"required" bson:"sponsor"`
	Description string           `json:"description" binding:"required" bson:"description"`
	ProblemSet  []ProblemProfile `json:"problem_set" binding:"required" bson:"problem_set"`
	StartTime   *time.Time       `json:"start_time" binding:"required" bson:"start_time"`
	EndTime     *time.Time       `json:"end_time" binding:"required" bson:"end_time"`
	FreezeTime  *time.Time       `json:"freeze_time" binding:"required" bson:"freeze_time"`
	Public      *bool            `json:"public" binding:"omitempty" bson:"public"`
	Code        string           `json:"code" bson:"code" bson:"code"`
}
