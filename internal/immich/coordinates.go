package immich

import (
	"encoding/json"
	"math"
)

// decodeCoordinates discards unrelated EXIF and accepts only a nullable pair.
// JSON decoding rejects nonnumbers and overflowing numbers without exposing values.
func decodeCoordinates(raw json.RawMessage) (*float64, *float64, error) {
	var exif struct {
		Latitude  json.RawMessage `json:"latitude"`
		Longitude json.RawMessage `json:"longitude"`
	}
	if len(raw) != 0 && json.Unmarshal(raw, &exif) != nil {
		return nil, nil, ErrMetadata
	}
	// RawMessage is nil only when absent; explicit null retains its JSON bytes.
	if (exif.Latitude == nil) != (exif.Longitude == nil) {
		return nil, nil, ErrMetadata
	}
	if exif.Latitude == nil {
		return nil, nil, nil
	}
	var lat, lon *float64
	if json.Unmarshal(exif.Latitude, &lat) != nil || json.Unmarshal(exif.Longitude, &lon) != nil {
		return nil, nil, ErrMetadata
	}
	if (lat == nil) != (lon == nil) {
		return nil, nil, ErrMetadata
	}
	if lat != nil && (math.IsNaN(*lat) || math.IsInf(*lat, 0) || *lat < -90 || *lat > 90 ||
		math.IsNaN(*lon) || math.IsInf(*lon, 0) || *lon < -180 || *lon > 180) {
		return nil, nil, ErrMetadata
	}
	return lat, lon, nil
}
