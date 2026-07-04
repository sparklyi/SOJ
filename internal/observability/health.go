package observability

import (
	"context"
	"fmt"
)

type CheckFunc func(context.Context) error

type Readiness struct {
	checks map[string]CheckFunc
}

func NewReadiness(checks map[string]CheckFunc) Readiness {
	copied := make(map[string]CheckFunc, len(checks))
	for name, check := range checks {
		if check != nil {
			copied[name] = check
		}
	}
	return Readiness{checks: copied}
}

func (r Readiness) Check(ctx context.Context) error {
	for name, check := range r.checks {
		if err := check(ctx); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}
