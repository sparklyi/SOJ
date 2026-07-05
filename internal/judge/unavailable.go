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

func (e *UnavailableEngine) Languages(ctx context.Context) ([]Language, error) {
	return []Language{}, nil
}

func (e *UnavailableEngine) err() error {
	if e.endpoint == "" {
		return fmt.Errorf("judge endpoint is not implemented")
	}
	return fmt.Errorf("judge endpoint %s is not implemented", e.endpoint)
}
