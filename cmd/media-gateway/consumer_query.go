package main

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
)

// catalogQuery contains validated HTTP selectors, never provider filter syntax.
type catalogQuery struct {
	Kind                   byte
	Limit                  int
	Cursor, ID, Collection string
	Root                   config.Root
}

// consumerQuery validates exact route/query syntax without repairing selectors.
func consumerQuery(r *http.Request, policy config.Policy) (catalogQuery, bool) {
	if r.Method != http.MethodGet || r.URL.RawPath != "" || r.URL.IsAbs() || r.URL.Opaque != "" || len(r.URL.RawQuery) > maxConsumerQueryBytes {
		return catalogQuery{}, false
	}
	query := catalogQuery{Kind: 'a', Limit: defaultConsumerLimit}
	switch r.URL.Path {
	case "/catalog/collections":
		query.Kind = 'c'
	case "/catalog/assets":
	default:
		id, found := strings.CutPrefix(r.URL.Path, "/catalog/assets/")
		return catalogQuery{Kind: 'a', ID: id, Limit: 1}, found && immich.ValidAssetID(id) && r.URL.RawQuery == "" && !r.URL.ForceQuery
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return catalogQuery{}, false
	}
	for name, values := range values {
		if len(values) != 1 || values[0] == "" {
			return catalogQuery{}, false
		}
		value := values[0]
		switch name {
		case "limit":
			if strings.ContainsFunc(value, func(c rune) bool { return c < '0' || c > '9' }) {
				return catalogQuery{}, false
			}
			query.Limit, err = strconv.Atoi(value)
			if err != nil || query.Limit < 1 || query.Limit > immich.MaxCandidates {
				return catalogQuery{}, false
			}
		case "cursor":
			if len(value) > maxConsumerCursorBytes {
				return catalogQuery{}, false
			}
			query.Cursor = value
		case "root":
			if query.Kind != 'a' {
				return catalogQuery{}, false
			}
			for _, root := range policy.Roots {
				if root.Name == value {
					query.Root = root
					break
				}
			}
		case "collection":
			if query.Kind != 'a' || !validCollection(value) {
				return catalogQuery{}, false
			}
			query.Collection = value
		default:
			return catalogQuery{}, false
		}
	}
	return query, query.Kind == 'c' || (query.Root.Name != "" && query.Collection != "")
}

// validCollection enforces canonical relative POSIX directory syntax after query decoding.
func validCollection(value string) bool {
	if value == "" || len(value) > 2<<10 || !utf8.ValidString(value) || strings.Contains(value, `\`) || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
