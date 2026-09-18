package immich

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPrivacyHiddenFields keeps non-sensitive validation active in either mode.
func TestPrivacyHiddenFields(t *testing.T) {
	for _, expose := range []bool{false, true} {
		for _, field := range []string{"width", "height", "duration", "fileCreatedAt", "localDateTime", "exifInfo"} {
			t.Run(fmt.Sprintf("%t/%s", expose, field), func(t *testing.T) {
				var item map[string]any
				json.Unmarshal([]byte(candidateJSON), &item)
				item[field] = "private-marker"
				raw, _ := json.Marshal(item)
				client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
					var query map[string]any
					if json.NewDecoder(r.Body).Decode(&query) != nil || query["withExif"] != expose {
						t.Error("wrong metadata request")
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, candidateResponse(string(raw), `null`, 1))
				}, time.Second)
				page, err := client.SearchCandidates(context.Background(), CandidateQuery{Limit: 1, IncludeSourceMetadata: expose})
				invalid := expose || field == "width" || field == "height" || field == "duration"
				if invalid {
					if err != ErrMetadata || page.Items != nil {
						t.Fatalf("invalid projected field accepted: %v", err)
					}
				} else if err != nil || len(page.Items) != 1 || strings.Contains(fmt.Sprint(page), "private-marker") {
					t.Fatalf("hidden metadata consumed: %v", err)
				}
			})
		}
	}
}
