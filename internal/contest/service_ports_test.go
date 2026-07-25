package contest

import (
	"context"
	"testing"

	"SOJ/internal/auth"
)

type contestReaderStub struct{}

func (contestReaderStub) GetContest(context.Context, int64) (ContestRecord, error) {
	return ContestRecord{ID: 1}, nil
}

func TestServiceAuthorizeContestRejudgeOnlyDependsOnContestReader(t *testing.T) {
	service := NewService(ServiceOptions{Contests: contestReaderStub{}})

	if err := service.AuthorizeContestRejudge(context.Background(), auth.Actor{UserID: 7, Role: auth.RoleAdmin}, 1); err != nil {
		t.Fatalf("AuthorizeContestRejudge() error = %v", err)
	}
}
