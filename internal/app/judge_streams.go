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
// The stream names live in the configuration (redis.run_stream and friends),
// which derives them from redis.stream when they are left empty. The worker
// publishes to them and the agent consumes them, so there is one definition.

// judgeAgentStreams selects which request streams an agent consumes.
type judgeAgentStreams struct {
	submissions bool
	runs        bool
}

// parseJudgeAgentStreams reads redis.agent_streams. "all" (the default) keeps
// one agent on both streams and sharing its sandbox slots; "runs" lets an
// operator dedicate a process to the playground so it cannot touch submission
// latency.
func parseJudgeAgentStreams(value string) (judgeAgentStreams, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", config.AgentStreamsAll:
		return judgeAgentStreams{submissions: true, runs: true}, nil
	case config.AgentStreamsSubmissions:
		return judgeAgentStreams{submissions: true}, nil
	case config.AgentStreamsRuns:
		return judgeAgentStreams{runs: true}, nil
	default:
		return judgeAgentStreams{}, fmt.Errorf("redis.agent_streams must be %q, %q or %q, got %q",
			config.AgentStreamsAll, config.AgentStreamsSubmissions, config.AgentStreamsRuns, value)
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
				Stream:     cfg.Redis.Stream,
				Group:      cfg.Redis.AgentGroup,
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
				Stream:     cfg.Redis.RunStream,
				Group:      cfg.Redis.RunGroup,
				Consumer:   judgeAgentConsumerName(),
				MaxLen:     cfg.Redis.StreamMaxLen,
				DeadMaxLen: cfg.Redis.DeadStreamMaxLen,
			}),
		})
	}
	return queues
}
