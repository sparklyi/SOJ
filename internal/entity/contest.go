package entity

import "time"

type ProblemProfile struct {
	ID   int    `json:"id" binding:"required,number" bson:"id"`
	Name string `json:"name" binding:"required" bson:"name"`
}

type Contest struct {
	ID          int              `json:"id" binding:"omitempty,number" bson:"id"`
	Name        string           `json:"name" binding:"required" bson:"name"`
	Tag         string           `json:"tag" binding:"required" bson:"tag"`
	Type        string           `json:"type" binding:"required" bson:"type"`
	Sponsor     string           `json:"sponsor" binding:"required" bson:"sponsor"`
	Description string           `json:"description" binding:"required" bson:"description"`
	ProblemSet  []ProblemProfile `json:"problem_set" binding:"omitempty" bson:"problem_set"`
	StartTime   *time.Time       `json:"start_time" binding:"required" bson:"start_time"`
	EndTime     *time.Time       `json:"end_time" binding:"required" bson:"end_time"`
	FreezeTime  *time.Time       `json:"freeze_time" binding:"required" bson:"freeze_time"`
	Public      *bool            `json:"public" binding:"omitempty" bson:"public"`
	Code        string           `json:"code" bson:"code" bson:"code"`
	Publish     *bool            `json:"publish" binding:"required,boolean" bson:"publish"`
}

// ContestList 比赛列表
type ContestList struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Name     string `json:"name" binding:"omitempty" bson:"name"`
	Tag      string `json:"tag" binding:"omitempty" bson:"tag"`
	Type     string `json:"type" binding:"omitempty" bson:"type"`
	Public   *bool  `json:"public" binding:"omitempty" bson:"public"`
	Publish  *bool  `json:"publish" binding:"omitempty" bson:"publish"`
	Page     int    `json:"page" binding:"omitempty" bson:"page"`
	PageSize int    `json:"page_size" binding:"omitempty" bson:"page_size"`
}

// ProblemResult 每一题的提交状态, 罚时, 得分
type ProblemResult struct {
	Name    string  `json:"name"`
	Status  int     `json:"status"  bson:"status"`
	Score   float64 `json:"score"  bson:"score"`
	Count   int     `json:"count"  bson:"count"`
	Penalty float64 `json:"penalty"  bson:"penalty"`
}

// ProblemSetResult 所有题目的提交结果
type ProblemSetResult struct {
	AcceptedCount int                    `json:"accepted_count"`
	ScoreCount    float64                `json:"score_count"`
	PenaltyCount  float64                `json:"penalty_count"`
	Details       map[uint]ProblemResult `json:"details"`
}

// ContestScore 比赛状态 题目通过数 总得分  总罚时
type ContestScore struct {
	Actual ProblemSetResult `json:"actual,omitempty" bson:"actual"`
	Freeze ProblemSetResult `json:"freeze,omitempty" bson:"freeze"`
}

func NewContestScore() ContestScore {
	return ContestScore{
		Actual: ProblemSetResult{
			Details: make(map[uint]ProblemResult),
		},
		Freeze: ProblemSetResult{
			Details: make(map[uint]ProblemResult),
		},
	}
}
