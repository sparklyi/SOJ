package problem

// Authoring flow step keys. They are part of the API contract and drive the
// frontend stepper.
const (
	AuthoringStepCreate    = "create"
	AuthoringStepStatement = "statement"
	AuthoringStepTestcase  = "testcase"
	AuthoringStepCheck     = "check"
	AuthoringStepReview    = "review"

	AuthoringStepStatusDone = "done"
	AuthoringStepStatusTodo = "todo"
)

// ProblemAuthoringFlow is the server-computed authoring progress. current_step
// is the first unfinished step ("" when everything is done) and remaining is
// the number of unfinished steps.
type ProblemAuthoringFlow struct {
	CurrentStep string                 `json:"current_step"`
	Remaining   int                    `json:"remaining"`
	Steps       []ProblemAuthoringStep `json:"steps"`
}

// ProblemAuthoringStep is one step in the authoring flow.
type ProblemAuthoringStep struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

// authoringFlowInput is the minimal set of facts needed to compute the flow.
// Keeping it a value object makes the flow a pure, directly testable function.
type authoringFlowInput struct {
	ProblemStatus      string
	StatementID        int64
	TestcaseSetID      int64
	HasCheck           bool
	CheckStatementID   int64
	CheckTestcaseSetID int64
	CheckValid         bool
}

func buildProblemAuthoringFlow(input authoringFlowInput) ProblemAuthoringFlow {
	statementDone := input.StatementID != 0
	testcaseDone := input.TestcaseSetID != 0
	checkDone := input.HasCheck &&
		input.CheckStatementID == input.StatementID &&
		input.CheckTestcaseSetID == input.TestcaseSetID &&
		input.CheckValid
	reviewDone := input.ProblemStatus == StatusInReview || input.ProblemStatus == StatusPublished

	steps := []ProblemAuthoringStep{
		{Key: AuthoringStepCreate, Status: AuthoringStepStatusDone},
		{Key: AuthoringStepStatement, Status: stepStatus(statementDone)},
		{Key: AuthoringStepTestcase, Status: stepStatus(testcaseDone)},
		{Key: AuthoringStepCheck, Status: stepStatus(checkDone)},
		{Key: AuthoringStepReview, Status: stepStatus(reviewDone)},
	}

	flow := ProblemAuthoringFlow{Steps: steps}
	for _, step := range steps {
		if step.Status != AuthoringStepStatusTodo {
			continue
		}
		if flow.CurrentStep == "" {
			flow.CurrentStep = step.Key
		}
		flow.Remaining++
	}
	return flow
}

func stepStatus(done bool) string {
	if done {
		return AuthoringStepStatusDone
	}
	return AuthoringStepStatusTodo
}
