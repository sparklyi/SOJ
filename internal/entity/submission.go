package entity

// Run 提交测评
type Run struct {
	ProblemID    int    `json:"problem_id,omitempty" binding:"required,number"`
	ProblemObjID string `json:"problem_obj_id,omitempty"`
	SourceCode   string `json:"source_code,omitempty" binding:"required"`
	LanguageID   int    `json:"language_id,omitempty" binding:"required"`
	ContestID    int    `json:"contest_id,omitempty" binding:"omitempty,number"`
	Limit
	CpuExtraLimit float64 `json:"cpu_extra_time,omitempty"`
	Case
}

// Judge 多测试点测评
type Judge struct {
	Submissions []Run `json:"submissions"`
}

// JudgeStatus 测评状态
type JudgeStatus struct {
	ID          int    `json:"id,omitempty"`
	Description string `json:"description,omitempty"`
}

// JudgeResult 测评结果
type JudgeResult struct {
	Stdout        string  `json:"stdout,omitempty"`
	Time          string  `json:"time,omitempty"`
	Memory        float64 `json:"memory,omitempty"`
	Stderr        string  `json:"stderr,omitempty"`
	Token         string  `json:"token,omitempty"`
	CompileOutput string  `json:"compile_output,omitempty"`
	Message       string  `json:"message,omitempty"`
	JudgeStatus   `json:"status,omitempty"`
}

// SubmissionList 获取测评列表
type SubmissionList struct {
	UserID     int    `json:"user_id,omitempty" binding:"omitempty,number"`
	UserName   string `json:"user_name,omitempty"`
	ProblemID  int    `json:"problem_id,omitempty" binding:"omitempty,number"`
	LanguageID int    `json:"language_id,omitempty" binding:"omitempty,number"`
	ContestID  int    `json:"contest_id,omitempty" binding:"omitempty,number"`
	Page       int    `json:"page,omitempty" binding:"omitempty,number"`
	PageSize   int    `json:"page_size,omitempty" binding:"omitempty,number"`
}
