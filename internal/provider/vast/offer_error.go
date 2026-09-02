package vast

import (
	"errors"
	"net/http"
	"strings"
)

const unavailableOfferMessage = "selected Vast offer is no longer available; rerun stint start"

// IsOfferUnavailableError reports marketplace races where an offer returned by
// search disappears before the create request reaches Vast. Vast normally uses
// 404/410 for this condition, but has also been observed returning HTTP 400
// with an embedded no_such_ask error code.
//
// Authentication, permission, rate-limit, billing, and arbitrary provider
// errors deliberately remain fatal so the lifecycle does not burn additional
// candidate attempts on failures another host cannot fix.
func IsOfferUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	if err.Error() == unavailableOfferMessage {
		return true
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if apiErr.StatusCode == http.StatusNotFound || apiErr.StatusCode == http.StatusGone {
		return true
	}
	return apiErr.StatusCode == http.StatusBadRequest && strings.Contains(strings.ToLower(apiErr.Detail), "no_such_ask")
}
