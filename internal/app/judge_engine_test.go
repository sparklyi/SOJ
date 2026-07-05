package app

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/config"
	"SOJ/internal/judge"
)

func TestNewJudgeEngineDefaultsToAgentProtocol(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{})

	languages, err := engine.Languages(context.Background())
	if err != nil {
		t.Fatalf("Languages returned error: %v", err)
	}
	if len(languages) != 0 {
		t.Fatalf("languages = %+v, want no baked-in languages from protocol stub", languages)
	}
	_, err = engine.Judge(context.Background(), judge.Request{LanguageID: 71, Source: []byte("package main")})
	if err == nil {
		t.Fatal("Judge returned nil error, want agent protocol unavailable until agent client is implemented")
	}
}

func TestFakeAcceptedJudgeEngineProvidesDefaultLanguage(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{Endpoint: "fake://accepted", Timeout: time.Second})

	languages, err := engine.Languages(context.Background())
	if err != nil {
		t.Fatalf("Languages returned error: %v", err)
	}
	if len(languages) != 1 {
		t.Fatalf("languages = %+v, want one fake language", languages)
	}
	if languages[0].ID != 71 || !languages[0].Enabled {
		t.Fatalf("language = %+v, want enabled fake language 71", languages[0])
	}
}

func TestNewJudgeEngineRejectsHTTPJudgeEndpoint(t *testing.T) {
	engine := newJudgeEngine(config.JudgeConfig{Endpoint: "http://legacy-judge:2358", Timeout: time.Second})

	_, err := engine.Languages(context.Background())
	if err == nil {
		t.Fatal("Languages returned nil error, want unsupported judge endpoint error")
	}
	if got, want := err.Error(), "unsupported judge endpoint http://legacy-judge:2358"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestNewWorkerObjectStorageAcceptsHTTPEndpoint(t *testing.T) {
	_, err := newWorkerObjectStorage(config.StorageConfig{
		Endpoint:     "http://minio:9000",
		Bucket:       "soj",
		AccessKey:    "minioadmin",
		SecretKey:    "minioadmin",
		UsePathStyle: true,
	})
	if err != nil {
		t.Fatalf("newWorkerObjectStorage returned error: %v", err)
	}
}
