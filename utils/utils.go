package utils

import "math/rand"

// GenerateRandCode 生成长度为length的随机验证码
func GenerateRandCode(length int) string {
	digits := "0123456789"
	code := make([]byte, length)
	for i := 0; i < length; i++ {
		code[i] = digits[rand.Intn(len(digits))]
	}
	return string(code)

}
