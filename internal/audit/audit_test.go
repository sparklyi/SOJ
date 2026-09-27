package audit

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

type recordingExecer struct {
	sql  string
	args []any
}

func (e *recordingExecer) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	e.sql = sql
	e.args = args
	return pgconn.CommandTag{}, nil
}

func TestInsertRejectsMalformedEvents(t *testing.T) {
	tests := map[string]Event{
		"unknown action":     {ActorUserID: 1, Action: "user.exploded", ObjectType: ObjectUser, ObjectID: 1},
		"unknown object":     {ActorUserID: 1, Action: ActionUserDisabled, ObjectType: "sprocket", ObjectID: 1},
		"zero object id":     {ActorUserID: 1, Action: ActionUserDisabled, ObjectType: ObjectUser},
		"negative actor":     {ActorUserID: -1, Action: ActionUserDisabled, ObjectType: ObjectUser, ObjectID: 1},
		"empty action":       {ActorUserID: 1, ObjectType: ObjectUser, ObjectID: 1},
		"empty object type":  {ActorUserID: 1, Action: ActionUserDisabled, ObjectID: 1},
		"negative object id": {ActorUserID: 1, Action: ActionUserDisabled, ObjectType: ObjectUser, ObjectID: -3},
	}
	for name, event := range tests {
		t.Run(name, func(t *testing.T) {
			if err := Insert(context.Background(), &recordingExecer{}, event); err == nil {
				t.Fatal("Insert() error = nil, want a validation error")
			}
		})
	}
}

func TestInsertEncodesEventArguments(t *testing.T) {
	execer := &recordingExecer{}
	event := Event{
		ActorUserID: 7,
		Action:      ActionProblemArchived,
		ObjectType:  ObjectProblem,
		ObjectID:    42,
		Reason:      "retired after contest",
		Metadata:    map[string]string{"previous_status": "published"},
	}
	if err := Insert(context.Background(), execer, event); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if len(execer.args) != 6 {
		t.Fatalf("Insert() args = %d, want 6", len(execer.args))
	}
	if execer.args[0] != int64(7) || execer.args[1] != string(ActionProblemArchived) || execer.args[2] != string(ObjectProblem) || execer.args[3] != int64(42) || execer.args[4] != "retired after contest" {
		t.Fatalf("Insert() args = %#v", execer.args)
	}
	metadata, ok := execer.args[5].([]byte)
	if !ok {
		t.Fatalf("metadata arg type = %T, want []byte", execer.args[5])
	}
	var decoded map[string]string
	if err := json.Unmarshal(metadata, &decoded); err != nil {
		t.Fatalf("metadata is not JSON: %v", err)
	}
	if decoded["previous_status"] != "published" {
		t.Fatalf("metadata = %v, want previous_status", decoded)
	}
}

func TestInsertStoresSystemEventsWithoutActor(t *testing.T) {
	execer := &recordingExecer{}
	event := Event{
		Action:     ActionUserRoleGranted,
		ObjectType: ObjectUser,
		ObjectID:   3,
		Reason:     "system registration",
	}
	if err := Insert(context.Background(), execer, event); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	if execer.args[0] != nil {
		t.Fatalf("actor arg = %#v, want nil for a system event", execer.args[0])
	}
}
