package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http" // web server function

	"github.com/go-chi/chi/v5" // add router
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	dsn := "postgres://user:password@localhost:5432/rephrasefb?sslmode=disable"
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

	// API server

	r := chi.NewRouter()

	r.Get("/health", func(w http.ResponseWriter, r *http.Request){
		w.Write([]byte("OK! RephraseFB API server is running"))
	})

	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
		type CreateUserRequest struct {
			ExternalUserID string `json:"externalUserId"`
		}
		var req CreateUserRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// INSERT
		var newID string
		err := db.QueryRow(
			"INSERT INTO users (external_user_id) VALUES ($1) RETURNING id",
			req.ExternalUserID,
		).Scan(&newID)

		if err != nil {
			log.Printf("failed to insert user: %v", err)
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":      newID,
			"message": "user created successfully",
		})
	})

	log.Println("Server starting on port 8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
