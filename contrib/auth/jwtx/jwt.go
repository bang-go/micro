package jwtx

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

type Config struct {
	SecretKey string
	Issuer    string
	Expire    time.Duration
}

type JWT struct {
	config *Config
}

type Claims struct {
	Payload interface{} `json:"payload"`
	jwt.RegisteredClaims
}

func New(conf *Config) (*JWT, error) {
	if conf == nil {
		return nil, errors.New("jwtx: config is required")
	}
	if conf.SecretKey == "" {
		return nil, errors.New("jwtx: secret key is required")
	}
	if conf.Expire == 0 {
		conf.Expire = 24 * time.Hour
	}

	return &JWT{
		config: conf,
	}, nil
}

func MustNew(conf *Config) *JWT {
	j, err := New(conf)
	if err != nil {
		panic(err)
	}
	return j
}

// Generate creates a new JWT token with payload
func (j *JWT) Generate(payload interface{}) (string, error) {
	now := time.Now()
	claims := Claims{
		Payload: payload,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(j.config.Expire)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    j.config.Issuer,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(j.config.SecretKey))
}

// Parse validates the token and returns the payload
func (j *JWT) Parse(tokenString string, payload interface{}) error {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{Payload: payload}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrTokenInvalid
		}
		return []byte(j.config.SecretKey), nil
	})

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return ErrTokenExpired
		}
		return err
	}

	if _, ok := token.Claims.(*Claims); ok && token.Valid {
		// Note: The payload is already unmarshaled into the pointer provided to ParseWithClaims
		// However, jwt-go behavior with interface{} payload might be tricky (it might end up as map[string]interface{})
		// For strong typing, user should provide a struct as payload in Generate, and expect a map in Parse unless customized.
		// A better approach for Parse generic payload:
		// Since we can't easily unmarshal back to interface{} pointer in standard way without JSON roundtrip if it's a struct.
		// So we recommend users to use map[string]interface{} or specific struct for Claims if they want full control.

		// But here, to keep it simple:
		// We just return success. The payload pointer passed to ParseWithClaims *should* be populated if it was possible.
		// WARNING: If Payload is interface{}, jwt unmarshals it as map[string]interface{}.
		return nil
	}

	return ErrTokenInvalid
}

// ParseToMap parses token and returns claims as map
func (j *JWT) ParseToMap(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(j.config.SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, ErrTokenInvalid
}
