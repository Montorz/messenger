package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
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

// Глобальные переменные для доступа из всех хендлеров
var (
	db        *storage.DB    // Подключение к SQLite
	hub       *chat.Hub      // WebSocket хаб (управление клиентами и комнатами)
	appLogger *logger.Logger // Логгер для записи в файл и консоль
)

func main() {
	// ИНИЦИАЛИЗАЦИЯ ЛОГГЕРА
	// Логи пишутся в папку ./logs/
	var err error
	appLogger, err = logger.NewLogger("./logs")
	if err != nil {
		log.Fatal("Ошибка создания логгера:", err)
	}
	defer appLogger.Close()

	appLogger.LogServerAction("Запуск сервера с TLS...")

	// ПОДКЛЮЧЕНИЕ К БАЗЕ ДАННЫХ SQLite
	// Файл БД: messenger.db (создаётся автоматически)
	db, err = storage.NewDB("./messenger.db")
	if err != nil {
		appLogger.LogError("Подключение к БД", err)
		log.Fatal("Ошибка подключения к БД:", err)
	}
	defer db.Close()

	appLogger.Info("База данных подключена")

	// СОЗДАНИЕ WEBSOCKET ХАБА
	// Запускаем хаб в отдельной горутине (неблокирующий цикл)
	hub = chat.NewHub(db, appLogger)
	go hub.Run()

	// РЕГИСТРАЦИЯ HTTP ХЕНДЛЕРОВ
	http.HandleFunc("/api/register", handleRegister)
	http.HandleFunc("/api/login", handleLogin)
	http.HandleFunc("/ws", handleWebSocket)

	// НАСТРОЙКА TLS (ШИФРОВАНИЕ)
	// Используется самоподписанный сертификат для разработки
	certFile := "server.crt"
	keyFile := "server.key"

	// Проверяем наличие файлов сертификатов
	if _, err := os.Stat(certFile); os.IsNotExist(err) {
		appLogger.LogServerAction("Сертификат не найден! Сгенерируйте его: openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes -subj '/CN=localhost'")
		log.Fatal("Сертификат не найден")
	}

	// Создаем TLS конфигурацию
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// HTTP сервер с поддержкой TLS
	server := &http.Server{
		Addr:      "0.0.0.0:8443", // Слушаем все сетевые интерфейсы (для сетевого режима)
		Handler:   nil,            // Используем стандартный мультиплексор http.DefaultServeMux
		TLSConfig: tlsConfig,
	}

	// ЗАПУСК СЕРВЕРА В ГОРУТИНЕ
	// Сервер работает асинхронно, чтобы можно было обработать сигналы
	go func() {
		appLogger.LogServerAction("Сервер запущен на https://localhost:8443")
		appLogger.Info("Регистрация: POST https://localhost:8443/api/register")
		appLogger.Info("Логин: POST https://localhost:8443/api/login")
		appLogger.Info("WebSocket Secure: wss://localhost:8443/ws?token=<jwt>")
		appLogger.Info("Используется самоподписанный сертификат")

		// ListenAndServeTLS запускает HTTPS сервер с указанными сертификатами
		if err := server.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			appLogger.LogError("Запуск сервера", err)
			log.Fatal("Ошибка сервера:", err)
		}
	}()

	// GRACEFUL SHUTDOWN И ОБРАБОТКА СИГНАЛОВ (SIGINT, SIGTERM, SIGHUP)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Запускаем горутину для обработки сигналов
	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGHUP:
				// SIGHUP: перечитать конфигурацию без остановки (kill -HUP <pid>)
				appLogger.LogServerAction("Получен сигнал SIGHUP, перечитываем конфигурацию...")
				appLogger.Info("Конфигурация перечитана, сервер продолжает работу")

			case syscall.SIGINT, syscall.SIGTERM:
				// SIGINT (Ctrl+C) или SIGTERM - завершаем работу
				appLogger.LogServerAction(fmt.Sprintf("Получен сигнал %s, выключение...", sig))
				appLogger.Info("Остановка сервера...")
				server.Close() // Закрываем HTTP сервер
				appLogger.LogServerAction("Сервер остановлен")
				return // Выходим из горутины (программа завершится)
			}
		}
	}()

	// Блокируем основной поток (ждём сигналов в горутине) select{} блокирует навсегда, пока не вызовут return в горутине
	select {}
}

// HTTP метод: обрабатывает POST /api/register
// Формат запроса: {"username": "gleb", "password": "123"}
// Формат ответа: {"token": "jwt...", "user": {"id": "uuid", "username": "gleb"}}
func handleRegister(w http.ResponseWriter, r *http.Request) {
	// Проверяем метод запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Структура для парсинга тела запроса
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	// Декодируем JSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Создаём пользователя в БД (пароль хешируется bcrypt)
	user, err := db.CreateUser(req.Username, req.Password)
	if err != nil {
		appLogger.LogError("Регистрация пользователя "+req.Username, err)
		http.Error(w, "Username already exists", http.StatusConflict)
		return
	}

	// Генерируем JWT токен для нового пользователя
	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		appLogger.LogError("Генерация токена", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Логируем действие
	appLogger.LogUserAction(user.Username, "регистрация")

	// Отправляем ответ с токеном и данными пользователя
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// HTTP метод: обрабатывает POST /api/login
// Формат запроса: {"username": "gleb", "password": "123"}
// Формат ответа: {"token": "jwt...", "user": {"id": "uuid", "username": "gleb"}}
func handleLogin(w http.ResponseWriter, r *http.Request) {
	// Проверяем метод запроса
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Структура для парсинга тела запроса
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	// Декодируем JSON
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Проверяем пароль (bcrypt сравнение с хешем в БД)
	user, err := db.CheckPassword(req.Username, req.Password)
	if err != nil {
		appLogger.LogError("Вход пользователя "+req.Username, err)
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Генерируем новый JWT токен
	token, err := auth.GenerateToken(user.ID, user.Username)
	if err != nil {
		appLogger.LogError("Генерация токена", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Обновляем время последнего визита в БД
	db.UpdateLastSeen(user.ID)
	appLogger.LogUserAction(user.Username, "вход в систему")

	// Отправляем ответ с токеном и данными пользователя
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// HTTP метод: GET обрабатывает WebSocket upgrade запрос
// Параметр запроса: ?token=<jwt>
func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Извлекаем JWT токен из query параметра
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "Token required", http.StatusUnauthorized)
		return
	}

	// Валидируем токен
	claims, err := auth.ValidateToken(token)
	if err != nil {
		appLogger.LogError("WebSocket аутентификация", err)
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	// Парсим UUID пользователя из claims
	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		http.Error(w, "Invalid user ID", http.StatusUnauthorized)
		return
	}

	// Обновляем время последнего визита
	db.UpdateLastSeen(userID)
	appLogger.LogUserAction(claims.Username, "WebSocket Secure подключение")

	// Выполняем upgrade HTTP до WebSocket и запускаем клиента
	chat.ServeWs(hub, w, r, userID, claims.Username)
}
