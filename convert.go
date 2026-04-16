// Package gaia 与数据转换相关的逻辑封装
// @author wanlizhan
// @created 2023-06-30
package gaia

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/xxzhwl/gaia/dics"
)

// StringToList 根据分隔符(;,|\n)，将字符串转为一个字符串list
func StringToList(str string) []string {
	list := make([]string, 0)
	if Empty(str) {
		return list
	}
	for _, delimiter := range []string{",", "|", "\n"} {
		str = strings.Replace(str, delimiter, ";", -1)
	}
	for _, v := range strings.Split(str, ";") {
		v = strings.TrimSpace(v)
		if !Empty(v) {
			list = append(list, v)
		}
	}
	return list
}

// TextToList 将文本转换成字符串列表，转换规则如下
// 1. 所有包括 \n \r , ; 的分隔符作为切割分隔点
// 2. 切割后，移除每一个结果元素前后的空白字符
// 3. 经过1，2步骤后，所有不为空的元素，作为列表的一个元素
func TextToList(text string) []string {
	text = strings.Replace(text, "\n", ";", -1)
	text = strings.Replace(text, "\r", ";", -1)
	text = strings.Replace(text, ",", ";", -1)
	return StringToListWithDelimit(text, ";")
}

// StringToKV 将字符串(如：kk:vv;kkk:vvv)转换为K-V字典数组
func StringToKV(str string) (map[string]string, error) {
	retval := make(map[string]string)
	str = strings.TrimSpace(str)
	//替换 &nbsp; 为空格式，避免分号干扰
	str = strings.Replace(str, "&nbsp;", " ", -1)
	items := strings.Split(str, ";")
	if len(items) > 0 {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if strings.Index(item, ":") > 0 {
				arr := strings.SplitN(item, ":", 2)
				if len(arr) == 2 {
					retval[arr[0]] = arr[1]
				} else {
					return nil, fmt.Errorf("string '%s' cannot convert K-V dictionary", str)
				}
			} else {
				return nil, fmt.Errorf("string '%s' cannot convert K-V dictionary", str)
			}
		}
	}
	return retval, nil
}

// KvToString 将key-value的map数据类型，转换为类似 kk:vv;kkk:vvv的字符串格式
func KvToString(data map[string]string) string {
	retval := ""
	if len(data) > 0 {
		arr := make([]string, 0)
		for k, v := range data {
			arr = append(arr, k+":"+v)
		}
		retval = strings.Join(arr, ";")
	}
	return retval
}

// StringToListWithDelimit 以特定的分隔符，将字符串split成列表
func StringToListWithDelimit(str string, sep string) []string {
	list := make([]string, 0)
	for _, v := range strings.Split(str, sep) {
		v = strings.TrimSpace(v)
		if len(v) > 0 {
			list = append(list, v)
		}
	}
	return list
}

// UnitBytesToReadable 将单位为byte的数据转换成可读的大小，如B, K, M, G
func UnitBytesToReadable[T uint | uint8 | uint16 | uint32 | uint64 | int | int8 | int16 | int32 | int64](unitByte T) string {
	kb := float64(unitByte) / 1024
	if kb < 1 {
		return fmt.Sprintf("%dB", unitByte)
	}
	mb := kb / 1024
	if mb < 1 {
		return Round(kb, 2) + "K"
	}
	gb := mb / 1024
	if gb < 1 {
		return Round(mb, 2) + "M"
	}
	return Round(gb, 2) + "G"
}

// UnitNanosecondToReadable 将单位为纳秒(nanosecond)的时间数据转换为可读的大小，比如ns, µs, ms, s, m, h, d
func UnitNanosecondToReadable(nanoSeconds int64) string {
	microSeconds := float64(nanoSeconds) / 1000
	if microSeconds < 1 {
		//不足1微秒，使用纳秒单位
		return fmt.Sprintf("%dns", nanoSeconds)
	}
	millSeconds := microSeconds / 1000
	if millSeconds < 1 {
		//不足1毫秒，使用微秒单位
		return fmt.Sprintf("%.2fµs", microSeconds)
	}

	seconds := float64(millSeconds) / 1000
	if seconds < 1 {
		//不足1秒，使用毫秒单位
		return fmt.Sprintf("%dms", int(millSeconds))
	}

	minutes := seconds / 60
	if minutes < 1 {
		//不足1分钟
		return Round(seconds, 2) + "s"
	}

	hours := minutes / 60
	if hours < 1 {
		//不足1小时
		return Round(minutes, 2) + "m"
	}

	days := hours / 24
	if days < 1 {
		//不足1天
		return Round(hours, 2) + "h"
	}

	//按天计算
	return Round(days, 2) + "d"
}

// UnitMicrosecondToReadable 将单位为微秒(microsecond)的时间数据转换为可读的大小，比如 s, m, h, d以后缀的单位
func UnitMicrosecondToReadable(microSeconds int64) string {
	return UnitNanosecondToReadable(microSeconds * 1e3)
}

// UnitMillisecondToReadable 将单位为毫秒(millisecond)的时间数据转换为可读的大小，比如 s, m, h, d以后缀的单位
func UnitMillisecondToReadable(milliSecond int64) string {
	return UnitNanosecondToReadable(milliSecond * 1e6)
}

// UnitSecondToReadable 将单位为秒(second)的时间数据转换为可读的大小，比如 s, m, h, d以后缀的单位
func UnitSecondToReadable(second int64) string {
	return UnitNanosecondToReadable(second * 1e9)
}

// SecsReadable 将时间秒，换转为可读的形式
// 如果转换为 1天15小时30分种15秒
// 15分钟20秒 等等
func SecsReadable(secs int64) string {
	readableValue := _secsReadable(secs)
	if len(readableValue) == 0 {
		return ""
	}
	return _removeRedundancy(readableValue)
}

func _secsReadable(secs int64) string {
	if secs < 0 {
		return ""
	}
	if secs < 60 {
		//1分钟以内
		return fmt.Sprintf("%ds", secs)
	}
	if secs < 3600 {
		//1小时以内
		return fmt.Sprintf("%dm%ds", secs/60, secs%60)
	}
	if secs < 3600*24 {
		//1天以内
		hours := secs / 3600
		minutes := (secs - hours*3600) / 60
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, secs-(hours*3600+minutes*60))
	}

	//超过1天的情况
	days := secs / (3600 * 24)
	hours := (secs - days*3600*24) / 3600
	minutes := (secs - days*3600*24 - hours*3600) / 60
	s := secs - days*3600*24 - hours*3600 - minutes*60
	return fmt.Sprintf("%dd%dh%dm%ds", days, hours, minutes, s)
}
func _removeRedundancy(readableTime string) string {
	replaceMap := map[string]string{
		"d": "天",
		"h": "小时",
		"m": "分钟",
		"s": "秒",
	}
	readableTime = strings.ReplaceAll(readableTime, "d0h0m0s", "d")
	readableTime = strings.ReplaceAll(readableTime, "h0m0s", "h")
	readableTime = strings.ReplaceAll(readableTime, "m0s", "m")
	for k, v := range replaceMap {
		readableTime = strings.ReplaceAll(readableTime, k, v)
	}

	return readableTime
}

// ParseDataToStruct 将srcData的数据转换到dstStruct结构体中，该方法提供数据明确化的功能
// dstStruct 要求为指针的结构体类型
func ParseDataToStruct(srcData any, dstStruct any) error {
	//如果srcData本来就是[]byte类型，则不需要再打成[]byte的JSON形式，认为它本来就是[]byte类型的json数据
	var jdata []byte
	switch srcData.(type) {
	case string:
		jdata = []byte(srcData.(string))
		break
	case []byte:
		jdata = srcData.([]byte)
		break
	default:
		// 说明是对象类型的，转为[]byte
		var jdataErr error
		jdata, jdataErr = json.Marshal(srcData)
		if jdataErr != nil {
			return jdataErr
		}
	}
	//这里的dstStruct本身为针指类型
	if err := json.Unmarshal(jdata, dstStruct); err != nil {
		return err
	}
	return nil
}

// ParseJsonToStruct 将JSON字符串转换到一个数据结构中
// 注意 dstStruct使用地址转入，与json.Unmarshal()类似
// 如果传入的不是一个JSON格式数据，则返回 error
func ParseJsonToStruct(strJson string, dstStruct any) error {
	//先进行一次预判断，大概确定是否为JSON格式的数据
	strJson = strings.TrimSpace(strJson)
	if IsJsonString(strJson) {
		//判断是一个JSON，尝试转换
		if err := json.Unmarshal([]byte(strJson), dstStruct); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("ParseJsonToStruct, data is invalid JSON, details: %s", strJson)
	}

	return nil
}

// ParseStructToMap 将结构体数据类型(也支持传入结构体类型的指针)，转换为map[string]any形式
// 此逻辑仅支持 一维结构体 中可导出的属性(属性首字母大写)转换为map[string]any，非可导出属性将被忽略
func ParseStructToMap(dataStruct any) (map[string]any, error) {
	if dataStruct == nil {
		return nil, nil
	}
	fv := reflect.ValueOf(dataStruct)
	if fv.Kind() == reflect.Ptr {
		//说明指针类型
		fv = fv.Elem()
	}
	if fv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("ParseStructToMap: input parameter not struct, is %s", fv.Kind().String())
	}
	numFields := fv.NumField()
	ft := fv.Type()
	retval := make(map[string]any, numFields)
	for i := 0; i < numFields; i++ {
		if fv.Field(i).CanInterface() {
			retval[ft.Field(i).Name] = fv.Field(i).Interface()
		}
	}
	return retval, nil
}

// ParseMapStringToStruct 将一个map[string]string类型的数据解析到一个明确的结构体中
// 该逻辑主要用于从DB中获取的数据转换为结构体数据
// 结构体可以支持别名，可以支持嵌套解析，如
//
//	type Demo struct {
//		Id       uint   "id"
//		Username string "username"
//		Ticket   string "ticket"
//	 	Abc struct {
//	     	Email string
//	 	}
//	 	InnerStatuct
//	}
//
// 解析时别名优先
func ParseMapStringToStruct(srcData map[string]string, dstStruct any) error {
	if len(srcData) == 0 {
		return nil
	}
	fv := reflect.ValueOf(dstStruct).Elem()
	ft := reflect.TypeOf(dstStruct).Elem()
	numField := fv.NumField()
	for i := 0; i < numField; i++ {
		//结构体字段别名
		alias := string(ft.Field(i).Tag)
		//结构体字段名
		name := ft.Field(i).Name

		switch ft.Field(i).Type.Kind() {
		case reflect.String:
			//string类型
			if val, ok := srcData[alias]; ok {
				fv.Field(i).SetString(val)
			} else {
				fv.Field(i).SetString(dics.S(srcData, name))
			}

		case reflect.Int64, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
			//int类型
			if _, ok := srcData[alias]; ok {
				fv.Field(i).SetInt(dics.I(srcData, alias))
			} else {
				fv.Field(i).SetInt(dics.I(srcData, name))
			}

		case reflect.Uint, reflect.Uint64, reflect.Uint8, reflect.Uint16, reflect.Uint32:
			//uint类型
			if _, ok := srcData[alias]; ok {
				fv.Field(i).SetUint(uint64(dics.I(srcData, alias)))
			} else {
				fv.Field(i).SetUint(uint64(dics.I(srcData, name)))
			}

		case reflect.Float32, reflect.Float64:
			//float类型
			if _, ok := srcData[alias]; ok {
				fv.Field(i).SetFloat(dics.F(srcData, alias))
			} else {
				fv.Field(i).SetFloat(dics.F(srcData, name))
			}
		case reflect.Bool:
			//bool类型
			if _, ok := srcData[alias]; ok {
				fv.Field(i).SetBool(dics.GetSafeBool(srcData, alias, false))
			} else {
				fv.Field(i).SetBool(dics.GetSafeBool(srcData, name, false))
			}
		case reflect.Struct, reflect.Slice, reflect.Map:
			//处理内嵌/嵌套结构体
			key := name
			if len(alias) > 0 {
				key = alias
			}
			jsonStr := srcData[key]
			if len(jsonStr) > 0 {
				if err := ParseDataToStruct([]byte(jsonStr), fv.Field(i).Addr().Interface()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Round 获取一个浮点数，保留小数点后n位精度后的字符串值
func Round(val float64, precision int) string {
	return fmt.Sprintf("%."+strconv.Itoa(precision)+"f", val)
}

// FloatTrunc 获取一个浮点数，保留小数点后N位
func FloatTrunc(val float64, precision int) float64 {
	return Trunc(val, precision)
}

// Trunc 同FloatTrunc
func Trunc(val float64, precision int) float64 {
	return math.Trunc(val*math.Pow10(precision)) / math.Pow10(precision)
}

// IntToBytes 将一个4字节(int32)的数字转成4个字节的byte类型
// @params int32 n 待转换的数字
// @return []byte
// @return error
func IntToBytes(n int32) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	if err := binary.Write(buf, binary.BigEndian, n); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// BytesToInt 将4个字节的[]byte转成int32类型的数据
// @params []byte b 待转换的字节序列
// @return int32
// @return error
func BytesToInt(b []byte) (int32, error) {
	buf := bytes.NewReader(b)
	var n int32
	if err := binary.Read(buf, binary.BigEndian, &n); err != nil {
		return 0, err
	}
	return n, nil
}

// BytesToString 将bytes转换为string数据类型，同 string(bytes)
func BytesToString(byteData []byte) string {
	return string(byteData)
}
