package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stef-k/media-gateway/internal/immich"
)

const maxConsumerCursorBytes = 2 << 10

// cursorKey is ephemeral process state; creation failure prevents binding a listener.
type cursorKey [32]byte

func newCursorKey(source io.Reader) (cursorKey, error) {
	var key cursorKey
	_, err := io.ReadFull(source, key[:])
	return key, err
}

// continuation contains no topology: version/kind/fingerprint, stream, provider cursor.
// The binary envelope avoids JSON escaping expanding a bounded provider token.
type continuation struct {
	Stream   int
	Provider string
}

// fingerprint hashes deterministic gateway-owned query definitions, never leaking paths.
func fingerprint(value any) [32]byte {
	body, _ := json.Marshal(value)
	return sha256.Sum256(body)
}

// sign authenticates a version-one continuation. Nil is the explicit terminal cursor.
func (key cursorKey) sign(kind byte, query [32]byte, state continuation) *string {
	body := make([]byte, 38, len(state.Provider)+70)
	body[0], body[1] = 1, kind
	copy(body[2:34], query[:])
	binary.BigEndian.PutUint32(body[34:38], uint32(state.Stream))
	body = append(body, state.Provider...)
	mac := hmac.New(sha256.New, key[:])
	mac.Write(body)
	token := base64.RawURLEncoding.EncodeToString(append(body, mac.Sum(nil)...))
	return &token
}

// verify rejects unauthenticated/cross-query state before any provider work.
func (key cursorKey) verify(token string, kind byte, query [32]byte, streams int) (continuation, bool) {
	if token == "" {
		return continuation{}, true
	}
	if len(token) > maxConsumerCursorBytes {
		return continuation{}, false
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(token)
	if err != nil || len(raw) < 70 || base64.RawURLEncoding.EncodeToString(raw) != token {
		return continuation{}, false
	}
	body, signature := raw[:len(raw)-32], raw[len(raw)-32:]
	mac := hmac.New(sha256.New, key[:])
	mac.Write(body)
	if !hmac.Equal(signature, mac.Sum(nil)) || body[0] != 1 || body[1] != kind || !hmac.Equal(body[2:34], query[:]) {
		return continuation{}, false
	}
	state := continuation{Stream: int(binary.BigEndian.Uint32(body[34:38])), Provider: string(body[38:])}
	if state.Stream >= streams || len(state.Provider) > immich.MaxCandidateCursorBytes || !utf8.ValidString(state.Provider) || strings.ContainsFunc(state.Provider, unicode.IsControl) {
		return continuation{}, false
	}
	if kind == 'a' && state.Provider == "" {
		return continuation{}, false
	}
	return state, true
}
