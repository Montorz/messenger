package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// LogLevel - уровень детализации логирования
type LogLevel int

const (
	DEBUG LogLevel = 0 // Отладка
	INFO  LogLevel = 1 // Информация
	WARN  LogLevel = 2 // Предупреждения
	ERROR LogLevel = 3 // Ошибки
)

// Logger - основная структура системы логирования
type Logger struct {
	infoLog  *log.Logger // [INFO] сообщения
	errorLog *log.Logger // [ERROR] сообщения
	debugLog *log.Logger // [DEBUG] сообщения
	file     *os.File    // Файл лога
	level    LogLevel    // Текущий уровень фильтрации
}

// NewLogger - создаёт новый логгер с записью в файл и консоль
func NewLogger(logDir string) (*Logger, error) {
	// Создаём директорию для логов (0755 = rwxr-xr-x - только чтения для других)
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("не удалось создать директорию логов: %v", err)
	}

	// Формируем имя файла
	logFileName := filepath.Join(logDir, fmt.Sprintf("server_%s.log", time.Now().Format("2006-01-02")))

	// Открываем файл (0666 = rw-rw-rw- - все могут читать и писать)
	file, err := os.OpenFile(logFileName, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать файл лога: %v", err)
	}

	// MultiWriter позволяет писать и в файл, и в консоль
	multiWriter := io.MultiWriter(file, os.Stdout)

	return &Logger{
		infoLog:  log.New(multiWriter, "[INFO] ", log.Ldate|log.Ltime),                 // (дата+время)
		errorLog: log.New(multiWriter, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile), // (дата+время+файл:строка)
		debugLog: log.New(multiWriter, "[DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile), // (дата+время+файл:строка)
		file:     file,
		level:    INFO,
	}, nil
}

// SetLevel - устанавливает уровень детализации логирования
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// Info - логирует информационное сообщение
func (l *Logger) Info(format string, v ...interface{}) {
	if l.level <= INFO {
		l.infoLog.Printf(format, v...)
	}
}

// Error - логирует ошибку с информацией о месте возникновения
func (l *Logger) Error(format string, v ...interface{}) {
	if l.level <= ERROR {
		l.errorLog.Printf(format, v...)
	}
}

// Debug - логирует отладочное сообщение (только при DEBUG уровне)
func (l *Logger) Debug(format string, v ...interface{}) {
	if l.level <= DEBUG {
		l.debugLog.Printf(format, v...)
	}
}

// Warn - логирует предупреждение (не критическая проблема)
func (l *Logger) Warn(format string, v ...interface{}) {
	if l.level <= WARN {
		l.infoLog.Printf("[WARN] "+format, v...)
	}
}

// Close - закрывает файл лога
func (l *Logger) Close() error {
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// LogMessage - логирует отправку/получение сообщения
func (l *Logger) LogMessage(from, to, content string) {
	l.Info("СООБЩЕНИЕ: [%s] -> [%s]: %s", from, to, content)
}

// LogUserAction - логирует действие пользователя
func (l *Logger) LogUserAction(username, action string) {
	l.Info("ПОЛЬЗОВАТЕЛЬ: %s - %s", username, action)
}

// LogServerAction - логирует действие сервера
func (l *Logger) LogServerAction(action string) {
	l.Info("СЕРВЕР: %s", action)
}

// LogError - логирует ошибку с деталями операции
func (l *Logger) LogError(operation string, err error) {
	l.Error("ОШИБКА: %s - %v", operation, err)
}

// GetCallerInfo - возвращает информацию о вызвавшей функции
func GetCallerInfo() string {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		return "unknown"
	}
	return fmt.Sprintf("%s:%d", filepath.Base(file), line)
}
