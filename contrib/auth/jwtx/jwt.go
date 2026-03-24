package jwtx

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bang-go/util"
	"github.com/golang-jwt/jwt/v5"
)

const defaultExpire = 24 * time.Hour

var (
	ErrNilConfig            = errors.New("jwtx: config is required")
	ErrSecretKeyRequired    = errors.New("jwtx: secret key is required")
	ErrInvalidMethod        = errors.New("jwtx: signing method must be HMAC")
	ErrInvalidTokenLifetime = errors.New("jwtx: invalid token lifetime")
	ErrTokenExpired         = errors.New("jwtx: token expired")
	ErrTokenInvalid         = errors.New("jwtx: token invalid")
)

type Config struct {
	SecretKey string
	Issuer    string
	Audience  []string
	Expire    time.Duration
	Leeway    time.Duration
	Method    jwt.SigningMethod
	TimeFunc  func() time.Time
}

type JWT[T any] struct {
	secret   []byte
	issuer   string
	audience []string
	expire   time.Duration
	method   jwt.SigningMethod
	timeFunc func() time.Time
	parser   *jwt.Parser
}

type Claims[T any] struct {
	Payload T `json:"payload"`
	jwt.RegisteredClaims
}

type IssueOption func(*issueOptions)

type issueOptions struct {
	subject   string
	audience  []string
	id        string
	notBefore *time.Time
	issuedAt  *time.Time
	expiresAt *time.Time
}

func New[T any](conf *Config) (*JWT[T], error) {
	if conf == nil {
		return nil, ErrNilConfig
	}

	secretKey := strings.TrimSpace(conf.SecretKey)
	if secretKey == "" {
		return nil, ErrSecretKeyRequired
	}

	method, err := normalizeMethod(conf.Method)
	if err != nil {
		return nil, err
	}

	expire := conf.Expire
	if expire <= 0 {
		expire = defaultExpire
	}

	timeFunc := conf.TimeFunc
	if timeFunc == nil {
		timeFunc = time.Now
	}

	issuer := strings.TrimSpace(conf.Issuer)
	audience := normalizeAudience(conf.Audience)

	parserOptions := []jwt.ParserOption{
		jwt.WithValidMethods([]string{method.Alg()}),
		jwt.WithLeeway(conf.Leeway),
		jwt.WithTimeFunc(timeFunc),
	}
	if issuer != "" {
		parserOptions = append(parserOptions, jwt.WithIssuer(issuer))
	}
	if len(audience) > 0 {
		parserOptions = append(parserOptions, jwt.WithAudience(audience...))
	}

	return &JWT[T]{
		secret:   []byte(secretKey),
		issuer:   issuer,
		audience: audience,
		expire:   expire,
		method:   method,
		timeFunc: timeFunc,
		parser:   jwt.NewParser(parserOptions...),
	}, nil
}

func MustNew[T any](conf *Config) *JWT[T] {
	client, err := New[T](conf)
	if err != nil {
		panic(err)
	}
	return client
}

func (j *JWT[T]) Generate(payload T, options ...IssueOption) (string, error) {
	now := j.timeFunc().UTC()
	opts := issueOptions{
		audience: append([]string(nil), j.audience...),
	}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	opts.subject = strings.TrimSpace(opts.subject)
	opts.id = strings.TrimSpace(opts.id)
	opts.audience = normalizeAudience(opts.audience)

	issuedAt := now
	if opts.issuedAt != nil {
		issuedAt = opts.issuedAt.UTC()
	}

	notBefore := issuedAt
	if opts.notBefore != nil {
		notBefore = opts.notBefore.UTC()
	}

	expiresAt := issuedAt.Add(j.expire)
	if opts.expiresAt != nil {
		expiresAt = opts.expiresAt.UTC()
	}
	if !expiresAt.After(issuedAt) || !expiresAt.After(notBefore) {
		return "", ErrInvalidTokenLifetime
	}

	token := jwt.NewWithClaims(j.method, Claims[T]{
		Payload: payload,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    j.issuer,
			Subject:   opts.subject,
			Audience:  jwt.ClaimStrings(opts.audience),
			ID:        opts.id,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			NotBefore: jwt.NewNumericDate(notBefore),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
	return token.SignedString(j.secret)
}

func (j *JWT[T]) Parse(tokenString string) (*Claims[T], error) {
	claims := &Claims[T]{}
	token, err := j.parser.ParseWithClaims(tokenString, claims, j.keyFunc)
	if err != nil {
		return nil, mapTokenError(err)
	}
	if !token.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

func (j *JWT[T]) ParsePayload(tokenString string) (T, error) {
	claims, err := j.Parse(tokenString)
	if err != nil {
		var zero T
		return zero, err
	}
	return claims.Payload, nil
}

func WithSubject(subject string) IssueOption {
	return func(opts *issueOptions) {
		opts.subject = subject
	}
}

func WithAudience(audience ...string) IssueOption {
	return func(opts *issueOptions) {
		opts.audience = normalizeAudience(audience)
	}
}

func WithJWTID(id string) IssueOption {
	return func(opts *issueOptions) {
		opts.id = id
	}
}

func WithNotBefore(at time.Time) IssueOption {
	return func(opts *issueOptions) {
		opts.notBefore = util.Ptr(at)
	}
}

func WithIssuedAt(at time.Time) IssueOption {
	return func(opts *issueOptions) {
		opts.issuedAt = util.Ptr(at)
	}
}

func WithExpiresAt(at time.Time) IssueOption {
	return func(opts *issueOptions) {
		opts.expiresAt = util.Ptr(at)
	}
}

func (j *JWT[T]) keyFunc(token *jwt.Token) (any, error) {
	if token == nil || token.Method == nil || token.Method.Alg() != j.method.Alg() {
		return nil, ErrTokenInvalid
	}
	return j.secret, nil
}

func mapTokenError(err error) error {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired):
		return fmt.Errorf("%w: %v", ErrTokenExpired, err)
	case errors.Is(err, jwt.ErrTokenMalformed),
		errors.Is(err, jwt.ErrTokenSignatureInvalid),
		errors.Is(err, jwt.ErrTokenInvalidClaims),
		errors.Is(err, jwt.ErrTokenUnverifiable),
		errors.Is(err, jwt.ErrTokenNotValidYet):
		return fmt.Errorf("%w: %v", ErrTokenInvalid, err)
	default:
		return err
	}
}

func normalizeMethod(method jwt.SigningMethod) (jwt.SigningMethod, error) {
	if method == nil {
		return jwt.SigningMethodHS256, nil
	}

	switch method.Alg() {
	case jwt.SigningMethodHS256.Alg():
		return jwt.SigningMethodHS256, nil
	case jwt.SigningMethodHS384.Alg():
		return jwt.SigningMethodHS384, nil
	case jwt.SigningMethodHS512.Alg():
		return jwt.SigningMethodHS512, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidMethod, method.Alg())
	}
}

func normalizeAudience(audience []string) []string {
	if len(audience) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(audience))
	seen := make(map[string]struct{}, len(audience))
	for _, item := range audience {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}
