package problem

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"SOJ/internal/apperror"
	"SOJ/internal/auth"
	"SOJ/internal/storage"
)

const problemSlugAttempts = 3

type problemAuthoringStore interface {
	GetProblem(ctx context.Context, id int64) (ProblemRecord, error)
	WithProblemAuthoringTx(ctx context.Context, fn func(context.Context, problemAuthoringTx) error) error
}

type problemAuthoringTx interface {
	CreateProblem(ctx context.Context, ownerUserID int64, input CreateProblemInput) (ProblemRecord, error)
	UpdateProblem(ctx context.Context, id int64, input UpdateProblemInput) (ProblemRecord, error)
	SetProblemStatus(ctx context.Context, id int64, status string) (ProblemRecord, error)
	ArchiveProblem(ctx context.Context, id int64) (ProblemRecord, error)
	LockProblemForUpdate(ctx context.Context, id int64) (ProblemRecord, error)
	NextProblemStatementVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentProblemStatement(ctx context.Context, problemID int64) error
	CreateProblemStatement(ctx context.Context, problemID int64, version int32, input CreateStatementInput) (Statement, error)
	ReplaceProblemTags(ctx context.Context, problemID int64, tags []TagInput) ([]Tag, error)
	NextTestcaseSetVersion(ctx context.Context, problemID int64) (int32, error)
	ClearCurrentTestcaseSet(ctx context.Context, problemID int64) error
	CreateTestcaseSet(ctx context.Context, problemID int64, version int32, storageKey, checksum string, sizeBytes int64, caseCount int32, createdBy int64) (TestcaseSetRecord, error)
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
	if err := (RBACProblemPolicy{}).CanCreate(actor); err != nil {
		return ProblemRecord{}, err
	}
	input = applyProblemDefaults(input)
	if err := validateCreateProblem(input); err != nil {
		return ProblemRecord{}, err
	}
	tagInputs, err := tagInputsFromNames(input.Tags)
	if err != nil {
		return ProblemRecord{}, err
	}
	for attempt := 0; attempt < problemSlugAttempts; attempt++ {
		slug, err := generateProblemSlug(input.Title)
		if err != nil {
			return ProblemRecord{}, err
		}
		input.Slug = slug
		created, err := a.createProblemOnce(ctx, actor, input, tagInputs)
		if err == nil {
			return created, nil
		}
		if !isProblemSlugConflict(err) {
			return ProblemRecord{}, err
		}
	}
	return ProblemRecord{}, apperror.Conflict("problem.slug_conflict", "problem slug already exists")
}

func (a *ProblemAuthoring) createProblemOnce(ctx context.Context, actor auth.Actor, input CreateProblemInput, tagInputs []TagInput) (ProblemRecord, error) {
	var created ProblemRecord
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
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
	if input.Status != nil {
		return ProblemRecord{}, apperror.Conflict("problem.status_managed_by_review", "problem status changes must use the review workflow")
	}
	var updated ProblemRecord
	err := a.store.WithProblemAuthoringTx(ctx, func(ctx context.Context, tx problemAuthoringTx) error {
		current, err := tx.LockProblemForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if err := canWriteProblem(actor, current); err != nil {
			return err
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
		// Statements do not carry their own title; they always mirror the
		// problem title at the time the version is written.
		input.Title = p.Title
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

// UploadTestcaseArchive streams an uploaded archive through validation, stores
// it under a random object key, and records a new testcase set version in one
// transaction. The archive is validated before anything is written, and the
// stored object is removed if the transaction fails.
func (a *ProblemAuthoring) UploadTestcaseArchive(ctx context.Context, actor auth.Actor, problemID int64, input UploadTestcaseInput) (TestcaseSetRecord, []Finding, error) {
	if a.archives == nil {
		return TestcaseSetRecord{}, nil, apperror.ServiceUnavailable("object storage unavailable")
	}
	if input.Source == nil || input.Size <= 0 {
		return TestcaseSetRecord{}, nil, apperror.BadRequest(codeZipInvalid, "testcase archive must be a valid zip file")
	}
	manifest, findings := ValidateArchive(io.NewSectionReader(input.Source, 0, input.Size), input.Size)
	if HasErrorFindings(findings) {
		return TestcaseSetRecord{}, findings, apperror.Unprocessable("testcase.archive_invalid", "testcase archive is invalid").
			WithDetails(map[string]any{"findings": findings})
	}

	current, err := a.store.GetProblem(ctx, problemID)
	if err != nil {
		return TestcaseSetRecord{}, findings, err
	}
	if err := canWriteProblem(actor, current); err != nil {
		return TestcaseSetRecord{}, findings, err
	}

	contentType := input.ContentType
	if contentType == "" {
		contentType = "application/zip"
	}
	key, err := testcaseArchiveKey(problemID)
	if err != nil {
		return TestcaseSetRecord{}, findings, err
	}
	digest := sha256.New()
	_, err = a.archives.Put(ctx, storage.Object{
		Key:         key,
		ContentType: contentType,
		Size:        input.Size,
		Metadata: map[string]string{
			"problem-id": fmt.Sprint(problemID),
		},
		Body: io.TeeReader(io.NewSectionReader(input.Source, 0, input.Size), digest),
	})
	if err != nil {
		return TestcaseSetRecord{}, findings, err
	}
	checksum := hex.EncodeToString(digest.Sum(nil))
	caseCount := int32(len(manifest.Cases))

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
		if err := tx.ClearCurrentTestcaseSet(ctx, problemID); err != nil {
			return err
		}
		created, err = tx.CreateTestcaseSet(ctx, problemID, version, key, checksum, input.Size, caseCount, actor.UserID)
		if err != nil {
			return err
		}
		return demotePublishedProblem(ctx, tx, p)
	})
	if err != nil {
		_ = a.archives.Delete(ctx, key)
		return TestcaseSetRecord{}, findings, err
	}
	return created, findings, nil
}

func applyProblemDefaults(input CreateProblemInput) CreateProblemInput {
	if strings.TrimSpace(input.Difficulty) == "" {
		input.Difficulty = DifficultyMedium
	}
	if strings.TrimSpace(input.Visibility) == "" {
		input.Visibility = VisibilityPrivate
	}
	if input.TimeLimitMS == 0 {
		input.TimeLimitMS = 1000
	}
	if input.MemoryLimitKB == 0 {
		input.MemoryLimitKB = 262144
	}
	if input.Tags == nil {
		input.Tags = []string{}
	}
	return input
}

func generateProblemSlug(title string) (string, error) {
	base := slugifyTag(title)
	if base == "" {
		base = "problem"
	}
	var random [3]byte
	if _, err := crand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate problem slug: %w", err)
	}
	return base + "-" + hex.EncodeToString(random[:]), nil
}

func isProblemSlugConflict(err error) bool {
	appErr, ok := apperror.From(err)
	return ok && appErr.Code == "problem.slug_conflict"
}
