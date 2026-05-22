package main

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/Montorz/messenger/internal/auth"
	"github.com/Montorz/messenger/internal/chat"
	"github.com/Montorz/messenger/internal/storage"
	"github.com/Montorz/messenger/pkg/logger"
	"github.com/google/uuid"
)

var db *storage.DB
var hub *chat.Hub
var appLogger *logger.Logger

func main() {
	// Создаём логгер
	var err error
	appLogger, err = logger.NewLogger("./logs")
	if err != nil {
		log.Fatal("Ошибка создания логгера:", err)
	}
	defer appLogger.Close()

	appLogger.LogServerAction("Запуск сервера с TLS...")

	// Инициализируем БД
	db, err = storage.NewDB("./messenger.db")
	if err != nil {
		appLogger.LogError("Подключение к БД", err)
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	appLogger.Info("База данных подключена")

	// Создаём хаб
	hub = chat.NewHub(db, appLogger)
	go hub.Run()

	// REST API endpoints
	http.HandleFunc("/api/register", handleRegister)
	http.HandleFunc("/api/login", handleLogin)
	http.HandleFunc("/ws", handleWebSocket)

	// Настройка TLS
	certFile := "server.crt"
	keyFile := "server.key"

	// Проверяем наличие сертификатов
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		appLogger.LogServerAction("Сертификат не найден! Сгенерируйте его: openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj '/CN=localhost'")
		log.Fatal("Сертификат не найден")
	}

	// Создаем TLS конфигурацию (упрощённая для совместимости)
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// Curves убрали для совместимости со старыми версиями Go
	}

	server := &http.Server{
		Addr:      ":8443", // HTTPS/WSS порт
		Handler:   nil,
		TLSConfig: tlsConfig,
	}

	// Graceful shutdown
	go func() {
		appLogger.LogServerAction("Сервер запущен на https://localhost:8443")
		appLogger.Info("Регистрация: POST https://localhost:8443/api/register")
		appLogger.Info("Логин: POST https://localhost:8443/api/login")
		appLogger.Info("WebSocket Secure: wss://localhost:8443/ws?token=<jwt>")
		appLogger.Info("Используется самоподписанный сертификат")

		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			appLogger.LogError("Запуск сервера", err)
			log.Fatal("Ошибка сервера:", err)
		}
	}()

	// Обработка сигналов
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	appLogger.LogServerAction("Получен сигнал завершения, выключение...")
	appLogger.Info("Остановка сервера...")
	server.Close()
	appLogger.LogServerAction("Сервер остановлен")
}

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

	user, err := db.CreateUser(req.Username, req.Password)
	if err != nil {
		appLogger.LogError("Регистрация пользователя "+req.Username, err)
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		appLogger.LogError("Генерация токена", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	appLogger.LogUserAction(user.Username, "регистрация")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

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

	user, err := db.CheckPassword(req.Username, req.Password)
	if err != nil {
		appLogger.LogError("Вход пользователя "+req.Username, err)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		appLogger.LogError("Генерация токена", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	db.UpdateLastSeen(user.ID)
	appLogger.LogUserAction(user.Username, "вход в систему")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusUnauthorized)
		return
	}

	claims, err := auth.ValidateToken(token)
	if err != nil {
		appLogger.LogError("WebSocket аутентификация", err)
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	db.UpdateLastSeen(userID)
	appLogger.LogUserAction(claims.Username, "WebSocket Secure подключение")

	chat.ServeWs(hub, w, r, userID, claims.Username)
}
