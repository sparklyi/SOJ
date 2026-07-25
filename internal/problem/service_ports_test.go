package problem

import (
	"context"
	"testing"

	"SOJ/internal/auth"
)

type problemReaderStub struct{}

func (problemReaderStub) GetProblem(context.Context, int64) (ProblemRecord, error) {
	return ProblemRecord{ID: 1, OwnerUserID: 7, Status: StatusDraft}, nil
}

func TestServiceGetProblemOnlyDependsOnProblemReader(t *testing.T) {
	service := NewService(ServiceOptions{Problems: problemReaderStub{}})

	got, err := service.GetProblem(context.Background(), auth.Actor{UserID: 7, Role: auth.RoleUser}, 1)
	if err != nil {
		t.Fatalf("GetProblem() error = %v", err)
	}
	if got.ID != 1 {
		t.Fatalf("GetProblem() ID = %d, want 1", got.ID)
	}
}
