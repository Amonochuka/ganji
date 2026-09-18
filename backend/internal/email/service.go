package email

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strings"
	"time"

	"github.com/Amonochuka/ganji-backend/internal/auth"
	"github.com/Amonochuka/ganji-backend/internal/config"
	"github.com/Amonochuka/ganji-backend/internal/deals"
)

const sendTimeout = 15 * time.Second

type UserFinder interface {
	FindByID(ctx context.Context, id string) (*auth.User, error)
}

type Service struct {
	cfg   *config.Config
	users UserFinder
}

func NewService(cfg *config.Config, users UserFinder) *Service {
	return &Service{cfg: cfg, users: users}
}

func (s *Service) Enabled() bool {
	return s.cfg.SMTPHost != "" && s.cfg.SMTPPort > 0 && s.cfg.SMTPFrom != "" &&
		((s.cfg.SMTPUser == "") == (s.cfg.SMTPPass == ""))
}

// PaymentLocked, DealDisputed, DealReleased and DealRefunded satisfy
// deals.DealNotifier. They run asynchronously with a bounded lifetime so a
// slow mail server never delays an escrow state transition.
func (s *Service) PaymentLocked(_ context.Context, deal *deals.Deal) {
	s.notify(deal, func(ctx context.Context, to, name string) error {
		return s.SendPaymentReceived(ctx, to, name, deal.Title, deal.AmountSats, deal.ID)
	})
}

func (s *Service) DealDisputed(_ context.Context, deal *deals.Deal) {
	s.notify(deal, func(ctx context.Context, to, name string) error {
		return s.SendDealDisputed(ctx, to, name, deal.Title, deal.DisputeReason, deal.ID)
	})
}

func (s *Service) DealReleased(_ context.Context, deal *deals.Deal) {
	s.notify(deal, func(ctx context.Context, to, name string) error {
		return s.SendDealReleased(ctx, to, name, deal.Title, deal.ID)
	})
}

func (s *Service) DealRefunded(_ context.Context, deal *deals.Deal) {
	s.notify(deal, func(ctx context.Context, to, name string) error {
		return s.SendDealRefunded(ctx, to, name, deal.Title, deal.ID)
	})
}

func (s *Service) notify(deal *deals.Deal, send func(context.Context, string, string) error) {
	if !s.Enabled() || s.users == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		user, err := s.users.FindByID(ctx, deal.FreelancerID)
		if err != nil || user == nil {
			log.Printf("email: look up freelancer for deal %s: %v", deal.ID, err)
			return
		}
		if err := send(ctx, user.Email, FirstName(user.DisplayName)); err != nil {
			log.Printf("email: notify freelancer for deal %s: %v", deal.ID, err)
		}
	}()
}

func (s *Service) SendPaymentReceived(ctx context.Context, freelancerEmail, freelancerName, dealTitle string, amountSats int64, dealID string) error {
	if !s.Enabled() {
		return nil // silently skip if not configured
	}

	subject := fmt.Sprintf("💰 Payment Received — \"%s\" Deal Locked", dealTitle)
	htmlBody := paymentReceivedHTML(freelancerName, dealTitle, amountSats, dealID, s.cfg.FrontendURL)
	textBody := paymentReceivedText(freelancerName, dealTitle, amountSats, dealID, s.cfg.FrontendURL)

	return s.send(ctx, freelancerEmail, subject, textBody, htmlBody)
}

func (s *Service) SendDealDisputed(ctx context.Context, freelancerEmail, freelancerName, dealTitle, reason string, dealID string) error {
	if !s.Enabled() {
		return nil
	}

	subject := fmt.Sprintf("⚠️ Dispute Raised — \"%s\"", dealTitle)
	htmlBody := dealDisputedHTML(freelancerName, dealTitle, reason, dealID, s.cfg.FrontendURL)
	textBody := dealDisputedText(freelancerName, dealTitle, reason, dealID, s.cfg.FrontendURL)

	return s.send(ctx, freelancerEmail, subject, textBody, htmlBody)
}

func (s *Service) SendDealReleased(ctx context.Context, freelancerEmail, freelancerName, dealTitle string, dealID string) error {
	if !s.Enabled() {
		return nil
	}

	subject := fmt.Sprintf("✅ Deal Released — \"%s\"", dealTitle)
	htmlBody := dealReleasedHTML(freelancerName, dealTitle, dealID, s.cfg.FrontendURL)
	textBody := dealReleasedText(freelancerName, dealTitle, dealID, s.cfg.FrontendURL)

	return s.send(ctx, freelancerEmail, subject, textBody, htmlBody)
}

func (s *Service) SendDealRefunded(ctx context.Context, freelancerEmail, freelancerName, dealTitle string, dealID string) error {
	if !s.Enabled() {
		return nil
	}

	subject := fmt.Sprintf("❌ Deal Refunded — \"%s\"", dealTitle)
	htmlBody := dealRefundedHTML(freelancerName, dealTitle, dealID, s.cfg.FrontendURL)
	textBody := dealRefundedText(freelancerName, dealTitle, dealID, s.cfg.FrontendURL)

	return s.send(ctx, freelancerEmail, subject, textBody, htmlBody)
}

func (s *Service) send(ctx context.Context, to, subject, textBody, htmlBody string) error {
	fromAddress, err := parseMailbox(s.cfg.SMTPFrom)
	if err != nil {
		return fmt.Errorf("invalid smtp from address: %w", err)
	}
	toAddress, err := parseMailbox(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	if err := safeHeader(subject); err != nil {
		return fmt.Errorf("invalid email subject: %w", err)
	}
	msg, err := buildMessage(s.cfg.SMTPFrom, to, subject, textBody, htmlBody)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.SMTPHost, s.cfg.SMTPPort)
	tlsConfig := &tls.Config{
		ServerName: s.cfg.SMTPHost,
		MinVersion: tls.VersionTLS12,
	}
	dialer := &net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if s.cfg.SMTPEncryption == "implicit_tls" {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("smtp tls handshake: %w", err)
		}
		conn = tlsConn
	}

	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("new smtp client: %w", err)
	}
	defer client.Quit()

	if s.cfg.SMTPEncryption == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("smtp server does not advertise STARTTLS")
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	} else if s.cfg.SMTPEncryption != "none" && s.cfg.SMTPEncryption != "implicit_tls" {
		return fmt.Errorf("unsupported SMTP_ENCRYPTION %q", s.cfg.SMTPEncryption)
	}

	if s.cfg.SMTPUser != "" {
		auth := smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPass, s.cfg.SMTPHost)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(fromAddress); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(toAddress); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	_, err = w.Write(msg)
	if err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	err = w.Close()
	if err != nil {
		return fmt.Errorf("smtp close: %w", err)
	}

	return nil
}

func buildMessage(from, to, subject, textBody, htmlBody string) ([]byte, error) {
	boundary, err := mimeBoundary()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", to))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("UTF-8", subject)))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/alternative; boundary=\"%s\"\r\n", boundary))
	buf.WriteString("\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	buf.WriteString(textBody)
	buf.WriteString("\r\n\r\n")
	buf.WriteString("--" + boundary + "\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	buf.WriteString(htmlBody)
	buf.WriteString("\r\n\r\n")
	buf.WriteString("--" + boundary + "--\r\n")
	return buf.Bytes(), nil
}

func parseMailbox(value string) (string, error) {
	if err := safeHeader(value); err != nil {
		return "", err
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address == "" {
		return "", errors.New("not an email address")
	}
	return address.Address, nil
}

func safeHeader(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("header contains a newline")
	}
	return nil
}

func mimeBoundary() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate MIME boundary: %w", err)
	}
	return "ganji-" + hex.EncodeToString(b), nil
}

func formatSats(sats int64) string {
	return fmt.Sprintf("%.8f", float64(sats)/1e8)
}

func paymentReceivedHTML(name, title string, amountSats int64, dealID, frontendURL string) string {
	tmpl := `
<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
  <div style="background: linear-gradient(135deg, #10b981 0%, #059669 100%); padding: 30px; border-radius: 12px 12px 0 0; text-align: center;">
    <h1 style="color: white; margin: 0; font-size: 24px;">💰 Payment Received</h1>
  </div>
  <div style="background: #f9fafb; padding: 30px; border-radius: 0 0 12px 12px; border: 1px solid #e5e7eb; border-top: none;">
    <p>Hi {{.Name}},</p>
    <p>Your client has paid the hold invoice for deal <strong>{{.Title}}</strong>.</p>
    <div style="background: white; padding: 20px; border-radius: 8px; border-left: 4px solid #10b981; margin: 20px 0;">
      <p style="margin: 0 0 10px;"><strong>Amount:</strong> {{.Amount}} BTC ({{.Sats}} sats)</p>
      <p style="margin: 0;"><strong>Status:</strong> <span style="color: #10b981; font-weight: 600;">LOCKED</span> — funds are now held in escrow</p>
    </div>
    <p>Next step: Submit your work via the dashboard.</p>
    <p style="text-align: center; margin: 30px 0;">
      <a href="{{.FrontendURL}}/deals/{{.DealID}}" style="background: #10b981; color: white; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600;">View Deal</a>
    </p>
    <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 20px 0;">
    <p style="font-size: 12px; color: #9ca3af;">Deal ID: {{.DealID}} • {{.Time}}</p>
  </div>
</body>
</html>
`
	data := map[string]string{
		"Name":        name,
		"Title":       title,
		"Amount":      formatSats(amountSats),
		"Sats":        fmt.Sprintf("%d", amountSats),
		"DealID":      dealID,
		"FrontendURL": frontendURL,
		"Time":        time.Now().Format("Jan 2, 2006 15:04 MST"),
	}
	return renderTemplate(tmpl, data)
}

func paymentReceivedText(name, title string, amountSats int64, dealID, frontendURL string) string {
	return fmt.Sprintf(`Hi %s,

Your client has paid the hold invoice for deal "%s".

Amount: %s BTC (%d sats)
Status: LOCKED — funds are now held in escrow

Next step: Submit your work via the dashboard.

View deal: %s/deals/%s

Deal ID: %s
Time: %s
`, name, title, formatSats(amountSats), amountSats, frontendURL, dealID, dealID, time.Now().Format("Jan 2, 2006 15:04 MST"))
}

func dealDisputedHTML(name, title, reason, dealID, frontendURL string) string {
	tmpl := `
<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
  <div style="background: linear-gradient(135deg, #f59e0b 0%, #d97706 100%); padding: 30px; border-radius: 12px 12px 0 0; text-align: center;">
    <h1 style="color: white; margin: 0; font-size: 24px;">⚠️ Dispute Raised</h1>
  </div>
  <div style="background: #f9fafb; padding: 30px; border-radius: 0 0 12px 12px; border: 1px solid #e5e7eb; border-top: none;">
    <p>Hi {{.Name}},</p>
    <p>The client has raised a dispute on deal <strong>{{.Title}}</strong>.</p>
    <div style="background: white; padding: 20px; border-radius: 8px; border-left: 4px solid #f59e0b; margin: 20px 0;">
      <p style="margin: 0 0 10px;"><strong>Client's Reason:</strong></p>
      <p style="margin: 0; font-style: italic; color: #6b7280;">{{.Reason}}</p>
    </div>
    <p>The deal is now frozen in the <strong>DISPUTED</strong> state. An operator will review and decide to either release the funds to you or refund the client.</p>
    <p style="text-align: center; margin: 30px 0;">
      <a href="{{.FrontendURL}}/deals/{{.DealID}}" style="background: #f59e0b; color: white; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600;">View Deal</a>
    </p>
    <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 20px 0;">
    <p style="font-size: 12px; color: #9ca3af;">Deal ID: {{.DealID}} • {{.Time}}</p>
  </div>
</body>
</html>
`
	data := map[string]string{
		"Name":        name,
		"Title":       title,
		"Reason":      reason,
		"DealID":      dealID,
		"FrontendURL": frontendURL,
		"Time":        time.Now().Format("Jan 2, 2006 15:04 MST"),
	}
	return renderTemplate(tmpl, data)
}

func dealDisputedText(name, title, reason, dealID, frontendURL string) string {
	return fmt.Sprintf(`Hi %s,

The client has raised a dispute on deal "%s".

Client's Reason: %s

The deal is now frozen in the DISPUTED state. An operator will review and decide to either release the funds to you or refund the client.

View deal: %s/deals/%s

Deal ID: %s
Time: %s
`, name, title, reason, frontendURL, dealID, dealID, time.Now().Format("Jan 2, 2006 15:04 MST"))
}

func dealReleasedHTML(name, title, dealID, frontendURL string) string {
	tmpl := `
<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
  <div style="background: linear-gradient(135deg, #10b981 0%, #059669 100%); padding: 30px; border-radius: 12px 12px 0 0; text-align: center;">
    <h1 style="color: white; margin: 0; font-size: 24px;">✅ Deal Released</h1>
  </div>
  <div style="background: #f9fafb; padding: 30px; border-radius: 0 0 12px 12px; border: 1px solid #e5e7eb; border-top: none;">
    <p>Hi {{.Name}},</p>
    <p>Deal <strong>{{.Title}}</strong> has been released. The escrow has been settled and funds have been forwarded to your Lightning invoice.</p>
    <div style="background: white; padding: 20px; border-radius: 8px; border-left: 4px solid #10b981; margin: 20px 0;">
      <p style="margin: 0;"><strong>Status:</strong> <span style="color: #10b981; font-weight: 600;">RELEASED</span></p>
    </div>
    <p>This work has been anchored to your Live CV as a verified entry.</p>
    <p style="text-align: center; margin: 30px 0;">
      <a href="{{.FrontendURL}}/deals/{{.DealID}}" style="background: #10b981; color: white; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600;">View Deal</a>
    </p>
    <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 20px 0;">
    <p style="font-size: 12px; color: #9ca3af;">Deal ID: {{.DealID}} • {{.Time}}</p>
  </div>
</body>
</html>
`
	data := map[string]string{
		"Name":        name,
		"Title":       title,
		"DealID":      dealID,
		"FrontendURL": frontendURL,
		"Time":        time.Now().Format("Jan 2, 2006 15:04 MST"),
	}
	return renderTemplate(tmpl, data)
}

func dealReleasedText(name, title, dealID, frontendURL string) string {
	return fmt.Sprintf(`Hi %s,

Deal "%s" has been released. The escrow has been settled and funds have been forwarded to your Lightning invoice.

Status: RELEASED

This work has been anchored to your Live CV as a verified entry.

View deal: %s/deals/%s

Deal ID: %s
Time: %s
`, name, title, frontendURL, dealID, dealID, time.Now().Format("Jan 2, 2006 15:04 MST"))
}

func dealRefundedHTML(name, title, dealID, frontendURL string) string {
	tmpl := `
<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; line-height: 1.6; color: #333; max-width: 600px; margin: 0 auto; padding: 20px;">
  <div style="background: linear-gradient(135deg, #ef4444 0%, #dc2626 100%); padding: 30px; border-radius: 12px 12px 0 0; text-align: center;">
    <h1 style="color: white; margin: 0; font-size: 24px;">❌ Deal Refunded</h1>
  </div>
  <div style="background: #f9fafb; padding: 30px; border-radius: 0 0 12px 12px; border: 1px solid #e5e7eb; border-top: none;">
    <p>Hi {{.Name}},</p>
    <p>Deal <strong>{{.Title}}</strong> has been refunded. The hold invoice was cancelled and funds have been returned to the client.</p>
    <div style="background: white; padding: 20px; border-radius: 8px; border-left: 4px solid #ef4444; margin: 20px 0;">
      <p style="margin: 0;"><strong>Status:</strong> <span style="color: #ef4444; font-weight: 600;">REFUNDED</span></p>
    </div>
    <p style="text-align: center; margin: 30px 0;">
      <a href="{{.FrontendURL}}/deals/{{.DealID}}" style="background: #ef4444; color: white; padding: 12px 24px; border-radius: 6px; text-decoration: none; font-weight: 600;">View Deal</a>
    </p>
    <hr style="border: none; border-top: 1px solid #e5e7eb; margin: 20px 0;">
    <p style="font-size: 12px; color: #9ca3af;">Deal ID: {{.DealID}} • {{.Time}}</p>
  </div>
</body>
</html>
`
	data := map[string]string{
		"Name":        name,
		"Title":       title,
		"DealID":      dealID,
		"FrontendURL": frontendURL,
		"Time":        time.Now().Format("Jan 2, 2006 15:04 MST"),
	}
	return renderTemplate(tmpl, data)
}

func dealRefundedText(name, title, dealID, frontendURL string) string {
	return fmt.Sprintf(`Hi %s,

Deal "%s" has been refunded. The hold invoice was cancelled and funds have been returned to the client.

Status: REFUNDED

View deal: %s/deals/%s

Deal ID: %s
Time: %s
`, name, title, frontendURL, dealID, dealID, time.Now().Format("Jan 2, 2006 15:04 MST"))
}

func renderTemplate(tmpl string, data map[string]string) string {
	t, err := template.New("email").Parse(tmpl)
	if err != nil {
		return fmt.Sprintf("Template error: %v", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Sprintf("Template execution error: %v", err)
	}
	return buf.String()
}

// Helper to extract first name from display name
func FirstName(displayName string) string {
	parts := strings.Fields(displayName)
	if len(parts) > 0 {
		return parts[0]
	}
	return displayName
}
