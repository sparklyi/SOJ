package problem

import (
	"bytes"
	"context"
	"fmt"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/storage"
)

type problemAuthoringStore interface {
	GetProblem(ctx context.Context, id int64) (ProblemRecord, error)
	WithProblemAuthoringTx(ctx context.Context, fn func(context.Context, problemAuthoringTx) error) error
}

type problemAuthoringTx interface {
	CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error)
	UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error)
	ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error)
	LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error)
	NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentProblemStatement(ctx context.Context, problemID int64) error
	CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error)
	ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error)
	NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error
	CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error)
	CreateArtifact(ctx context.Context, artifact ArtifactRecord) (ArtifactRecord, error)
	problemPublishReadStore
}

// ProblemAuthoring owns problem creation and mutation workflows.
type ProblemAuthoring struct {
	store    problemAuthoringStore
	archives testcaseArchiveWriter
}

// NewProblemAuthoring builds an authoring service with its transactional store and archive sink.
// It panics if store is nil.
func NewProblemAuthoring(store problemAuthoringStore, archives testcaseArchiveWriter) *ProblemAuthoring {
	if store == nil {
		panic("problem authoring store is required")
	}
	return &ProblemAuthoring{store: store, archives: archives}
}

func (a *ProblemAuthoring) CreateProblem(ctx context.Context, actor auth.Actor, input CreateProblemInput) (ProblemRecord, error) {
	if err := requireAuthenticated(actor); err != nil {
		return ProblemRecord{}, err
	}
	if err := validateCreateProblem(input); err != nil {
		return ProblemRecord{}, err
	}
	tagInputs, err := tagInputsFromNames(input.Tags)
	if err != nil {
		return ProblemRecord{}, err
	}
	var created ProblemRecord
	err = a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		var err error
		created, err = tx.CreateProblem(ctx, actor.UserID, input)
		if err != nil {
			return err
		}
		if len(tagInputs) > 0 {
			_, err = tx.ReplaceProblemTags(ctx, created.ID, tagInputs)
		}
		return err
	})
	return created, err
}

func (a *ProblemAuthoring) UpdateProblem(ctx context.Context, actor auth.Actor, id int64, input UpdateProblemInput) (ProblemRecord, error) {
	var updated ProblemRecord
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		current, err := tx.LockProblemForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, current); err != nil {
			return err
		}
		if input.Status != nil && *input.Status == StatusPublished {
			if err := ensurePublishable(ctx, tx, id); err != nil {
				return err
			}
		}
		if err := validateUpdateProblem(input); err != nil {
			return err
		}
		tagInputs, err := tagInputsFromNames(input.Tags)
		if err != nil {
			return err
		}
		updated, err = tx.UpdateProblem(ctx, id, input)
		if err != nil {
			return err
		}
		if input.Tags != nil {
			_, err = tx.ReplaceProblemTags(ctx, id, tagInputs)
		}
		return err
	})
	return updated, err
}

func (a *ProblemAuthoring) ArchiveProblem(ctx context.Context, actor auth.Actor, id int64) (ProblemRecord, error) {
	var archived ProblemRecord
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		current, err := tx.LockProblemForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, current); err != nil {
			return err
		}
		archived, err = tx.ArchiveProblem(ctx, id)
		return err
	})
	return archived, err
}

func (a *ProblemAuthoring) CreateStatement(ctx context.Context, actor auth.Actor, problemID int64, input CreateStatementInput) (Statement, error) {
	if !input.MakeCurrent {
		input.MakeCurrent = true
	}
	if err := validateStatement(input); err != nil {
		return Statement{}, err
	}
	var statement Statement
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		p, err := tx.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		version, err := tx.NextProblemStatementVersion(ctx, problemID)
		if err != nil {
			return err
		}
		if input.MakeCurrent {
			if err := tx.ClearCurrentProblemStatement(ctx, problemID); err != nil {
				return err
			}
		}
		statement, err = tx.CreateProblemStatement(ctx, problemID, version, input)
		if err != nil {
			return err
		}
		return demotePublishedProblem(ctx, tx, p)
	})
	return statement, err
}

func (a *ProblemAuthoring) AssignTags(ctx context.Context, actor auth.Actor, problemID int64, input AssignTagsInput) ([]Tag, error) {
	if err := validateTags(input.Tags); err != nil {
		return nil, err
	}
	var tags []Tag
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		p, err := tx.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		tags, err = tx.ReplaceProblemTags(ctx, problemID, input.Tags)
		return err
	})
	return tags, err
}

func (a *ProblemAuthoring) UploadTestcaseArchive(ctx context.Context, actor auth.Actor, problemID int64, input UploadTestcaseInput) (TestcaseSetRecord, error) {
	if a.archives == nil {
		return TestcaseSetRecord{}, apperror.ServiceUnavailable("object storage unavailable")
	}
	if err := validateTestcaseArchive(input.Content, input.CaseCount, input.ChecksumSHA256, defaultMaxTestcaseArchiveBytes); err != nil {
		return TestcaseSetRecord{}, err
	}
	current, err := a.store.GetProblem(ctx, problemID)
	if err != nil {
		return TestcaseSetRecord{}, err
	}
	if err := canWriteProblem(actor, current); err != nil {
		return TestcaseSetRecord{}, err
	}

	actualChecksum := sha256Hex(input.Content)
	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/zip"
	}
	key, err := testcaseArchiveKey(problemID, actualChecksum)
	if err != nil {
		return TestcaseSetRecord{}, err
	}
	if _, err := a.archives.Put(ctx, storage.Object{
		Key:         key,
		ContentType: contentType,
		Size:        int64(len(input.Content)),
		Metadata: map[string]string{
			"problem-id": fmt.Sprint(problemID),
			"sha256":     actualChecksum,
		},
		Body: bytes.NewReader(input.Content),
	}); err != nil {
		return TestcaseSetRecord{}, err
	}

	var created TestcaseSetRecord
	err = a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		p, err := tx.LockProblemForUpdate(ctx, problemID)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, p); err != nil {
			return err
		}
		version, err := tx.NextTestcaseSetVersion(ctx, problemID)
		if err != nil {
			return err
		}
		artifact, err := tx.CreateArtifact(ctx, ArtifactRecord{
			OwnerType:      "testcase",
			OwnerID:        problemID,
			Kind:           "testcase_archive",
			StorageKey:     key,
			ChecksumSHA256: actualChecksum,
			SizeBytes:      int64(len(input.Content)),
			ContentType:    contentType,
		})
		if err != nil {
			return err
		}
		if artifact.ID == 0 {
			return apperror.Internal()
		}
		if err := tx.ClearCurrentTestcaseSet(ctx, problemID); err != nil {
			return err
		}
		created, err = tx.CreateTestcaseSet(ctx, problemID, version, key, actualChecksum, int64(len(input.Content)), input.CaseCount, actor.UserID)
		if err != nil {
			return err
		}
		return demotePublishedProblem(ctx, tx, p)
	})
	if err != nil {
		_ = a.archives.Delete(ctx, key)
	}
	return created, err
}
