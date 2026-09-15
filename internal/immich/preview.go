package immich

import (
	"context"
	"errors"
	"io"
	"net/http"
)

// MaxPreviewBytes bounds the provider representation before public headers commit.
const MaxPreviewBytes = 16 << 20

// ErrPreview is a fixed, log-safe representation validation outcome.
var ErrPreview = errors.New("immich: invalid preview response")

// Preview contains only validated representation metadata and a bounded stream.
// The caller must close Body, including for public HEAD requests.
type Preview struct {
	ContentType string
	Length      int64
	Body        io.ReadCloser
}

// Preview retrieves only the fixed generated preview. Callers must first authorize
// current Asset metadata with publication.Evaluate; this method grants no permission.
func (c *Client) Preview(ctx context.Context, id string) (Preview, error) {
	if !uuidV4.MatchString(id) {
		return Preview{}, ErrInvalidID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+id+"/thumbnail?size=preview", nil)
	if err != nil {
		return Preview{}, ErrTransport
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Accept", "image/jpeg, image/webp")
	// Require the actual representation length, not transparent decompression.
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.http.Do(req)
	if err != nil {
		return Preview{}, transportError(ctx, err)
	}
	if err := validatePreview(resp); err != nil {
		resp.Body.Close()
		return Preview{}, err
	}
	return Preview{
		ContentType: resp.Header.Get("Content-Type"),
		Length:      resp.ContentLength,
		Body:        &representationBody{ctx: ctx, body: resp.Body, remaining: resp.ContentLength},
	}, nil
}

// validatePreview rejects redirects, encodings and ambiguous representation headers.
func validatePreview(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return ErrMissing
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuth
	default:
		return ErrProvider
	}
	kind := resp.Header.Values("Content-Type")
	if len(kind) != 1 || (kind[0] != "image/jpeg" && kind[0] != "image/webp") {
		return ErrPreview
	}
	if resp.ContentLength <= 0 || resp.ContentLength > MaxPreviewBytes || len(resp.TransferEncoding) != 0 ||
		len(resp.Header.Values("Content-Length")) != 1 || len(resp.Header.Values("Content-Encoding")) != 0 ||
		len(resp.Header.Values("Content-Range")) != 0 {
		return ErrPreview
	}
	return nil
}
