package constant

const (
	JudgePending = iota
	JudgeInQueue
	JudgeAccepted
	Judge

	JudgeUnknown
)

var tab = []string{
	"Pending",
	"In Queue",
	"Processing",
	"Accepted",
	"Wrong Answer",
	"Time Limit Exceeded",
	"Compilation Error",
	"Runtime Error (SIGSEGV)",
	"Runtime Error (SIGXFSZ)",
	"Runtime Error (SIGFPE)",
	"Runtime Error (SIGABRT)",
	"Runtime Error (NZEC)",
	"Runtime Error",
	"Internal Error",
	"Exec Format Error",
}
