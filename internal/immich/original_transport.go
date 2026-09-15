package immich

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// originalHTTP bounds connection/header work while leaving body lifetime to the
// caller. One HTTP/1 connection per original permits checking wire headers before
// net/http normalizes duplicate lengths or removes transfer-encoding fields.
func originalHTTP(timeout time.Duration) *http.Client {
	return originalHTTPWithInactivity(timeout, 60*time.Second)
}

// originalHTTPWithInactivity keeps the production lifetime fixed while permitting
// short-duration transport tests without changing operator configuration.
func originalHTTPWithInactivity(timeout, inactivity time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: min(5*time.Second, timeout)}
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			// No environment proxy, compression, retries via reused connections or HTTP/2.
			DisableKeepAlives:      true,
			DisableCompression:     true,
			ResponseHeaderTimeout:  timeout,
			MaxResponseHeaderBytes: 16 << 10,
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				conn, err := dialer.DialContext(ctx, network, address)
				return originalConnection(conn, err, inactivity)
			},
			DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				ctx, cancel := context.WithTimeout(ctx, min(5*time.Second, timeout))
				defer cancel()
				conn, err := (&tls.Dialer{NetDialer: dialer}).DialContext(ctx, network, address)
				return originalConnection(conn, err, inactivity)
			},
		},
	}
}

// originalConnection inspects only the first response header, retaining normal
// net.Conn deadlines/cancellation and net/http's status, framing and body parser.
func originalConnection(conn net.Conn, err error, inactivity time.Duration) (net.Conn, error) {
	if err != nil {
		return nil, err
	}
	return &originalConn{Conn: conn, reader: bufio.NewReaderSize(conn, 16<<10), inactivity: inactivity}, nil
}

// originalConn rejects ambiguous framing that the standard HTTP parser normalizes.
// Header storage is bounded; bytes following the header remain a streaming reader.
type originalConn struct {
	net.Conn
	reader     *bufio.Reader
	header     *bytes.Reader
	checked    bool
	inactivity time.Duration
}

// Read gates the first header block before handing it to net/http. Informational
// responses are rejected: this seam requires a direct final provider response.
func (c *originalConn) Read(p []byte) (int, error) {
	if !c.checked {
		header, err := c.readHeader()
		if err != nil {
			return 0, err
		}
		c.checked = true
		c.header = bytes.NewReader(header)
	}
	if c.header.Len() != 0 {
		return c.header.Read(p)
	}
	// Refresh only body I/O; response-header acquisition keeps its hard timeout.
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.inactivity)); err != nil {
		return 0, err
	}
	return c.reader.Read(p)
}

// readHeader caps raw headers and preserves duplicate field evidence before parsing.
func (c *originalConn) readHeader() ([]byte, error) {
	var header []byte
	for {
		line, err := c.reader.ReadSlice('\n')
		if err != nil || len(header)+len(line) > 16<<10 {
			return nil, ErrOriginal
		}
		header = append(header, line...)
		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
	}
	reader := textproto.NewReader(bufio.NewReader(bytes.NewReader(header)))
	status, err := reader.ReadLine()
	parts := strings.SplitN(status, " ", 3)
	if err != nil || len(parts) != 3 || strings.HasPrefix(parts[1], "1") {
		return nil, ErrOriginal
	}
	fields, err := reader.ReadMIMEHeader()
	if err != nil {
		return nil, ErrOriginal
	}
	// Error responses may be chunked; their status mapping precedes representation validation.
	if (parts[1] == "200" || parts[1] == "206") && (len(fields.Values("Content-Length")) > 1 || len(fields.Values("Transfer-Encoding")) != 0) {
		return nil, ErrOriginal
	}
	return header, nil
}
