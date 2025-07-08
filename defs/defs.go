// Package defs 基础结构体以及公共常量定义逻辑
// @author wanlizhan
// @created 2023-07-07
package defs

// 定义一系列的跟API数据传输相关的常量信息
const (
	//PacketCipher 数据传输过程中加密标记
	PacketCipher string = "cipher"

	//PacketDeflate 数据传输过程中数据压缩标记
	PacketDeflate string = "deflate"

	//HeaderAuthorization HTTP头部key，用于设置签名信息
	HeaderAuthorization string = "Authorization"

	//HeaderTransFormat HTTP头部key，用于设置处理处理指令
	//格式如
	// cipher:1234,defalate 表示数据经过了加密和压缩处理，加密密钥通过 1234 进行映射
	// cipher:probe/1234,deflate 表示数据经过了加密和压缩处理，加密密钥通过 probe 前缀标识和 1234 进行映射
	// deflate 表示数据仅经过了压缩处理
	HeaderTransFormat string = "TransFormat"
)

// KVStct Key-Value结构定义
// value即为key的具体值
type KVStct struct {
	Key   string
	Value string
}

// KNStct Key-Name通用结构定义
// 用于表示某一个key，以及该key所对应的名称(一般为中文说明)
type KNStct struct {
	Key  string
	Name string
}

// ValueChange 值变更通用结构体定义
type ValueChange struct {
	OriginValue string //原值
	NewValue    string //新值
}

// IntegerType 泛型Integer类型定义
type IntegerType interface {
	int8 | int16 | int32 | int64 | int | uint8 | uint16 | uint32 | uint64 | uint
}

// FloatType 泛型Float类型定义
type FloatType interface {
	float32 | float64
}

// NumberType 泛型数字类型
type NumberType interface {
	IntegerType | FloatType
}
