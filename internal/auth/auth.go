// Package auth handles password hashing and JWT session cookies for the
// config-plane admin UI.
package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const cookieName = "knobs_session"
const sessionTTL = 7 * 24 * time.Hour

// DummyHash is a valid bcrypt hash used to equalize login timing when the
// user does not exist, defeating username enumeration via response latency.
var DummyHash = mustDummyHash()

func mustDummyHash() string {
	h, err := bcrypt.GenerateFromPassword([]byte("knobs-dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(h)
}

type Authenticator struct {
	secret       []byte
	cookieSecure bool
}

func New(secret string, cookieSecure bool) *Authenticator {
	return &Authenticator{secret: []byte(secret), cookieSecure: cookieSecure}
}

func (a *Authenticator) Hash(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}

func (a *Authenticator) Check(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func (a *Authenticator) Issue(userID uuid.UUID) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(sessionTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

func (a *Authenticator) Verify(token string) (uuid.UUID, error) {
	parsed, err := jwt.ParseWithClaims(token, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return a.secret, nil
		})
	if err != nil {
		return uuid.Nil, err
	}
	claims, ok := parsed.Claims.(*jwt.RegisteredClaims)
	if !ok || !parsed.Valid {
		return uuid.Nil, errors.New("invalid token")
	}
	return uuid.Parse(claims.Subject)
}

func (a *Authenticator) SetCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.cookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func (a *Authenticator) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/",
		HttpOnly: true, Secure: a.cookieSecure, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// CookieName exposes the session cookie name for handlers reading the request.
func CookieName() string { return cookieName }
