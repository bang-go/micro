package jwtx

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type userPayload struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func TestNewValidation(t *testing.T) {
	_, err := New[userPayload](nil)
	if !errors.Is(err, ErrNilConfig) {
		t.Fatalf("New(nil) error = %v, want %v", err, ErrNilConfig)
	}

	_, err = New[userPayload](&Config{})
	if !errors.Is(err, ErrSecretKeyRequired) {
		t.Fatalf("New(empty secret) error = %v, want %v", err, ErrSecretKeyRequired)
	}

	_, err = New[userPayload](&Config{SecretKey: "   "})
	if !errors.Is(err, ErrSecretKeyRequired) {
		t.Fatalf("New(blank secret) error = %v, want %v", err, ErrSecretKeyRequired)
	}
}

func TestNewNormalizesAndClonesInput(t *testing.T) {
	conf := &Config{
		SecretKey: " secret ",
		Issuer:    " micro ",
		Audience:  []string{" api ", "admin", "api", ""},
	}

	client, err := New[userPayload](conf)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	token, err := client.Generate(userPayload{UserID: "u-1"}, WithAudience(" api ", "internal", "", "internal"))
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	claims, err := client.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got, want := claims.Issuer, "micro"; got != want {
		t.Fatalf("claims.Issuer = %q, want %q", got, want)
	}
	if got, want := len(claims.Audience), 2; got != want {
		t.Fatalf("len(claims.Audience) = %d, want %d", got, want)
	}
	if got, want := claims.Audience[0], "api"; got != want {
		t.Fatalf("claims.Audience[0] = %q, want %q", got, want)
	}
	if got, want := claims.Audience[1], "internal"; got != want {
		t.Fatalf("claims.Audience[1] = %q, want %q", got, want)
	}

	conf.SecretKey = "mutated"
	conf.Issuer = "mutated"
	conf.Audience[0] = "mutated"

	payload, err := client.ParsePayload(token)
	if err != nil {
		t.Fatalf("ParsePayload() after mutation error = %v", err)
	}
	if got, want := payload.UserID, "u-1"; got != want {
		t.Fatalf("payload.UserID = %q, want %q", got, want)
	}
}

func TestNewRejectsInvalidMethod(t *testing.T) {
	_, err := New[userPayload](&Config{
		SecretKey: "secret",
		Method:    jwt.SigningMethodRS256,
	})
	if !errors.Is(err, ErrInvalidMethod) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidMethod)
	}
}

func TestMustNew(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustNew() did not panic")
		}
	}()

	_ = MustNew[userPayload](&Config{})
}

func TestGenerateAndParse(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	client, err := New[userPayload](&Config{
		SecretKey: "secret",
		Issuer:    "micro",
		Audience:  []string{"api"},
		Expire:    time.Hour,
		TimeFunc:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	token, err := client.Generate(
		userPayload{UserID: "u-1", Role: "admin"},
		WithSubject(" subject-1 "),
		WithJWTID(" jwt-1 "),
	)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	claims, err := client.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if got, want := claims.Payload.UserID, "u-1"; got != want {
		t.Fatalf("claims.Payload.UserID = %q, want %q", got, want)
	}
	if got, want := claims.Subject, "subject-1"; got != want {
		t.Fatalf("claims.Subject = %q, want %q", got, want)
	}
	if got, want := claims.ID, "jwt-1"; got != want {
		t.Fatalf("claims.ID = %q, want %q", got, want)
	}
	if got, want := claims.Issuer, "micro"; got != want {
		t.Fatalf("claims.Issuer = %q, want %q", got, want)
	}
	if got, want := claims.Audience[0], "api"; got != want {
		t.Fatalf("claims.Audience[0] = %q, want %q", got, want)
	}
}

func TestParsePayload(t *testing.T) {
	client, err := New[userPayload](&Config{SecretKey: "secret"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	token, err := client.Generate(userPayload{UserID: "u-2", Role: "member"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	payload, err := client.ParsePayload(token)
	if err != nil {
		t.Fatalf("ParsePayload() error = %v", err)
	}

	if got, want := payload.Role, "member"; got != want {
		t.Fatalf("payload.Role = %q, want %q", got, want)
	}
}

func TestGenerateRejectsInvalidTokenLifetime(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	client, err := New[userPayload](&Config{
		SecretKey: "secret",
		TimeFunc:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = client.Generate(
		userPayload{UserID: "u-invalid"},
		WithIssuedAt(now),
		WithNotBefore(now.Add(time.Hour)),
		WithExpiresAt(now.Add(30*time.Minute)),
	)
	if !errors.Is(err, ErrInvalidTokenLifetime) {
		t.Fatalf("Generate() error = %v, want %v", err, ErrInvalidTokenLifetime)
	}
}

func TestParseExpiredToken(t *testing.T) {
	now := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	client, err := New[userPayload](&Config{
		SecretKey: "secret",
		Expire:    time.Minute,
		TimeFunc:  func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	token, err := client.Generate(userPayload{UserID: "u-3"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	expiredClient, err := New[userPayload](&Config{
		SecretKey: "secret",
		Expire:    time.Minute,
		TimeFunc:  func() time.Time { return now.Add(2 * time.Minute) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = expiredClient.Parse(token)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrTokenExpired)
	}
}

func TestParseRejectsInvalidSignature(t *testing.T) {
	signer, err := New[userPayload](&Config{SecretKey: "secret-a"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token, err := signer.Generate(userPayload{UserID: "u-4"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	verifier, err := New[userPayload](&Config{SecretKey: "secret-b"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = verifier.Parse(token)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrTokenInvalid)
	}
}

func TestParseRejectsWrongAudience(t *testing.T) {
	signer, err := New[userPayload](&Config{
		SecretKey: "secret",
		Audience:  []string{"api"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	token, err := signer.Generate(userPayload{UserID: "u-5"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	verifier, err := New[userPayload](&Config{
		SecretKey: "secret",
		Audience:  []string{"admin"},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = verifier.Parse(token)
	if !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("Parse() error = %v, want %v", err, ErrTokenInvalid)
	}
}
