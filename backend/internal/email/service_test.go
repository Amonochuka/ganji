package email

import (
	"context"
	"strings"
	"testing"

	"github.com/Amonochuka/ganji-backend/internal/config"
)

func TestBuildMessageUsesEncodedSubjectAndUniqueBoundary(t *testing.T) {
	first, err := buildMessage("from@example.com", "to@example.com", "💰 Payment Received", "text", "<p>html</p>")
	if err != nil {
		t.Fatalf("build first message: %v", err)
	}
	second, err := buildMessage("from@example.com", "to@example.com", "💰 Payment Received", "text", "<p>html</p>")
	if err != nil {
		t.Fatalf("build second message: %v", err)
	}
	if !strings.Contains(string(first), "Subject: =?UTF-8?q?") {
		t.Fatalf("expected MIME-encoded subject, got %q", first)
	}
	if string(first) == string(second) {
		t.Fatal("expected a unique MIME boundary per message")
	}
}

func TestSendRejectsHeaderInjectionBeforeDialing(t *testing.T) {
	svc := NewService(&config.Config{
		SMTPHost:       "127.0.0.1",
		SMTPPort:       1,
		SMTPFrom:       "from@example.com",
		SMTPEncryption: "none",
	}, nil)
	err := svc.SendPaymentReceived(context.Background(), "to@example.com", "Ada", "ok\r\nBcc: victim@example.com", 1, "deal")
	if err == nil || !strings.Contains(err.Error(), "header contains a newline") {
		t.Fatalf("expected header rejection, got %v", err)
	}
}

func TestEnabledAllowsUnauthenticatedLocalSMTP(t *testing.T) {
	svc := NewService(&config.Config{SMTPHost: "localhost", SMTPPort: 1025, SMTPFrom: "from@example.com"}, nil)
	if !svc.Enabled() {
		t.Fatal("expected unauthenticated SMTP configuration to be enabled")
	}
}
