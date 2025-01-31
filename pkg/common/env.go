package common

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

func GetEnvStr(key, defaultValue string) string {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}
	return val
}

func GetEnvInt(key string, defaultValue int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}

	parsedVal, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return parsedVal
}

func GetEnvBool(s string, b bool) bool {
	val := os.Getenv(s)
	if val == "" {
		return b
	}

	parsedVal, err := strconv.ParseBool(val)
	if err != nil {
		return b
	}
	return parsedVal
}

func GetEnvSlice[T any](key string, defaultValue []T) []T {
	val := os.Getenv(key)
	if val == "" {
		return defaultValue
	}

	parts := strings.Split(val, ",")
	result := make([]T, 0, len(parts))

	for _, part := range parts {
		var item T
		trimmedPart := strings.TrimSpace(part)

		switch any(item).(type) {
		case string:
			item = any(trimmedPart).(T)
		case int:
			parsedVal, err := strconv.Atoi(trimmedPart)
			if err != nil {
				return defaultValue
			}
			item = any(parsedVal).(T)
		case bool:
			parsedVal, err := strconv.ParseBool(trimmedPart)
			if err != nil {
				return defaultValue
			}
			item = any(parsedVal).(T)
		default:
			// Return default if type is unsupported
			return defaultValue
		}

		result = append(result, item)
	}

	return result
}

var backOffBase = float64(GetEnvInt("EXPONENTIAL_BACKOFF_BASE", 2))
var backOffMax = float64(GetEnvInt("EXPONENTIAL_BACKOFF_MAX", 30))

func GetRetryDuration(numRetries int) time.Duration {
	return time.Duration(math.Min(backOffMax, math.Pow(backOffBase, float64(numRetries)))) * time.Second
}
