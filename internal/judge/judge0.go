package judge

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Judge0Client struct {
	baseURL    string
	httpClient *http.Client
	apiKey     string
}

func NewJudge0Client(baseURL string, httpClient *http.Client, apiKey string) *Judge0Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Judge0Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		apiKey:     apiKey,
	}
}

func (c *Judge0Client) Judge(ctx context.Context, request Request) (Result, error) {
	if c.baseURL == "" {
		return Result{}, fmt.Errorf("judge0 base url is empty")
	}
	if len(request.Testcases) == 0 {
		return c.submit(ctx, request, "")
	}

	var final Result
	final.Verdict = VerdictAccepted
	final.JudgedAt = time.Now().UTC()
	for _, tc := range request.Testcases {
		result, err := c.submit(ctx, request, tc.InputKey, tc.ExpectedOutputKey)
		if err != nil {
			return Result{}, err
		}
		if result.TimeMS > final.TimeMS {
			final.TimeMS = result.TimeMS
		}
		if result.MemoryKB > final.MemoryKB {
			final.MemoryKB = result.MemoryKB
		}
		if result.Verdict != VerdictAccepted {
			final = result
			break
		}
	}
	return final, nil
}

func (c *Judge0Client) Languages(ctx context.Context) ([]Language, error) {
	endpoint := c.baseURL + "/languages"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	c.decorate(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("judge0 languages status %d", resp.StatusCode)
	}

	var payload []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	languages := make([]Language, 0, len(payload))
	for _, item := range payload {
		languages = append(languages, Language{
			ID:        item.ID,
			Name:      item.Name,
			Enabled:   true,
			TimeLimit: time.Second,
			MemoryKB:  262144,
		})
	}
	return languages, nil
}

func (c *Judge0Client) submit(ctx context.Context, request Request, stdin string, expectedOutput ...string) (Result, error) {
	body := map[string]any{
		"language_id": request.LanguageID,
		"source_code": base64.StdEncoding.EncodeToString(request.Source),
		"stdin":       base64.StdEncoding.EncodeToString([]byte(firstNonEmpty(stdin, request.Stdin))),
	}
	if expected := firstNonEmpty(expectedOutput...); expected != "" {
		body["expected_output"] = base64.StdEncoding.EncodeToString([]byte(expected))
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}

	endpoint := c.baseURL + "/submissions?" + url.Values{
		"base64_encoded": []string{"true"},
		"wait":           []string{"true"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.decorate(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("judge0 submission status %d", resp.StatusCode)
	}

	var payload struct {
		Stdout        string `json:"stdout"`
		Stderr        string `json:"stderr"`
		CompileOutput string `json:"compile_output"`
		Message       string `json:"message"`
		Time          string `json:"time"`
		Memory        int    `json:"memory"`
		Status        struct {
			ID          int    `json:"id"`
			Description string `json:"description"`
		} `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}
	timeMS := 0
	if seconds, err := strconv.ParseFloat(payload.Time, 64); err == nil {
		timeMS = int(seconds * 1000)
	}
	return Result{
		Verdict:       mapJudge0Verdict(payload.Status.ID),
		TimeMS:        timeMS,
		MemoryKB:      payload.Memory,
		Stdout:        decodeMaybeBase64(payload.Stdout),
		Stderr:        decodeMaybeBase64(payload.Stderr),
		CompileOutput: decodeMaybeBase64(payload.CompileOutput),
		ErrorMessage:  firstNonEmpty(decodeMaybeBase64(payload.Message), payload.Status.Description),
		JudgedAt:      time.Now().UTC(),
	}, nil
}

func (c *Judge0Client) decorate(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("X-Auth-Token", c.apiKey)
	}
}

func mapJudge0Verdict(statusID int) Verdict {
	switch statusID {
	case 3:
		return VerdictAccepted
	case 4:
		return VerdictWrongAnswer
	case 5:
		return VerdictTimeLimitExceeded
	case 6:
		return VerdictCompileError
	case 7, 8, 9, 10, 11, 12:
		return VerdictRuntimeError
	default:
		return VerdictSystemError
	}
}

func decodeMaybeBase64(value string) string {
	if value == "" {
		return ""
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return value
	}
	return string(decoded)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
