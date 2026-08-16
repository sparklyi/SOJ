package problem

import (
	"context"
	"net/http"

	"SOJ/internal/apperror"
)

type problemPublishReadStore interface {
	GetCurrentProblemStatement(ctx context.Context, problemID int64) (Statement, error)
	GetCurrentReadyTestcaseSet(ctx context.Context, problemID int64) (TestcaseSetRecord, error)
	GetLatestCompletedProblemCheckRun(ctx context.Context, problemID, statementID, testcaseSetID int64) (ProblemCheckRunRecord, error)
	ListProblemCheckFindings(ctx context.Context, runID int64) ([]ProblemCheckFindingRecord, error)
}

type problemStatusUpdater interface {
	SetProblemStatus(ctx context.Context, id int64, status string) (ProblemRecord, error)
}

type problemAuthoringReadiness struct {
	statement   *Statement
	testcaseSet *TestcaseSetRecord
	latestCheck *ProblemCheckRun
	blockers    []ProblemAuthoringBlocker
}

func ensurePublishable(ctx context.Context, store problemPublishReadStore, problemID int64) error {
	readiness, err := loadProblemAuthoringReadiness(ctx, store, problemID)
	if err != nil {
		return err
	}
	if len(readiness.blockers) > 0 {
		blocker := readiness.blockers[0]
		return apperror.Unprocessable(blocker.Code, blocker.Message)
	}
	return nil
}

func demotePublishedProblem(ctx context.Context, store problemStatusUpdater, problem ProblemRecord) error {
	if problem.Status != StatusPublished {
		return nil
	}
	status := StatusDraft
	_, err := store.SetProblemStatus(ctx, problem.ID, status)
	return err
}

func loadProblemAuthoringReadiness(ctx context.Context, store problemPublishReadStore, problemID int64) (problemAuthoringReadiness, error) {
	state := problemAuthoringReadiness{blockers: []ProblemAuthoringBlocker{}}
	statement, err := store.GetCurrentProblemStatement(ctx, problemID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.statement_required", Message: "current statement is required before publishing"})
	} else {
		state.statement = &statement
	}

	testcaseSet, err := store.GetCurrentReadyTestcaseSet(ctx, problemID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.testcase_required", Message: "current ready testcase set is required before publishing"})
		return state, nil
	}
	state.testcaseSet = &testcaseSet

	if state.statement == nil {
		return state, nil
	}
	runRecord, err := store.GetLatestCompletedProblemCheckRun(ctx, problemID, state.statement.ID, testcaseSet.ID)
	if err != nil {
		if !isNotFoundError(err) {
			return problemAuthoringReadiness{}, err
		}
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.check_required", Message: "run a problem check for the current testcase set before publishing"})
		return state, nil
	}
	run := problemCheckRunFromRecord(runRecord)
	findings, err := store.ListProblemCheckFindings(ctx, run.ID)
	if err != nil {
		return problemAuthoringReadiness{}, err
	}
	run.Findings = make([]ProblemCheckFinding, 0, len(findings))
	for _, finding := range findings {
		run.Findings = append(run.Findings, problemCheckFindingFromRecord(finding))
	}
	state.latestCheck = &run
	if !run.Summary.Valid {
		state.blockers = append(state.blockers, ProblemAuthoringBlocker{Code: "problem.check_failed", Message: "the current testcase set has validation errors"})
	}
	return state, nil
}

func isNotFoundError(err error) bool {
	appErr, ok := apperror.From(err)
	return ok && appErr.HTTPStatus == http.StatusNotFound
}
