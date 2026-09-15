package immich

import (
	"context"
	"io"
)

// representationBody enforces the declared bound and sanitizes errors throughout streaming.
// A short body is an error, never a successfully completed smaller representation.
type representationBody struct {
	ctx       context.Context
	body      io.ReadCloser
	remaining int64
}

// Read never yields more than the validated length or exposes a raw upstream error.
func (b *representationBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	if err == io.EOF && b.remaining == 0 {
		return n, io.EOF
	}
	if err != nil {
		return n, transportError(b.ctx, err)
	}
	return n, nil
}

// Close releases the upstream body without exposing transport-specific diagnostics.
func (b *representationBody) Close() error {
	if err := b.body.Close(); err != nil {
		return transportError(b.ctx, err)
	}
	return nil
}
