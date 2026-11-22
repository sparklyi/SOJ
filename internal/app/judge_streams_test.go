package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"SOJ/internal/config"

	"github.com/redis/go-redis/v9"
)

// TestParseJudgeAgentStreams covers the values an operator can put in
// redis.agent_streams: the empty default, every accepted name in any case with
// surrounding whitespace, and a value that names no stream at all.
func TestParseJudgeAgentStreams(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		want      judgeAgentStreams
		wantErr   bool
		errSubstr string
	}{
		{
			name:  "blank defaults to all",
			value: "",
			want:  judgeAgentStreams{submissions: true, runs: true},
		},
		{
			name:  "whitespace only defaults to all",
			value: "   \t",
			want:  judgeAgentStreams{submissions: true, runs: true},
		},
		{
			name:  "all",
			value: "all",
			want:  judgeAgentStreams{submissions: true, runs: true},
		},
		{
			name:  "all is case insensitive",
			value: "ALL",
			want:  judgeAgentStreams{submissions: true, runs: true},
		},
		{
			name:  "all is trimmed",
			value: " All ",
			want:  judgeAgentStreams{submissions: true, runs: true},
		},
		{
			name:  "submissions",
			value: "submissions",
			want:  judgeAgentStreams{submissions: true},
		},
		{
			name:  "submissions is case insensitive",
			value: "Submissions",
			want:  judgeAgentStreams{submissions: true},
		},
		{
			name:  "runs",
			value: "runs",
			want:  judgeAgentStreams{runs: true},
		},
		{
			name:  "runs is case insensitive",
			value: "RUNS",
			want:  judgeAgentStreams{runs: true},
		},
		{
			name:      "unknown value is refused",
			value:     "both",
			wantErr:   true,
			errSubstr: "both",
		},
		{
			name:      "partial name is refused",
			value:     "run",
			wantErr:   true,
			errSubstr: "run",
		},
		{
			name:      "comma separated list is refused rather than picking one",
			value:     "runs,submissions",
			wantErr:   true,
			errSubstr: "runs,submissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseJudgeAgentStreams(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseJudgeAgentStreams(%q) returned %+v, want error", tt.value, got)
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Fatalf("error %q does not name the rejected value %q", err, tt.errSubstr)
				}
				for _, allowed := range []string{config.AgentStreamsAll, config.AgentStreamsSubmissions, config.AgentStreamsRuns} {
					if !strings.Contains(err.Error(), allowed) {
						t.Fatalf("error %q does not name the allowed value %q", err, allowed)
					}
				}
				if got != (judgeAgentStreams{}) {
					t.Fatalf("streams = %+v on error, want zero value", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseJudgeAgentStreams(%q) returned error: %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("streams = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestJudgeAgentRequestQueuesSelectsStreams checks that each selection builds
// exactly the request streams the agent will consume, in the same order every
// time, and that building them does not touch Redis: the client is nil, so any
// command would panic on the nil pointer.
func TestJudgeAgentRequestQueuesSelectsStreams(t *testing.T) {
	cfg := config.Config{}
	cfg.Redis.Stream = "soj:judge:tasks"
	cfg.Redis.RunStream = "soj:judge:tasks:runs"

	tests := []struct {
		name    string
		streams judgeAgentStreams
		want    []string
	}{
		{
			name:    "all consumes submissions before runs",
			streams: judgeAgentStreams{submissions: true, runs: true},
			want:    []string{"submissions", "runs"},
		},
		{
			name:    "submissions only",
			streams: judgeAgentStreams{submissions: true},
			want:    []string{"submissions"},
		},
		{
			name:    "runs only",
			streams: judgeAgentStreams{runs: true},
			want:    []string{"runs"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Five builds, because readiness keys on these names and a map
			// iteration order anywhere in the path would show up as a flap.
			for i := 0; i < 5; i++ {
				queues := judgeAgentRequestQueues(nil, cfg, tt.streams)
				if len(queues) != len(tt.want) {
					t.Fatalf("build %d: got %d queues, want %d", i, len(queues), len(tt.want))
				}
				for j, want := range tt.want {
					if queues[j].Name != want {
						t.Fatalf("build %d: queue %d named %q, want %q", i, j, queues[j].Name, want)
					}
					if queues[j].Queue == nil {
						t.Fatalf("build %d: queue %d (%s) has no task queue", i, j, queues[j].Name)
					}
				}
			}
		})
	}
}

// TestJudgeAgentRequestQueuesReflectsParsedSelection keeps the parse and the
// queue set in agreement: whatever redis.agent_streams names must be exactly the
// streams the agent then consumes, so a new value cannot parse but consume
// nothing.
func TestJudgeAgentRequestQueuesReflectsParsedSelection(t *testing.T) {
	cfg := config.Config{}
	cfg.Redis.Stream = "soj:judge:tasks"
	cfg.Redis.RunStream = "soj:judge:tasks:runs"

	tests := []struct {
		value string
		want  []string
	}{
		{value: "", want: []string{"submissions", "runs"}},
		{value: config.AgentStreamsAll, want: []string{"submissions", "runs"}},
		{value: config.AgentStreamsSubmissions, want: []string{"submissions"}},
		{value: config.AgentStreamsRuns, want: []string{"runs"}},
	}

	for _, tt := range tests {
		t.Run("agent_streams="+tt.value, func(t *testing.T) {
			streams, err := parseJudgeAgentStreams(tt.value)
			if err != nil {
				t.Fatalf("parseJudgeAgentStreams(%q) returned error: %v", tt.value, err)
			}
			queues := judgeAgentRequestQueues(nil, cfg, streams)
			got := make([]string, 0, len(queues))
			for _, q := range queues {
				got = append(got, q.Name)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("queues = %v, want %v", got, tt.want)
			}
		})
	}
}

// recordingHook records the commands a queue issues and answers them without
// sending anything, so a test can see which Redis stream and consumer group a
// queue is wired to with no server behind it.
type recordingHook struct {
	commands *[][2]string
}

func (h recordingHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h recordingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		args := cmd.Args()
		// XGROUP CREATE <stream> <group> <id> MKSTREAM
		if len(args) >= 4 {
			if name, ok := args[0].(string); ok && name == "xgroup" {
				stream, _ := args[2].(string)
				group, _ := args[3].(string)
				*h.commands = append(*h.commands, [2]string{stream, group})
			}
		}
		// Nothing is sent: the point is the wiring, not a round trip.
		return nil
	}
}

func (h recordingHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestJudgeAgentRequestQueuesTargetConfiguredStreams checks the wiring the
// labels alone cannot: each selection must build queues against the configured
// request stream and consumer group, so a submissions/runs swap would fail
// here even though the names would still read correctly.
func TestJudgeAgentRequestQueuesTargetConfiguredStreams(t *testing.T) {
	cfg := config.Config{}
	cfg.Redis.Stream = "soj:judge:tasks"
	cfg.Redis.RunStream = "custom:runs"
	cfg.Redis.AgentGroup = "judge-agents"
	cfg.Redis.RunGroup = "judge-run-agents"

	tests := []struct {
		name       string
		streams    judgeAgentStreams
		wantWiring [][2]string
	}{
		{
			name:    "all",
			streams: judgeAgentStreams{submissions: true, runs: true},
			wantWiring: [][2]string{
				{cfg.Redis.Stream, cfg.Redis.AgentGroup},
				{cfg.Redis.RunStream, cfg.Redis.RunGroup},
			},
		},
		{
			name:       "submissions only",
			streams:    judgeAgentStreams{submissions: true},
			wantWiring: [][2]string{{cfg.Redis.Stream, cfg.Redis.AgentGroup}},
		},
		{
			name:       "runs only",
			streams:    judgeAgentStreams{runs: true},
			wantWiring: [][2]string{{cfg.Redis.RunStream, cfg.Redis.RunGroup}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commands [][2]string
			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
			client.AddHook(recordingHook{commands: &commands})
			defer client.Close()

			queues := judgeAgentRequestQueues(client, cfg, tt.streams)
			if len(commands) != 0 {
				t.Fatalf("building queues issued %d commands, want none", len(commands))
			}

			ctx := context.Background()
			for _, q := range queues {
				if err := q.Queue.Ensure(ctx); err != nil {
					t.Fatalf("Ensure(%s) returned error: %v", q.Name, err)
				}
			}

			if len(commands) != len(tt.wantWiring) {
				t.Fatalf("wiring = %v, want %v", commands, tt.wantWiring)
			}
			for i, want := range tt.wantWiring {
				if commands[i] != want {
					t.Fatalf("queue %d wired to %v, want %v", i, commands[i], want)
				}
			}
		})
	}
}
