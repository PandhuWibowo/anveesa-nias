// Tool to fix inactive users
// Run: go run fix_users.go

package main

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./bin/nias.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Set all existing users to active
	result, err := db.Exec(`UPDATE users SET is_active = 1 WHERE is_active IS NULL OR is_active = 0`)
	if err != nil {
		log.Fatal("Failed to update users:", err)
	}

	affected, _ := result.RowsAffected()
	fmt.Printf("✓ Updated %d users to active status\n", affected)

	// Verify
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM users WHERE is_active = 1`).Scan(&count)
	fmt.Printf("✓ Total active users: %d\n", count)

	// Show all users
	rows, _ := db.Query(`
		SELECT id, username, role, COALESCE(is_active, 0) as is_active 
		FROM users 
		ORDER BY id
	`)
	defer rows.Close()

	fmt.Println("\nAll users:")
	for rows.Next() {
		var id int
		var username, role string
		var isActive int
		rows.Scan(&id, &username, &role, &isActive)
		status := "Inactive"
		if isActive == 1 {
			status = "Active"
		}
		fmt.Printf("  %d. %s (%s) - %s\n", id, username, role, status)
	}
}
