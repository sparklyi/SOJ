# Testcase Archive Resource Limits Bug Reproduction

Issue: #30

## Bug Statement

Testcase ZIP uploads are bounded only after multipart parsing and full archive reads. ZIP entries have no shared file-count, per-entry, total-uncompressed-size, or compression-ratio budget across upload validation, problem checks, and worker parsing.

## Minimal Cases

### Oversized multipart request

- Input: a testcase upload whose declared request size exceeds the upload request budget.
- Before fix: multipart parsing starts and the handler returns the generic missing-archive validation error.
- After fix: the handler rejects the request before multipart parsing with HTTP 413 and `testcase.archive_too_large`.

### ZIP resource budgets

- Input: archives exceeding compressed bytes, file count, single-entry bytes, total uncompressed bytes, or compression ratio.
- Before fix: the shared resource validator accepts every case.
- After fix: each archive returns its stable `testcase.*_exceeded` error before unbounded decompression.

### Problem check and worker paths

- Input: a highly compressed valid-looking testcase pair, or an entry larger than the per-entry budget.
- Before fix: problem check reports the archive valid and worker parsing reads the full entry.
- After fix: problem check records a publish-blocking resource finding and worker parsing returns the same bounded resource error.

## Local Executors

- `go test ./internal/problem -run 'TestValidateTestcaseArchiveResources|TestValidateTestcaseArchiveRejectsHighCompressionRatio|TestParseTestcaseArchiveCasesRejectsOversizedEntry|TestUploadTestcasesRejectsOversizedContentLength|TestRunProblemCheckReportsArchiveFindings'`

The cases are deterministic and require no database, object store, or network service.

## Handoff

Implement one ZIP resource validator, bounded request/archive readers, and consistent error mapping in upload, problem check, and worker parsing paths. Preserve existing structural validation and testcase ordering behavior.
