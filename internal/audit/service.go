package audit

import (
	"context"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/authz"
)

type EventList struct {
	Items    []Record `json:"items"`
	Total    int64    `json:"total"`
	Page     int32    `json:"page"`
	PageSize int32    `json:"page_size"`
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

// ListEvents returns one page of audit history. It is the admin console's only
// read surface, so it is gated by the same system-management permission as the
// surfaces whose actions it records.
func (s *Service) ListEvents(ctx context.Context, actor auth.Actor, filter ListFilter) (EventList, error) {
	if err := authz.Authorize(authz.NewSubject(actor), authz.PermissionSystemManage); err != nil {
		return EventList{}, apperror.Forbidden("forbidden", "required permission is missing")
	}
	if filter.ObjectType != "" && !filter.ObjectType.Valid() {
		return EventList{}, apperror.BadRequest("audit.invalid_object_type", "object_type is invalid")
	}
	if filter.Action != "" && !filter.Action.Valid() {
		return EventList{}, apperror.BadRequest("audit.invalid_action", "action is invalid")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	items, total, err := s.store.ListEvents(ctx, filter)
	if err != nil {
		return EventList{}, err
	}
	return EventList{Items: items, Total: total, Page: filter.Page, PageSize: filter.PageSize}, nil
}
