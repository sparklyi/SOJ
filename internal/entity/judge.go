package entity

type Submit struct {
	SourceCode     string `json:"source_code" binding:"required"`
	LanguageID     string `json:"language_id" binding:"required,number"`
	Stdin          string `json:"stdin"`
	ExpectedOutput string `json:"expected_output"`
}

type Judge struct {
	ProblemID string `json:"problem_id" binding:"required"`
	Submit
}
