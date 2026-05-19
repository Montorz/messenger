package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Montorz/messenger/internal/auth"
	"github.com/Montorz/messenger/internal/chat"
	"github.com/Montorz/messenger/internal/storage"
	"github.com/google/uuid"
)

var db *storage.DB
var hub *chat.Hub

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Инициализируем БД
	var err error
	db, err = storage.NewDB("./messenger.db")
	if err != nil {
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	// Создаём хаб
	hub = chat.NewHub()
	go hub.Run()

	// REST API endpoints
	http.HandleFunc("/api/register", handleRegister)
	http.HandleFunc("/api/login", handleLogin)
	http.HandleFunc("/ws", handleWebSocket)

	// Запускаем сервер
	server := &http.Server{Addr: ":8080", Handler: nil}

	go func() {
		log.Println("Сервер запущен на http://localhost:8080")
		log.Println("Регистрация: POST /api/register")
		log.Println("Логин: POST /api/login")
		log.Println("WebSocket: ws://localhost:8080/ws?token=<jwt>")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Ошибка сервера:", err)
		}
	}()

	// shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Выключение сервера...")
	server.Close()
}

// handleRegister обрабатывает регистрацию
func handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Создаём пользователя
	user, err := db.CreateUser(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	// Генерируем токен
	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// handleLogin обрабатывает вход
func handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Проверяем пользователя и пароль
	user, err := db.CheckPassword(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Генерируем токен
	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Обновляем last_seen
	db.UpdateLastSeen(user.ID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// handleWebSocket обрабатывает WebSocket подключения с JWT
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusUnauthorized)
		return
	}

	// Проверяем токен
	claims, err := auth.ValidateToken(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	// Обновляем last_seen
	db.UpdateLastSeen(userID)

	log.Printf("WebSocket подключение: %s (%s)", claims.Username, userID)
	chat.ServeWs(hub, w, r, userID, claims.Username)
}
