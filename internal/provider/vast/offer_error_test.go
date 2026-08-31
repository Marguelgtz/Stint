package vast

import (
	"errors"
	"net/http"
	"testing"
)

func TestIsOfferUnavailableError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "observed no such ask 400",
			err: &APIError{
				StatusCode: http.StatusBadRequest,
				Status:     "400 Bad Request",
				Detail:     "error 404/3603: no_such_ask Instance type by id 37888568 is not available.",
			},
			want: true,
		},
		{name: "ordinary not found", err: &APIError{StatusCode: http.StatusNotFound}, want: true},
		{name: "gone", err: &APIError{StatusCode: http.StatusGone}, want: true},
		{name: "classified legacy unavailable", err: errors.New(unavailableOfferMessage), want: true},
		{name: "bad request other", err: &APIError{StatusCode: http.StatusBadRequest, Detail: "invalid image"}},
		{name: "forbidden", err: &APIError{StatusCode: http.StatusForbidden, Detail: "permission denied"}},
		{name: "rate limit", err: &APIError{StatusCode: http.StatusTooManyRequests, Detail: "slow down"}},
		{name: "generic", err: errors.New("network timeout")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsOfferUnavailableError(tt.err); got != tt.want {
				t.Fatalf("IsOfferUnavailableError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
