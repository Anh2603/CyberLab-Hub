package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidToken = errors.New("invalid token")

type Identity struct {
	UserID   int64
	Username string
	Role     string
}

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type Manager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewManager(secret string, ttl time.Duration) *Manager {
	return &Manager{secret: []byte(secret), ttl: ttl, now: time.Now}
}

func HashPassword(password string) ([]byte, error) {
	if len(password) < 8 || len(password) > 72 {
		return nil, errors.New("password must contain between 8 and 72 bytes")
	}
	return bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
}

func VerifyPassword(hash []byte, password string) bool {
	return bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil
}

func (m *Manager) Issue(identity Identity) (string, error) {
	now := m.now().UTC()
	claims := Claims{
		Username: identity.Username,
		Role:     identity.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "cyberlab-hub",
			Subject:   strconv.FormatInt(identity.UserID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

func (m *Manager) Parse(raw string) (Identity, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return m.secret, nil
	}, jwt.WithIssuer("cyberlab-hub"), jwt.WithExpirationRequired(), jwt.WithTimeFunc(m.now))
	if err != nil || !token.Valid {
		return Identity{}, ErrInvalidToken
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || userID <= 0 || claims.Username == "" || (claims.Role != "learner" && claims.Role != "admin") {
		return Identity{}, ErrInvalidToken
	}
	return Identity{UserID: userID, Username: claims.Username, Role: claims.Role}, nil
}
