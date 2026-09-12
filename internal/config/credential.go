package config

import (
	"errors"
	"io"
	"os"
	"strings"
)

// readCredential accepts a protected regular file containing one ASCII token.
// A single editor-style final newline is allowed; other whitespace is rejected.
func readCredential(filename string) (string, error) {
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("config: credential must be a readable regular file")
	}
	f, err := os.Open(filename)
	if err != nil {
		return "", errors.New("config: cannot open credential file")
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0400 == 0 {
		return "", errors.New("config: credential file requires owner read permission and no group or other permissions")
	}
	const maxKey = 4096
	data, err := io.ReadAll(io.LimitReader(f, maxKey+1))
	if err != nil || len(data) > maxKey {
		return "", errors.New("config: cannot read credential within 4096 byte limit")
	}
	key := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if key == "" {
		return "", errors.New("config: credential must contain a non-empty ASCII token")
	}
	for _, r := range key {
		if r < 33 || r > 126 {
			return "", errors.New("config: credential must contain one ASCII token without whitespace")
		}
	}
	return key, nil
}
