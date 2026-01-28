// Package gaia 包注释
// @author wanlizhan
// @created 2024/5/29
package gaia

import (
	"errors"
	"fmt"
	"github.com/xxzhwl/gaia/cvt"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type DataChecker struct {
}

// NewDataChecker 创建DataChecker实例
func NewDataChecker() *DataChecker {
	return &DataChecker{}
}

// Require 检查数据是否为空
func (d *DataChecker) Require(v any, label string) error {
	if !Empty(v) {
		return nil
	}
	return fmt.Errorf("%s数据不允许为空", label)
}

// Date 验证日期年月日类型
func (d *DataChecker) Date(value any, label string) error {
	return d.DateWithSplit(value, label, "-")
}

// DateWithSplit 验证日期年月日类型，可以传入分隔符
func (d *DataChecker) DateWithSplit(value any, label, split string) error {
	reg := strings.Join([]string{`^\d{4}`, `\d{2}`, `\d{2}$`}, split)
	if val, ok := value.(string); ok {
		re := regexp.MustCompile(reg)
		if re.MatchString(val) {
			return nil
		}
	}
	return fmt.Errorf("%s要求日期类型(yyyy%smm%sdd)数据", label, split, split)
}

// Month 验证年月类型
func (d *DataChecker) Month(v any, label string) error {
	if val, ok := v.(string); ok {
		re := regexp.MustCompile(`^\d{4}-\d{2}$`)
		if re.MatchString(val) {
			return nil
		}
	}
	return fmt.Errorf("%s要求日期类型(yyyy-mm)数据", label)
}

// Datetime 验证时间类型
func (d *DataChecker) Datetime(v any, label string) error {
	if val, ok := v.(string); ok {
		if regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{1,2}:\d{1,2}:\d{1,2}$`).MatchString(val) {
			return nil
		}
	}
	return fmt.Errorf("%s要求日期时间类型(yyyy-mm-dd HH:MM:SS)数据", label)
}

// Time 时间校验
func (d *DataChecker) Time(v any, label string) error {
	if d.Date(v, label) == nil || d.Datetime(v, label) == nil {
		return nil
	}
	return fmt.Errorf("%s要求时间类型(yyyy-mm-dd 或 yyyy-mm-dd HH:MM:SS)的数据", label)
}

// TimeHour 时分秒校验 会对时间格式和数值合法性进行校验
func (d *DataChecker) TimeHour(v any, label string) error {
	if val, ok := v.(string); ok {
		if regexp.MustCompile(`^\d{1,2}:\d{1,2}:\d{1,2}$`).MatchString(val) {
			if _, err := time.Parse(TimeHourFormat, val); err != nil {
				return fmt.Errorf("%s要求合法的时间类型数值:%s", label, err.Error())
			}
			return nil
		}
	}
	return fmt.Errorf("%s要求时间类型(HH:MM:SS)的数据", label)
}

// TimeHourSplit 时分秒校验 会对时间格式和数值合法性进行校验
func (d *DataChecker) TimeHourSplit(v any, label, split string) error {
	reg := strings.Join([]string{`^\d{1,2}`, `\d{1,2}`, `\d{1,2}$`}, split)
	if val, ok := v.(string); ok {
		if regexp.MustCompile(reg).MatchString(val) {
			if _, err := time.Parse(TimeHourFormat, val); err != nil {
				return fmt.Errorf("%s要求合法的时间类型数值:%s", label, err.Error())
			}
			return nil
		}
	}
	return fmt.Errorf("%s要求时间类型(HH%sMM%sSS)的数据", label, split, split)
}

// Mail 验证邮箱地址格式
func (d *DataChecker) Mail(v any, label string) error {
	if val, ok := v.(string); ok {
		pattern := `^[a-zA-Z0-9_.+-]+@[a-zA-Z0-9-]+\.[a-zA-Z0-9-.]+$`
		regex := regexp.MustCompile(pattern)
		if regex.MatchString(val) {
			return nil
		}
		return fmt.Errorf("%s要求为邮箱类型", label)
	}
	return fmt.Errorf("%s要求输入为字符串类型", label)
}

// CheckStructDataValid 根据结构体本身定义的tag信息，检查结构体实际数据的合法性
// 该方法可用于针对接口数据的快速逻辑校验
// 校验时即可以传入值类型，也可以传入引用类型，可定义的结构体形式如下
//
//	type ReqData struct {
//		Main
//		Data struct {
//			TaskId     json.Number    `memo:"任务ID" require:"1" validator:"int" range:"4;5;6"`
//			Operator   string         `memo:"操作人"`
//			Columns    map[string]Col `memo:"需要获取的字段列信息" require:"1"`
//			List       struct {
//				MyList  []any `memo:"我的列表" require:"1"`
//				MyList1 [4]int64      `memo:"我的列表1" require:"1"`
//			}
//			Person []struct {
//				Id   int64
//				Name string `memo:"姓名" require:"1"`
//				List struct {
//					A string `memo:"A" require:"1"`
//				}
//			} `memo:"人员信息" require:"1"`
//		}
//		Reason   string      `memo:"操作原因" require:"1"`
//	}
//
// 支持的数据校验类型如下
// 1. string非空校验，指定require:"1"
// 2. string / int 类型的枚举校验，指定range:"1;2;3"
// 3. int / float 类型的区间校验，指定gt:"10" lt:"100" ,其它可支持的指令如gte/ge/lte/le
func (d *DataChecker) CheckStructDataValid(stctBody any) error {
	return _checkStructDataValid(stctBody, "")
}

func _checkStructDataValid(stctBody any, strctName string) error {
	obj := newStructValidChecker(stctBody, strctName)
	return obj.checkData()
}

type structValidChecker struct {
	stctBody any
	stctName string

	ft reflect.Type
	fv reflect.Value
}

func newStructValidChecker(stctBody any, stctName string) structValidChecker {
	return structValidChecker{
		stctBody: stctBody,
		stctName: stctName,
	}
}

func (o *structValidChecker) checkData() error {
	if err := o.init(); err != nil {
		return err
	}

	//遍历结构体中的每一项数据，如果存在嵌套数据结构，则进行递归处理
	for i := 0; i < o.ft.NumField(); i++ {
		//依次检查结构体中的每一个属性以及属性值情况
		switch o.ft.Field(i).Type.Kind() {
		case reflect.Struct:
			//结构类型，考虑递归处理，需要检查是继承组合结构，还是普通结构
			if err := o.checkStruct(i); err != nil {
				return err
			}

		case reflect.Map:
			//map类型的数据
			if err := o.checkMap(i); err != nil {
				return err
			}

		case reflect.Slice, reflect.Array:
			//slice, array类型
			if err := o.checkList(i); err != nil {
				return err
			}

		case reflect.String:
			//string类型
			if err := o.checkString(i); err != nil {
				return err
			}

		case reflect.Int64, reflect.Uint64, reflect.Int32, reflect.Uint32, reflect.Int, reflect.Uint,
			reflect.Int16, reflect.Uint16, reflect.Int8, reflect.Uint8, reflect.Float64, reflect.Float32:
			//数值类型的数据校验 range / gt / gte / ge / lt / lte / le
			//统一按float64进行处理

			if err := o.checkDecimal(i); err != nil {
				return err
			}
		}

	}
	return nil
}

// CheckDataType 检查数据类型的合法性
func CheckDataType(dataType string, v any, label string) error {
	if len(dataType) == 0 {
		//测试没有的指定数据类型，不需要进行特殊的数据验证
		return nil
	}

	fcall := reflect.ValueOf(NewDataChecker()).MethodByName(Title(dataType))
	if !fcall.IsValid() {
		return fmt.Errorf("指定的数据类型%s，不在合法范围内！", dataType)
	}

	if method, ok := fcall.Interface().(func(any, string) error); ok {
		return method(v, label)
	} else {
		return fmt.Errorf("指定的数据类型校验逻辑%s，定义不合法！", dataType)
	}
}

// 初始化反射对象 ft, fv
func (o *structValidChecker) init() error {
	switch reflect.TypeOf(o.stctBody).Kind() {
	case reflect.Ptr:
		//传入的结构体为结构体指针
		o.ft = reflect.TypeOf(o.stctBody).Elem()
		if o.ft.Kind() != reflect.Struct {
			return errors.New("CheckStructValid校验逻辑只允许校验有效的Struct类型！")
		}

		o.fv = reflect.ValueOf(o.stctBody).Elem()
	case reflect.Struct:
		//传入的结构体为值类型
		o.ft = reflect.TypeOf(o.stctBody)
		o.fv = reflect.ValueOf(o.stctBody)
	default:
		return errors.New("CheckStructValid校验逻辑只允许校验有效的Struct类型！")
	}
	return nil
}

// 针对struct类型的校验处理
func (o *structValidChecker) checkStruct(i int) error {
	label := o.stctName
	if !o.ft.Field(i).Anonymous {
		//普通结构
		label = label + o.ft.Field(i).Name + "."
	}
	var err error
	if o.fv.Field(i).CanAddr() {
		//尽量使用引用的方式进行递归数据校验
		err = _checkStructDataValid(o.fv.Field(i).Addr().Interface(), label)
	} else {
		err = _checkStructDataValid(o.fv.Field(i).Interface(), label)
	}
	return err
}

// 对针map数据的校验处理
func (o *structValidChecker) checkMap(i int) error {
	ft := o.ft
	fv := o.fv

	//字段名称
	fieldName := ft.Field(i).Name

	if ft.Field(i).Tag.Get("require") == "1" {
		//该字段要求不能为空
		if fv.Field(i).Len() == 0 {
			return fmt.Errorf("字段%s要求不为空！", o.stctName+fieldName)
		}
	}

	//确定字典表中的每一项是否为结构类型，如果是，则继续递归检查结构类型中的数据
	if fv.Field(i).Len() > 0 {
		for _, mapKey := range fv.Field(i).MapKeys() {
			switch fv.Field(i).MapIndex(mapKey).Kind() {
			case reflect.Struct:
				//如果map项是struct类型，继续检查结构中的数据合法性
				label := o.stctName + fieldName + ".."
				if err := _checkStructDataValid(fv.Field(i).MapIndex(mapKey).Interface(), label); err != nil {
					return err
				}
			}
		}

	}
	return nil
}

// 针对array / slice数据的校验处理
func (o *structValidChecker) checkList(i int) error {
	ft := o.ft
	fv := o.fv

	//字段名称
	fieldName := ft.Field(i).Name

	if ft.Field(i).Tag.Get("require") == "1" {
		//该字段要求不能为空
		if fv.Field(i).Len() == 0 {
			return fmt.Errorf("字段%s要求不为空！", o.stctName+fieldName)
		}
	}

	//确定列表中的每一项是否为结构类型，如果是，则继续递归检查结构类型中的数据
	if fv.Field(i).Len() > 0 {
		for j := 0; j < fv.Field(i).Len(); j++ {
			//遍历名称中的每一项数据
			switch fv.Field(i).Index(j).Kind() {
			case reflect.Struct:
				//如果列表项是struct类型，继续检查结构中的数据合法性
				label := o.stctName + fieldName + "..."
				var err error
				if fv.Field(i).Index(j).CanAddr() {
					//尽量使用引用的方式递归校验
					err = _checkStructDataValid(fv.Field(i).Index(j).Addr().Interface(), label)
				} else {
					err = _checkStructDataValid(fv.Field(i).Index(j).Interface(), label)
				}
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// 针对string的数据校验处理
func (o *structValidChecker) checkString(i int) error {
	ft := o.ft
	fv := o.fv
	//字段名称
	fieldName := ft.Field(i).Name
	//值
	val := fv.Field(i).String()

	if ft.Field(i).Tag.Get("require") == "1" {
		//要求字段不能为空
		if len(val) == 0 {
			return fmt.Errorf("字段%s要求不为空！", o.stctName+fieldName)
		}
	}

	if length := ft.Field(i).Tag.Get("length"); len(length) > 0 {
		//如果只有一个数字，说明限定该字段长度必须为xxx长度，比如length:"6",该字段长度应该为6
		fixLength := cvt.GetSafeInt64(length, 0)
		if fixLength != 0 && int64(len(val)) != fixLength {
			return fmt.Errorf("字段%s要求固定长度%d！", o.stctName+fieldName, fixLength)
		}
		//否则应该是一个逗号分割开的
		//length:"6," 长度应>=6
		//length:",6" 长度应<=6并>=0
		//length:"6,10" 长度应>=6并<=10

		split := strings.Split(length, ",")
		if len(split) != 2 {
			return fmt.Errorf("字段%s设置长度限制格式错误！", o.stctName+fieldName)
		}
		minLength, maxLength := cvt.GetSafeInt64(split[0], 0), cvt.GetSafeInt64(split[1], 0)

		if int64(len(val)) < minLength || (int64(len(val)) > maxLength && maxLength != 0) {
			return fmt.Errorf("字段%s要求长度在%d~%d之间", o.stctName+fieldName, minLength, maxLength)
		}
	}

	if validator := ft.Field(i).Tag.Get("validator"); len(validator) > 0 {
		//该字段带有验证逻辑
		if len(val) > 0 {
			if err := CheckDataType(validator, val, o.stctName+fieldName); err != nil {
				return err
			}
		}
	}

	if rang := ft.Field(i).Tag.Get("range"); len(rang) > 0 {
		//存在对数据的范围校验
		if len(val) > 0 {
			strlist := StringToList(rang)
			if !InList(val, strlist) {
				return fmt.Errorf("字段%s的值要求在[%s]范围内", o.stctName+fieldName, rang)
			}
		}
	}
	return nil
}

// 针对数值类型(integer / float)的数据校验处理
func (o *structValidChecker) checkDecimal(i int) error {
	ft := o.ft
	fv := o.fv
	stctName := o.stctName

	//字段名称
	fieldName := ft.Field(i).Name

	//此处如果使用fv.Field(i).Interface()，则非导出的字段会出错，使用严格的匹配方式
	var val float64
	switch fv.Field(i).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		val = float64(fv.Field(i).Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		val = float64(fv.Field(i).Uint())
	case reflect.Float32, reflect.Float64:
		val = fv.Field(i).Float()
	}

	if ft.Field(i).Tag.Get("require") == "1" {
		//要求字段不能为空
		if val == 0 {
			return fmt.Errorf("字段%s要求不为空！", o.stctName+fieldName)
		}
	}
	//范围校验
	if rang := ft.Field(i).Tag.Get("range"); len(rang) > 0 {
		//存在对数据的范围校验
		if err := o.checkDecimalRange(val, rang, fieldName); err != nil {
			return err
		}
	}

	//Int / float类型区间校验
	cmdInfos := map[string]string{
		"lt":  "小于",
		"lte": "小于或等于",
		"le":  "小于或等于",
		"gt":  "大于",
		"gte": "大于或等于",
		"ge":  "大于或等于",
	}

	for cmd, cmdName := range cmdInfos {
		boundaryValue := ft.Field(i).Tag.Get(cmd)
		if len(boundaryValue) == 0 {
			continue
		}
		ok, err := _checkValidCompare(val, cmd, boundaryValue)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("字段%s的值要求 %s %s", stctName+fieldName, cmdName, boundaryValue)
		}
	}

	return nil
}

func (o *structValidChecker) checkDecimalRange(val float64, rang, fieldName string) error {
	strlist := StringToList(rang)
	isok := false
	for _, itm := range strlist {
		itmVal, err := cvt.GetFloat64(itm, fmt.Sprintf("结构体字段 %s Tag(range)要求int类型列表，实际是[%s]",
			fieldName, rang), 0)
		if err != nil {
			return err
		}
		if val == itmVal {
			isok = true
			break
		}
	}
	if !isok {
		return fmt.Errorf("字段%s的值要求在[%s]范围内", o.stctName+fieldName, rang)
	}
	return nil
}

// int / float类型的比较
func _checkValidCompare(aVal any, cmd string, bVal any) (bool, error) {
	a, err := cvt.GetFloat64(aVal, fmt.Sprintf("无效的对比数据项 %v", aVal), 0)
	if err != nil {
		return false, err
	}
	b, err := cvt.GetFloat64(bVal, fmt.Sprintf("无效的对比数据项 %v", bVal), 0)
	if err != nil {
		return false, err
	}
	switch cmd {
	case "gt":
		return a > b, nil
	case "gte", "ge":
		return a >= b, nil
	case "lt":
		return a < b, nil
	case "lte", "le":
		return a <= b, nil
	default:
		return false, fmt.Errorf("配置错误，不支持的区间配置指令[%s]", cmd)
	}
}
