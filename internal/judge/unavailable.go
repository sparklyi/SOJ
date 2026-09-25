package judge

import (
	"context"
	"fmt"
	"strings"
)

type UnavailableEngine struct {
	endpoint string
}

func NewUnavailableEngine(endpoint string) *UnavailableEngine {
	return &UnavailableEngine{endpoint: strings.TrimSpace(endpoint)}
}

func (e *UnavailableEngine) Judge(ctx context.Context, request Request) (Result, error) {
	return Result{}, e.err()
}

// Run fails for the same reason Judge does: this endpoint does not execute
// anything in this process. It is explicit rather than omitted so that a
// misconfigured self-run surfaces as a system error naming the endpoint,
// instead of silently returning an empty successful result.
func (e *UnavailableEngine) Run(ctx context.Context, request RunRequest) (Result, error) {
	return Result{}, e.err()
}

func (e *UnavailableEngine) err() error {
	if e.endpoint == "" {
		return fmt.Errorf("judge endpoint is not implemented")
	}
	return fmt.Errorf("judge endpoint %s is not implemented", e.endpoint)
}
