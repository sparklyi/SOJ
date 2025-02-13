package entity

// Run 提交测评
type Run struct {
	ProblemID    int    `json:"problem_id,omitempty" binding:"required,number"`
	ProblemObjID string `json:"problem_obj_id,omitempty"`
	SourceCode   string `json:"source_code,omitempty" binding:"required"`
	LanguageID   int    `json:"language_id,omitempty" binding:"required"`
	ContestID    int    `json:"contest_id_id,omitempty"`
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
