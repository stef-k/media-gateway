package config

import (
	"errors"
	"path"
	"slices"
	"strings"
	"unicode/utf8"
)

// validate rejects ambiguous root identities and duplicate segment/scope pairs.
func (p Policy) validate() error {
	if err := validateRoots(p.Roots); err != nil {
		return err
	}
	if len(p.Rules) == 0 {
		return errors.New("config: policy.rules must not be empty")
	}
	scopes := make(map[string]bool)
	for _, rule := range p.Rules {
		if err := validateRule(rule, p.Roots); err != nil {
			return err
		}
		// Sort a copy: scope ordering has no meaning and startup input is read only.
		roots := slices.Clone(rule.Roots)
		slices.Sort(roots)
		scope := rule.Segment + "\x00" + strings.Join(roots, ",")
		if scopes[scope] {
			return errors.New("config: policy.rules requires one rule per segment and unordered root scope")
		}
		scopes[scope] = true
	}
	return nil
}

// validateRoots requires meaningful, canonical and non-overlapping namespaces.
func validateRoots(roots []Root) error {
	if len(roots) == 0 {
		return errors.New("config: policy.roots must not be empty")
	}
	names := make(map[string]bool)
	for i, root := range roots {
		if !validRootName(root.Name) || names[root.Name] {
			return errors.New("config: policy.roots name requires unique 1..64 lowercase ASCII letters, digits or hyphens with alphanumeric ends")
		}
		names[root.Name] = true
		if root.Path == "/" || !path.IsAbs(root.Path) || path.Clean(root.Path) != root.Path || strings.Contains(root.Path, "\\") || unsafeText(root.Path) || !utf8.ValidString(root.Path) {
			return errors.New("config: policy.roots path requires canonical absolute POSIX paths other than /")
		}
		for _, previous := range roots[:i] {
			if root.Path == previous.Path || strings.HasPrefix(root.Path, previous.Path+"/") || strings.HasPrefix(previous.Path, root.Path+"/") {
				return errors.New("config: policy.roots paths must be unique and non-overlapping")
			}
		}
	}
	return nil
}

// validRootName accepts only stable consumer-safe ASCII identifiers.
func validRootName(name string) bool {
	if len(name) < 1 || len(name) > 64 || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

// validateRule keeps literal components, media and optional root references closed.
func validateRule(rule Rule, roots []Root) error {
	if rule.Segment == "" || rule.Segment == "." || rule.Segment == ".." || strings.ContainsAny(rule.Segment, `/\*?[]{}()|^$+`) || unsafeText(rule.Segment) || !utf8.ValidString(rule.Segment) {
		return errors.New("config: policy.rules segment must be a literal directory component without pattern syntax")
	}
	if len(rule.Media) == 0 {
		return errors.New("config: policy.rules media must not be empty")
	}
	media := make(map[string]bool)
	for _, kind := range rule.Media {
		if (kind != "image" && kind != "video") || media[kind] {
			return errors.New("config: policy.rules media requires unique image or video values")
		}
		media[kind] = true
	}
	if rule.Roots != nil && len(rule.Roots) == 0 {
		return errors.New("config: policy.rules roots must be omitted for global scope or contain root names")
	}
	seen := make(map[string]bool)
	for _, name := range rule.Roots {
		if seen[name] || !slices.ContainsFunc(roots, func(root Root) bool { return root.Name == name }) {
			return errors.New("config: policy.rules roots requires unique references to configured root names")
		}
		seen[name] = true
	}
	return nil
}
