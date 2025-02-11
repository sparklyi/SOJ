package entity

// Run 提交测评
type Run struct {
	ProblemID     int     `json:"problem_id" binding:"required,number"`
	SourceCode    string  `json:"source_code" binding:"required"`
	LanguageID    int     `json:"language_id" binding:"required,number"`
	CompetitionID int     `json:"competition_id" binding:"omitempty,number"`
	CpuTimeLimit  float64 `json:"cpu_time_limit" binding:"omitempty,number"`
	CpuExtraLimit float64 `json:"cpu_extra_limit" binding:"omitempty,number"`
	MemoryLimit   float64 `json:"memory_limit" binding:"omitempty,number"`
	Case
}

// Submissions 多测试点测评
type Submissions struct {
	Submissions []Run `json:"submissions"`
}
