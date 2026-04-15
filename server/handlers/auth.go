package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"
	"unicode"

	"github.com/anveesa/nias/config"
	appdb "github.com/anveesa/nias/db"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 12 // Higher than default (10) for better security
	minPwdLen  = 8
)

type Claims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Mutex to prevent race condition in first-user registration
var registerMu sync.Mutex

func SetupHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"auth_enabled": cfg.AuthEnabled,
		})
	}
}

func LoginHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"bad request"}`, http.StatusBadRequest)
			return
		}

		var (
			id       int64
			hash     string
			role     string
			username string
		)
		err := appdb.DB.QueryRow(
			`SELECT id, username, password, role FROM users WHERE username = ?`, body.Username,
		).Scan(&id, &username, &hash, &role)
		if err != nil {
			// Use constant-time comparison to prevent timing attacks
			bcrypt.CompareHashAndPassword([]byte("$2a$12$dummy.hash.for.timing"), []byte(body.Password))
			http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
			return
		}
		if err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(body.Password)); err != nil {
			http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
			return
		}

		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &Claims{
			UserID:   id,
			Username: username,
			Role:     role,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "anveesa-nias",
				Subject:   username,
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.JWTExpiry) * time.Hour)),
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				NotBefore: jwt.NewNumericDate(time.Now()),
			},
		})
		tokenStr, err := token.SignedString([]byte(cfg.JWTSecret))
		if err != nil {
			http.Error(w, `{"error":"token error"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"token": tokenStr,
			"user":  map[string]any{"id": id, "username": username, "role": role},
		})
	}
}

// validatePassword checks password strength
func validatePassword(password string) error {
	if len(password) < minPwdLen {
		return errors.New("password must be at least 8 characters")
	}
	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		switch {
		case unicode.IsUpper(c):
			hasUpper = true
		case unicode.IsLower(c):
			hasLower = true
		case unicode.IsDigit(c):
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return errors.New("password must contain uppercase, lowercase, and a digit")
	}
	return nil
}

func RegisterHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Username == "" || body.Password == "" {
			http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
			return
		}

		// Validate username length
		if len(body.Username) < 3 || len(body.Username) > 50 {
			http.Error(w, `{"error":"username must be 3-50 characters"}`, http.StatusBadRequest)
			return
		}

		// Validate password strength
		if err := validatePassword(body.Password); err != nil {
			http.Error(w, jsonError(err.Error()), http.StatusBadRequest)
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcryptCost)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		// Use mutex to prevent race condition when determining first user
		registerMu.Lock()
		defer registerMu.Unlock()

		var count int
		appdb.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count)
		role := "user"
		if count == 0 {
			role = "admin"
		}

		res, err := appdb.DB.Exec(
			`INSERT INTO users (username, password, role) VALUES (?, ?, ?)`,
			body.Username, string(hash), role,
		)
		if err != nil {
			// Generic error to prevent username enumeration
			http.Error(w, `{"error":"registration failed"}`, http.StatusConflict)
			return
		}
		id, _ := res.LastInsertId()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id": id, "username": body.Username, "role": role,
		})
	}
}

// jwtSecret is set at startup so MeHandler can parse tokens.
var jwtSecret []byte
var jwtSecretOnce sync.Once

func SetJWTSecret(s string) {
	jwtSecretOnce.Do(func() {
		jwtSecret = []byte(s)
	})
}

func MeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		auth := r.Header.Get("Authorization")
		if len(auth) < 8 || auth[:7] != "Bearer " {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		tokenStr := auth[7:]
		token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
			// Validate algorithm to prevent algorithm confusion attacks
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("invalid signing method")
			}
			return jwtSecret, nil
		})
		if err != nil || !token.Valid {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		claims := token.Claims.(*Claims)

		// Validate issuer
		if claims.Issuer != "anveesa-nias" && claims.Issuer != "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":       claims.UserID,
			"username": claims.Username,
			"role":     claims.Role,
		})
	}
}
