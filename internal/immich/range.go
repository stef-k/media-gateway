package immich

import (
	"errors"
	"strconv"
	"strings"
)

// ErrRange is a sanitized caller syntax failure, checked only after authorization.
var ErrRange = errors.New("invalid range")

// ByteRange is a parsed single range. Its zero value means no range; private
// fields prevent callers from supplying unvalidated upstream header text.
type ByteRange struct {
	present      bool
	first, last  int64
	suffix, open bool
}

// ParseRange accepts one bounded byte range without whitespace, signs or lists.
func ParseRange(values []string) (ByteRange, error) {
	if len(values) == 0 {
		return ByteRange{}, nil
	}
	if len(values) != 1 || len(values[0]) > 128 {
		return ByteRange{}, ErrRange
	}
	unit, spec, ok := strings.Cut(values[0], "=")
	if !ok || !strings.EqualFold(unit, "bytes") {
		return ByteRange{}, ErrRange
	}
	first, last, ok := strings.Cut(spec, "-")
	if !ok || (first == "" && last == "") {
		return ByteRange{}, ErrRange
	}
	r := ByteRange{present: true, suffix: first == "", open: last == ""}
	var err error
	if first != "" {
		r.first, err = decimal(first)
		if err != nil {
			return ByteRange{}, ErrRange
		}
	}
	if last != "" {
		r.last, err = decimal(last)
		if err != nil {
			return ByteRange{}, ErrRange
		}
	}
	if (r.suffix && r.last == 0) || (!r.suffix && !r.open && r.first > r.last) {
		return ByteRange{}, ErrRange
	}
	return r, nil
}

// decimal admits unsigned decimal syntax in the positive signed-int64 domain.
func decimal(s string) (int64, error) {
	if s == "" {
		return 0, ErrRange
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, ErrRange
		}
	}
	return strconv.ParseInt(s, 10, 64)
}

// String serializes only validated numeric fields, never the caller's raw value.
func (r ByteRange) String() string {
	if !r.present {
		return ""
	}
	if r.suffix {
		return "bytes=-" + strconv.FormatInt(r.last, 10)
	}
	s := "bytes=" + strconv.FormatInt(r.first, 10) + "-"
	if !r.open {
		s += strconv.FormatInt(r.last, 10)
	}
	return s
}

// bounds computes the exact satisfiable interval for a provider's positive total.
func (r ByteRange) bounds(total int64) (int64, int64, bool) {
	if total <= 0 || !r.present {
		return 0, 0, false
	}
	if r.suffix {
		return max(0, total-r.last), total - 1, true
	}
	if r.first >= total {
		return 0, 0, false
	}
	if r.open {
		return r.first, total - 1, true
	}
	return r.first, min(r.last, total-1), true
}

// validateContentRange checks both wire grammar and the requested mathematics.
func validateContentRange(values []string, r ByteRange, length int64, unsatisfied bool) (string, error) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes ") {
		return "", ErrOriginal
	}
	interval, totalText, ok := strings.Cut(strings.TrimPrefix(values[0], "bytes "), "/")
	total, err := decimal(totalText)
	if !ok || err != nil || total <= 0 {
		return "", ErrOriginal
	}
	first, last, satisfiable := r.bounds(total)
	if unsatisfied {
		if interval != "*" || satisfiable {
			return "", ErrOriginal
		}
		return "bytes */" + strconv.FormatInt(total, 10), nil
	}
	startText, endText, ok := strings.Cut(interval, "-")
	start, e1 := decimal(startText)
	end, e2 := decimal(endText)
	if !ok || e1 != nil || e2 != nil || !satisfiable || start != first || end != last || length != end-start+1 {
		return "", ErrOriginal
	}
	return "bytes " + strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10) + "/" + strconv.FormatInt(total, 10), nil
}
