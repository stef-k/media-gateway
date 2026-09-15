package immich

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
)

// ErrOriginal is a fixed, log-safe original representation validation outcome.
var ErrOriginal = errors.New("immich: invalid original response")

// Original contains validated source framing and an unbuffered, length-enforced body.
// The caller must close Body, including when serving gateway HEAD.
type Original struct {
	ContentType string
	Length      int64
	Body        io.ReadCloser
}

// Original opens only source image bytes. The caller must first authorize fresh
// Asset metadata. No query opts into Immich's edited-media behavior.
func (c *Client) Original(ctx context.Context, id string) (Original, error) {
	if !ValidAssetID(id) {
		return Original{}, ErrInvalidID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+id+"/original", nil)
	if err != nil {
		return Original{}, ErrTransport
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := c.streaming.Do(req)
	if err != nil {
		return Original{}, transportError(ctx, err)
	}
	if err := validateOriginal(resp); err != nil {
		resp.Body.Close()
		return Original{}, err
	}
	return Original{ContentType: resp.Header.Get("Content-Type"), Length: resp.ContentLength,
		Body: &representationBody{ctx: ctx, body: resp.Body, remaining: resp.ContentLength}}, nil
}

// validateOriginal accepts direct source images, without preview's format/size ceiling.
func validateOriginal(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return ErrMissing
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuth
	default:
		return ErrProvider
	}
	kinds := resp.Header.Values("Content-Type")
	if len(kinds) != 1 {
		return ErrOriginal
	}
	kind, params, err := mime.ParseMediaType(kinds[0])
	if err != nil || !strings.HasPrefix(kind, "image/") || len(params) != 0 {
		return ErrOriginal
	}
	lengths := resp.Header.Values("Content-Length")
	if len(lengths) != 1 {
		return ErrOriginal
	}
	length, err := strconv.ParseInt(lengths[0], 10, 64)
	if err != nil || length <= 0 || length != resp.ContentLength || len(resp.TransferEncoding) != 0 ||
		len(resp.Header.Values("Content-Encoding")) != 0 || len(resp.Header.Values("Content-Range")) != 0 {
		return ErrOriginal
	}
	return nil
}
