package language

import "fmt"

const (
	GoID    int64 = 60
	Cpp17ID int64 = 54
)

type Profile struct {
	ID             int64
	Slug           string
	Name           string
	Runtime        string
	SourceFilename string
	BinaryFilename string
	CompileCommand []string
	RunCommand     []string
}

type Registry struct {
	byID map[int64]Profile
}

func NewRegistry(profiles ...Profile) *Registry {
	registry := &Registry{byID: make(map[int64]Profile)}
	for _, profile := range profiles {
		registry.Register(profile)
	}
	return registry
}

func DefaultRegistry() *Registry {
	return NewRegistry(GoProfile(), Cpp17Profile())
}

func (r *Registry) Register(profile Profile) {
	r.byID[profile.ID] = profile
}

func (r *Registry) ResolveID(id int64) (Profile, error) {
	profile, ok := r.byID[id]
	if !ok {
		return Profile{}, fmt.Errorf("language profile %d not found", id)
	}
	return profile, nil
}

func GoProfile() Profile {
	return Profile{
		ID:             GoID,
		Slug:           "go",
		Name:           "Go",
		Runtime:        "go",
		SourceFilename: "main.go",
		BinaryFilename: "main",
		CompileCommand: []string{"go", "build", "-o", "{{binary}}", "{{source}}"},
		RunCommand:     []string{"{{binary}}"},
	}
}

func Cpp17Profile() Profile {
	return Profile{
		ID:             Cpp17ID,
		Slug:           "cpp17",
		Name:           "C++17",
		Runtime:        "g++17",
		SourceFilename: "main.cpp",
		BinaryFilename: "main",
		CompileCommand: []string{"g++", "-std=c++17", "-O2", "-pipe", "-o", "{{binary}}", "{{source}}"},
		RunCommand:     []string{"{{binary}}"},
	}
}
