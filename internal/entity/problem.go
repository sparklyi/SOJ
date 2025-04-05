package entity

// Case 样例
type Case struct {
	Stdin          string `json:"stdin,omitempty" bson:"stdin"`
	ExpectedOutput string `json:"expected_output,omitempty" bson:"expected_output"`
}

// Limit 语言时空限制
type Limit struct {
	CpuTimeLimit   float64 `json:"cpu_time_limit,omitempty" bson:"cpu_time_limit"`
	CpuMemoryLimit float64 `json:"cpu_memory_limit,omitempty" bson:"cpu_memory_limit"`
}

// Problem 题目创建及更新
type Problem struct {
	ID                uint             `json:"id" binding:"omitempty,number" bson:"id"`
	Name              string           `json:"name" binding:"required" bson:"name"`
	Tag               []string         `json:"tag" bson:"tag"`
	TimeLimit         string           `json:"time_limit" bson:"time_limit"`
	MemoryLimit       string           `json:"memory_limit" bson:"memory_limit"`
	Description       string           `json:"description" binding:"required" bson:"description"`
	InputDescription  string           `json:"input_description" bson:"input_description"`
	OutputDescription string           `json:"output_description" bson:"output_description"`
	Level             string           `json:"level" binding:"required,oneof=easy mid hard" bson:"level"`
	Example           []Case           `json:"example" bson:"example"`
	LangLimit         map[string]Limit `json:"lang_limit" bson:"lang_limit"`
	ReMark            string           `json:"remark" bson:"remark"`
	Visible           *bool            `json:"visible" binding:"omitempty,boolean" bson:"visible"`
	Owner             *uint            `json:"owner" binding:"omitempty,number" bson:"owner"`
}

// ProblemList 题目列表
type ProblemList struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Name     string `json:"name"`
	Level    string `json:"level" binding:"omitempty,oneof=easy mid hard"`
	Page     int    `json:"page" binding:"omitempty,number"`
	PageSize int    `json:"page_size" binding:"omitempty,number"`
	//Tag   string `json:"tag"`
	UserID int `json:"user_id" binding:"omitempty,number"`
}

// TestCase 创建及更新测试点
type TestCase struct {
	Content []Case `json:"content" binding:"required" bson:"content"`
}
