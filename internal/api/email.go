package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strings"
)

// errEmailNotConfigured is returned when SMTP settings aren't set - lets
// handleSignup respond with a clear message instead of a confusing dial
// failure deep inside net/smtp.
var errEmailNotConfigured = errors.New("email sending is not configured on this server")

const verificationCodeLength = 6

// generateVerificationCode returns a random N-digit numeric code using
// crypto/rand (not math/rand) - this gates account creation, so it should be
// unpredictable the same way refresh tokens are (see auth.go).
func generateVerificationCode() (string, error) {
	const digits = "0123456789"
	buf := make([]byte, verificationCodeLength)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	code := make([]byte, verificationCodeLength)
	for i, b := range buf {
		code[i] = digits[int(b)%len(digits)]
	}
	return string(code), nil
}

// hashVerificationCode SHA-256-hashes a code for storage, mirroring how
// refresh tokens are hashed rather than stored in the clear (auth.go).
func hashVerificationCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

// maskEmail returns a partially-hidden version of an email for safe display
// (e.g. in a "we sent a code to j***@example.com" message), e.g.
// "jane.doe@example.com" -> "j*******@example.com".
func maskEmail(email string) string {
	at := strings.IndexByte(email, '@')
	if at <= 1 {
		return email
	}
	return email[:1] + strings.Repeat("*", at-1) + email[at:]
}

// sendVerificationEmail sends a short plain-text message containing code to
// toEmail via SMTP. net/smtp.SendMail negotiates STARTTLS automatically when
// the server advertises it (true of every modern relay on port 587), so no
// extra TLS handling is needed here.
func sendVerificationEmail(cfg *Config, toEmail, code string) error {
	if cfg.SMTPHost == "" || cfg.SMTPPort == "" || cfg.SMTPFrom == "" {
		return errEmailNotConfigured
	}

	addr := net.JoinHostPort(cfg.SMTPHost, cfg.SMTPPort)
	var auth smtp.Auth
	if cfg.SMTPUsername != "" {
		auth = smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, cfg.SMTPHost)
	}

	subject := "Your Aura verification code"
	body := fmt.Sprintf(
		"Your verification code is: %s\r\n\r\nThis code expires in 15 minutes.\r\nIf you didn't request this, you can safely ignore this email.\r\n",
		code)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.SMTPFrom, toEmail, subject, body)

	return smtp.SendMail(addr, auth, cfg.SMTPFrom, []string{toEmail}, []byte(msg))
}
