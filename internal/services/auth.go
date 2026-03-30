package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

func HashPassword(password string) (string, error) {
	h := sha256.Sum256([]byte(password))
	hexHash := hex.EncodeToString(h[:])
	hash, err := bcrypt.GenerateFromPassword([]byte(hexHash), 12)
	if err != nil { return "", err }
	return string(hash), nil
}

func VerifyPassword(password, hash string) bool {
	h := sha256.Sum256([]byte(password))
	hexHash := hex.EncodeToString(h[:])
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(hexHash)) == nil
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy_timing_equalization"), 12)

func DummyPasswordCheck() {
	bcrypt.CompareHashAndPassword(dummyHash, []byte("wrong_password_for_timing"))
}

type TokenClaims struct {
	Username  string `json:"sub"`
	TokenType string `json:"type"`
	jwt.RegisteredClaims
}

func CreateAccessToken(username, secret string, expireMinutes int) (string, int, error) {
	now := time.Now()
	claims := TokenClaims{
		Username:  username,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireMinutes) * time.Minute)),
			ID:        uuid.New().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil { return "", 0, err }
	return signed, expireMinutes * 60, nil
}

func CreateRefreshToken(username, secret string, expireDays int) (string, string, error) {
	now := time.Now()
	claims := TokenClaims{
		Username:  username,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Duration(expireDays) * 24 * time.Hour)),
			ID:        uuid.New().String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil { return "", "", err }
	hash := sha256.Sum256([]byte(signed))
	return signed, hex.EncodeToString(hash[:]), nil
}

func VerifyAccessToken(tokenStr, secret string) (string, error) {
	claims := &TokenClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid { return "", fmt.Errorf("invalid token") }
	if claims.TokenType != "access" { return "", fmt.Errorf("not an access token") }
	return claims.Username, nil
}

func VerifyRefreshToken(tokenStr, secret string) (string, string, error) {
	claims := &TokenClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})
	if err != nil || !token.Valid { return "", "", fmt.Errorf("invalid token") }
	if claims.TokenType != "refresh" { return "", "", fmt.Errorf("not a refresh token") }
	hash := sha256.Sum256([]byte(tokenStr))
	return claims.Username, hex.EncodeToString(hash[:]), nil
}

func GenerateTOTPSecret(username string) (string, string, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "SynCent",
		AccountName: username,
	})
	if err != nil { return "", "", err }
	return key.Secret(), key.URL(), nil
}

func VerifyTOTP(secret, code string) bool {
	return totp.Validate(code, secret)
}

func VerifyTOTPWithReplay(ctx context.Context, pool *pgxpool.Pool, username, secret, code string) bool {
	if !totp.Validate(code, secret) { return false }

	var count int
	pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM used_totp_codes WHERE username=$1 AND code=$2 AND used_at > NOW() - INTERVAL '90 seconds'`,
		username, code).Scan(&count)
	if count > 0 { return false }
	pool.Exec(ctx, `INSERT INTO used_totp_codes (username, code) VALUES ($1, $2)`, username, code)
	pool.Exec(ctx, `DELETE FROM used_totp_codes WHERE used_at < NOW() - INTERVAL '5 minutes'`)
	return true
}
