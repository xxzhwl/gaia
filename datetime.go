package gaia

import (
	"database/sql/driver"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

func init() {
	//默认使用东八区为本地时间，即北京时间
	SetTZUTC8()
}

// DateTimeFormat 标准的日期时间模板常量
const DateTimeFormat = "2006-01-02 15:04:05"

// DateTimeMillsFormat 毫秒级
const DateTimeMillsFormat = "2006-01-02 15:04:05.000"

// DateFormat 标准的日期模板常量
const DateFormat = "2006-01-02"

// TimeHourFormat 标准的时分秒模板常量
const TimeHourFormat = "15:04:05"

// DateTimeFormatUTC 标准的UTC日期时间模板常量
const DateTimeFormatUTC = "2006-01-02T15:04:05.000Z"

// 将Y,m,d,H,i,s的标识，转成golang能识别的2006,01,02,15,04,05标识
func formatConvert(format string) string {
	var conf = map[string]string{
		"Y": "2006",
		"m": "01",
		"d": "02",
		"H": "15",
		"i": "04",
		"s": "05",
	}
	for k, v := range conf {
		format = strings.Replace(format, k, v, -1)
	}
	return format
}

// SetTZUTC8 设置时区为 UTC+8 (东八区, 北京时间)
func SetTZUTC8() {
	os.Setenv("TZ", "Asia/Shanghai")
}

// Date 获取一个当前时间，如Y-m-d H:i:s
func Date(format string) string {
	format = formatConvert(format)
	return time.Now().Format(format)
}

// DateFromUnix 根据一个UnixStamp，以及相应的时间格式(如Y-m-d H:i:s)，转成一个时间(string类型)
// 其中传入unixstamp可以支持的长度为秒、毫秒、微秒、和纳秒
func DateFromUnix(format string, unixstamp int64) string {
	format = formatConvert(format)
	return UnixToTime(unixstamp).Format(format)
}

// UnixToTime 将unix时间戳转换为时间类型
// 其中传入unixstamp可以支持的长度为秒、毫秒、微秒、和纳秒
func UnixToTime(unixstamp int64) time.Time {
	if unixstamp >= 1e18 {
		//说明是纳秒
		return time.Unix(0, unixstamp)
	} else if unixstamp >= 1e15 {
		//说明是微秒
		//return time.Unix(unixstamp/1e6, (unixstamp%1e6)*1e3).Format(format)
		return time.UnixMicro(unixstamp)
	} else if unixstamp >= 1e12 {
		//说明是毫秒
		//return time.Unix(unixstamp/1e3, (unixstamp%1e3)*1e6).Format(format)
		return time.UnixMilli(unixstamp)
	} else {
		//说明是秒
		return time.Unix(unixstamp, 0)
	}
}

// StrToTime 将string的时间格式类型，转成一个time.Time的可操作对象，如果传入的值为空，则返回零值时间对象
// 支持以下时间格式形式的转换
// 2006-01-02 15:04:05
// 2006-01-02 15:04
// 2006-01-02 15
// 2006-01-02
// 2006-01
// 2006-01-02T15:04:05.000Z
func StrToTime(value string) (time.Time, error) {
	if len(value) == 0 {
		//空值，返回零值时间
		return time.Time{}, nil
	}
	matchList := map[string]string{
		"2006-01-02 15:04:05":      `^\d{4}-\d{2}-\d{2}\s\d{2}:\d{2}:\d{2}$`,
		"2006-01-02 15:04":         `^\d{4}-\d{2}-\d{2}\s\d{2}:\d{2}$`,
		"2006-01-02 15":            `^\d{4}-\d{2}-\d{2}\s\d{2}$`,
		"2006-01-02":               `^\d{4}-\d{2}-\d{2}$`,
		"2006-01":                  `^\d{4}-\d{2}$`,
		"2006-01-02T15:04:05.000Z": `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{3}Z$`, //同RFC3339标准
	}
	for format, itm := range matchList {
		re := regexp.MustCompile(itm)
		if re.MatchString(value) {
			return _strToTime(value, format)
		}
	}
	return time.Time{}, fmt.Errorf("date '%s' can not convert to time.Time object", value)
}

// 将string的时间格式类型(如2023-11-12)，根据format格式(如Y-m-d)，转成一个time.Time的可操作对像
func _strToTime(value string, format string) (time.Time, error) {
	format = formatConvert(format)
	if IsUTCTime(value) {
		return time.Parse(format, value)
	}
	//使用本地时间解析
	return time.ParseInLocation(format, value, time.Local)
}

// IsUTCTime 判断一个String是不是标准UTC时间
func IsUTCTime(value string) bool {
	itm := `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}.\d{3}Z$`
	re := regexp.MustCompile(itm)
	return re.MatchString(value)
}

// StrUTCToTime 将string的UTC时间格式类型，转成一个time.Time的可操作对象
func StrUTCToTime(value string) (time.Time, error) {
	if IsUTCTime(value) {
		location, err := time.Parse("2006-01-02T15:04:05.000Z", value)
		if err != nil {
			return time.Time{}, err
		}
		return location, nil
	}
	return time.Time{}, fmt.Errorf("date '%s' can not convert to time.Time object", value)
}

// LoadLocalLocation 获取本地时区实例
func LoadLocalLocation() *time.Location {
	return time.Local
}

// TimeToStr 将Time类型转换成string的Y-m-d H:i:s类型
func TimeToStr(t time.Time, format string) string {
	format = formatConvert(format)
	return t.Format(format)
}

// StrToUnix 将string的时间类型，转成一个int类型的unix时间戳
func StrToUnix(date string) (int64, error) {
	tm, tmErr := StrToTime(date)
	if tmErr != nil {
		return 0, tmErr
	}
	return tm.Unix(), nil
}

// StrTimeFormat 将一个时间修饰成一个指定的格式，比如2023-11-15 13:38:19通过Y-m-d H:i格式化后，可得到2023-11-15 13:38
func StrTimeFormat(date string, format string) (string, error) {
	tm, tmErr := StrToTime(date)
	if tmErr != nil {
		return "", tmErr
	}
	return TimeToStr(tm, format), nil
}

// StrTimeTrunc5Min 对时间向下取5min的整值，如传入2023-11-15 13:38:19，将返回2023-11-15 13:35:00
func StrTimeTrunc5Min(date string) (string, error) {
	tm, err := StrToTime(date)
	if err != nil {
		return "", err
	}
	return TimeTruncate(tm, 5*time.Minute).Format("2006-01-02 15:04:05"), nil
}

// StrTimeTrunc10Min 对时间向下取10min的整值，如传入2023-11-15 13:38:19，将返回2023-11-15 13:30:00
func StrTimeTrunc10Min(date string) (string, error) {
	tm, err := StrToTime(date)
	if err != nil {
		return "", err
	}
	return TimeTruncate(tm, 10*time.Minute).Format("2006-01-02 15:04:05"), nil
}

// TimeTruncate 向下或向上取整时间
// d 表示向下取整多少时间长度，同时也支持负数形式，如果提供负数，则表示向上取整时间
// 比如，基准时间为 2023-08-20 12:21:25 时
// d = 60*time.Second, 返回 2023-08-20 12:21:00
// d = 300*time.Second, 返回 2023-08-20 12:20:00
// d = -60*time.Second，返回 2023-08-20 12:22:00
// d = -300*time.Second，返回 2023-08-20 12:25:00 等等
func TimeTruncate(rawTime time.Time, d time.Duration) time.Time {
	if d == 0 {
		return rawTime
	}
	if d > 0 {
		//表过向下取整时间
		return rawTime.Truncate(d)
	}

	//表示需要向上取整时间
	d = -d
	if rawTime.Equal(rawTime.Truncate(d)) {
		//说明时间正好是处于取整的临界点，此时不需要处理
		return rawTime
	}

	//加一个取整周期，再使用向下取整
	return rawTime.Add(d).Truncate(d)
}

// DiffNowToBeforeTime 计算从开始时间点到当前时间点的时长，返回值单位为s
// 传入参数单位为纳秒(ns)，即通过time.Now().UnixNano()生成
func DiffNowToBeforeTime(startNs int64) float64 {
	return Trunc(float64(time.Now().UnixNano()-startNs)/1e9, 4)
}

// DiffNowToFormatTime 计算某一个时间点(格式如 2023-03-22 09:01:02)到当前时间点的时长，返回值单位为s
// 传入的参数为 YYYY-mm-dd HH:MM:SS 格式的string类型数据，如果传入的时间不合法，则返回-1
func DiffNowToFormatTime(dateTimeStr string) float64 {
	us, err := StrToUnix(dateTimeStr)
	if err != nil {
		return -1
	}
	return DiffNowToBeforeTime(us * 1e9)
}

// CalExpireDate 计算过期时间
func CalExpireDate(startDate string, expireDays int) (string, error) {
	today, err := StrToTime(startDate)
	if err != nil {
		return "", err
	}
	hour := 24 * expireDays
	d, err := time.ParseDuration(fmt.Sprintf("%dh", hour))
	if err != nil {
		return "", err
	}
	expire := today.Add(d)
	expireDate := TimeToStr(expire, "Y-m-d")

	return expireDate, nil
}

// LocalTime 自定义时间类型，用于JSON序列化和数据库操作
type LocalTime time.Time

// MarshalJSON 实现JSON序列化接口，将LocalTime转换为"2006-01-02 15:04:05"格式字符串
func (t *LocalTime) MarshalJSON() ([]byte, error) {
	tTime := time.Time(*t)
	return []byte(fmt.Sprintf("\"%v\"", tTime.Format("2006-01-02 15:04:05"))), nil
}

// Value 实现driver.Valuer接口，用于数据库存储
func (t LocalTime) Value() (driver.Value, error) {
	var zeroTime time.Time
	tlt := time.Time(t)
	//判断给定时间是否和默认零时间的时间戳相同
	if tlt.UnixNano() == zeroTime.UnixNano() {
		return nil, nil
	}
	return tlt, nil
}

// Scan 实现sql.Scanner接口，用于从数据库读取时间数据
func (t *LocalTime) Scan(v interface{}) error {
	if value, ok := v.(time.Time); ok {
		*t = LocalTime(value)
		return nil
	}
	return fmt.Errorf("can not convert %v to timestamp", v)
}
