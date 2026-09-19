package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Amonochuka/ganji-backend/internal/deals"
	"github.com/Amonochuka/ganji-backend/internal/lnbits"
)

type handlerPaymentChecker struct {
	paid bool
	err  error
}

func (f *handlerPaymentChecker) CheckPayment(
	ctx context.Context,
	checkingID string,
) (*lnbits.CheckPaymentResponse, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &lnbits.CheckPaymentResponse{
		Paid: f.paid,
	}, nil
}

func setupWebhookRouter(service *Service) *gin.Engine {
	return setupWebhookRouterWithSecret(service, "")
}

func setupWebhookRouterWithSecret(service *Service, secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	handler := NewHandler(service, secret)
	RegisterRoutes(router, handler)

	return router
}

func TestHandleLNbitsWebhookRejectsBadSignature(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{paid: true}
	dealReader := &fakeDealReader{
		deal: &deals.Deal{ID: "deal-123", Status: deals.StatusAwaitingPayment},
	}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouterWithSecret(service, "wallet-secret")

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(`{"checking_id":"checking-123"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("LNbits-Signature", "t=1,v1=bogus")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", recorder.Code)
	}
	if dealReader.updatedDealID != "" {
		t.Fatalf("expected no deal update, got %q", dealReader.updatedDealID)
	}
}

func TestHandleLNbitsWebhookAcceptsValidSignature(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{paid: true}
	dealReader := &fakeDealReader{
		deal: &deals.Deal{ID: "deal-123", Status: deals.StatusAwaitingPayment},
	}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouterWithSecret(service, "wallet-secret")

	body := `{"checking_id":"checking-123"}`
	timestamp := time.Now().Unix()
	header := "t=" + strconv.FormatInt(timestamp, 10) + ",v1=" + signBody(body, "wallet-secret", timestamp)

	request := httptest.NewRequest(http.MethodPost, "/webhooks/lnbits", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("LNbits-Signature", header)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}
	if dealReader.updatedDealID != "deal-123" {
		t.Fatalf("expected deal to be updated, got %q", dealReader.updatedDealID)
	}
}

func TestVerifyLNbitsSignature(t *testing.T) {
	body := []byte(`{"checking_id":"checking-123"}`)
	now := time.Now().Unix()
	header := "t=" + strconv.FormatInt(now, 10) + ",v1=" + signBody(string(body), "secret", now)

	tests := []struct {
		name   string
		body   []byte
		header string
		secret string
		want   bool
	}{
		{name: "valid", body: body, header: header, secret: "secret", want: true},
		{name: "no secret skips verification", body: body, header: "", secret: "", want: true},
		{name: "missing header refuses", body: body, header: "", secret: "secret", want: false},
		{name: "tampered body refuses", body: []byte(`{"checking_id":"other"}`), header: header, secret: "secret", want: false},
		{name: "wrong secret refuses", body: body, header: header, secret: "wrong", want: false},
		{name: "stale timestamp refuses", body: body, header: "t=" + strconv.FormatInt(now-3600, 10) + ",v1=" + signBody(string(body), "secret", now-3600), secret: "secret", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyLNbitsSignature(tc.body, tc.header, tc.secret); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func signBody(body, secret string, timestamp int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "." + body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestHandleLNbitsWebhookRejectsOversizedBody(t *testing.T) {
	dealReader := &fakeDealReader{
		deal: &deals.Deal{ID: "deal-123", Status: deals.StatusAwaitingPayment},
	}

	service := NewService(dealReader, &handlerPaymentChecker{paid: true})
	router := setupWebhookRouter(service)

	// maxWebhookBodyBytes is 1 MiB; send over the cap. The body must be
	// rejected before it is parsed or forwarded to the service.
	oversized := strings.Repeat("x", maxWebhookBodyBytes+1)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/lnbits", strings.NewReader(oversized))
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status 413, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if dealReader.updatedDealID != "" {
		t.Fatalf("expected no deal update, got %q", dealReader.updatedDealID)
	}
}

func TestHandleLNbitsWebhookReturnsOKForPaidPayment(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{
		paid: true,
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusAwaitingPayment,
		},
	}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouter(service)

	body := `{"checking_id":"checking-123"}`

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(body),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	expectedBody := `{"status":"ok"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
	}

	if dealReader.updatedDealID != "deal-123" {
		t.Fatalf(
			"expected deal-123 to be updated, got %q",
			dealReader.updatedDealID,
		)
	}
}

func TestHandleLNbitsWebhookRejectsInvalidJSON(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{
		paid: true,
	}

	dealReader := &fakeDealReader{}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouter(service)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(`{"checking_id":`),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}

	expectedBody := `{"error":"invalid webhook payload"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
	}
}

func TestHandleLNbitsWebhookIgnoresUnpaidPayment(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{
		paid: false,
	}

	dealReader := &fakeDealReader{
		deal: &deals.Deal{
			ID:     "deal-123",
			Status: deals.StatusAwaitingPayment,
		},
	}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouter(service)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(`{"checking_id":"checking-123"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	expectedBody := `{"status":"ignored","reason":"payment not successful"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	var expectedJSON map[string]string
	var actualJSON map[string]string

	if err := json.Unmarshal([]byte(expectedBody), &expectedJSON); err != nil {
		t.Fatalf("failed to parse expected JSON: %v", err)
	}

	if err := json.Unmarshal([]byte(actualBody), &actualJSON); err != nil {
		t.Fatalf("failed to parse actual JSON: %v", err)
	}

	if len(expectedJSON) != len(actualJSON) {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
	}

	for key, expectedValue := range expectedJSON {
		if actualJSON[key] != expectedValue {
			t.Fatalf(
				"expected %s=%q, got %q",
				key,
				expectedValue,
				actualJSON[key],
			)
		}
	}

	if dealReader.updatedDealID != "" {
		t.Fatalf(
			"expected no deal update, got %q",
			dealReader.updatedDealID,
		)
	}
}

func TestHandleLNbitsWebhookReturnsNotFoundForMissingDeal(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{
		paid: true,
	}

	dealReader := &fakeDealReader{
		err: deals.ErrDealNotFound,
	}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouter(service)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(`{"checking_id":"checking-123"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", recorder.Code)
	}

	expectedBody := `{"error":"no deal for checking_id"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	var expectedJSON map[string]string
	var actualJSON map[string]string

	if err := json.Unmarshal([]byte(expectedBody), &expectedJSON); err != nil {
		t.Fatalf("failed to parse expected JSON: %v", err)
	}

	if err := json.Unmarshal([]byte(actualBody), &actualJSON); err != nil {
		t.Fatalf("failed to parse actual JSON: %v", err)
	}

	if len(expectedJSON) != len(actualJSON) {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
	}

	for key, expectedValue := range expectedJSON {
		if actualJSON[key] != expectedValue {
			t.Fatalf(
				"expected %s=%q, got %q",
				key,
				expectedValue,
				actualJSON[key],
			)
		}
	}
}

func TestHandleLNbitsWebhookReturnsInternalServerErrorForLNbitsFailure(t *testing.T) {
	paymentChecker := &handlerPaymentChecker{
		err: errors.New("lnbits unavailable"),
	}

	dealReader := &fakeDealReader{}

	service := NewService(dealReader, paymentChecker)
	router := setupWebhookRouter(service)

	request := httptest.NewRequest(
		http.MethodPost,
		"/webhooks/lnbits",
		strings.NewReader(`{"checking_id":"checking-123"}`),
	)
	request.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}

	expectedBody := `{"error":"internal failure"}`
	actualBody := strings.TrimSpace(recorder.Body.String())

	if actualBody != expectedBody {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
	}
}
