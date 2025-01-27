package common

import (
	"os"
	"strconv"
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
