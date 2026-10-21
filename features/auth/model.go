package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	RoleAdmin     = "admin"
	RoleModerator = "moderator"
	RoleUser      = "user"

	tokenPrefix = "ct_live_"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrValidation   = errors.New("validation")
)

type Principal struct {
	TokenID   uuid.UUID  `json:"id"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	CreatedBy *uuid.UUID `json:"created_by,omitempty"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type Token struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Role       string     `json:"role"`
	CreatedBy  *uuid.UUID `json:"created_by"`
	ExpiresAt  *time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type NewToken struct {
	Token Token
	// Secret is returned to the caller exactly once and never persisted.
	Secret string
}

func NewSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashSecret(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func HashHex(secret string) string {
	return hex.EncodeToString(HashSecret(secret))
}

func IsAdmin(role string) bool { return role == RoleAdmin }

func IsStaff(role string) bool {
	return role == RoleAdmin || role == RoleModerator
}

func ValidRole(role string) bool {
	return role == RoleAdmin || role == RoleModerator || role == RoleUser
}

func CleanName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 1 || len([]rune(name)) > 120 {
		return "", false
	}
	return name, true
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}
