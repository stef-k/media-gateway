// Package publication evaluates provider metadata against validated configuration.
// It performs no I/O and neither identifies assets nor enables media delivery.
package publication

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stef-k/media-gateway/internal/config"
)

// Match contains only logical identity and the root-relative parent collection.
// It never contains the configured absolute root or absolute provider asset path.
type Match struct {
	RootName       string
	CollectionPath string
}

// Evaluate authorizes canonical provider metadata using immutable config.Load policy.
// It performs no I/O, cleaning, decoding or mutation. Only exact image/video media
// is accepted; eligibility does not establish delivery support. Denial returns zero
// context, including when an invalid in-memory policy resolves multiple roots.
func Evaluate(policy config.Policy, originalPath, media string) (Match, bool) {
	if (media != "image" && media != "video") || !canonicalAssetPath(originalPath) {
		return Match{}, false
	}
	var root config.Root
	relative := ""
	for _, candidate := range policy.Roots {
		if !canonicalAssetPath(candidate.Path) {
			return Match{}, false
		}
		if value, inside := strings.CutPrefix(originalPath, candidate.Path+"/"); inside {
			if relative != "" {
				return Match{}, false
			}
			root, relative = candidate, value
		}
	}
	// Exclude the basename and every directory at or above the unique root.
	slash := strings.LastIndexByte(relative, '/')
	if slash < 0 {
		return Match{}, false
	}
	collection := relative[:slash]
	dirs := strings.Split(collection, "/")
	for _, rule := range policy.Rules {
		if rule.Roots != nil && !slices.Contains(rule.Roots, root.Name) {
			continue
		}
		if slices.Contains(rule.Media, media) && slices.Contains(dirs, rule.Segment) {
			return Match{RootName: root.Name, CollectionPath: collection}, true
		}
	}
	return Match{}, false
}

// canonicalAssetPath validates POSIX metadata without repairing ambiguous input.
func canonicalAssetPath(value string) bool {
	if !strings.HasPrefix(value, "/") || !utf8.ValidString(value) ||
		strings.TrimSpace(value) != value || strings.Contains(value, "\\") ||
		strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	for _, component := range strings.Split(value[1:], "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}
