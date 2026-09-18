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
	ContentType  string
	Length       int64
	Body         io.ReadCloser
	Status       int
	ContentRange string
}

// Original opens only source image bytes. The caller must first authorize fresh
// Asset metadata. No query opts into Immich's edited-media behavior.
func (c *Client) Original(ctx context.Context, id string) (Original, error) {
	return c.original(ctx, id, "image", ByteRange{})
}

// VideoOriginal opens exact video source bytes with a validated optional range.
func (c *Client) VideoOriginal(ctx context.Context, id string, requested ByteRange) (Original, error) {
	source, err := c.original(ctx, id, "video", requested)
	if err == ErrMissing && requested.present {
		return c.disambiguateRangeMissing(ctx, id, requested)
	}
	return source, err
}

// disambiguateRangeMissing handles Immich 3.2.0's pre-header file-send 404.
// One un-ranged source GET supplies existence/length only; its body is never read.
func (c *Client) disambiguateRangeMissing(ctx context.Context, id string, requested ByteRange) (Original, error) {
	probe, err := c.original(ctx, id, "video", ByteRange{})
	if err != nil {
		return Original{}, err
	}
	probe.Body.Close()
	if _, _, satisfiable := requested.bounds(probe.Length); satisfiable {
		return Original{}, ErrOriginal
	}
	return Original{Status: http.StatusRequestedRangeNotSatisfiable,
		ContentRange: "bytes */" + strconv.FormatInt(probe.Length, 10),
		Body:         io.NopCloser(strings.NewReader(""))}, nil
}

// original shares the fixed source operation for already-authorized media kinds.
func (c *Client) original(ctx context.Context, id, media string, requested ByteRange) (Original, error) {
	if !ValidAssetID(id) {
		return Original{}, ErrInvalidID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+id+"/original", nil)
	if err != nil {
		return Original{}, ErrTransport
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Accept-Encoding", "identity")
	if requested.present {
		req.Header.Set("Range", requested.String())
	}
	resp, err := c.streaming.Do(req)
	if err != nil {
		return Original{}, transportError(ctx, err)
	}
	if err := validateOriginal(resp, media, requested); err != nil {
		resp.Body.Close()
		return Original{}, err
	}
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		resp.Body.Close()
		return Original{Status: resp.StatusCode, ContentRange: resp.Header.Get("Content-Range"), Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return Original{Status: resp.StatusCode, ContentRange: resp.Header.Get("Content-Range"), ContentType: resp.Header.Get("Content-Type"), Length: resp.ContentLength,
		Body: &representationBody{ctx: ctx, body: resp.Body, remaining: resp.ContentLength}}, nil
}

// validateOriginal checks status, media type and exact range framing before any
// public success, without preview's format/size ceiling.
func validateOriginal(resp *http.Response, media string, requested ByteRange) error {
	switch resp.StatusCode {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
	case http.StatusNotFound:
		return ErrMissing
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrAuth
	default:
		return ErrProvider
	}
	if requested.present {
		if resp.StatusCode != 206 && resp.StatusCode != 416 {
			return ErrOriginal
		}
		ranges := resp.Header.Values("Accept-Ranges")
		if len(ranges) > 1 || (len(ranges) == 1 && ranges[0] != "bytes") {
			return ErrOriginal
		}
		if resp.StatusCode == 416 {
			value, err := validateContentRange(resp.Header.Values("Content-Range"), requested, 0, true)
			if err != nil {
				return err
			}
			resp.Header.Set("Content-Range", value)
			return nil
		}
	} else if resp.StatusCode != 200 {
		return ErrOriginal
	}
	kinds := resp.Header.Values("Content-Type")
	if len(kinds) != 1 {
		return ErrOriginal
	}
	kind, params, err := mime.ParseMediaType(kinds[0])
	if err != nil || !(strings.HasPrefix(kind, media+"/") || (media == "video" && kind == "application/mxf")) || len(params) != 0 {
		return ErrOriginal
	}
	lengths := resp.Header.Values("Content-Length")
	if len(lengths) != 1 {
		return ErrOriginal
	}
	length, err := decimal(lengths[0])
	if err != nil || length <= 0 || length != resp.ContentLength || len(resp.TransferEncoding) != 0 ||
		len(resp.Header.Values("Content-Encoding")) != 0 {
		return ErrOriginal
	}
	if requested.present {
		value, err := validateContentRange(resp.Header.Values("Content-Range"), requested, length, false)
		if err != nil {
			return err
		}
		resp.Header.Set("Content-Range", value)
	} else if len(resp.Header.Values("Content-Range")) != 0 {
		return ErrOriginal
	}
	return nil
}
