package submission

import "context"

type sourceWriter interface {
	Put(context.Context, string, int64, []byte) (SourceObject, error)
}

type sourceReader interface {
	Get(context.Context, string) ([]byte, error)
}
