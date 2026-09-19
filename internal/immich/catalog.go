package immich

import (
	"slices"
	"strings"

	"github.com/stef-k/media-gateway/internal/config"
)

// DiscoveryStream is a gateway-owned root/convention candidate stream, not authority.
type DiscoveryStream struct {
	Root    config.Root
	Segment string
	Media   []string
}

// CollectionSelector resolves a logical consumer selector to trusted policy config.
// Path remains a relative directory selector; no filesystem is accessed.
type CollectionSelector struct {
	Root config.Root
	Path string
}

// DiscoveryStreams merges effective media sets and fixes root/segment traversal order.
func DiscoveryStreams(policy config.Policy) []DiscoveryStream {
	streams := []DiscoveryStream{}
	for _, root := range policy.Roots {
		mediaBySegment := map[string][]string{}
		for _, rule := range policy.Rules {
			if rule.Roots != nil && !slices.Contains(rule.Roots, root.Name) {
				continue
			}
			for _, media := range rule.Media {
				if !slices.Contains(mediaBySegment[rule.Segment], media) {
					mediaBySegment[rule.Segment] = append(mediaBySegment[rule.Segment], media)
				}
			}
		}
		for segment, media := range mediaBySegment {
			slices.Sort(media)
			streams = append(streams, DiscoveryStream{Root: root, Segment: segment, Media: media})
		}
	}
	slices.SortFunc(streams, func(a, b DiscoveryStream) int {
		if order := strings.Compare(a.Root.Name, b.Root.Name); order != 0 {
			return order
		}
		return strings.Compare(a.Segment, b.Segment)
	})
	return streams
}

// candidateFilter constructs only reviewed Search API v2 predicates. Immich's
// SQL patterns overmatch case/accents; every result still needs exact policy checks.
func candidateFilter(query CandidateQuery) map[string]any {
	media := []string{"IMAGE", "VIDEO"}
	pathFilter := map[string]string{}
	if stream := query.Discovery; stream != nil {
		media = make([]string, len(stream.Media))
		for i, value := range stream.Media {
			media[i] = strings.ToUpper(value)
		}
		pathFilter["startsWith"] = escapeSearchPattern(stream.Root.Path + "/")
		pathFilter["like"] = "/" + escapeSearchPattern(stream.Segment) + "/"
	}
	if collection := query.Collection; collection != nil {
		pathFilter["startsWith"] = escapeSearchPattern(collection.Root.Path + "/" + collection.Path + "/")
	}
	filter := map[string]any{
		"type":      map[string]any{"in": media},
		"isOffline": map[string]bool{"eq": false},
		"trashedAt": map[string]any{"eq": nil},
	}
	if len(pathFilter) > 0 {
		filter["originalPath"] = pathFilter
	}
	return filter
}

// escapeSearchPattern keeps configured percent/underscore literals in SQL LIKE.
// Immich supplies the surrounding wildcard for like and trailing one for startsWith.
func escapeSearchPattern(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
