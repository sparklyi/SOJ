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
	JudgeFreeze
)
const (
	PenaltyTime = 20
	ReliveTime  = 24 //解榜时间默认为比赛结束24h后
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
	"Freeze",
}

const (
	DefaultJudgeTimeLimit   = 2          //默认时间限制(s)
	DefaultJudgeMemoryLimit = 512 * 1024 //默认内存限制(KB)
)
