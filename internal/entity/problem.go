package entity

type Case struct {
	Input  string `json:"input"`
	Output string `json:"output"`
}

type Problem struct {
	ID                int    `json:"id" binding:"omitempty,number"`
	Title             string `json:"title" binding:"required"`
	Tag               string `json:"tag"`
	TimeLimit         string `json:"time_limit"`
	MemoryLimit       string `json:"memory_limit" `
	Description       string `json:"description" binding:"required"`
	InputDescription  string `json:"input_description" `
	OutputDescription string `json:"output_description"`
	Level             string `json:"level" binding:"required"`
	Example           []Case `json:"example"`
}
