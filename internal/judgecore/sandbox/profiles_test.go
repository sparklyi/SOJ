package sandbox

import "SOJ/internal/language"

// testGoProfile and testCpp17Profile mirror deploy/languages/*/language.yaml.
// The sandbox takes the runner image from the profile, so the image field is
// part of what these tests exercise.
func testGoProfile() language.Profile {
	return language.Profile{
		Slug:       "go",
		Name:       "Go",
		Version:    "1.24",
		SourceFile: "main.go",
		BinaryFile: "main",
		Compile:    []string{"go", "build", "-o", "{{binary}}", "{{source}}"},
		Run:        []string{"{{binary}}"},
		Image:      "soj-runner-go:test",
	}
}

func testCpp17Profile() language.Profile {
	return language.Profile{
		Slug:       "cpp17",
		Name:       "C++17",
		Version:    "17",
		SourceFile: "main.cpp",
		BinaryFile: "main",
		Compile:    []string{"g++", "-std=c++17", "-O2", "-pipe", "-o", "{{binary}}", "{{source}}"},
		Run:        []string{"{{binary}}"},
		Image:      "soj-runner-cpp17:test",
	}
}
