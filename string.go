// Package gaia 与字符串处理相关的封装库
// @author wanlizhan
// @created 2023-06-30
package gaia

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia/cvt"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// SplitStr 切割字符串
func SplitStr(str string) []string {
	str = strings.ReplaceAll(str, ",", ";")
	str = strings.ReplaceAll(str, "，", ";")
	str = strings.ReplaceAll(str, "；", ";")
	return strings.Split(str, ";")
}

// SubStr 获取字符串子串
// str 原始字符串
// start 开始下标
// offset 截取的长度，字节数
func SubStr(str string, start, offset int) string {
	size := len(str)
	if size == 0 {
		return ""
	}
	if start < size {
		if start+offset <= size {
			return str[start : start+offset]
		} else {
			return str[start:size]
		}
	}
	return ""
}

// SubStrUnicode 获取字符串子串，支持UTF-8的Unicode码点，即可支持中英文的混合截取
// str 原始字符串
// start 开始下标
// offset 截取的长度，文字数，包括英文字符和中文文字
func SubStrUnicode(str string, start, offset uint) string {
	if len(str) == 0 {
		return str
	}
	runeStr := []rune(str)
	size := len(runeStr)
	if size == 0 {
		return ""
	}
	startInt := int(start)
	offsetInt := int(offset)
	if startInt < size {
		if startInt+offsetInt <= size {
			return string(runeStr[startInt : startInt+offsetInt])
		} else {
			return string(runeStr[startInt:size])
		}
	}
	return ""
}

// GetUUID 获取一个全局唯一的值(字符串类型)
func GetUUID() string {
	//纳秒+随机数，然后压缩成36进制(0-9,a-z)的数值表示
	return ParseIntBase36(time.Now().UnixNano() + int64(Rand(10, 99)))
}

// ParseIntBase36 将一个16进制的数字，转换成36位进制(0-9,a-z)的压缩字符串
func ParseIntBase36(num int64) string {
	loadArr := make([]string, 0)
	var nextBit int64
	var curBit int64
	for {
		nextBit = num / 36
		if nextBit == 0 {
			//需要转换的数已经小于36
			curBit = num
		} else {
			curBit = num % 36
		}
		loadArr = append(loadArr, _getBase36Flag(int(curBit)))
		if nextBit == 0 {
			break
		}
		num = nextBit
	}

	//由于写入的时候是低位占高位，这里需要倒换顺序
	size := len(loadArr)
	retval := make([]string, size)
	for i := 0; i < size; i++ {
		retval[size-i-1] = loadArr[i]
	}

	return strings.Join(retval, "")
}

func _getBase36Flag(i int) string {
	if i < 10 {
		return fmt.Sprintf("%d", i)
	} else if i >= 10 && i < 36 {
		return string(byte(i - 10 + 97))
	} else {
		return ""
	}

}

// CheckPassComplexity 检查密码复杂度，不合法密码则返回error信息
// 密码复杂度至少要求由 小写字母、大写字母以及数字组成，并且长度不小于8位
func CheckPassComplexity(password string) error {
	err := errors.New("密码复杂度要求至少由数字、大写字母、小写字母组成，并且长度不小于8位！")

	//密码长度
	length := len(password)
	if length < 8 {
		return err
	}

	isNum := false
	isLowerCase := false
	isUpperCase := false

	byteArr := []byte(password)
	for _, i := range byteArr {
		if i >= 48 && i <= 57 {
			isNum = true
		} else if i >= 65 && i <= 90 {
			isUpperCase = true
		} else if i >= 97 && i <= 122 {
			isLowerCase = true
		}
	}

	if isNum && isLowerCase && isUpperCase {
		return nil
	}
	return err
}

// Base64Encode 将一个字符串编码为base64格式的数据
func Base64Encode(str string) string {
	return base64.StdEncoding.EncodeToString([]byte(str))
}

// Base64Decode 将一个base64的字符串数据解码成一个普通字符串
func Base64Decode(base64Str string) (string, error) {
	if result, err := base64.StdEncoding.DecodeString(base64Str); err != nil {
		return "", err
	} else {
		return string(result), nil
	}
}

// CamelCaseToFilename 将驼峰命名的字符串形式，转换成以下画线表标的驼峰命名形式
func CamelCaseToFilename(src string) string {
	reg := regexp.MustCompile(`([A-Z])`)
	result := reg.ReplaceAllString(src, "_$1")
	return strings.ToLower(strings.TrimLeft(result, "_"))
}

// UnderlineToCamelCase 将下划线方式的命名形式转为驼峰命名形式
func UnderlineToCamelCase(str string) string {
	if len(str) == 0 {
		return ""
	}
	arr := strings.Split(str, "_")
	result := make([]string, 0)
	for _, itm := range arr {
		result = append(result, strings.Title(itm))
	}
	return strings.Join(result, "")
}

// Sha256 将数据HASH成sha256值
func Sha256(str string) string {
	h := sha256.New()
	h.Write([]byte(str))
	return fmt.Sprintf("%X", h.Sum(nil))
}

// Sha512 将数据HASH成sha512值
func Sha512(str string) string {
	h := sha512.New()
	h.Write([]byte(str))
	return fmt.Sprintf("%X", h.Sum(nil))
}

// BuildMd5 根据输入的值，获取一个经过md5编码的key
// 该函数主要应用于对一组数据的唯一性标识
func BuildMd5(args ...interface{}) string {
	loads := make([]byte, 0)
	if len(args) == 0 {
		loads = append(loads, []byte("")...)
	} else {
		for _, v := range args {
			//任何类型的值都按base-16解析成字符串并转化为[]byte类型
			loads = append(loads, []byte(fmt.Sprintf("%x", v))...)
		}
	}
	//按16进制的形式返回字符串
	return fmt.Sprintf("%x", md5.Sum(loads))
}

// IsHexStr 判断一个字符串是否为十六进制形式
func IsHexStr(needle string) bool {
	reg := regexp.MustCompile(`^[0-9a-fA-F]+$`)
	return reg.MatchString(needle)
}

// IsHexBytes 判断一个字符串是否为十六进制形式
func IsHexBytes(needle []byte) bool {
	reg := regexp.MustCompile(`^[0-9a-fA-F]+$`)
	return reg.Match(needle)
}

// IsAlphaNum 判断一个字符串是否仅为字母、数字、以及下划线的组合
// 常用于判断此字符串是否可用于表示变量、函数等名称
func IsAlphaNum(needle string) bool {
	reg := regexp.MustCompile(`^\w+$`)
	return reg.MatchString(needle)
}

// EncryptPassword 将密码加密成密文返回
func EncryptPassword(password string) string {
	salt := "Wi$1h9gj8"
	return BuildMd5(BuildMd5(password) + salt)
}

// GetCodeByMessage 通过错误消息中的信息，获取对应的错误码，如信息 [50001]数据库访问失败， 可以返回 5001 状态码
func GetCodeByMessage(message string, defCode int64) int64 {
	if len(message) > 0 {
		reg := regexp.MustCompile(`^\[(\d+)]`)
		result := reg.FindAllStringSubmatch(message, 1)
		if len(result) > 0 {
			return cvt.GetSafeInt64(result[0][1], defCode)
		}
	}
	return defCode
}

// BuildRealtimeFilename 根据时间生成一个时实的文件名，生成规则为:
// filePrefix+goid+time.txt
// scopeSecs 有效值范围为[1, 10]，否则文件生成的粒度为 1min
func BuildRealtimeFilename(filePrefix string, scopeSecs int) string {
	scope := 0
	if scopeSecs > 0 && scopeSecs <= 10 {
		scope = scopeSecs
	}
	goid := GetGoRoutineId()
	nowtime := time.Now()
	if scope == 0 {
		return fmt.Sprintf("%s_%s_%s.txt", filePrefix, goid, nowtime.Format("200601021504"))
	} else {
		astringeSecs := nowtime.Second() / scope
		return fmt.Sprintf("%s_%s_%s%02d%02d.txt",
			filePrefix, goid, nowtime.Format("200601021504"), scope, astringeSecs)
	}
}

// IsRealtimeFileCanHandle 判断指定的实时生成文件，是否已成为可处理的历史文件
func IsRealtimeFileCanHandle(fname string) bool {
	segs := strings.Split(fname, "_")
	if len(segs) == 0 {
		return false
	}

	//文件时间
	filetime := segs[len(segs)-1]
	filetime = strings.ReplaceAll(filetime, ".txt", "")
	if len(filetime) < 12 {
		return false
	}

	//当前时间，精确到分钟
	nowtime := time.Now().Format("200601021504")

	if filetime[0:12] < nowtime {
		//说明当前文件的生成时间是在当前分钟值之前，满足转存条件
		return true
	} else if filetime[0:12] == nowtime {
		if len(filetime) == 16 {
			//说明当前格式是16位可精确到秒级转存的形式，后四位中的前2位指定秒级范围(即多少秒生成一个文件)，一般为 01-10，
			//后2位为收敛时间，一般为 {秒数}/{对应的指定秒级范围}
			scope := cvt.GetSafeInt64(filetime[12:14], -1)
			secs := cvt.GetSafeInt64(filetime[14:16], -1)
			if int(secs) < time.Now().Second()/int(scope) {
				return true
			}
		}
	}
	return false
}

// 定义FetchTextVariables函数所需要的变量提供的正则表达式
var _reFetchTextVariables = regexp.MustCompile(`\${([-\w.\[\]]+)}`)

// FetchTextVariables 提供文本中的表示变量的key列表
// 表示变量的形式如
// ${variable_name}
// ${key1.key2.key3}
// ${obj_list[i].key1}
// ${list[0].objList[1].key1}
// ${list[0].objList[i].key1}
// 即支持 dic.GetValueByMapPath() 所要求的数据路径表示的变量形式提取
// 比如：格式为 https://${centralservice}/${path}/index 的文本，通过 FetchTextVariables 可提取出
// []string{"centralservice", "path"}
func FetchTextVariables(text string) []string {
	result := _reFetchTextVariables.FindAllStringSubmatch(text, -1)
	if len(result) == 0 {
		return nil
	}
	retvals := make([]string, 0)
	for _, item := range result {
		if len(item) == 2 {
			retvals = append(retvals, item[1])
		}
	}
	return retvals
}

// Title 将字符串s的首字母转换为大写，如 abcHello -> AbcHello
// 此函数可用于取代 strings.Title() 函数
func Title(s string) string {
	return cases.Title(language.Make(s), cases.NoLower).String(s)
}

// AppendParamsToURL 追加额外的参数信息到原有的URL地址上，原有的URL即可以无query参数，也可以已经存在query参数
func AppendParamsToURL(rawUrl string, params map[string]string) (string, error) {
	if len(params) == 0 {
		return rawUrl, nil
	}
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

	urlPath, _, found := strings.Cut(rawUrl, "?")
	if found {
		//解析参数内容
		obj, err := url.Parse(rawUrl)
		if err != nil {
			return "", err
		}
		parsedValues := obj.Query()
		for key := range parsedValues {
			values.Set(key, parsedValues.Get(key))
		}
	}
	return urlPath + "?" + values.Encode(), nil
}

// IsJsonString 判断传入的数据是否为一个JSON字符串
func IsJsonString(s string) bool {
	data := strings.TrimSpace(s)
	if (strings.HasPrefix(data, "{") && strings.HasSuffix(data, "}")) ||
		(strings.HasPrefix(data, "[") && strings.HasSuffix(data, "]")) {
		return json.Valid([]byte(data))
	}
	return false
}

// PatchTrimSpace 对字符串列表批量进行每个元素的首尾去空格
func PatchTrimSpace(stringList []string) []string {
	if len(stringList) == 0 {
		return []string{}
	} else {
		res := MapListByFunc(stringList, func(v string) string {
			return strings.TrimSpace(v)
		})
		return res
	}
}

// RemoveUnixPrintColor 移除unix控制台下的color修饰输出
func RemoveUnixPrintColor(str string) string {
	re := regexp.MustCompile(`\033\[\d+m`)
	return re.ReplaceAllString(str, "")
}

// PrettyPrint 按Json格式打印
func PrettyPrint(v interface{}) {
	str, err := PrettyString(v)
	if err != nil {
		fmt.Println("PrettyPrint:", err, "\n", v)
	} else {
		fmt.Println(str)
	}
}

// PrettyString 按Json格式格式化输出。返回的err可以根据情况处理，一般不影响字符串输出
func PrettyString(v interface{}) (string, error) {
	var b json.RawMessage
	var err error

	switch v.(type) {
	case string:
		b = []byte(v.(string))
	case []byte:
		b = v.([]byte)
	default:
		if b, err = json.Marshal(v); err != nil {
			return string(b), err
		}
	}

	if !json.Valid(b) {
		return string(b), nil
	}

	var out bytes.Buffer
	if err = json.Indent(&out, b, "", "  "); err != nil {
		return string(b), err
	}
	return out.String(), nil
}

// MustPrettyString 格式化输出，但不会报错，出错时返回的是空
func MustPrettyString(v any) (res string) {
	res, _ = PrettyString(v)
	return
}
