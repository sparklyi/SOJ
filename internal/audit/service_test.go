package audit

import (
	"context"
	"testing"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
)

type stubStore struct {
	filter ListFilter
	items  []Record
	total  int64
}

func (s *stubStore) ListEvents(_ context.Context, filter ListFilter) ([]Record, int64, error) {
	s.filter = filter
	return s.items, s.total, nil
}

func TestListEventsRequiresSystemManage(t *testing.T) {
	service := NewService(&stubStore{})
	_, err := service.ListEvents(context.Background(), auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleReviewer}}, ListFilter{})
	appErr, ok := apperror.From(err)
	if !ok || appErr.HTTPStatus != 403 || appErr.Code != "forbidden" {
		t.Fatalf("ListEvents() error = %v, want 403 forbidden", err)
	}
}

func TestListEventsNormalizesPaging(t *testing.T) {
	store := &stubStore{}
	service := NewService(store)
	admin := auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleAdmin}}

	list, err := service.ListEvents(context.Background(), admin, ListFilter{ObjectType: ObjectProblem, ObjectID: 42})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if list.Page != 1 || list.PageSize != 20 {
		t.Fatalf("ListEvents() paging = (%d, %d), want (1, 20)", list.Page, list.PageSize)
	}
	if store.filter.ObjectType != ObjectProblem || store.filter.ObjectID != 42 {
		t.Fatalf("store filter = %+v, want the object filter", store.filter)
	}

	pageSize := int32(500)
	_, err = service.ListEvents(context.Background(), admin, ListFilter{Page: 3, PageSize: pageSize})
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if store.filter.Page != 3 || store.filter.PageSize != 20 {
		t.Fatalf("store filter paging = (%d, %d), want (3, 20)", store.filter.Page, store.filter.PageSize)
	}
}

func TestListEventsRejectsUnknownFilters(t *testing.T) {
	service := NewService(&stubStore{})
	admin := auth.Actor{UserID: 7, Roles: []auth.Role{auth.RoleRoot}}

	if _, err := service.ListEvents(context.Background(), admin, ListFilter{ObjectType: "sprocket"}); !isBadRequest(err, "audit.invalid_object_type") {
		t.Fatalf("unknown object type error = %v", err)
	}
	if _, err := service.ListEvents(context.Background(), admin, ListFilter{Action: "user.exploded"}); !isBadRequest(err, "audit.invalid_action") {
		t.Fatalf("unknown action error = %v", err)
	}
}

func isBadRequest(err error, code string) bool {
	appErr, ok := apperror.From(err)
	return ok && appErr.HTTPStatus == 400 && appErr.Code == code
}
