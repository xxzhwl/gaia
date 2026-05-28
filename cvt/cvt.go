// Package cvt 数据获取逻辑，主要是针对interface{}的数据类型，转换成确定的数据类型
// @author wanlizhan
// @create 2023-11-28
package cvt

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/xxzhwl/gaia/defs"
)

// GetInt64 尝试获取一个int64类型的数据
// 其中string类型也将尝试转换成int类型
// 如果获取失败，则返回error，错误信息为指定的errmsg
func GetInt64(v any, errmsg string, defaultValue int64) (int64, error) {
	//对于float类型，如果被转成了科学计数法的形式，将会失败
	switch val := v.(type) {
	case float64:
		return int64(val), nil
	case float32:
		return int64(val), nil
	default:
		strv := fmt.Sprintf("%v", v)

		//兼容
		//str="12347778"
		//str="1.6612766913592737e+18"
		//str="1234.457" 形式的转换

		if i, err := strconv.ParseInt(strv, 10, 64); err == nil {
			return i, nil
		}
		if i, err := strconv.ParseFloat(strv, 64); err == nil {
			return int64(i), nil
		}

		//到此，说明此数据无法进行转换，即转换失败
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}

}

// GetSafeInt64 尝试获取一个int64的类型数据，如果获取失败，返回默认值
func GetSafeInt64(v any, defaultValue int64) int64 {
	val, _ := GetInt64(v, "", defaultValue)
	return val
}

// GetFloat64 尝试获取一个float64类型的数据
// 其实string类型，int类型，都将尝试转成float64类型
// 如果获取失败，则返回error，其提示内容为errmsg指定的信息
func GetFloat64(v any, errmsg string, defaultValue float64) (float64, error) {
	strv := fmt.Sprintf("%v", v)
	if fv, err := strconv.ParseFloat(strv, 64); err != nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	} else {
		return fv, nil
	}
}

// GetSafeFloat64 尝试获取一个float64类型的数据，如果获取失败，返回默认值
func GetSafeFloat64(v any, defaultValue float64) float64 {
	val, _ := GetFloat64(v, "", defaultValue)
	return val
}

// GetString 尝试获取一个字符串数据
// 其中int类型，float类型，也将尝试转换成string类型返回
// 如果获取失败，返回一个error，内容为errmsg信息
func GetString(v any, errmsg string, defaultValue string) (string, error) {
	if v == nil {
		v = ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.String, reflect.Int64, reflect.Uint64, reflect.Int32, reflect.Uint32,
		reflect.Int, reflect.Uint, reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8:
		//string或int
		val := fmt.Sprintf("%v", v)
		if len(val) > 0 {
			return val, nil
		}
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	case reflect.Float64, reflect.Float32:
		//float类型
		if f64, ok := v.(float64); ok {
			return strconv.FormatFloat(f64, 'f', -1, 64), nil
		}
		if f32, ok := v.(float32); ok {
			return strconv.FormatFloat(float64(f32), 'f', -1, 64), nil
		}
		return defaultValue, errors.New("float can not convert")
	case reflect.Bool:
		//bool类型，使用0或1标识结果
		if b, ok := v.(bool); ok {
			if b {
				return "1", nil
			} else {
				return "0", nil
			}
		} else {
			return defaultValue, errors.New("bool can not convert")
		}
	default:
		//非string, int或float类型，不支持向string转换的类型
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
}

// GetSafeString 尝试获取一个string类型的数据，如果获取失败，返回默认值
func GetSafeString(v any, defaultValue string) string {
	val, _ := GetString(v, "", defaultValue)
	return val
}

// GetSafeStringList 尝试获取一个string类型的列表数据，如果获取失败，返回默认值
func GetSafeStringList(v any, defaultValue []string) []string {
	val, _ := GetStringList(v, "", defaultValue)
	return val
}

// GetStringList 尝试获取一个字符串列表数据
// 其中int类型，float类型，也将尝试转换成string类型返回
// 如果获取失败，返回一个error，内容为errmsg信息
func GetStringList(v any, errmsg string, defaultValue []string) ([]string, error) {
	if v == nil {
		v = []string{}
	}

	//判断v是否是slice，如果不是slice 直接返回
	if !(reflect.TypeOf(v).Kind() == reflect.Slice) {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
	// 处理每个元素
	sliceValue := reflect.ValueOf(v)
	var err error
	result := make([]string, sliceValue.Len())
	for i := 0; i < sliceValue.Len(); i++ {
		result[i], err = GetString(sliceValue.Index(i).Interface(), errmsg, "")
		if err != nil {
			return defaultValue, err
		}
	}
	return result, nil
}

// GetMustString 将数据强制转换为字符串类型
// 如果是map, array, slice复合结构，则转换为JSON返回
func GetMustString(v any) string {
	if v == nil {
		return ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		jsn, _ := json.Marshal(v)
		return string(jsn)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// NumToBool 将数字类型转换为布尔类型
func NumToBool[T defs.NumberType](v T) bool {
	return v != 0
}

// BoolToInt 将bool类型转换为int值
func BoolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// GetBool 尝试获取一个bool类型
// 其中int类型，float类型，0则为true，其他false
// 字符串类型，长度大于0为true，其他false
// 如果获取失败，返回一个error，内容为errmsg信息
func GetBool(v any, errmsg string, defaultValue bool) (bool, error) {
	if v == nil {
		v = ""
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Bool:
		return v.(bool), nil
	case reflect.String:
		if len(v.(string)) > 0 {
			return true, nil
		}
		return false, nil
	case reflect.Int64, reflect.Uint64, reflect.Int32, reflect.Uint32,
		reflect.Int, reflect.Uint, reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8:
		val := fmt.Sprintf("%v", v)
		if val != "0" {
			return true, nil
		}
		return false, nil
	case reflect.Float64, reflect.Float32:
		//float类型
		if f64, ok := v.(float64); ok && f64 != 0 {
			return true, nil
		}
		if f32, ok := v.(float32); ok && f32 != 0 {
			return true, nil
		}
		return false, nil
	default:
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		} else {
			return defaultValue, nil
		}
	}
}

// GetSafeBool 尝试获取一个安全的bool类型
func GetSafeBool(v any) bool {
	res, _ := GetBool(v, "", false)
	return res
}

// timeLayouts 默认尝试解析的时间字符串布局列表
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.000",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
	time.RFC1123,
	time.RFC1123Z,
}

// GetTime 尝试获取一个 time.Time 类型的数据
// 支持的输入：
//  1. time.Time 直接返回
//  2. 数字（int/int64/float64）：作为 unix 时间戳，>=1e12 视为毫秒，否则视为秒
//  3. 字符串：依次尝试常见布局解析；纯数字字符串按数字规则处理
//
// 解析失败时返回 errmsg 描述的错误，并返回 defaultValue
func GetTime(v any, errmsg string, defaultValue time.Time) (time.Time, error) {
	if v == nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, nil
	}
	switch val := v.(type) {
	case time.Time:
		return val, nil
	case *time.Time:
		if val != nil {
			return *val, nil
		}
	}

	// 数字时间戳
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := reflect.ValueOf(v).Int()
		return unixToTime(n), nil
	case reflect.Float32, reflect.Float64:
		n := int64(reflect.ValueOf(v).Float())
		return unixToTime(n), nil
	}

	// 字符串解析
	strv, err := GetString(v, "", "")
	if err != nil || len(strv) == 0 {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, nil
	}
	strv = strings.TrimSpace(strv)

	// 纯数字字符串当作时间戳
	if isAllDigit(strv) {
		if n, err := strconv.ParseInt(strv, 10, 64); err == nil {
			return unixToTime(n), nil
		}
	}

	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, strv, time.Local); err == nil {
			return t, nil
		}
	}

	if len(errmsg) > 0 {
		return defaultValue, errors.New(errmsg)
	}
	return defaultValue, fmt.Errorf("can not parse time from %q", strv)
}

// GetSafeTime 尝试获取一个 time.Time 类型，失败时返回默认值
func GetSafeTime(v any, defaultValue time.Time) time.Time {
	t, _ := GetTime(v, "", defaultValue)
	return t
}

// GetTimeWithLayout 使用指定布局解析时间字符串
func GetTimeWithLayout(v any, layout string, defaultValue time.Time) (time.Time, error) {
	if v == nil {
		return defaultValue, errors.New("value is nil")
	}
	if t, ok := v.(time.Time); ok {
		return t, nil
	}
	strv, err := GetString(v, "", "")
	if err != nil {
		return defaultValue, err
	}
	return time.ParseInLocation(layout, strings.TrimSpace(strv), time.Local)
}

// GetDuration 尝试获取一个 time.Duration 类型的数据
// 支持的输入：
//  1. time.Duration 直接返回
//  2. 数字：按"毫秒"解释（如 200 -> 200ms）
//  3. 字符串：优先 time.ParseDuration（"200ms"、"1h30m"），失败则按毫秒数字解析
func GetDuration(v any, errmsg string, defaultValue time.Duration) (time.Duration, error) {
	if v == nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, nil
	}
	switch val := v.(type) {
	case time.Duration:
		return val, nil
	}

	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		ms := reflect.ValueOf(v).Int()
		return time.Duration(ms) * time.Millisecond, nil
	case reflect.Float32, reflect.Float64:
		ms := int64(reflect.ValueOf(v).Float())
		return time.Duration(ms) * time.Millisecond, nil
	}

	strv, err := GetString(v, "", "")
	if err != nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, err
	}
	strv = strings.TrimSpace(strv)
	if d, err := time.ParseDuration(strv); err == nil {
		return d, nil
	}
	// 退化为毫秒数字
	if n, err := strconv.ParseInt(strv, 10, 64); err == nil {
		return time.Duration(n) * time.Millisecond, nil
	}
	if f, err := strconv.ParseFloat(strv, 64); err == nil {
		return time.Duration(f * float64(time.Millisecond)), nil
	}
	if len(errmsg) > 0 {
		return defaultValue, errors.New(errmsg)
	}
	return defaultValue, fmt.Errorf("can not parse duration from %q", strv)
}

// GetSafeDuration 尝试获取一个 time.Duration 类型，失败时返回默认值
func GetSafeDuration(v any, defaultValue time.Duration) time.Duration {
	d, _ := GetDuration(v, "", defaultValue)
	return d
}

// byteSizeUnits 字节单位映射，统一按二进制（1KB = 1024B）解释
var byteSizeUnits = map[string]int64{
	"B":   1,
	"K":   1 << 10,
	"KB":  1 << 10,
	"KIB": 1 << 10,
	"M":   1 << 20,
	"MB":  1 << 20,
	"MIB": 1 << 20,
	"G":   1 << 30,
	"GB":  1 << 30,
	"GIB": 1 << 30,
	"T":   1 << 40,
	"TB":  1 << 40,
	"TIB": 1 << 40,
	"P":   1 << 50,
	"PB":  1 << 50,
	"PIB": 1 << 50,
}

// GetByteSize 解析字节大小字符串，返回字节数
// 支持："1024"、"10MB"、"1.5GiB" 等；纯数字按字节解释
func GetByteSize(v any, errmsg string, defaultValue int64) (int64, error) {
	if v == nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, nil
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return reflect.ValueOf(v).Int(), nil
	case reflect.Float32, reflect.Float64:
		return int64(reflect.ValueOf(v).Float()), nil
	}
	strv, err := GetString(v, "", "")
	if err != nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, err
	}
	strv = strings.TrimSpace(strv)
	if len(strv) == 0 {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, errors.New("empty byte size string")
	}

	// 拆分数字和单位
	idx := 0
	for i, r := range strv {
		if unicode.IsDigit(r) || r == '.' || r == '-' || r == '+' {
			idx = i + 1
			continue
		}
		break
	}
	numPart := strings.TrimSpace(strv[:idx])
	unitPart := strings.ToUpper(strings.TrimSpace(strv[idx:]))

	if len(numPart) == 0 {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, fmt.Errorf("invalid byte size %q", strv)
	}

	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, err
	}
	if len(unitPart) == 0 {
		return int64(num), nil
	}
	mul, ok := byteSizeUnits[unitPart]
	if !ok {
		if len(errmsg) > 0 {
			return defaultValue, errors.New(errmsg)
		}
		return defaultValue, fmt.Errorf("unknown byte size unit %q", unitPart)
	}
	return int64(num * float64(mul)), nil
}

// GetSafeByteSize 解析字节大小，失败返回默认值
func GetSafeByteSize(v any, defaultValue int64) int64 {
	n, _ := GetByteSize(v, "", defaultValue)
	return n
}

// unixToTime 根据数字大小自动判断秒/毫秒，返回本地时区的 time.Time
func unixToTime(n int64) time.Time {
	// >=1e12 视为毫秒时间戳（约 2001-09-09 之后的毫秒级）
	if n >= 1_000_000_000_000 {
		return time.Unix(n/1000, (n%1000)*int64(time.Millisecond))
	}
	return time.Unix(n, 0)
}

// isAllDigit 判断字符串是否全为数字
func isAllDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}