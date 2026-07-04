package app

import (
	"context"
	"testing"
	"time"

	"SOJ/internal/config"
)

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
