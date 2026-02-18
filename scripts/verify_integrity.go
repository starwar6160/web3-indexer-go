package main

import (
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// #nosec G101 - This is a fallback example URL for local development
		dbURL = "postgres://postgres:password@localhost:15432/web3_indexer?sslmode=disable"
	}

	db, err := sqlx.Connect("pgx", dbURL)
	if err != nil {
		log.Fatalf("❌ Integrity Check Failed: Database connection error: %v", err)
	}
	defer db.Close()

	fmt.Println("🔍 Starting Data Integrity Audit...")

	var blocks []struct {
		Number     int64  `db:"number"`
		Hash       string `db:"hash"`
		ParentHash string `db:"parent_hash"`
	}

	// 检查最近的 1000 个区块
	err = db.Select(&blocks, "SELECT number, hash, parent_hash FROM blocks ORDER BY number DESC LIMIT 1000")
	if err != nil {
		log.Fatalf("❌ Failed to fetch blocks: %v", err)
	}

	if len(blocks) < 2 {
		fmt.Println("⚠️ Insufficient data for integrity check.")
		return
	}

	errors := 0
	for i := 0; i < len(blocks)-1; i++ {
		current := blocks[i]
		previous := blocks[i+1]

		if current.ParentHash != previous.Hash {
			fmt.Printf("🚨 HASH CHAIN BROKEN: Block #%d parent_hash does not match #%d hash!\n", current.Number, previous.Number)
			errors++
		}

		if current.Number != previous.Number+1 {
			fmt.Printf("🚨 SEQUENCE GAP DETECTED: Missing blocks between #%d and #%d\n", current.Number, previous.Number)
			errors++
		}
	}

	reportFile := "docs/integrity_report.log"
	// 📋 Modern Go 1.13+ octal literal: 0o600 instead of 0600
	f, err := os.OpenFile(reportFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("Failed to open report file: %v", err)
	}
	defer f.Close()

	timestamp := time.Now().Format(time.RFC3339)
	status := "PASS"
	if errors > 0 {
		status = fmt.Sprintf("FAIL (%d errors)", errors)
	}

	logMsg := fmt.Sprintf("[%s] Integrity Check: %s | Checked %d blocks | Head: #%d\n",
		timestamp, status, len(blocks), blocks[0].Number)

	// #nosec G104 - Write errors are non-critical for logging
	f.WriteString(logMsg) // #nosec G104
	fmt.Printf("✅ Audit Complete. Status: %s. Report saved to %s\n", status, reportFile)

	if errors > 0 {
		os.Exit(1)
	}
}
