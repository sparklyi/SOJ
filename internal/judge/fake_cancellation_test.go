package judge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFakeEngineCanceledContextDoesNotConsumeResult(t *testing.T) {
	for _, operation := range []string{"Judge", "Run"} {
		for _, cancellation := range []string{"canceled", "deadline exceeded"} {
			t.Run(operation+"/"+cancellation, func(t *testing.T) {
				engine := NewFakeEngine(Result{Verdict: VerdictWrongAnswer, Stdout: "still queued"})
				var ctx context.Context
				var cancel context.CancelFunc
				var wantErr error
				switch cancellation {
				case "canceled":
					ctx, cancel = context.WithCancel(context.Background())
					cancel()
					wantErr = context.Canceled
				case "deadline exceeded":
					ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
					wantErr = context.DeadlineExceeded
				}
				defer cancel()

				call := func(ctx context.Context) (Result, error) {
					if operation == "Judge" {
						return engine.Judge(ctx, Request{LanguageSlug: "go", Source: []byte("package main")})
					}
					return engine.Run(ctx, RunRequest{LanguageSlug: "go", Source: []byte("package main")})
				}

				if _, err := call(ctx); !errors.Is(err, wantErr) {
					t.Fatalf("call with %s context: error = %v, want %v", cancellation, err, wantErr)
				}
				result, err := call(context.Background())
				if err != nil {
					t.Fatalf("live call: %v", err)
				}
				if result.Verdict != VerdictWrongAnswer || result.Stdout != "still queued" {
					t.Fatalf("live call returned %+v; canceled call consumed the scripted result", result)
				}
				if operation == "Judge" && len(engine.Requests()) != 2 {
					t.Fatalf("Judge recorded %d requests, want 2", len(engine.Requests()))
				}
				if operation == "Run" && len(engine.RunRequests()) != 2 {
					t.Fatalf("Run recorded %d requests, want 2", len(engine.RunRequests()))
				}
			})
		}
	}
}
