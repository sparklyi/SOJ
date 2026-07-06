package observability

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

type CheckFunc func(context.Context) error

type ReadinessMetrics interface {
	RecordReadinessCheck(dependency, result string, duration time.Duration)
}

type Readiness struct {
	checks  map[string]CheckFunc
	metrics ReadinessMetrics
}

func NewReadiness(checks map[string]CheckFunc) Readiness {
	return NewReadinessWithMetrics(checks, nil)
}

func NewReadinessWithMetrics(checks map[string]CheckFunc, metrics ReadinessMetrics) Readiness {
	copied := make(map[string]CheckFunc, len(checks))
	for name, check := range checks {
		if check != nil {
			copied[name] = check
		}
	}
	return Readiness{checks: copied, metrics: metrics}
}

func (r Readiness) Check(ctx context.Context) error {
	names := make([]string, 0, len(r.checks))
	for name := range r.checks {
		names = append(names, name)
	}
	sort.Strings(names)

	var failures []error
	for _, name := range names {
		check := r.checks[name]
		started := time.Now()
		if err := check(ctx); err != nil {
			r.record(name, "error", time.Since(started))
			failures = append(failures, fmt.Errorf("%s: %w", name, err))
			continue
		}
		r.record(name, "success", time.Since(started))
	}
	return errors.Join(failures...)
}

func (r Readiness) record(dependency, result string, duration time.Duration) {
	if r.metrics != nil {
		r.metrics.RecordReadinessCheck(dependency, result, duration)
	}
}
