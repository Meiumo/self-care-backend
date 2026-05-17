package subscription

import (
	"errors"
	"net/http"
	"strings"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

// LRAccessMiddleware gates POST .../live-response (generate) on /live-response.
// Feedback, followup and chat sub-routes are always allowed.
func (s *Service) LRAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only restrict the generate endpoint — chi does not strip r.URL.Path in middleware,
		// so we check the suffix rather than "/" alone.
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/live-response") {
			next.ServeHTTP(w, r)
			return
		}

		userID := jwtutil.UserID(r.Context())
		_, err := s.CheckAndConsumeLR(r.Context(), userID)
		if err != nil {
			if errors.Is(err, ErrTrialExpiredAndLimitReached) {
				respond.Error(w, http.StatusPaymentRequired,
					"trial expired — upgrade to premium")
				return
			}
			respond.Internal(w, err)
			return
		}

		next.ServeHTTP(w, r)
	})
}
