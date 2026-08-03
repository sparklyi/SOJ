package submission

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	judgeevents "SOJ/internal/judge/events"
	"SOJ/internal/problem"
	"SOJ/internal/storage"

	"golang.org/x/sync/singleflight"
)

const defaultTestcaseCacheBytes = 256 << 20

type testcaseLoader interface {
	Load(context.Context, judgeevents.TestcaseSetRef) ([]problem.Testcase, error)
}

type testcaseCacheMetrics interface {
	RecordTestcaseCache(result string)
	ObserveTestcaseCachePhase(phase, result string, duration time.Duration)
	ObserveTestcaseCacheBytes(bytes int64)
	RecordTestcaseCacheEviction()
}

// TestcaseCacheOptions configures the in-memory testcase cache.
type TestcaseCacheOptions struct {
	MaxBytes int64
	Metrics  testcaseCacheMetrics
}

// TestcaseCache stores parsed testcase sets with a bounded LRU policy.
type TestcaseCache struct {
	storage  storage.ObjectStorage
	metrics  testcaseCacheMetrics
	maxBytes int64

	mu           sync.Mutex
	entries      map[testcaseCacheKey]*list.Element
	lru          *list.List
	currentBytes int64
	loads        singleflight.Group
}

type testcaseCacheKey struct {
	id   int64
	hash string
}

type testcaseCacheEntry struct {
	key   testcaseCacheKey
	cases []problem.Testcase
	bytes int64
}

// NewTestcaseCache creates a testcase cache backed by object storage.
func NewTestcaseCache(objectStorage storage.ObjectStorage, options TestcaseCacheOptions) *TestcaseCache {
	maxBytes := options.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultTestcaseCacheBytes
	}
	return &TestcaseCache{
		storage:  objectStorage,
		metrics:  options.Metrics,
		maxBytes: maxBytes,
		entries:  make(map[testcaseCacheKey]*list.Element),
		lru:      list.New(),
	}
}

// Load returns the parsed testcase set identified by the event reference.
func (c *TestcaseCache) Load(ctx context.Context, ref judgeevents.TestcaseSetRef) ([]problem.Testcase, error) {
	key, err := testcaseCacheKeyFromRef(ref)
	if err != nil {
		c.record("error")
		return nil, err
	}
	if cases, ok := c.get(key); ok {
		c.record("hit")
		return cases, nil
	}
	c.record("miss")

	value, err, _ := c.loads.Do(key.string(), func() (any, error) {
		if cases, ok := c.get(key); ok {
			return cases, nil
		}
		data, err := c.download(ctx, ref)
		if err != nil {
			return nil, err
		}
		started := time.Now()
		cases, err := problem.ParseTestcaseArchive(data, problem.TestcaseArchiveOptions{
			ExpectedCaseCount: ref.CaseCount,
			ExpectedSHA256:    ref.ChecksumSHA256,
			TimeLimit:         time.Duration(ref.TimeLimitMS) * time.Millisecond,
			MemoryKB:          ref.MemoryKB,
		})
		if c.metrics != nil {
			result := "success"
			if err != nil {
				result = "error"
			}
			c.metrics.ObserveTestcaseCachePhase("unpack", result, time.Since(started))
		}
		if err != nil {
			return nil, err
		}
		c.put(key, cases)
		return cases, nil
	})
	if err != nil {
		c.record("error")
		return nil, err
	}
	return cloneTestcases(value.([]problem.Testcase)), nil
}

func testcaseCacheKeyFromRef(ref judgeevents.TestcaseSetRef) (testcaseCacheKey, error) {
	if ref.ID <= 0 {
		return testcaseCacheKey{}, errors.New("testcase_set.id must be positive")
	}
	if strings.TrimSpace(ref.ChecksumSHA256) == "" {
		return testcaseCacheKey{}, errors.New("testcase_set.checksum_sha256 is required")
	}
	if strings.TrimSpace(ref.StorageKey) == "" {
		return testcaseCacheKey{}, errors.New("testcase_set.storage_key is required")
	}
	if ref.CaseCount <= 0 {
		return testcaseCacheKey{}, errors.New("testcase_set.case_count must be positive")
	}
	return testcaseCacheKey{id: ref.ID, hash: strings.ToLower(strings.TrimSpace(ref.ChecksumSHA256))}, nil
}

func (k testcaseCacheKey) string() string {
	return fmt.Sprintf("%d:%s", k.id, k.hash)
}

func (c *TestcaseCache) download(ctx context.Context, ref judgeevents.TestcaseSetRef) ([]byte, error) {
	started := time.Now()
	if c.storage == nil {
		c.observeDownload("error", started)
		return nil, errors.New("testcase object storage unavailable")
	}
	body, info, err := c.storage.Get(ctx, ref.StorageKey)
	if err != nil {
		c.observeDownload("error", started)
		return nil, err
	}
	if body == nil {
		c.observeDownload("error", started)
		return nil, errors.New("testcase object body is nil")
	}
	if info.Size > problem.MaxTestcaseArchiveBytes {
		_ = body.Close()
		c.observeDownload("error", started)
		return nil, fmt.Errorf("testcase archive exceeds %d bytes", problem.MaxTestcaseArchiveBytes)
	}
	data, err := readLimitedObject(body, problem.MaxTestcaseArchiveBytes)
	result := "success"
	if err != nil {
		result = "error"
	}
	c.observeDownload(result, started)
	return data, err
}

func readLimitedObject(body io.ReadCloser, maxBytes int64) ([]byte, error) {
	if body == nil {
		return nil, errors.New("testcase object body is nil")
	}
	data, readErr := io.ReadAll(io.LimitReader(body, maxBytes+1))
	closeErr := body.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("testcase archive exceeds %d bytes", maxBytes)
	}
	return data, nil
}

func (c *TestcaseCache) observeDownload(result string, started time.Time) {
	if c.metrics != nil {
		c.metrics.ObserveTestcaseCachePhase("download", result, time.Since(started))
	}
}

func (c *TestcaseCache) get(key testcaseCacheKey) ([]problem.Testcase, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.lru.MoveToFront(element)
	entry := element.Value.(testcaseCacheEntry)
	return cloneTestcases(entry.cases), true
}

func (c *TestcaseCache) put(key testcaseCacheKey, cases []problem.Testcase) {
	entry := testcaseCacheEntry{key: key, cases: cloneTestcases(cases), bytes: testcaseBytes(cases)}
	if entry.bytes > c.maxBytes {
		return
	}

	c.mu.Lock()
	if _, ok := c.entries[key]; ok {
		c.mu.Unlock()
		return
	}
	evictions := 0
	for c.currentBytes+entry.bytes > c.maxBytes && c.lru.Len() > 0 {
		oldest := c.lru.Back()
		old := oldest.Value.(testcaseCacheEntry)
		delete(c.entries, old.key)
		c.lru.Remove(oldest)
		c.currentBytes -= old.bytes
		evictions++
	}
	c.entries[key] = c.lru.PushFront(entry)
	c.currentBytes += entry.bytes
	currentBytes := c.currentBytes
	if c.metrics != nil {
		c.metrics.ObserveTestcaseCacheBytes(currentBytes)
	}
	c.mu.Unlock()

	if c.metrics == nil {
		return
	}
	for i := 0; i < evictions; i++ {
		c.metrics.RecordTestcaseCacheEviction()
	}
}

func cloneTestcases(cases []problem.Testcase) []problem.Testcase {
	return append([]problem.Testcase(nil), cases...)
}

func testcaseBytes(cases []problem.Testcase) int64 {
	var size int64
	for _, testcase := range cases {
		size += int64(len(testcase.InputKey) + len(testcase.OutputKey))
	}
	return size
}

func (c *TestcaseCache) record(result string) {
	if c.metrics != nil {
		c.metrics.RecordTestcaseCache(result)
	}
}

var _ testcaseLoader = (*TestcaseCache)(nil)
