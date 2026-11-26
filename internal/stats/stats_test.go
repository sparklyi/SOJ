package stats

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type fakeStore struct {
	mu                 sync.Mutex
	counts             int
	problems           int64
	submissions        int64
	languages          int64
	failProblemCount   bool
	submissionCountErr error
	languageCountErr   error
}

func (s *fakeStore) CountPublishedProblems(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts++
	if s.failProblemCount {
		return 0, errors.New("pg down")
	}
	return s.problems, nil
}

func (s *fakeStore) CountSubmissions(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts++
	return s.submissions, s.submissionCountErr
}

func (s *fakeStore) CountEnabledLanguages(context.Context) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts++
	return s.languages, s.languageCountErr
}

type fakeCache struct {
	mu     sync.Mutex
	facts  Facts
	hasKey bool
	getErr error
	setErr error
	sets   int
}

func (c *fakeCache) Get(context.Context) (Facts, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return Facts{}, false, c.getErr
	}
	return c.facts, c.hasKey, nil
}

func (c *fakeCache) Set(_ context.Context, facts Facts) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	if c.setErr != nil {
		return c.setErr
	}
	c.facts = facts
	c.hasKey = true
	return nil
}

func newTestService(store Store, cache Cache) *Service {
	return NewService(store, cache, slog.Default())
}

func TestSiteFactsFallsBackToPostgresOnCacheMiss(t *testing.T) {
	store := &fakeStore{problems: 3, submissions: 12, languages: 3}
	cache := &fakeCache{}
	service := newTestService(store, cache)

	facts, err := service.SiteFacts(context.Background())
	if err != nil {
		t.Fatalf("SiteFacts returned error: %v", err)
	}
	if facts.Problems != 3 || facts.Submissions != 12 || facts.Languages != 3 {
		t.Fatalf("facts = %+v", facts)
	}
	// cache-aside：回源之后要回填缓存。
	if cache.sets != 1 {
		t.Fatalf("cache sets = %d, want 1", cache.sets)
	}
}

func TestSiteFactsServesCacheHitWithoutTouchingPostgres(t *testing.T) {
	store := &fakeStore{problems: 99, submissions: 99, languages: 99}
	cache := &fakeCache{facts: Facts{Problems: 3, Submissions: 12, Languages: 3}, hasKey: true}
	service := newTestService(store, cache)

	facts, err := service.SiteFacts(context.Background())
	if err != nil {
		t.Fatalf("SiteFacts returned error: %v", err)
	}
	if facts.Problems != 3 {
		t.Fatalf("facts = %+v, want cached value", facts)
	}
}

func TestSiteFactsIgnoresCacheErrors(t *testing.T) {
	store := &fakeStore{problems: 3, submissions: 12, languages: 3}
	cache := &fakeCache{getErr: errors.New("redis down")}
	service := newTestService(store, cache)

	facts, err := service.SiteFacts(context.Background())
	if err != nil {
		t.Fatalf("SiteFacts returned error: %v", err)
	}
	if facts.Problems != 3 {
		t.Fatalf("facts = %+v, want postgres fallback", facts)
	}
}

func TestRefreshRepopulatesCacheAfterPostgresWrite(t *testing.T) {
	store := &fakeStore{problems: 1, submissions: 1, languages: 1}
	cache := &fakeCache{facts: Facts{Problems: 1, Submissions: 1, Languages: 1}, hasKey: true}
	service := newTestService(store, cache)

	// 领域服务先写 PG（这里模拟为改计数），再调 Refresh。
	store.mu.Lock()
	store.problems = 2
	store.submissions = 3
	store.mu.Unlock()
	service.Refresh(context.Background())

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.facts.Problems != 2 || cache.facts.Submissions != 3 {
		t.Fatalf("cache = %+v, want refreshed aggregate", cache.facts)
	}
}

func TestRefreshWithoutCacheSkipsStore(t *testing.T) {
	store := &fakeStore{problems: 3, submissions: 12, languages: 5}
	service := newTestService(store, nil)

	service.Refresh(t.Context())
	if store.counts != 0 {
		t.Errorf("Refresh made %d store calls, want 0", store.counts)
	}

	facts, err := service.SiteFacts(t.Context())
	if want := (Facts{Problems: 3, Submissions: 12, Languages: 5}); err != nil || facts != want {
		t.Fatalf("SiteFacts = %+v, %v, want %+v", facts, err, want)
	}
	if store.counts != 3 {
		t.Errorf("SiteFacts made %d store calls, want 3", store.counts)
	}
}

func TestRefreshSwallowsPostgresErrors(t *testing.T) {
	store := &fakeStore{failProblemCount: true}
	cache := &fakeCache{facts: Facts{Problems: 1, Submissions: 1, Languages: 1}, hasKey: true}
	service := newTestService(store, cache)

	service.Refresh(context.Background()) // 不能 panic，也不能动缓存

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.facts.Problems != 1 {
		t.Fatalf("cache was modified despite postgres failure: %+v", cache.facts)
	}
}

func TestSiteStatsDoNotCachePartialCounts(t *testing.T) {
	countErr := errors.New("count failed")
	tests := []struct {
		name  string
		store *fakeStore
	}{
		{"submission count fails", &fakeStore{problems: 7, submissionCountErr: countErr}},
		{"enabled language count fails", &fakeStore{problems: 7, submissions: 42, languageCountErr: countErr}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("cache miss", func(t *testing.T) {
				cache := &fakeCache{}
				service := newTestService(tt.store, cache)

				facts, err := service.SiteFacts(t.Context())
				if !errors.Is(err, countErr) {
					t.Errorf("SiteFacts error = %v, want %v", err, countErr)
				}
				if facts != (Facts{}) {
					t.Errorf("SiteFacts = %+v, want zero facts", facts)
				}
				if cache.sets != 0 {
					t.Errorf("cache writes = %d, want 0", cache.sets)
				}
			})
			t.Run("refresh", func(t *testing.T) {
				cached := Facts{Problems: 3, Submissions: 12, Languages: 5}
				cache := &fakeCache{facts: cached, hasKey: true}
				service := newTestService(tt.store, cache)

				service.Refresh(t.Context())
				if !cache.hasKey || cache.facts != cached {
					t.Errorf("cache = %+v, found = %t, want cached facts %+v", cache.facts, cache.hasKey, cached)
				}
				if cache.sets != 0 {
					t.Errorf("cache writes = %d, want 0", cache.sets)
				}
			})
		})
	}
}
