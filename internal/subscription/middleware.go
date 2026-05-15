package subscription

import (
	"errors"
	"net/http"

	"github.com/romangolovachev/selfcare/pkg/jwtutil"
	"github.com/romangolovachev/selfcare/pkg/respond"
)

// LRAccessMiddleware gates POST / (generate) on /live-response.
// Feedback and followup sub-routes are always allowed.
func (s *Service) LRAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only restrict the generate endpoint
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			next.ServeHTTP(w, r)
			return
		}

		userID := jwtutil.UserID(r.Context())
		_, err := s.CheckAndConsumeLR(r.Context(), userID)
		if err != nil {
			if errors.Is(err, ErrTrialExpiredAndLimitReached) {
				respond.Error(w, http.StatusPaymentRequired,
					"trial expired — upgrade to premium or wait for monthly reset")
				return
			}
			respond.Internal(w, err)
			return
		}

		next.ServeHTTP(w, r)
	})
}
