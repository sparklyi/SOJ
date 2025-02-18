package constant

const (
	JudgePD = iota
	JudgeIQ
	JudgePR
	JudgeAC
	JudgeWA
	JudgeTLE
	JudgeCE
	JudgeRESIGSEGV
	JudgeRESIGXFSZ
	JudgeRESIGFPE
	JudgeRESIGABRT
	JudgeRENZEC
	JudgeRE
	JudgeIE
	JudgeEFE
	JudgeUnknown
)

var JudgeCode2Details = []string{
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
	"Unknown Error",
}
