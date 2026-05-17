// Package payments integrates with Продамус to create and confirm payments.
package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	premiumAmount = "299"
	premiumName   = "SelfCare Premium"
)

// Service handles payment link creation and webhook processing.
type Service struct {
	db          *pgxpool.Pool
	payformURL  string // e.g. https://yourshop.payform.ru
	secretKey   string // Продамус secret key for HMAC verification
	successURL  string
}

func NewService(db *pgxpool.Pool, payformURL, secretKey, successURL string) *Service {
	return &Service{
		db:         db,
		payformURL: payformURL,
		secretKey:  secretKey,
		successURL: successURL,
	}
}

// CreatePaymentURL builds a Продамус payment link for the given user.
// The link encodes order params as query parameters — no HTTP call required.
func (s *Service) CreatePaymentURL(userID int64) string {
	params := url.Values{}
	params.Set("do", "pay")
	params.Set("amount", premiumAmount)
	params.Set("order_id", fmt.Sprintf("user_%d", userID))
	params.Set("customer_extra", strconv.FormatInt(userID, 10))
	params.Set("products[0][name]", premiumName)
	params.Set("products[0][price]", premiumAmount)
	params.Set("products[0][quantity]", "1")
	params.Set("success_url", s.successURL)
	return s.payformURL + "/?" + params.Encode()
}

// HandleWebhook processes a Продамус payment webhook.
// Продамус sends application/x-www-form-urlencoded; payment_status == "success" means paid.
func (s *Service) HandleWebhook(ctx context.Context, payload []byte, sig string) error {
	if !s.validSignature(payload, sig) {
		return fmt.Errorf("webhook: invalid signature")
	}

	// Продамус sends form-encoded data, NOT JSON.
	values, err := url.ParseQuery(string(payload))
	if err != nil {
		return fmt.Errorf("webhook: parse form: %w", err)
	}

	if values.Get("payment_status") != "success" {
		return nil
	}

	customerExtra := values.Get("customer_extra")
	userID, err := strconv.ParseInt(customerExtra, 10, 64)
	if err != nil || userID == 0 {
		return fmt.Errorf("webhook: invalid customer_extra %q", customerExtra)
	}

	_, err = s.db.Exec(ctx,
		`UPDATE users SET is_premium = TRUE WHERE id = $1`,
		userID,
	)
	return err
}

func (s *Service) validSignature(payload []byte, sig string) bool {
	mac := hmac.New(sha256.New, []byte(s.secretKey))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}
