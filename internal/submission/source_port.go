package submission

import "context"

type sourceWriter interface {
	Put(context.Context, string, int64, []byte) (SourceObject, error)
}

// sourceStorage is object storage as a run sees it. A run's source is uploaded
// before admission is decided, so a refusal has to be able to take the upload
// back -- which is why a run asks for more than sourceWriter.
type sourceStorage interface {
	sourceWriter
	Delete(ctx context.Context, storageKey string) error
}

type sourceReader interface {
	Get(context.Context, string) ([]byte, error)
}
