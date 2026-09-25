package judgecore

import "SOJ/internal/language"

// testGoProfile mirrors deploy/languages/go/language.yaml. The process sandbox
// executes these commands with the host toolchain, so the tests that run real
// programs depend on the argv being real.
func testGoProfile() language.Profile {
	return language.Profile{
		Slug:          "go",
		Name:          "Go",
		Version:       "1.24",
		SourceFile:    "main.go",
		BinaryFile:    "main",
		Compile:       []string{"go", "build", "-o", "{{binary}}", "{{source}}"},
		Run:           []string{"{{binary}}"},
		Image:         "ghcr.io/sparklyi/soj-runner-go:test",
		TimeLimitMS:   language.DefaultTimeLimitMS,
		MemoryLimitKB: language.DefaultMemoryLimitKB,
	}
}

func testCpp17Profile() language.Profile {
	return language.Profile{
		Slug:          "cpp17",
		Name:          "C++17",
		Version:       "17",
		SourceFile:    "main.cpp",
		BinaryFile:    "main",
		Compile:       []string{"g++", "-std=c++17", "-O2", "-pipe", "-o", "{{binary}}", "{{source}}"},
		Run:           []string{"{{binary}}"},
		Image:         "ghcr.io/sparklyi/soj-runner-cpp17:test",
		TimeLimitMS:   language.DefaultTimeLimitMS,
		MemoryLimitKB: language.DefaultMemoryLimitKB,
	}
}
