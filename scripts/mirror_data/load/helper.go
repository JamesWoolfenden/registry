package main

import (
	"fmt"
	"os"
	"strconv"
)

func safeStringFromMap(m map[string]interface{}, key string) (string, error) {
	val, exists := m[key]
	if !exists {
		return "", fmt.Errorf("key %s not found", key)
	}
	str, ok := val.(string)
	if !ok {
		return "", fmt.Errorf("key %s is not a string", key)
	}
	return str, nil
}

func safeMapFromMap(m map[string]interface{}, key string) (map[string]interface{}, error) {
	val, exists := m[key]
	if !exists {
		return nil, fmt.Errorf("key %s not found", key)
	}
	mapVal, ok := val.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("key %s is not a map", key)
	}
	return mapVal, nil
}

// Configuration helpers
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			return parsed
		}
	}
	return defaultValue
}
