package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"SOJ/internal/queue"
	"SOJ/internal/submission"
)

func TestParseJudgeAgentLanguageSlots(t *testing.T) {
	got, err := parseJudgeAgentLanguageSlots("go=2, cpp17=4")
	if err != nil {
		t.Fatalf("parseJudgeAgentLanguageSlots returned error: %v", err)
	}
	if got["go"] != 2 || got["cpp17"] != 4 {
		t.Fatalf("slots = %+v, want go=2 cpp17=4", got)
	}

	_, err = parseJudgeAgentLanguageSlots("go=0")
	if err == nil {
		t.Fatal("expected invalid slot count error")
	}
}

func TestParseJudgeAgentLanguageSlotsNormalizesAndPreserves(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  map[string]int
	}{
		{
			name:  "distinct languages are preserved",
			input: "go=2,cpp=3,python=1",
			want:  map[string]int{"go": 2, "cpp": 3, "python": 1},
		},
		{
			name:  "whitespace and case are normalized",
			input: "  Go = 2 ,  CPP17=4 ",
			want:  map[string]int{"go": 2, "cpp17": 4},
		},
		{
			name:  "blank entries are skipped",
			input: "go=2,, ,python=1,",
			want:  map[string]int{"go": 2, "python": 1},
		},
		{
			name:  "matching repeated counts collapse to one entry",
			input: "go=2, GO = 2 ,Go=2",
			want:  map[string]int{"go": 2},
		},
		{
			name:  "empty value yields an empty map",
			input: " , ,",
			want:  map[string]int{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJudgeAgentLanguageSlots(tc.input)
			if err != nil {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) returned error: %v", tc.input, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) = %+v, want %+v", tc.input, got, tc.want)
			}
			for language, count := range tc.want {
				if got[language] != count {
					t.Fatalf("parseJudgeAgentLanguageSlots(%q) = %+v, want %s=%d", tc.input, got, language, count)
				}
			}
		})
	}
}

func TestParseJudgeAgentLanguageSlotsRejectsConflictingCounts(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "case-insensitive duplicate with different count",
			input: "go=2, GO =3",
			want:  "go",
		},
		{
			name:  "same conflict with reversed entry order",
			input: "GO =3, go=2",
			want:  "go",
		},
		{
			name:  "exact duplicate with different count",
			input: "go=2,go=5",
			want:  "go",
		},
		{
			name:  "conflict hidden behind whitespace padding",
			input: "cpp17=4, Cpp17 = 1",
			want:  "cpp17",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJudgeAgentLanguageSlots(tc.input)
			if err == nil {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) = %+v, want conflict error", tc.input, got)
			}
			if !strings.Contains(err.Error(), "conflicting") {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) error = %q, want it to report a conflict", tc.input, err.Error())
			}
			if !strings.Contains(err.Error(), strconv.Quote(tc.want)) {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) error = %q, want it to name language %q", tc.input, err.Error(), tc.want)
			}
		})
	}
}

func TestParseJudgeAgentLanguageSlotsRejectsMalformedEntries(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing count", input: "go", want: "want language=count"},
		{name: "missing language", input: "=2", want: "language is required"},
		{name: "zero count", input: "go=0", want: "count must be positive"},
		{name: "negative count", input: "go=-1", want: "count must be positive"},
		{name: "non-numeric count", input: "go=two", want: "count must be positive"},
		{name: "blank language with count", input: "   =3", want: "language is required"},
		{name: "malformed entry after valid one", input: "go=2,cpp", want: "want language=count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseJudgeAgentLanguageSlots(tc.input)
			if err == nil {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) = %+v, want error containing %q", tc.input, got, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("parseJudgeAgentLanguageSlots(%q) error = %q, want it to contain %q", tc.input, err.Error(), tc.want)
			}
		})
	}
}

func TestJudgeAgentSlotLimiterReleasesGlobalAndLanguageSlots(t *testing.T) {
	limiter := newJudgeAgentSlotLimiter(1, map[string]int{"go": 1})
	ctx := context.Background()

	release, err := limiter.Acquire(ctx, "go")
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	if got := limiter.Available(); got != 0 {
		t.Fatalf("available = %d, want 0 while slot is held", got)
	}
	release()
	if got := limiter.Available(); got != 1 {
		t.Fatalf("available = %d, want 1 after release", got)
	}
}

func TestJudgeAgentSlotLimiterHonorsLanguageLimitAndCancellation(t *testing.T) {
	limiter := newJudgeAgentSlotLimiter(2, map[string]int{"go": 1})
	ctx := context.Background()
	release, err := limiter.Acquire(ctx, "go")
	if err != nil {
		t.Fatalf("Acquire returned error: %v", err)
	}
	defer release()

	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = limiter.Acquire(waitCtx, "go")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire err = %v, want deadline exceeded", err)
	}
	if got := limiter.Available(); got != 1 {
		t.Fatalf("available = %d, want canceled language acquire to release global slot", got)
	}
}

func TestRunJudgeAgentLoopProcessesMessagesConcurrentlyWithinSlots(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queue := &scriptedJudgeQueue{
		batches: [][]queue.Message{
			{{ID: "1", Payload: []byte(`{"language_slug":"go"}`)}, {ID: "2", Payload: []byte(`{"language_slug":"cpp17"}`)}},
		},
	}
	processor := &blockingJudgeProcessor{entered: make(chan string, 2), release: make(chan struct{})}
	limiter := newJudgeAgentSlotLimiter(2, map[string]int{"go": 1, "cpp17": 1})
	metrics := &recordingJudgeAgentSlotMetrics{}

	done := make(chan error, 1)
	go func() {
		done <- runJudgeAgentLoop(ctx, processor, queue, 2, time.Millisecond, limiter, metrics)
	}()

	first := <-processor.entered
	second := <-processor.entered
	if first == second {
		t.Fatalf("expected two different messages to start, got %q and %q", first, second)
	}
	close(processor.release)
	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("runJudgeAgentLoop returned error: %v", err)
	}
	if got := len(queue.acked); got != 2 {
		t.Fatalf("acked = %d, want 2", got)
	}
	if !metrics.observed("global", "", 2, 2) {
		t.Fatalf("slot metrics did not record occupied global slots: %+v", metrics.observations)
	}
}

type scriptedJudgeQueue struct {
	mu      sync.Mutex
	batches [][]queue.Message
	acked   []string
}

func (q *scriptedJudgeQueue) Ensure(context.Context) error { return nil }

func (q *scriptedJudgeQueue) Publish(context.Context, int64, []byte) (string, error) {
	return "", nil
}

func (q *scriptedJudgeQueue) Consume(ctx context.Context, limit int, block time.Duration) ([]queue.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.batches) == 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(block):
			return nil, nil
		}
	}
	batch := q.batches[0]
	q.batches = q.batches[1:]
	if len(batch) > limit {
		remaining := append([]queue.Message(nil), batch[limit:]...)
		q.batches = append([][]queue.Message{remaining}, q.batches...)
		batch = batch[:limit]
	}
	return batch, nil
}

func (q *scriptedJudgeQueue) ClaimStale(context.Context, time.Duration, int) ([]queue.Message, error) {
	return nil, nil
}

func (q *scriptedJudgeQueue) Ack(_ context.Context, messageID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.acked = append(q.acked, messageID)
	return nil
}

func (q *scriptedJudgeQueue) DeadLetter(context.Context, queue.Message, string) error {
	return nil
}

func (q *scriptedJudgeQueue) Close() error { return nil }

type blockingJudgeProcessor struct {
	entered chan string
	release chan struct{}
}

func (p *blockingJudgeProcessor) ProcessRequestMessage(ctx context.Context, message queue.Message, requestQueue submission.MessageAcker) error {
	p.entered <- message.ID
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return requestQueue.Ack(ctx, message.ID)
}

type recordingJudgeAgentSlotMetrics struct {
	mu           sync.Mutex
	observations []judgeAgentSlotUsage
}

func (m *recordingJudgeAgentSlotMetrics) ObserveJudgeAgentSlots(scope, language string, used, capacity int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.observations = append(m.observations, judgeAgentSlotUsage{
		Scope:    scope,
		Language: language,
		Used:     used,
		Capacity: capacity,
	})
}

func (m *recordingJudgeAgentSlotMetrics) observed(scope, language string, used, capacity int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, observation := range m.observations {
		if observation.Scope == scope && observation.Language == language && observation.Used == used && observation.Capacity == capacity {
			return true
		}
	}
	return false
}
