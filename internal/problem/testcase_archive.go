package problem

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"SOJ/internal/apperror"
)

// Canonical testcase archive budget defaults. They live here because the
// archive parser is the single source of truth for what a valid testcase set
// looks like; handlers and services merely forward the request.
const (
	// MaxTestcaseArchiveBytes is the maximum stored testcase archive size.
	MaxTestcaseArchiveBytes = 128 << 20

	// defaultMaxTestcaseUploadRequestBytes leaves room for multipart framing
	// around a maximum-size archive.
	defaultMaxTestcaseUploadRequestBytes = MaxTestcaseArchiveBytes + (1 << 20)

	defaultMaxTestcaseEntryBytes       = 16 << 20
	defaultMaxTestcaseTotalBytes       = 128 << 20
	defaultMaxTestcaseFiles            = 2048
	defaultMaxTestcaseCompressionRatio = 200

	testcaseReadBufferBytes = 32 << 10
)

// Finding codes shared with the check service and the HTTP layer.
const (
	codeZipInvalid          = "testcase.zip_invalid"
	codeFileNameInvalid     = "testcase.file_name_invalid"
	codeEntryEmpty          = "testcase.entry_empty"
	codeInputMissing        = "testcase.input_missing"
	codeOutputMissing       = "testcase.output_missing"
	codeCaseDuplicate       = "testcase.case_duplicate"
	codeArchiveEmpty        = "testcase.archive_empty"
	codeFileCountExceeded   = "testcase.file_count_exceeded"
	codeEntryTooLarge       = "testcase.entry_too_large"
	codeTotalSizeExceeded   = "testcase.total_size_exceeded"
	codeCompressionExceeded = "testcase.compression_ratio_exceeded"
	codeArchiveTooLarge     = "testcase.archive_too_large"
	codeArchiveCorrupted    = "testcase.archive_corrupted"
	codeFileIgnored         = "testcase.file_ignored"
	codeCaseCountMismatch   = "testcase.case_count_mismatch"
)

// Finding is one validation message produced by the archive parser. Severity is
// either ProblemCheckSeverityError or ProblemCheckSeverityWarning.
type Finding struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	File     string `json:"file,omitempty"`
	Message  string `json:"message"`
}

// ArchiveCase is one input/output pair discovered in a testcase archive, in
// natural sort order. Paths use the original zip member names.
type ArchiveCase struct {
	Key        string
	InputPath  string
	OutputPath string
	InputSize  int64
	OutputSize int64
}

// ArchiveManifest is the structured result of validating an archive. An empty
// Cases slice with no error findings means the archive was valid but contained
// no pairs, which the parser reports as testcase.archive_empty.
type ArchiveManifest struct {
	Cases    []ArchiveCase
	Warnings []Finding
}

// ArchiveOptions overrides parser budgets and controls whether member contents
// are retained. The zero value uses the canonical defaults.
type ArchiveOptions struct {
	MaxBytes            int64
	MaxEntryBytes       uint64
	MaxTotalBytes       uint64
	MaxFiles            int
	MaxCompressionRatio uint64
	ExpectedSHA256      string
	KeepContents        bool
	TimeLimit           time.Duration
	MemoryKB            int64
}

func (o ArchiveOptions) withDefaults() ArchiveOptions {
	if o.MaxBytes <= 0 {
		o.MaxBytes = MaxTestcaseArchiveBytes
	}
	if o.MaxEntryBytes == 0 {
		o.MaxEntryBytes = defaultMaxTestcaseEntryBytes
	}
	if o.MaxTotalBytes == 0 {
		o.MaxTotalBytes = defaultMaxTestcaseTotalBytes
	}
	if o.MaxFiles == 0 {
		o.MaxFiles = defaultMaxTestcaseFiles
	}
	if o.MaxCompressionRatio == 0 {
		o.MaxCompressionRatio = defaultMaxTestcaseCompressionRatio
	}
	return o
}

// ValidateArchive validates an uploaded archive without retaining member
// contents. Validation results are data: only I/O failures surface as errors,
// and even those are reported as findings.
func ValidateArchive(r io.ReaderAt, size int64) (ArchiveManifest, []Finding) {
	result := parseArchive(r, size, ArchiveOptions{})
	return result.manifest, result.findings
}

// VerifyArchive validates a stored archive against its recorded checksum
// without retaining member contents.
func VerifyArchive(r io.ReaderAt, size int64, expectedSHA256 string) (ArchiveManifest, []Finding) {
	result := parseArchive(r, size, ArchiveOptions{ExpectedSHA256: expectedSHA256})
	return result.manifest, result.findings
}

// LoadArchive parses an archive and retains the input/output bytes for judging.
// Any error-severity finding is returned alongside the (possibly partial)
// cases; callers must treat a non-empty error findings set as a failure.
func LoadArchive(r io.ReaderAt, size int64, options ArchiveOptions) ([]Testcase, []Finding) {
	options.KeepContents = true
	result := parseArchive(r, size, options)
	return result.testcases, result.findings
}

// ArchiveFindingsError converts the first error-severity finding into an app
// error, or returns nil when the archive produced only warnings.
func ArchiveFindingsError(findings []Finding) error {
	for _, finding := range findings {
		if finding.Severity != ProblemCheckSeverityError {
			continue
		}
		status := http.StatusBadRequest
		switch finding.Code {
		case codeArchiveCorrupted:
			status = http.StatusUnprocessableEntity
		case codeArchiveTooLarge:
			status = http.StatusRequestEntityTooLarge
		}
		if finding.File != "" {
			return apperror.New(finding.Code, finding.File+": "+finding.Message, status)
		}
		return apperror.New(finding.Code, finding.Message, status)
	}
	return nil
}

// HasErrorFindings reports whether any finding has error severity.
func HasErrorFindings(findings []Finding) bool {
	for _, finding := range findings {
		if finding.Severity == ProblemCheckSeverityError {
			return true
		}
	}
	return false
}

type archiveParseResult struct {
	manifest  ArchiveManifest
	testcases []Testcase
	findings  []Finding
}

type archiveEntry struct {
	file    *zip.File
	key     string
	size    int64
	content string
}

func parseArchive(r io.ReaderAt, size int64, options ArchiveOptions) archiveParseResult {
	options = options.withDefaults()
	result := archiveParseResult{manifest: ArchiveManifest{Cases: []ArchiveCase{}, Warnings: []Finding{}}}
	if size <= 0 {
		result.findings = append(result.findings, errorFinding(codeZipInvalid, "", "testcase archive must be a valid zip file"))
		return result
	}
	if size > options.MaxBytes {
		result.findings = append(result.findings, errorFinding(codeArchiveTooLarge, "", "testcase archive is too large"))
		return result
	}
	if expected := strings.TrimSpace(options.ExpectedSHA256); expected != "" {
		actual, err := sha256ReaderAt(r)
		if err != nil {
			result.findings = append(result.findings, errorFinding(codeArchiveCorrupted, "", "testcase archive cannot be read"))
			return result
		}
		if !strings.EqualFold(expected, actual) {
			result.findings = append(result.findings, errorFinding(codeArchiveCorrupted, "", "testcase archive checksum does not match"))
			return result
		}
	}

	reader, err := zip.NewReader(r, size)
	if err != nil {
		result.findings = append(result.findings, errorFinding(codeZipInvalid, "", "testcase archive must be a valid zip file"))
		return result
	}

	inputs := map[string]archiveEntry{}
	outputs := map[string]archiveEntry{}
	var declaredTotal, actualTotal uint64
	fileCount := 0
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		fileCount++
		if fileCount > options.MaxFiles {
			result.findings = append(result.findings, errorFinding(codeFileCountExceeded, file.Name, "testcase archive has too many files"))
			continue
		}

		name := normalizeArchivePath(file.Name)
		if ignoredArchivePath(name) {
			warning := warningFinding(codeFileIgnored, file.Name, "ignored testcase archive member")
			result.manifest.Warnings = append(result.manifest.Warnings, warning)
			result.findings = append(result.findings, warning)
			continue
		}
		kind, key, ok := classifyTestcaseFile(name)
		if !ok {
			result.findings = append(result.findings, errorFinding(codeFileNameInvalid, file.Name, "testcase file name is invalid"))
			continue
		}

		// Budget checks run before reading. When a declared size breaches a
		// budget we still record the logical entry so one oversized member does
		// not cascade into a pile of missing-pair findings.
		declared := file.UncompressedSize64
		switch {
		case declared > options.MaxEntryBytes:
			result.findings = append(result.findings, errorFinding(codeEntryTooLarge, file.Name, "testcase archive entry is too large"))
			recordArchiveEntry(&result, inputs, outputs, kind, key, archiveEntry{file: file, key: key, size: int64(declared)})
			continue
		case declared > options.MaxTotalBytes || declaredTotal > options.MaxTotalBytes-declared:
			result.findings = append(result.findings, errorFinding(codeTotalSizeExceeded, file.Name, "testcase archive expands to too much data"))
			recordArchiveEntry(&result, inputs, outputs, kind, key, archiveEntry{file: file, key: key, size: int64(declared)})
			continue
		case compressionRatioExceeded(declared, file.CompressedSize64, options.MaxCompressionRatio):
			result.findings = append(result.findings, errorFinding(codeCompressionExceeded, file.Name, "testcase archive compression ratio is too high"))
			recordArchiveEntry(&result, inputs, outputs, kind, key, archiveEntry{file: file, key: key, size: int64(declared)})
			continue
		}
		declaredTotal += declared

		content, readBytes, err := readArchiveEntry(file, options, options.MaxEntryBytes)
		if err != nil {
			if errors.Is(err, errArchiveEntryTooLarge) {
				result.findings = append(result.findings, errorFinding(codeEntryTooLarge, file.Name, "testcase archive entry is too large"))
				continue
			}
			result.findings = append(result.findings, errorFinding(codeZipInvalid, file.Name, "testcase archive entry cannot be read"))
			continue
		}
		if readBytes == 0 {
			result.findings = append(result.findings, errorFinding(codeEntryEmpty, file.Name, "testcase file must not be empty"))
			continue
		}
		if readBytes > options.MaxTotalBytes || actualTotal > options.MaxTotalBytes-readBytes {
			result.findings = append(result.findings, errorFinding(codeTotalSizeExceeded, file.Name, "testcase archive expands to too much data"))
			continue
		}
		actualTotal += readBytes
		recordArchiveEntry(&result, inputs, outputs, kind, key, archiveEntry{file: file, key: key, size: int64(readBytes), content: content})
	}

	for key, input := range inputs {
		if _, ok := outputs[key]; !ok {
			result.findings = append(result.findings, errorFinding(codeOutputMissing, input.file.Name, fmt.Sprintf("%s has no matching %s.ans", input.file.Name, key)))
		}
	}
	for key, output := range outputs {
		if _, ok := inputs[key]; !ok {
			result.findings = append(result.findings, errorFinding(codeInputMissing, output.file.Name, fmt.Sprintf("%s has no matching %s.in", output.file.Name, key)))
		}
	}

	keys := make([]string, 0, len(inputs))
	for key := range inputs {
		if _, ok := outputs[key]; ok {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		result.findings = append(result.findings, errorFinding(codeArchiveEmpty, "", "testcase archive has no input/output pairs"))
		return result
	}
	sortTestcaseKeys(keys)

	result.manifest.Cases = make([]ArchiveCase, 0, len(keys))
	result.testcases = make([]Testcase, 0, len(keys))
	for index, key := range keys {
		input := inputs[key]
		output := outputs[key]
		result.manifest.Cases = append(result.manifest.Cases, ArchiveCase{
			Key:        key,
			InputPath:  input.file.Name,
			OutputPath: output.file.Name,
			InputSize:  input.size,
			OutputSize: output.size,
		})
		result.testcases = append(result.testcases, Testcase{
			ID:        int64(index + 1),
			InputKey:  input.content,
			OutputKey: output.content,
			TimeLimit: options.TimeLimit,
			MemoryKB:  options.MemoryKB,
		})
	}
	return result
}

var errArchiveEntryTooLarge = errors.New("testcase archive entry is too large")

func recordArchiveEntry(result *archiveParseResult, inputs, outputs map[string]archiveEntry, kind int, key string, entry archiveEntry) {
	target := inputs
	if kind == archiveOutput {
		target = outputs
	}
	if previous, exists := target[key]; exists {
		result.findings = append(result.findings, errorFinding(codeCaseDuplicate, entry.file.Name, fmt.Sprintf("%s duplicates %s", entry.file.Name, previous.file.Name)))
		return
	}
	target[key] = entry
}

// readArchiveEntry streams an entry through a fixed buffer, optionally keeping
// the decoded bytes. Reading to EOF is what lets archive/zip verify the CRC.
func readArchiveEntry(file *zip.File, options ArchiveOptions, maxBytes uint64) (string, uint64, error) {
	reader, err := file.Open()
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = reader.Close() }()

	var kept bytes.Buffer
	writer := io.Writer(io.Discard)
	if options.KeepContents {
		writer = &kept
	}
	buffer := make([]byte, testcaseReadBufferBytes)
	var total uint64
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			total += uint64(n)
			if total > maxBytes {
				return "", total, errArchiveEntryTooLarge
			}
			if _, writeErr := writer.Write(buffer[:n]); writeErr != nil {
				return "", total, writeErr
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return "", total, readErr
		}
	}
	return kept.String(), total, nil
}

func sha256ReaderAt(r io.ReaderAt) (string, error) {
	hash := sha256.New()
	buffer := make([]byte, testcaseReadBufferBytes)
	var offset int64
	for {
		n, err := r.ReadAt(buffer, offset)
		if n > 0 {
			if _, writeErr := hash.Write(buffer[:n]); writeErr != nil {
				return "", writeErr
			}
			offset += int64(n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

const (
	archiveInput = iota + 1
	archiveOutput
)

// classifyTestcaseFile maps a normalized member name to a case key. The
// extension match is case-insensitive; the stem is not.
func classifyTestcaseFile(name string) (int, string, bool) {
	base := name
	if index := strings.LastIndexByte(name, '/'); index >= 0 {
		base = name[index+1:]
	}
	dot := strings.LastIndexByte(base, '.')
	if dot <= 0 || dot == len(base)-1 {
		return 0, "", false
	}
	stem := base[:dot]
	extension := strings.ToLower(base[dot+1:])
	switch extension {
	case "in":
		return archiveInput, stem, true
	case "ans", "out":
		return archiveOutput, stem, true
	default:
		return 0, "", false
	}
}

func normalizeArchivePath(name string) string {
	return strings.ReplaceAll(name, "\\", "/")
}

// ignoredArchivePath reports members excluded from pairing: any path segment
// starting with a dot (including editor and macOS metadata) or the __MACOSX
// directory emitted by Finder.
func ignoredArchivePath(name string) bool {
	for _, segment := range strings.Split(name, "/") {
		if segment == "" {
			continue
		}
		if strings.HasPrefix(segment, ".") || segment == "__MACOSX" {
			return true
		}
	}
	return false
}

func compressionRatioExceeded(uncompressed, compressed, maxRatio uint64) bool {
	if maxRatio == 0 || uncompressed == 0 {
		return false
	}
	if compressed == 0 {
		return true
	}
	if compressed > ^uint64(0)/maxRatio {
		return false
	}
	return uncompressed > compressed*maxRatio
}

func errorFinding(code, file, message string) Finding {
	return Finding{Severity: ProblemCheckSeverityError, Code: code, File: file, Message: message}
}

func warningFinding(code, file, message string) Finding {
	return Finding{Severity: ProblemCheckSeverityWarning, Code: code, File: file, Message: message}
}

// sortTestcaseKeys sorts case keys in natural order. The comparison is a total
// order so the resulting case ids are deterministic.
func sortTestcaseKeys(keys []string) {
	sort.Slice(keys, func(i, j int) bool { return naturalLess(keys[i], keys[j]) })
}

type naturalToken struct {
	digit bool
	text  string
}

// naturalLess compares two strings run by run. Digit runs compare by numeric
// magnitude without integer conversion; other runs compare case-insensitively
// with a byte-order tie-break. Digit runs always sort before non-digit runs at
// the same position, which keeps the relation transitive.
func naturalLess(a, b string) bool {
	left := naturalTokens(a)
	right := naturalTokens(b)
	for i := 0; i < len(left) && i < len(right); i++ {
		if comparison := compareNaturalTokens(left[i], right[i]); comparison != 0 {
			return comparison < 0
		}
	}
	return len(left) < len(right)
}

func naturalTokens(value string) []naturalToken {
	tokens := make([]naturalToken, 0, 4)
	for index := 0; index < len(value); {
		isDigit := value[index] >= '0' && value[index] <= '9'
		end := index + 1
		for end < len(value) && (value[end] >= '0' && value[end] <= '9') == isDigit {
			end++
		}
		tokens = append(tokens, naturalToken{digit: isDigit, text: value[index:end]})
		index = end
	}
	return tokens
}

func compareNaturalTokens(a, b naturalToken) int {
	if a.digit != b.digit {
		if a.digit {
			return -1
		}
		return 1
	}
	if a.digit {
		if comparison := compareDigitRuns(a.text, b.text); comparison != 0 {
			return comparison
		}
		return strings.Compare(a.text, b.text)
	}
	if comparison := strings.Compare(strings.ToLower(a.text), strings.ToLower(b.text)); comparison != 0 {
		return comparison
	}
	return strings.Compare(a.text, b.text)
}

// compareDigitRuns compares numeric strings by digit count then lexicographically
// after stripping leading zeros. This avoids integer overflow on long runs.
func compareDigitRuns(a, b string) int {
	a = strings.TrimLeft(a, "0")
	b = strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}
