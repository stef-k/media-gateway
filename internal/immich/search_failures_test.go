package immich

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestSearchResponseFailures proves that any invalid item discards the entire page.
func TestSearchResponseFailures(t *testing.T) {
	good := candidateResponse(candidateJSON, `null`, 1)
	for _, tc := range []struct {
		name              string
		status            int
		contentType, body string
		want              error
	}{
		{"unauthorized", 401, "application/json", good, ErrAuth},
		{"forbidden", 403, "application/json", good, ErrAuth},
		{"missing", 404, "application/json", good, ErrMissing},
		{"server", 503, "application/json", good, ErrProvider},
		{"partial", 206, "application/json", good, ErrProvider},
		{"empty status", 204, "application/json", "", ErrProvider},
		{"wrong content type", 200, "text/html", good, ErrMetadata},
		{"missing content type", 200, "", good, ErrMetadata},
		{"malformed", 200, "application/json", "body-marker", ErrMetadata},
		{"trailing JSON", 200, "application/json", good + `{}`, ErrMetadata},
		{"oversized", 200, "application/json", good + strings.Repeat(" ", maxMetadata), ErrMetadata},
		{"missing assets", 200, "application/json", `{}`, ErrMetadata},
		{"null assets", 200, "application/json", `{"assets":null}`, ErrMetadata},
		{"missing items", 200, "application/json", `{"assets":{"count":0,"nextCursor":null}}`, ErrMetadata},
		{"null items", 200, "application/json", `{"assets":{"items":null,"count":0,"nextCursor":null}}`, ErrMetadata},
		{"count mismatch", 200, "application/json", candidateResponse(candidateJSON, `null`, 0), ErrMetadata},
		{"missing count", 200, "application/json", strings.Replace(good, `,"count":1`, "", 1), ErrMetadata},
		{"missing cursor", 200, "application/json", strings.Replace(good, `,"nextCursor":null`, "", 1), ErrMetadata},
		{"empty cursor", 200, "application/json", candidateResponse(candidateJSON, `""`, 1), ErrMetadata},
		{"cursor type", 200, "application/json", candidateResponse(candidateJSON, `42`, 1), ErrMetadata},
		{"cursor oversized", 200, "application/json", candidateResponse(candidateJSON, `"`+strings.Repeat("x", 1025)+`"`, 1), ErrMetadata},
		{"cursor control", 200, "application/json", candidateResponse(candidateJSON, `"bad\n"`, 1), ErrMetadata},
		{"excess candidates", 200, "application/json", candidateResponse(candidateJSON+","+candidateJSON, `null`, 2), ErrMetadata},
		{"missing ID", 200, "application/json", strings.Replace(good, `"id":"`+assetID+`",`, "", 1), ErrMetadata},
		{"invalid ID", 200, "application/json", strings.Replace(good, assetID, "../private", 1), ErrMetadata},
		{"empty path", 200, "application/json", strings.Replace(good, "/outside/private/../photo.jpg", "", 1), ErrMetadata},
		{"missing type", 200, "application/json", strings.Replace(good, `"type":"IMAGE",`, "", 1), ErrMetadata},
		{"unknown type", 200, "application/json", strings.Replace(good, "IMAGE", "FUTURE", 1), ErrUnsupported},
		{"missing width", 200, "application/json", strings.Replace(good, `"width":1200,`, "", 1), ErrMetadata},
		{"negative width", 200, "application/json", strings.Replace(good, `"width":1200`, `"width":-1`, 1), ErrMetadata},
		{"fraction height", 200, "application/json", strings.Replace(good, `"height":null`, `"height":1.5`, 1), ErrMetadata},
		{"overflow width", 200, "application/json", strings.Replace(good, `"width":1200`, `"width":9007199254740992`, 1), ErrMetadata},
		{"missing time", 200, "application/json", strings.Replace(good, `"fileCreatedAt":"2026-09-12T12:34:56.123Z",`, "", 1), ErrMetadata},
		{"invalid time", 200, "application/json", strings.Replace(good, "2026-09-12T15:34:56.123Z", "2026-02-30T15:34:56Z", 1), ErrMetadata},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = []string{tc.contentType}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}, time.Second)
			got, err := client.SearchCandidates(context.Background(), CandidateQuery{Limit: 1})
			if !errors.Is(err, tc.want) || got.Items != nil || got.NextCursor != "" {
				t.Fatalf("unexpected failure: %v", err)
			}
			for _, secret := range []string{testKey, client.searchEndpoint, strings.TrimSuffix(client.searchEndpoint, "/api/search/metadata"), "/outside/private/../photo.jpg", "body-marker"} {
				if strings.Contains(fmt.Sprintf("%+v", err), secret) {
					t.Fatal("sensitive error")
				}
			}
		})
	}
}

// TestSearchPageBounds checks empty, full and exact-ID results without silent truncation.
func TestSearchPageBounds(t *testing.T) {
	for _, tc := range []struct {
		name, items  string
		count, limit int
		id           string
		want         error
	}{
		{"empty", "", 0, 1, "", nil},
		{"maximum", strings.TrimSuffix(strings.Repeat(candidateJSON+",", 100), ","), 100, 100, "", nil},
		{"over maximum", strings.TrimSuffix(strings.Repeat(candidateJSON+",", 101), ","), 101, 100, "", ErrMetadata},
		{"exact missing", "", 0, 1, assetID, nil},
		{"exact mismatched", strings.Replace(candidateJSON, assetID, "b1234567-abcd-4abc-8abc-123456789abc", 1), 1, 1, assetID, ErrMetadata},
		{"exact excess", candidateJSON + "," + candidateJSON, 2, 100, assetID, ErrMetadata},
		{"exact case", candidateJSON, 1, 1, strings.ToUpper(assetID), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, candidateResponse(tc.items, `null`, tc.count))
			}, time.Second)
			got, err := client.SearchCandidates(context.Background(), CandidateQuery{Limit: tc.limit, ID: tc.id})
			if !errors.Is(err, tc.want) {
				t.Fatalf("unexpected result: %v", err)
			}
			if err != nil && (got.Items != nil || got.NextCursor != "") {
				t.Fatal("partial result escaped")
			}
			if err == nil && (len(got.Items) != tc.count || got.NextCursor != "") {
				t.Fatal("page count or terminal cursor changed")
			}
		})
	}
}
