package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	GoogleClientID     string
	GoogleClientSecret string
	TwitterClientID    string
	TwitterClientSecret string
	SessionSecret      []byte
	RedirectURL        string
	TwitterRedirectURL string
	DatabasePath       string
	Port               string
	SheetRange         string
	SessionMaxAge      int
	LogLevel           string
	Environment        string
	EncryptionKey      []byte
}

func LoadConfig() (*Config, error) {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using system environment variables")
	}

	config := &Config{}

	config.GoogleClientID = os.Getenv("GOOGLE_CLIENT_ID")
	if config.GoogleClientID == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_ID environment variable is required")
	}

	config.GoogleClientSecret = os.Getenv("GOOGLE_CLIENT_SECRET")
	if config.GoogleClientSecret == "" {
		return nil, fmt.Errorf("GOOGLE_CLIENT_SECRET environment variable is required")
	}

	sessionSecret := os.Getenv("SESSION_SECRET")
	if sessionSecret == "" {
		return nil, fmt.Errorf("SESSION_SECRET environment variable is required")
	}

	if len(sessionSecret) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must be at least 32 characters long")
	}

	config.SessionSecret = []byte(sessionSecret)

	config.TwitterClientID = os.Getenv("TWITTER_CLIENT_ID")
	config.TwitterClientSecret = os.Getenv("TWITTER_CLIENT_SECRET")
	
	config.RedirectURL = getEnvWithDefault("REDIRECT_URL", "http://localhost:8080/auth/callback")
	config.TwitterRedirectURL = getEnvWithDefault("TWITTER_REDIRECT_URL", "http://localhost:8080/auth/twitter/callback")
	config.DatabasePath = getEnvWithDefault("DATABASE_PATH", "./directory.db")
	config.Port = getEnvWithDefault("PORT", "8080")
	config.SheetRange = getEnvWithDefault("SHEET_RANGE", "A:Z")
	config.LogLevel = getEnvWithDefault("LOG_LEVEL", "INFO")
	config.Environment = getEnvWithDefault("ENVIRONMENT", "development")

	maxAge, err := strconv.Atoi(getEnvWithDefault("SESSION_MAX_AGE", "86400"))
	if err != nil {
		return nil, fmt.Errorf("invalid SESSION_MAX_AGE: %v", err)
	}
	config.SessionMaxAge = maxAge

	// Load encryption key for token encryption
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	if encryptionKey == "" {
		return nil, fmt.Errorf("ENCRYPTION_KEY environment variable is required")
	}
	
	if len(encryptionKey) != 64 { // 32 bytes in hex = 64 characters
		return nil, fmt.Errorf("ENCRYPTION_KEY must be exactly 64 hex characters (32 bytes)")
	}
	
	keyBytes, err := hex.DecodeString(encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("ENCRYPTION_KEY must be valid hex: %v", err)
	}
	config.EncryptionKey = keyBytes

	return config, nil
}

func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func GenerateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func GenerateCSRFToken() (string, error) {
	return GenerateSecureToken(32)
}

type SessionData struct {
	UserEmail     string    `json:"user_email"`
	Authenticated bool      `json:"authenticated"`
	CSRFToken     string    `json:"csrf_token"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *SessionData) IsExpired(maxAge int) bool {
	return time.Since(s.CreatedAt) > time.Duration(maxAge)*time.Second
}
