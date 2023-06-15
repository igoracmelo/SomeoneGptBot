package env

import (
	"fmt"
	"os"
	"strconv"
)

func Must(key string) string {
	val := os.Getenv(key)
	if val == "" {
		panic(fmt.Sprintf("empty env variable '%v'", key))
	}
	return val
}

func MustInt64(key string) int64 {
	val := Must(key)
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("failed to parse env variable '%v' with value '%v' to int64", key, val))
	}
	return n
}
