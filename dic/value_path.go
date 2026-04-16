// Package dic map[string]any 值路径逻辑封装
// @author wanlizhan
// @created 2023-03-11
package dic

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type keyType int

const (
	mapKey   keyType = iota //形式如 abc
	indexKey                //形式如 abc[0]
	sliceKey                //形式如 abc[i]
)

// 逻辑体
type valuePath struct {
	rawMap     any
	rawKeyPath string

	keyCaseInsensitive bool //key值大小写是否要求敏感，默认为大小写敏感

	_keyNodeList []keyNode //解析后的路径节点列表
	_hasSlice    bool      //此路径中是否存在列表的路径表示
	_cpv         []any     //当前路径指向值
}

// key路径节点信息
type keyNode struct {
	keyName string  //路径名称 如 key1.key2... 中的 key1, key2
	keyType keyType //路径节点类型

	index int //如果是 indexKey 则此值表示为index下标，否则此值无效

}

// 实例化
func newValuePath() *valuePath {
	return &valuePath{}
}

func (o *valuePath) setKeyCaseInsensitive() {
	o.keyCaseInsensitive = true
}

// 通过一个路径，获取对应的值，路径的形式如下
// rawMap 要求为map[string]any类型
// keypath 要求的格式如下
// key1.key2.key3...
// obj_list[i].key1
// list[0].objList[1].key1
// list[0].objList[i].key1
// []中如果是非数值(如 i, k 之类)，则获取整个列表中所有对应的值，返回的是一个列表
// []中如果是数值(如 0, 1, 2 之类)，则获取对应列表中元素的值
func (o *valuePath) getValue(rawData map[string]any, keypath string) (any, error) {
	//1. 原始值设置
	o.rawMap = rawData
	o.rawKeyPath = keypath

	//2. 路径解析到 _keyNodeList 属性中
	if err := o._parseKeyNode(); err != nil {
		return nil, err
	}

	//3. 路径遍历
	o._cpv = []any{rawData}
	var err error
	for _, node := range o._keyNodeList {
		switch node.keyType {
		case mapKey:
			o._cpv, err = o._scanMapKey(o._cpv, node.keyName)
			if err != nil {
				return nil, err
			}
		case indexKey:
			o._cpv, err = o._scanIndexKey(o._cpv, node.keyName, node.index)
			if err != nil {
				return nil, err
			}
		case sliceKey:
			o._cpv, err = o._scanSliceKey(o._cpv, node.keyName)
			if err != nil {
				return nil, err
			}
		default:
			return nil, o.errKeyPathInvalid()
		}
	}

	if o._hasSlice {
		return o._cpv, nil
	} else {
		if len(o._cpv) > 0 {
			return o._cpv[0], nil
		} else {
			return nil, nil
		}
	}
}

func (o *valuePath) _scanMapKey(data []any, key string) ([]any, error) {
	rets := make([]any, 0)
	for _, item := range data {
		mapData, ok := item.(map[string]any)
		if !ok {
			return nil, o.errMapTypeInvalid(key)
		}
		if v, ok := mapData[key]; ok {
			//直接匹配成功
			rets = append(rets, v)
			continue
		}
		if o.keyCaseInsensitive {
			//不要求大小写敏感，再尝试宽松匹配
			isMatched := false
			for k, v := range mapData {
				if strings.ToLower(key) == strings.ToLower(k) {
					//匹配成功
					rets = append(rets, v)
					isMatched = true
					break
				}
			}
			if isMatched {
				continue
			}
		}

		//到此，最终匹配失败
		return nil, o.errValueNotFound()
	}
	return rets, nil
}

func (o *valuePath) _scanIndexKey(data []any, key string, index int) ([]any, error) {
	rets := make([]any, 0)
	for _, item := range data {
		list, err := o._getSlice(item, key)
		if err != nil {
			return nil, err
		}
		if len(list) > index {
			rets = append(rets, list[index])
		} else {
			return nil, o.errValueNotFound()
		}
	}
	return rets, nil
}

func (o *valuePath) _scanSliceKey(data []any, key string) ([]any, error) {
	rets := make([]any, 0)
	for _, item := range data {
		list, err := o._getSlice(item, key)
		if err != nil {
			return nil, err
		}
		for _, itm := range list {
			rets = append(rets, itm)
		}
	}
	return rets, nil
}

func (o *valuePath) _getSlice(item any, key string) ([]any, error) {
	mapData, ok := item.(map[string]any)
	if !ok {
		return nil, o.errMapTypeInvalid(key)
	}
	v, ok := mapData[key]
	if !ok {
		return nil, o.errValueNotFound()
	}
	list, ok := v.([]any)
	if !ok {
		return nil, o.errSliceTypeInvalid(key)
	}
	return list, nil
}

func (o *valuePath) _parseKeyNode() error {
	arr := strings.Split(o.rawKeyPath, ".")
	re := regexp.MustCompile(`\[(\w+)]`)
	for _, key := range arr {
		if !strings.Contains(key, "[") {
			o._keyNodeList = append(o._keyNodeList, keyNode{
				keyName: key,
				keyType: mapKey,
			})
			continue
		}
		result := re.FindAllStringSubmatch(key, 1)
		if len(result) == 0 {
			return o.errKeyPathInvalid()
		}
		idx := result[0][1]
		if resArr := strings.SplitN(key, "[", 2); len(resArr) > 0 {
			key = resArr[0]
		} else {
			return o.errKeyPathInvalid()
		}

		if n, err := strconv.Atoi(idx); err == nil {
			//说明是一个int类型，认为是具体的下标值
			o._keyNodeList = append(o._keyNodeList, keyNode{
				keyName: key,
				keyType: indexKey,
				index:   n,
			})
		} else {
			//无法转换为int，说明是一个非int类型，认为是下标值的广义表示，即需要取列表中所有下标的数据值
			o._keyNodeList = append(o._keyNodeList, keyNode{
				keyName: key,
				keyType: sliceKey,
			})
			o._hasSlice = true
		}
	}

	if len(o._keyNodeList) == 0 {
		return o.errKeyPathInvalid()
	}

	return nil
}

func (o *valuePath) errKeyPathInvalid() error {
	return fmt.Errorf("值路径 %s 非法", o.rawKeyPath)
}

func (o *valuePath) errValueNotFound() error {
	return fmt.Errorf("指定的数据中不存在路径 %s 所对应的值，请检查目标数据的完善性", o.rawKeyPath)
}

func (o *valuePath) errMapTypeInvalid(key string) error {
	return fmt.Errorf("路径 %s 中的 %s 所对应的数据不是map[string]any数据类型，请检查目标数据的合法性", o.rawKeyPath, key)
}

func (o *valuePath) errSliceTypeInvalid(key string) error {
	return fmt.Errorf("路径 %s 中的 %s 所对应的数据不是[]any数据类型，请检查目标数据的合法性", o.rawKeyPath, key)
}
