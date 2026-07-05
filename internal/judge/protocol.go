package judge

const ProtocolVersion = "soj-judge-agent.v1"

type AgentRequest struct {
	ProtocolVersion string          `json:"protocol_version"`
	AttemptID       string          `json:"attempt_id,omitempty"`
	LanguageID      int64           `json:"language_id"`
	Source          []byte          `json:"source"`
	Stdin           string          `json:"stdin,omitempty"`
	Testcases       []AgentTestcase `json:"testcases,omitempty"`
	TimeoutMS       int64           `json:"timeout_ms,omitempty"`
}

type AgentTestcase struct {
	InputKey          string `json:"input_key"`
	ExpectedOutputKey string `json:"expected_output_key"`
	TimeLimitMS       int64  `json:"time_limit_ms,omitempty"`
	MemoryKB          int64  `json:"memory_kb,omitempty"`
}

type AgentResult struct {
	ProtocolVersion string            `json:"protocol_version"`
	Verdict         Verdict           `json:"verdict"`
	TimeMS          int               `json:"time_ms,omitempty"`
	MemoryKB        int               `json:"memory_kb,omitempty"`
	Stdout          string            `json:"stdout,omitempty"`
	Stderr          string            `json:"stderr,omitempty"`
	CompileOutput   string            `json:"compile_output,omitempty"`
	ErrorMessage    string            `json:"error_message,omitempty"`
	Cases           []AgentCaseResult `json:"cases,omitempty"`
	Manifest        AgentManifest     `json:"manifest,omitempty"`
}

type AgentCaseResult struct {
	Index          int     `json:"index"`
	Verdict        Verdict `json:"verdict"`
	TimeMS         int     `json:"time_ms,omitempty"`
	MemoryKB       int     `json:"memory_kb,omitempty"`
	CheckerMessage string  `json:"checker_message,omitempty"`
}

type AgentManifest struct {
	JudgeCoreVersion string `json:"judge_core_version,omitempty"`
	SandboxProfile   string `json:"sandbox_profile,omitempty"`
	LanguageRuntime  string `json:"language_runtime,omitempty"`
	TestcaseSetHash  string `json:"testcase_set_hash,omitempty"`
	TraceID          string `json:"trace_id,omitempty"`
}

func NewAgentRequest(request Request) AgentRequest {
	testcases := make([]AgentTestcase, 0, len(request.Testcases))
	for _, testcase := range request.Testcases {
		testcases = append(testcases, AgentTestcase{
			InputKey:          testcase.InputKey,
			ExpectedOutputKey: testcase.ExpectedOutputKey,
			TimeLimitMS:       testcase.TimeLimit.Milliseconds(),
			MemoryKB:          testcase.MemoryKB,
		})
	}
	return AgentRequest{
		ProtocolVersion: ProtocolVersion,
		LanguageID:      request.LanguageID,
		Source:          append([]byte(nil), request.Source...),
		Stdin:           request.Stdin,
		Testcases:       testcases,
		TimeoutMS:       request.Timeout.Milliseconds(),
	}
}

func (result AgentResult) ToResult() Result {
	cases := make([]CaseResult, 0, len(result.Cases))
	for _, item := range result.Cases {
		cases = append(cases, CaseResult{
			Index:          item.Index,
			Verdict:        item.Verdict,
			TimeMS:         item.TimeMS,
			MemoryKB:       item.MemoryKB,
			CheckerMessage: item.CheckerMessage,
		})
	}
	return Result{
		Verdict:       result.Verdict,
		TimeMS:        result.TimeMS,
		MemoryKB:      result.MemoryKB,
		Stdout:        result.Stdout,
		Stderr:        result.Stderr,
		CompileOutput: result.CompileOutput,
		ErrorMessage:  result.ErrorMessage,
		Cases:         cases,
		Manifest: Manifest{
			JudgeCoreVersion: result.Manifest.JudgeCoreVersion,
			LanguageRuntime:  result.Manifest.LanguageRuntime,
			SandboxProfile:   result.Manifest.SandboxProfile,
			TestcaseSetHash:  result.Manifest.TestcaseSetHash,
			TraceID:          result.Manifest.TraceID,
		},
	}
}
