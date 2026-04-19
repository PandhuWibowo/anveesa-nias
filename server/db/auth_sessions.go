package db

import (
	"database/sql"
	"time"
)

type AuthSession struct {
	ID         int64     `json:"id"`
	TokenID    string    `json:"token_id"`
	IP         string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Current    bool      `json:"current"`
}

type LoginEvent struct {
	ID            int64     `json:"id"`
	Username      string    `json:"username"`
	IP            string    `json:"ip_address"`
	UserAgent     string    `json:"user_agent"`
	Success       bool      `json:"success"`
	FailureReason string    `json:"failure_reason"`
	CreatedAt     time.Time `json:"created_at"`
}

func CreateAuthSession(userID int64, tokenID, ip, userAgent string, expiresAt time.Time) error {
	_, err := DB.Exec(ConvertQuery(`
		INSERT INTO auth_sessions (user_id, token_id, ip_address, user_agent, last_seen_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`), userID, tokenID, ip, userAgent, time.Now().UTC(), expiresAt.UTC(), time.Now().UTC())
	return err
}

func IsSessionActive(tokenID string) (bool, error) {
	var count int
	err := DB.QueryRow(ConvertQuery(`
		SELECT COUNT(*)
		FROM auth_sessions
		WHERE token_id = ?
		  AND revoked_at IS NULL
		  AND expires_at > ?
	`), tokenID, time.Now().UTC()).Scan(&count)
	return count > 0, err
}

func TouchSession(tokenID string) error {
	_, err := DB.Exec(ConvertQuery(`UPDATE auth_sessions SET last_seen_at = ? WHERE token_id = ?`), time.Now().UTC(), tokenID)
	return err
}

func RevokeSession(tokenID string, userID int64) error {
	_, err := DB.Exec(ConvertQuery(`
		UPDATE auth_sessions
		SET revoked_at = ?
		WHERE token_id = ? AND user_id = ? AND revoked_at IS NULL
	`), time.Now().UTC(), tokenID, userID)
	return err
}

func RevokeAllUserSessions(userID int64, exceptTokenID string) error {
	if exceptTokenID != "" {
		_, err := DB.Exec(ConvertQuery(`
			UPDATE auth_sessions
			SET revoked_at = ?
			WHERE user_id = ? AND token_id <> ? AND revoked_at IS NULL
		`), time.Now().UTC(), userID, exceptTokenID)
		return err
	}
	_, err := DB.Exec(ConvertQuery(`
		UPDATE auth_sessions
		SET revoked_at = ?
		WHERE user_id = ? AND revoked_at IS NULL
	`), time.Now().UTC(), userID)
	return err
}

func ListUserSessions(userID int64) ([]AuthSession, error) {
	rows, err := DB.Query(ConvertQuery(`
		SELECT id, token_id, COALESCE(ip_address, ''), COALESCE(user_agent, ''), last_seen_at, expires_at, revoked_at, created_at
		FROM auth_sessions
		WHERE user_id = ?
		ORDER BY created_at DESC
		LIMIT 25
	`), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []AuthSession
	for rows.Next() {
		var s AuthSession
		var revoked sql.NullTime
		if err := rows.Scan(&s.ID, &s.TokenID, &s.IP, &s.UserAgent, &s.LastSeenAt, &s.ExpiresAt, &revoked, &s.CreatedAt); err != nil {
			return nil, err
		}
		if revoked.Valid {
			rt := revoked.Time
			s.RevokedAt = &rt
		}
		sessions = append(sessions, s)
	}
	if sessions == nil {
		sessions = []AuthSession{}
	}
	return sessions, nil
}

func RecordLoginEvent(userID *int64, username, ip, userAgent string, success bool, failureReason string) error {
	var uid any
	if userID != nil {
		uid = *userID
	}
	successInt := 0
	if success {
		successInt = 1
	}
	_, err := DB.Exec(ConvertQuery(`
		INSERT INTO login_events (user_id, username, ip_address, user_agent, success, failure_reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`), uid, username, ip, userAgent, successInt, failureReason, time.Now().UTC())
	return err
}

func ListLoginEvents(userID int64) ([]LoginEvent, error) {
	rows, err := DB.Query(ConvertQuery(`
		SELECT id, username, COALESCE(ip_address, ''), COALESCE(user_agent, ''), success, COALESCE(failure_reason, ''), created_at
		FROM login_events
		WHERE user_id = ? OR (user_id IS NULL AND username = (SELECT username FROM users WHERE id = ?))
		ORDER BY created_at DESC
		LIMIT 25
	`), userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []LoginEvent
	for rows.Next() {
		var e LoginEvent
		var successInt int
		if err := rows.Scan(&e.ID, &e.Username, &e.IP, &e.UserAgent, &successInt, &e.FailureReason, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Success = successInt == 1
		events = append(events, e)
	}
	if events == nil {
		events = []LoginEvent{}
	}
	return events, nil
}

