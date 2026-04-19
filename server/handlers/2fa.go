package handlers

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	appdb "github.com/anveesa/nias/db"
	"golang.org/x/crypto/bcrypt"
)

// Setup2FA generates a new TOTP secret and QR code for the user
func Setup2FA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID, _ := strconv.ParseInt(userIDStr, 10, 64)

		// Get username
		var username string
		err := appdb.DB.QueryRow(`SELECT username FROM users WHERE id = ?`, userID).Scan(&username)
		if err != nil {
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}

		// Generate TOTP key
		key, err := totp.Generate(totp.GenerateOpts{
			Issuer:      "Anveesa Nias",
			AccountName: username,
			Period:      30,
			Digits:      otp.DigitsSix,
			Algorithm:   otp.AlgorithmSHA1,
		})
		if err != nil {
			http.Error(w, `{"error":"failed to generate key"}`, http.StatusInternalServerError)
			return
		}

		// Generate backup codes (10 codes)
		backupCodes := make([]string, 10)
		for i := 0; i < 10; i++ {
			b := make([]byte, 6)
			rand.Read(b)
			backupCodes[i] = fmt.Sprintf("%X-%X", b[:3], b[3:])
		}
		backupCodesJSON, _ := json.Marshal(backupCodes)

		// Save secret (not enabled yet)
		_, err = appdb.DB.Exec(`UPDATE users SET totp_secret = ?, backup_codes = ? WHERE id = ?`,
			key.Secret(), string(backupCodesJSON), userID)
		if err != nil {
			http.Error(w, `{"error":"failed to save secret"}`, http.StatusInternalServerError)
			return
		}

		// Return secret and QR code URL
		json.NewEncoder(w).Encode(map[string]interface{}{
			"secret":        key.Secret(),
			"qr_code":       key.URL(),
			"backup_codes":  backupCodes,
		})
	}
}

// Enable2FA verifies the TOTP code and enables 2FA for the user
func Enable2FA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID, _ := strconv.ParseInt(userIDStr, 10, 64)

		var body struct {
			Code string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Get secret
		var secret string
		err := appdb.DB.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&secret)
		if err != nil || secret == "" {
			http.Error(w, `{"error":"2FA not set up"}`, http.StatusBadRequest)
			return
		}

		// Verify code
		valid := totp.Validate(body.Code, secret)
		if !valid {
			http.Error(w, `{"error":"invalid code"}`, http.StatusBadRequest)
			return
		}

		// Enable 2FA
		_, err = appdb.DB.Exec(`UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID)
		if err != nil {
			http.Error(w, `{"error":"failed to enable 2FA"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "2FA enabled successfully"})
	}
}

// Disable2FA disables 2FA for the user (requires password or backup code)
func Disable2FA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID, _ := strconv.ParseInt(userIDStr, 10, 64)

		var body struct {
			Password   string `json:"password"`
			BackupCode string `json:"backup_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Verify password or backup code
		var storedPassword, backupCodesJSON string
		err := appdb.DB.QueryRow(`SELECT password, COALESCE(backup_codes, '[]') FROM users WHERE id = ?`, userID).
			Scan(&storedPassword, &backupCodesJSON)
		if err != nil {
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}

		verified := false

		// Check password
		if body.Password != "" {
			verified = bcrypt.CompareHashAndPassword([]byte(storedPassword), []byte(body.Password)) == nil
		}

		// Check backup code
		if body.BackupCode != "" {
			var backupCodes []string
			json.Unmarshal([]byte(backupCodesJSON), &backupCodes)
			for i, code := range backupCodes {
				if code == body.BackupCode {
					// Remove used backup code
					backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
					newJSON, _ := json.Marshal(backupCodes)
					appdb.DB.Exec(`UPDATE users SET backup_codes = ? WHERE id = ?`, string(newJSON), userID)
					verified = true
					break
				}
			}
		}

		if !verified {
			http.Error(w, `{"error":"invalid credentials"}`, http.StatusUnauthorized)
			return
		}

		// Disable 2FA
		_, err = appdb.DB.Exec(`UPDATE users SET totp_enabled = 0, totp_secret = NULL, backup_codes = NULL WHERE id = ?`, userID)
		if err != nil {
			http.Error(w, `{"error":"failed to disable 2FA"}`, http.StatusInternalServerError)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"message": "2FA disabled successfully"})
	}
}

// Verify2FA verifies a TOTP code during login
func Verify2FA() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		var body struct {
			Username   string `json:"username"`
			Code       string `json:"code"`
			BackupCode string `json:"backup_code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, `{"error":"invalid request"}`, http.StatusBadRequest)
			return
		}

		// Get user
		var userID int64
		var secret, backupCodesJSON string
		var totpEnabled int
		
		query := `SELECT id, totp_secret, totp_enabled, COALESCE(backup_codes, '[]') FROM users WHERE username = ?`
		if appdb.IsPostgreSQL() || appdb.IsMySQL() {
			query = `SELECT id, totp_secret, totp_enabled, COALESCE(backup_codes, '[]') FROM users WHERE username = $1`
		}
		
		err := appdb.DB.QueryRow(query, body.Username).Scan(&userID, &secret, &totpEnabled, &backupCodesJSON)
		if err != nil {
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}

		if totpEnabled == 0 {
			http.Error(w, `{"error":"2FA not enabled"}`, http.StatusBadRequest)
			return
		}

		verified := false

		// Verify TOTP code
		if body.Code != "" {
			verified = totp.Validate(body.Code, secret)
		}

		// Verify backup code
		if body.BackupCode != "" {
			var backupCodes []string
			json.Unmarshal([]byte(backupCodesJSON), &backupCodes)
			for i, code := range backupCodes {
				if code == body.BackupCode {
					// Remove used backup code
					backupCodes = append(backupCodes[:i], backupCodes[i+1:]...)
					newJSON, _ := json.Marshal(backupCodes)
					appdb.DB.Exec(`UPDATE users SET backup_codes = ? WHERE id = ?`, string(newJSON), userID)
					verified = true
					break
				}
			}
		}

		if !verified {
			http.Error(w, `{"error":"invalid code"}`, http.StatusUnauthorized)
			return
		}

		json.NewEncoder(w).Encode(map[string]bool{"verified": true})
	}
}

// Get2FAStatus returns the 2FA status for the current user
func Get2FAStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		userIDStr := r.Header.Get("X-User-ID")
		if userIDStr == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		userID, _ := strconv.ParseInt(userIDStr, 10, 64)

		var totpEnabled int
		var backupCodesJSON string
		err := appdb.DB.QueryRow(`SELECT COALESCE(totp_enabled, 0), COALESCE(backup_codes, '[]') FROM users WHERE id = ?`, userID).
			Scan(&totpEnabled, &backupCodesJSON)
		if err != nil {
			http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
			return
		}

		var backupCodes []string
		json.Unmarshal([]byte(backupCodesJSON), &backupCodes)

		json.NewEncoder(w).Encode(map[string]interface{}{
			"enabled":             totpEnabled == 1,
			"backup_codes_count":  len(backupCodes),
		})
	}
}
