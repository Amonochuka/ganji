package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	gin.SetMode(gin.TestMode)

	router := gin.New()
	handler := NewHandler(service)
	RegisterRoutes(router, handler)

	return router
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

	if actualBody != expectedBody {
		t.Fatalf("expected body %s, got %s", expectedBody, actualBody)
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
