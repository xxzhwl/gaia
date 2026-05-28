// Package gaia 随机相关逻辑封装
// @author wanlizhan
// @created 2023-07-07
package gaia

import (
	"math/rand"
	"strings"
	"time"
)

// GetRandString 生成返回一个随机长度的string，可能的组成为 大、小写字母、数字
// length 指定需要返回的随机字符串长度
func GetRandString(length int) string {
	retval := make([]string, 0)
	for i := 0; i < length; i++ {
		retval = append(retval, GetRandChar())
	}
	return strings.Join(retval, "")
}

// GetRandChar 获取一个随机的字符
func GetRandChar() string {
	strscope := "aAbBcCdDeEfFgGhHiIgGkKmMlLnNoOpPkKrRsStTuUvVwWxXyYzZ0123456789"
	return string(strscope[Rand(0, len(strscope)-1)])
}

// GetRandHexSecretKey 获取一个指定长度的用于密钥的16进制字符串
func GetRandHexSecretKey(length int) string {
	retval := make([]string, 0)
	for i := 0; i < length; i++ {
		retval = append(retval, GetRandHexChar())
	}
	return strings.Join(retval, "")
}

// GetRandHexChar 获取一个随机的字符(可用于16进制解析的，即范围为0-9和a-f)
func GetRandHexChar() string {
	strscope := "0123456789abcdef"
	return string(strscope[Rand(0, len(strscope)-1)])
}

// Rand 获取一个从[min, max]之间的随机数
func Rand(min, max int) int {
	max += 1
	time.Sleep(time.Nanosecond * 1)
	rand.Seed(time.Now().UnixNano())
	num := rand.Intn(max)
	if min > num {
		return min + num%(max-min)
	}
	return num
}

// GetRandNum 获取随机数字字符串
func GetRandNum() string {
	strscope := "0123456789"
	return string(strscope[Rand(0, len(strscope)-1)])
}

// GetRandStringWithScope 生成返回一个随机长度的string，可能的组成为 大、小写字母、数字
// length 指定需要返回的随机字符串长度
func GetRandStringWithScope(length int, sourceStr string) string {
	retval := make([]string, 0)
	for i := 0; i < length; i++ {
		retval = append(retval, string(sourceStr[Rand(0, len(sourceStr)-1)]))
	}
	return strings.Join(retval, "")
}
