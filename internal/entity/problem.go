package entity

// Case 样例
type Case struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

// Problem 题目创建及更新
type Problem struct {
	ID                uint     `json:"id" binding:"omitempty,number"`
	Name              string   `json:"name" binding:"required"`
	Tag               []string `json:"tag"`
	TimeLimit         string   `json:"time_limit"`
	MemoryLimit       string   `json:"memory_limit"`
	Description       string   `json:"description" binding:"required"`
	InputDescription  string   `json:"input_description" `
	OutputDescription string   `json:"output_description"`
	Level             string   `json:"level" binding:"required,oneof=easy mid hard"`
	Example           []Case   `json:"example"`
	ReMark            string   `json:"remark"`
	Visible           *bool    `json:"visible" binding:"omitempty,boolean"`
	Owner             *uint    `json:"owner" binding:"omitempty,number"`
}

// ProblemList 题目列表
type ProblemList struct {
	ID       int    `json:"id" binding:"omitempty,number"`
	Name     string `json:"name"`
	Level    string `json:"level" binding:"omitempty,oneof=easy mid hard"`
	Page     int    `json:"page" binding:"omitempty,number"`
	PageSize int    `json:"page_size" binding:"omitempty,number"`
	//Tag   string `json:"tag"`
}

// TestCase 创建及更新测试点
type TestCase struct {
	Content []Case `json:"content" binding:"required"`
}
