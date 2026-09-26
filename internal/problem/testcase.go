package problem

import "time"

// Testcase is one judge case decoded from a testcase archive.
type Testcase struct {
	ID        int64
	InputKey  string
	OutputKey string
	TimeLimit time.Duration
	MemoryKB  int64
}
