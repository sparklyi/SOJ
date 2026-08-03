package submission

import (
	"context"
	"time"

	"SOJ/internal/judge"
	"SOJ/internal/problem"
	"SOJ/internal/queue"
)

type serviceTestOptions struct {
	Repository              *memoryRepo
	ProblemReader           problem.Reader
	TestcaseResolver        problem.TestcaseResolver
	SourceStore             sourceWriter
	Judge                   judge.JudgeEngine
	ContestSubmissionPolicy ContestSubmissionPolicy
	ContestVisibilityPolicy ContestResultVisibilityPolicy
	TerminalHook            TerminalHook
	Now                     func() time.Time
	RunWait                 time.Duration
	RunTimeout              time.Duration
	RunContext              context.Context
	RunParallelism          int
}

func newServiceForTest(options serviceTestOptions) *Service {
	problems := options.ProblemReader
	if problems == nil {
		problems = fakeProblemReader{}
	}
	sourceStore := options.SourceStore
	if sourceStore == nil {
		sourceStore = NewMemorySourceStore()
	}
	judgeEngine := options.Judge
	if judgeEngine == nil {
		judgeEngine = judge.NewFakeEngine()
	}
	creator := NewSubmissionCreator(SubmissionCreatorOptions{
		Store:         options.Repository,
		ProblemReader: problems,
		SourceStore:   sourceStore,
		ContestPolicy: options.ContestSubmissionPolicy,
		Now:           options.Now,
	})
	reader := NewSubmissionReader(options.Repository, options.ContestVisibilityPolicy)
	runs := NewRunService(RunServiceOptions{
		Store:         options.Repository,
		ProblemReader: problems,
		SourceStore:   sourceStore,
		Judge:         judgeEngine,
		Now:           options.Now,
		Wait:          options.RunWait,
		Timeout:       options.RunTimeout,
		Context:       options.RunContext,
		Parallelism:   options.RunParallelism,
	})
	languages := NewLanguageService(options.Repository, judgeEngine)
	completer := NewSubmissionCompleter(options.Repository, options.TerminalHook, options.Now)
	return NewService(creator, reader, runs, languages, completer)
}

type workerTestOptions struct {
	Repository       *memoryRepo
	Queue            queue.TaskQueue
	Judge            judgeRunner
	ProblemReader    problem.Reader
	TestcaseResolver workerTestcaseResolver
	SourceStore      sourceReader
	MaxAttempts      int32
	Backoff          func(int32) time.Duration
	Now              func() time.Time
	Metrics          workerTestMetrics
}

type workerTestMetrics interface {
	taskDispatchMetrics
	taskProcessMetrics
}

type workerTestcaseResolver interface {
	testcaseSnapshotResolver
	testcaseMetadataResolver
}

func newWorkerForTest(options workerTestOptions) *Worker {
	taskQueue := options.Queue
	if taskQueue == nil {
		taskQueue = &memoryQueue{}
	}
	problems := options.ProblemReader
	if problems == nil {
		problems = fakeProblemReader{}
	}
	testcases := options.TestcaseResolver
	if testcases == nil {
		testcases = fakeTestcaseResolver{}
	}
	sources := options.SourceStore
	if sources == nil {
		sources = NewMemorySourceStore()
	}
	judgeEngine := options.Judge
	if judgeEngine == nil {
		judgeEngine = judge.NewFakeEngine()
	}
	dispatcher := NewTaskDispatcher(TaskDispatcherOptions{
		Store:            options.Repository,
		Queue:            taskQueue,
		TestcaseResolver: testcases,
		Metrics:          options.Metrics,
		Now:              options.Now,
		Backoff:          options.Backoff,
	})
	failures := NewTaskFailureHandler(options.Repository, taskQueue, options.MaxAttempts, options.Backoff, options.Now)
	processor := NewTaskProcessor(TaskProcessorOptions{
		Store:            options.Repository,
		Failures:         failures,
		Queue:            taskQueue,
		Judge:            judgeEngine,
		ProblemReader:    problems,
		TestcaseResolver: testcases,
		SourceStore:      sources,
		Metrics:          options.Metrics,
	})
	return NewWorker(dispatcher, processor, taskQueue)
}
