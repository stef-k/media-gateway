// Package publication evaluates provider metadata against validated configuration.
// It performs no I/O and neither identifies assets nor enables media delivery.
package publication

import (
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stef-k/media-gateway/internal/config"
)

// Eligible reports whether any allowed root and exact directory rule permit media.
// Policy must originate from config.Load and remain unchanged during evaluation.
// originalPath is provider-reported metadata, never a URL or a local filesystem path
// to open. media must already be normalized to "image" or "video" by the provider
// adapter. Eligibility alone does not establish support for delivering that type.
// Inputs are read only; malformed metadata is denied without cleaning or decoding.
func Eligible(policy config.Policy, originalPath, media string) bool {
	if (media != "image" && media != "video") || !canonicalAssetPath(originalPath) {
		return false
	}
	for _, root := range policy.AllowedRoots {
		// Retain the separator so a root cannot match a neighboring name's prefix.
		prefix := root
		if root != "/" {
			prefix += "/"
		}
		relative, inside := strings.CutPrefix(originalPath, prefix)
		if !inside {
			continue
		}
		// Exclude the asset basename and every component at or above this root.
		slash := strings.LastIndexByte(relative, '/')
		if slash < 0 {
			continue
		}
		for _, dir := range strings.Split(relative[:slash], "/") {
			for _, rule := range policy.Rules {
				if dir == rule.Segment && slices.Contains(rule.Media, media) {
					return true
				}
			}
		}
	}
	return false
}

// canonicalAssetPath validates POSIX metadata without repairing ambiguous input.
func canonicalAssetPath(value string) bool {
	return value != "/" && path.IsAbs(value) && path.Clean(value) == value &&
		utf8.ValidString(value) && strings.TrimSpace(value) == value &&
		!strings.Contains(value, "\\") && !strings.ContainsFunc(value, unicode.IsControl)
}
