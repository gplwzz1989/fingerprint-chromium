package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory      = 64 * 1024
	passwordIterations  = 3
	passwordParallelism = 2
	passwordSaltSize    = 16
	passwordKeySize     = 32
)

func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 256 {
		return "", errors.New("密码长度必须在 12 到 256 个字符之间")
	}

	salt := make([]byte, passwordSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("生成密码盐失败: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, passwordIterations,
		passwordMemory, passwordParallelism, passwordKeySize)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		passwordMemory, passwordIterations, passwordParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, errors.New("密码哈希格式无效")
	}
	parameters := make(map[string]uint32)
	for _, parameter := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(parameter, "=", 2)
		if len(pair) != 2 {
			return false, errors.New("密码哈希参数无效")
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false, errors.New("密码哈希参数无效")
		}
		parameters[pair[0]] = uint32(value)
	}
	memory, okMemory := parameters["m"]
	iterations, okIterations := parameters["t"]
	parallelism, okParallelism := parameters["p"]
	if !okMemory || !okIterations || !okParallelism || memory == 0 ||
		iterations == 0 || parallelism == 0 || memory > 1<<20 ||
		iterations > 10 || parallelism > 8 {
		return false, errors.New("密码哈希参数缺失")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return false, errors.New("密码盐格式无效")
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return false, errors.New("密码摘要格式无效")
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory,
		uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}
