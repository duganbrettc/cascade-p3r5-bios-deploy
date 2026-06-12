package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

var (
	db       *sql.DB
	tokens   = map[string]int{}
	tokensMu sync.RWMutex
)

type User struct {
	ID          int       `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Bio         string    `json:"bio"`
	Location    string    `json:"location"`
	Website     string    `json:"website"`
	CreatedAt   time.Time `json:"created_at"`
}

type UserListItem struct {
	UserID      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsFollowing bool   `json:"is_following"`
}

type UserProfile struct {
	UserID         int       `json:"user_id"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"display_name"`
	Bio            string    `json:"bio"`
	Location       string    `json:"location"`
	Website        string    `json:"website"`
	CreatedAt      time.Time `json:"created_at"`
	FollowerCount  int       `json:"follower_count"`
	FollowingCount int       `json:"following_count"`
	IsFollowing    bool      `json:"is_following"`
}

type Post struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://persona:persona@db:5432/persona?sslmode=disable"
	}

	var err error
	db, err = sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	for i := 0; i < 30; i++ {
		if err = db.Ping(); err == nil {
			break
		}
		log.Printf("waiting for db (%d/30): %v", i+1, err)
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("cannot connect to db: %v", err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", handleHealthz)

	mux.HandleFunc("/api/auth/signup", withCORS(methodGate(http.MethodPost, handleSignup)))
	mux.HandleFunc("/api/auth/login", withCORS(methodGate(http.MethodPost, handleLogin)))

	mux.HandleFunc("/api/users/me", withCORS(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleGetMe(w, r)
		case http.MethodPatch:
			handlePatchMe(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// /api/posts/by/ must be registered before /api/posts/ so the longer prefix wins
	mux.HandleFunc("/api/posts/by/", withCORS(methodGate(http.MethodGet, handleGetPostsByUsername)))
	mux.HandleFunc("/api/posts", withCORS(methodGate(http.MethodPost, handleCreatePost)))

	mux.HandleFunc("/api/follow/status", withCORS(methodGate(http.MethodGet, handleFollowStatus)))
	mux.HandleFunc("/api/follow/", withCORS(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			handleFollow(w, r)
		case http.MethodDelete:
			handleUnfollow(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	mux.HandleFunc("/api/timeline", withCORS(methodGate(http.MethodGet, handleTimeline)))

	// /api/users/{username} must be registered before /api/users so the longer prefix wins
	mux.HandleFunc("/api/users/", withCORS(methodGate(http.MethodGet, handleGetUser)))
	mux.HandleFunc("/api/users", withCORS(methodGate(http.MethodGet, handleGetUsers)))

	log.Printf("listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func corsHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

func withCORS(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		corsHeaders(w)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func methodGate(method string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func storeToken(token string, userID int) {
	tokensMu.Lock()
	defer tokensMu.Unlock()
	tokens[token] = userID
}

func authFromRequest(r *http.Request) (int, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return 0, false
	}
	token := authHeader[7:]
	tokensMu.RLock()
	userID, ok := tokens[token]
	tokensMu.RUnlock()
	return userID, ok
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func nullableString(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "ok")
}

func handleSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var userID int
	err = db.QueryRow(
		`INSERT INTO users (username, password_hash, display_name, bio, location, website)
		 VALUES ($1, $2, '', '', '', '') RETURNING id`,
		req.Username, string(hash),
	).Scan(&userID)
	if err != nil {
		http.Error(w, "conflict or db error", http.StatusConflict)
		return
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	storeToken(token, userID)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"user_id":       userID,
		"session_token": token,
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var userID int
	var passwordHash string
	err := db.QueryRow(
		`SELECT id, password_hash FROM users WHERE username = $1`,
		req.Username,
	).Scan(&userID, &passwordHash)
	if err == sql.ErrNoRows {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	storeToken(token, userID)

	writeJSON(w, http.StatusOK, map[string]string{"session_token": token})
}

func handleGetMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var u User
	var displayName, bio, location, website sql.NullString
	err := db.QueryRow(
		`SELECT id, username, display_name, bio, location, website, created_at FROM users WHERE id = $1`,
		userID,
	).Scan(&u.ID, &u.Username, &displayName, &bio, &location, &website, &u.CreatedAt)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u.DisplayName = nullableString(displayName)
	u.Bio = nullableString(bio)
	u.Location = nullableString(location)
	u.Website = nullableString(website)

	writeJSON(w, http.StatusOK, u)
}

func handlePatchMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisplayName *string `json:"display_name"`
		Bio         *string `json:"bio"`
		Location    *string `json:"location"`
		Website     *string `json:"website"`
		Password    *string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Bio != nil && len([]rune(*req.Bio)) > 160 {
		http.Error(w, "bio exceeds 160 characters", http.StatusUnprocessableEntity)
		return
	}

	if req.DisplayName != nil {
		if _, err := db.Exec(`UPDATE users SET display_name = $1 WHERE id = $2`, *req.DisplayName, userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if req.Bio != nil {
		if _, err := db.Exec(`UPDATE users SET bio = $1 WHERE id = $2`, *req.Bio, userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if req.Location != nil {
		if _, err := db.Exec(`UPDATE users SET location = $1 WHERE id = $2`, *req.Location, userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if req.Website != nil {
		if _, err := db.Exec(`UPDATE users SET website = $1 WHERE id = $2`, *req.Website, userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	if req.Password != nil {
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if _, err := db.Exec(`UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), userID); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	var u User
	var displayName, bio, location, website sql.NullString
	err := db.QueryRow(
		`SELECT id, username, display_name, bio, location, website, created_at FROM users WHERE id = $1`,
		userID,
	).Scan(&u.ID, &u.Username, &displayName, &bio, &location, &website, &u.CreatedAt)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	u.DisplayName = nullableString(displayName)
	u.Bio = nullableString(bio)
	u.Location = nullableString(location)
	u.Website = nullableString(website)

	writeJSON(w, http.StatusOK, u)
}

func handleGetUsers(w http.ResponseWriter, r *http.Request) {
	callerID, _ := authFromRequest(r)

	rows, err := db.Query(`
		SELECT u.id, u.username, COALESCE(u.display_name, ''),
			CASE WHEN $1 > 0 AND EXISTS(
				SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = u.id
			) THEN TRUE ELSE FALSE END AS is_following
		FROM users u
		ORDER BY u.id`,
		callerID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	users := []UserListItem{}
	for rows.Next() {
		var u UserListItem
		if err := rows.Scan(&u.UserID, &u.Username, &u.DisplayName, &u.IsFollowing); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}
	writeJSON(w, http.StatusOK, users)
}

func handleGetUser(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/users/")
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}
	callerID, _ := authFromRequest(r)

	var u UserProfile
	var displayName, bio, location, website sql.NullString
	err := db.QueryRow(`
		SELECT u.id, u.username,
			COALESCE(u.display_name, ''), COALESCE(u.bio, ''),
			COALESCE(u.location, ''), COALESCE(u.website, ''),
			u.created_at,
			(SELECT COUNT(*) FROM follows WHERE followee_id = u.id) AS follower_count,
			(SELECT COUNT(*) FROM follows WHERE follower_id = u.id) AS following_count,
			CASE WHEN $1 > 0 AND EXISTS(
				SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = u.id
			) THEN TRUE ELSE FALSE END AS is_following
		FROM users u WHERE u.username = $2`,
		callerID, username,
	).Scan(&u.UserID, &u.Username, &displayName, &bio, &location, &website, &u.CreatedAt,
		&u.FollowerCount, &u.FollowingCount, &u.IsFollowing)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	u.DisplayName = nullableString(displayName)
	u.Bio = nullableString(bio)
	u.Location = nullableString(location)
	u.Website = nullableString(website)

	writeJSON(w, http.StatusOK, u)
}

func handleCreatePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if len([]rune(req.Body)) > 280 {
		http.Error(w, "body exceeds 280 characters", http.StatusUnprocessableEntity)
		return
	}

	var p Post
	err := db.QueryRow(
		`INSERT INTO posts (user_id, body) VALUES ($1, $2)
		 RETURNING id, user_id, body, created_at`,
		userID, req.Body,
	).Scan(&p.ID, &p.UserID, &p.Body, &p.CreatedAt)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	err = db.QueryRow(`SELECT username FROM users WHERE id = $1`, userID).Scan(&p.Username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

func handleGetPostsByUsername(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimPrefix(r.URL.Path, "/api/posts/by/")
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	rows, err := db.Query(`
		SELECT p.id, p.user_id, u.username, p.body, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE u.username = $1
		ORDER BY p.created_at DESC
		LIMIT 50`,
		username,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	posts := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.Body, &p.CreatedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		posts = append(posts, p)
	}
	writeJSON(w, http.StatusOK, posts)
}

func handleFollow(w http.ResponseWriter, r *http.Request) {
	callerID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	username := strings.TrimPrefix(r.URL.Path, "/api/follow/")
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	var followeeID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&followeeID)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(
		`INSERT INTO follows (follower_id, followee_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		callerID, followeeID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleUnfollow(w http.ResponseWriter, r *http.Request) {
	callerID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	username := strings.TrimPrefix(r.URL.Path, "/api/follow/")
	if username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	var followeeID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&followeeID)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(
		`DELETE FROM follows WHERE follower_id = $1 AND followee_id = $2`,
		callerID, followeeID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func handleFollowStatus(w http.ResponseWriter, r *http.Request) {
	callerID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		http.Error(w, "username query param required", http.StatusBadRequest)
		return
	}

	var followeeID int
	err := db.QueryRow(`SELECT id FROM users WHERE username = $1`, username).Scan(&followeeID)
	if err == sql.ErrNoRows {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	var following bool
	err = db.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM follows WHERE follower_id = $1 AND followee_id = $2)`,
		callerID, followeeID,
	).Scan(&following)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"is_following": following})
}

func handleTimeline(w http.ResponseWriter, r *http.Request) {
	callerID, ok := authFromRequest(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	rows, err := db.Query(`
		SELECT p.id, p.user_id, u.username, p.body, p.created_at
		FROM posts p
		JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1 OR p.user_id IN (
			SELECT followee_id FROM follows WHERE follower_id = $1
		)
		ORDER BY p.created_at DESC
		LIMIT 50`,
		callerID,
	)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	posts := []Post{}
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.UserID, &p.Username, &p.Body, &p.CreatedAt); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		posts = append(posts, p)
	}
	writeJSON(w, http.StatusOK, posts)
}
