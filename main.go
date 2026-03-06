package main

import (
	"database/sql"
	"log"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := "postgres://user:password@localhost:5432/writing_app?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("failed to connect database: %v", err)
	}
	defer db.Close()

	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		external_user_id VARCHAR(255) UNIQUE NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS tasks (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		condition VARCHAR(50) NOT NULL CHECK (condition IN ('control', 'experimental')),
		final_text TEXT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS suggestions (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		task_id UUID NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
		tracking_id VARCHAR(255) NOT NULL,
		action VARCHAR(50) NOT NULL CHECK (action IN ('accepted', 'ignored')),
		category VARCHAR(50) NOT NULL CHECK (category IN ('Mechanics', 'Naturalness', 'Clarity', 'Mixed-Language')),
		original_text TEXT NOT NULL,
		suggested_text TEXT NOT NULL,
		reason_text TEXT,
		audio_played VARCHAR(50) NOT NULL CHECK (audio_played IN ('played', 'ignored')),
		created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err = db.Exec(schema); err != nil {
		log.Fatalf("failed to execute migration: %v", err)
	}

	log.Println("migration completed successfully")
}
