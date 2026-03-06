package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"      
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv" 
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/api/option"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
)

func main() {
	// -環境変数の読み込み
	err := godotenv.Load()
	if err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	apiKey := os.Getenv("GEMINI_API_KEY")
	dbURL := os.Getenv("DATABASE_URL")

	if apiKey == "" {
		log.Fatal("GEMINI_API_KEY must be set")
	}
	if dbURL == "" {
		// フォールバック（未設定時のデフォルト）
		dbURL = "postgres://user:password@localhost:5432/rephrasefb?sslmode=disable"
	}

	// database
	db, err := sql.Open("pgx", dbURL)
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
    
    CREATE UNIQUE INDEX IF NOT EXISTS idx_suggestions_tracking_id ON suggestions (tracking_id);
    `
	if _, err = db.Exec(schema); err != nil {
		log.Fatalf("failed to execute migration: %v", err)
	}
	log.Println("migration completed successfully")

	// init Gemini
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	model := client.GenerativeModel("gemini-2.5-flash")

	// ルーターの初期化とCORS設定
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{"http://localhost:*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type"},
	}))

	// endpoints ---

	// ヘルスチェック
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK! RephraseFB API server is running"))
	})

	// 日本語補完・提案API
	r.Post("/suggestions", func(w http.ResponseWriter, r *http.Request) {
		type SuggestRequest struct {
			TargetSentence string `json:"target_sentence"`
			Context        string `json:"context"`
		}
		var req SuggestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		// Prompt
		prompt := fmt.Sprintf(`Your primary task is to help users who mix Japanese words or phrases into their English sentences because they don't know the correct English equivalent. You must identify the Japanese part, understand its meaning in context, and then revise the entire sentence to be grammatically correct and sound natural to a native English speaker.

        Instructions:
        1. Analyze the user's text to find the Japanese word or phrase.
        2. Translate that Japanese part into the most appropriate English equivalent for the context.
        3. Rewrite the entire sentence to be natural and fluent.
        4. Respond ONLY with the raw corrected sentence.

        Context before the target sentence: %s
        User's Input: %s
        Corrected Sentence:`, req.Context, req.TargetSentence)

		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			log.Printf("AI Error: %v", err)
			http.Error(w, "AI generation failed", http.StatusInternalServerError)
			return
		}

		var suggestionText string
		if len(resp.Candidates) > 0 {
			for _, part := range resp.Candidates[0].Content.Parts {
				suggestionText += fmt.Sprintf("%v", part)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"suggestion": suggestionText})
	})

	// 自然な英文チェックAPI 
	r.Post("/naturalness", func(w http.ResponseWriter, r *http.Request) {
		type NaturalRequest struct {
			Sentence   string   `json:"sentence"`
			Context    string   `json:"context"`
			Evolutions []string `json:"evolutions"`
		}
		var req NaturalRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		//Prompt
		prompt := fmt.Sprintf(`You are an expert English writing assistant and a consistent, insightful coach for Japanese learners.
        
        **--- TASK ---**
        Analyze the "User's Sentence" based on the "Context", and then generate a single, raw JSON response.
        
        **TASK RULES:**
        1. Linguistic Scope: Mechanics, Naturalness, Clarity ONLY.
        2. Definition of "Perfect": Grammatically correct and natural-sounding.
        3. Action for "Imperfect": Provide suggestion (different from original), category, and reason (IN JAPANESE).

        Context: %s
        User's Sentence: %s
        
        Respond ONLY with a single, raw JSON object:
        {
          "is_perfect": boolean,
          "suggestion": "string",
          "category": "string",
          "reason": "string"
        }`, req.Context, req.Sentence)

		resp, err := model.GenerateContent(ctx, genai.Text(prompt))
		if err != nil {
			http.Error(w, "AI generation failed", http.StatusInternalServerError)
			return
		}

		// JSON部分のみを抽出
		var rawResponse string
		if len(resp.Candidates) > 0 {
			for _, part := range resp.Candidates[0].Content.Parts {
				rawResponse += fmt.Sprintf("%v", part)
			}
		}
		//クリーニング処理
		rawResponse = strings.TrimSpace(rawResponse)
		if strings.HasPrefix(rawResponse, "```json") {
			rawResponse = strings.TrimPrefix(rawResponse, "```json")
		} else if strings.HasPrefix(rawResponse, "```") {
			rawResponse = strings.TrimPrefix(rawResponse, "```")
		}
		rawResponse = strings.TrimSuffix(strings.TrimSpace(rawResponse), "```")
		rawResponse = strings.TrimSpace(rawResponse)

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(rawResponse))
	})



	// ユーザー作成
	r.Post("/users", func(w http.ResponseWriter, r *http.Request) {
		type CreateUserRequest struct {
			ExternalUserID string `json:"externalUserId"`
		}
		var req CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var id string
		err := db.QueryRow(
			`INSERT INTO users (external_user_id) 
			 VALUES ($1) 
			 ON CONFLICT (external_user_id) 
			 DO UPDATE SET updated_at = CURRENT_TIMESTAMP 
			 RETURNING id`,
			req.ExternalUserID,
		).Scan(&id)

		if err != nil {
			log.Printf("failed to insert user: %v", err)
			http.Error(w, "failed to create user", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":      id,
			"message": "user created successfully",
		})
	})

	// タスク作成
	r.Post("/tasks", func(w http.ResponseWriter, r *http.Request) {
		type CreateTaskRequest struct {
			UserID    string `json:"userId"`
			Condition string `json:"condition"`
		}
		var req CreateTaskRequest

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		var newID string
		err := db.QueryRow(
			"INSERT INTO tasks (user_id, condition, final_text) VALUES ($1, $2, '') RETURNING id",
			req.UserID, req.Condition,
		).Scan(&newID)

		if err != nil {
			log.Printf("failed to insert task: %v", err)
			http.Error(w, "failed to create task", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": newID})
	})

	// タスク更新 (課題提出)
	r.Put("/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		taskID := chi.URLParam(r, "id")
		type UpdateTaskRequest struct {
			FinalText string `json:"finalText"`
		}
		var req UpdateTaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		_, err := db.Exec(
			"UPDATE tasks SET final_text = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2",
			req.FinalText, taskID,
		)
		if err != nil {
			log.Printf("failed to update task: %v", err)
			http.Error(w, "failed to update task", http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "task updated successfully"}`))
	})
	//提案を受け入れたかどうか記録
	r.Post("/suggestions/log", func(w http.ResponseWriter, r *http.Request) {
		type LogRequest struct {
			UserID        string `json:"userId"`
			TaskID        string `json:"taskId"`
			TrackingID    string `json:"trackingId"`
			Action        string `json:"action"`
			Category      string `json:"category"`
			OriginalText  string `json:"originalText"`
			SuggestedText string `json:"suggestedText"`
			ReasonText    string `json:"reasonText"`
		}
		var req LogRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		validCategories := map[string]bool{
            "Mechanics":      true,
            "Naturalness":    true,
            "Clarity":        true,
            "Mixed-Language": true,
        }
        if !validCategories[req.Category] {
            log.Printf("Invalid category from AI: '%s', falling back to 'Naturalness'", req.Category)
            req.Category = "Naturalness"
        }

		_, err := db.Exec(
			`INSERT INTO suggestions 
            (user_id, task_id, tracking_id, action, category, original_text, suggested_text, reason_text, audio_played) 
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'ignored')
            ON CONFLICT (tracking_id) DO NOTHING`,
			req.UserID, req.TaskID, req.TrackingID, req.Action, req.Category, req.OriginalText, req.SuggestedText, req.ReasonText,
		)
		if err != nil {
			log.Printf("DB Log Error: %v", err)
			http.Error(w, "failed to log", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})

	// 提案アクション更新
	r.Put("/suggestions/log/{trackingId}/action", func(w http.ResponseWriter, r *http.Request) {
		trackingID := chi.URLParam(r, "trackingId")
		type UpdateRequest struct {
			Action string `json:"action"`
		}
		var req UpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		_, err := db.Exec("UPDATE suggestions SET action = $1 WHERE tracking_id = $2", req.Action, trackingID)
		if err != nil {
			log.Printf("Update Action Error: %v", err)
			http.Error(w, "failed to update action", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Post("/tts", func(w http.ResponseWriter, r *http.Request) {
		type TTSRequest struct {
			Text string `json:"text"`
		}
		var req TTSRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		// 環境変数からサービスアカウントJSONを取得
		saJson := os.Getenv("GOOGLE_SERVICE_ACCOUNT_JSON")
		if saJson == "" {
			log.Println("GOOGLE_SERVICE_ACCOUNT_JSON is not set")
			http.Error(w, "TTS credentials not configured", http.StatusInternalServerError)
			return
		}

		// TTSクライアントの作成
		client, err := texttospeech.NewClient(ctx, option.WithCredentialsJSON([]byte(saJson)))
		if err != nil {
			log.Printf("failed to create TTS client: %v", err)
			http.Error(w, "TTS client error", http.StatusInternalServerError)
			return
		}
		defer client.Close()

		//Chirpモデルの設定
		ttsReq := &texttospeechpb.SynthesizeSpeechRequest{
			Input: &texttospeechpb.SynthesisInput{
				InputSource: &texttospeechpb.SynthesisInput_Text{Text: req.Text},
			},
			Voice: &texttospeechpb.VoiceSelectionParams{
				LanguageCode: "en-US",
				Name:         "en-us-Chirp3-HD-Achernar",
			},
			AudioConfig: &texttospeechpb.AudioConfig{
				AudioEncoding: texttospeechpb.AudioEncoding_MP3,
			},
		}

		// 音声生成の実行
		resp, err := client.SynthesizeSpeech(ctx, ttsReq)
		if err != nil {
			log.Printf("failed to synthesize speech: %v", err)
			http.Error(w, "TTS synthesis failed", http.StatusInternalServerError)
			return
		}

		// バイナリデータ(MP3)を直接レスポンスとして返す
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write(resp.AudioContent)
	})

	// 音声再生ログ
	r.Put("/suggestions/log/{trackingId}/audio", func(w http.ResponseWriter, r *http.Request) {
		trackingID := chi.URLParam(r, "trackingId")
		_, err := db.Exec("UPDATE suggestions SET audio_played = 'played' WHERE tracking_id = $1", trackingID)
		if err != nil {
			log.Printf("Update Audio Error: %v", err)
			http.Error(w, "failed to update audio status", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// --- 6. サーバー起動 ---
	log.Println("Server starting on port 8080...")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}