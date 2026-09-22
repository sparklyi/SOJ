package app

import (
	"fmt"
	"strings"

	"SOJ/internal/config"
	"SOJ/internal/queue"

	"github.com/redis/go-redis/v9"
)

// Self-runs travel on their own request stream.
//
// A run and a submission are the same kind of work, so they share a table, a
// task lifecycle and a result stream. They do not share a request stream, for
// two reasons: playground traffic is high-volume and disposable, so it must
// never queue in front of a formal submission; and separating the streams is
// what makes it possible to run a second agent dedicated to runs, with its own
// parallelism, without changing any code.
//
// The names are defined once, here, because the worker publishes to them and the
// agent consumes them -- two independent literals would be a silent split.
func judgeRunStream(cfg config.Config) string {
	return envOr("SOJ_JUDGE_RUN_STREAM", cfg.Redis.Stream+":runs")
}

func judgeRunGroup() string {
	return envOr("SOJ_JUDGE_RUN_GROUP", "judge-run-agents")
}

// judgeAgentStreams selects which request streams an agent consumes.
type judgeAgentStreams struct {
	submissions bool
	runs        bool
}

const (
	judgeAgentStreamsAll         = "all"
	judgeAgentStreamsSubmissions = "submissions"
	judgeAgentStreamsRuns        = "runs"
)

// parseJudgeAgentStreams reads SOJ_JUDGE_AGENT_STREAMS. "all" (the default)
// keeps one agent on both streams and sharing its sandbox slots; "runs" lets an
// operator dedicate a process to the playground so it cannot touch submission
// latency.
func parseJudgeAgentStreams(value string) (judgeAgentStreams, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", judgeAgentStreamsAll:
		return judgeAgentStreams{submissions: true, runs: true}, nil
	case judgeAgentStreamsSubmissions:
		return judgeAgentStreams{submissions: true}, nil
	case judgeAgentStreamsRuns:
		return judgeAgentStreams{runs: true}, nil
	default:
		return judgeAgentStreams{}, fmt.Errorf("SOJ_JUDGE_AGENT_STREAMS must be %q, %q or %q, got %q", judgeAgentStreamsAll, judgeAgentStreamsSubmissions, judgeAgentStreamsRuns, value)
	}
}

// judgeRequestStream is one request stream an agent consumes, named so readiness
// can report which one is unreachable.
type judgeRequestStream struct {
	Name  string
	Queue queue.TaskQueue
}

// judgeAgentRequestQueues builds the streams this agent consumes, in a stable
// order so readiness keys do not move between restarts.
func judgeAgentRequestQueues(client *redis.Client, cfg config.Config, streams judgeAgentStreams) []judgeRequestStream {
	queues := make([]judgeRequestStream, 0, 2)
	if streams.submissions {
		queues = append(queues, judgeRequestStream{
			Name: "submissions",
			Queue: queue.NewRedisStreamQueue(client, queue.RedisStreamConfig{
				Stream:     envOr("SOJ_JUDGE_REQUEST_STREAM", cfg.Redis.Stream),
				Group:      envOr("SOJ_JUDGE_AGENT_GROUP", "judge-agents"),
				Consumer:   judgeAgentConsumerName(),
				MaxLen:     cfg.Redis.StreamMaxLen,
				DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
			}),
		})
	}
	if streams.runs {
		queues = append(queues, judgeRequestStream{
			Name: "runs",
			Queue: queue.NewRedisStreamQueue(client, queue.RedisStreamConfig{
				Stream:     judgeRunStream(cfg),
				Group:      judgeRunGroup(),
				Consumer:   judgeAgentConsumerName(),
				MaxLen:     cfg.Redis.StreamMaxLen,
				DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
			}),
		})
	}
	return queues
}
