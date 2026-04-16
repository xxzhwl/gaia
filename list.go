// Package gaia 与list(slice)相关的逻辑封装
// @author wanlizhan
// @created 2023-06-30
package gaia

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/xxzhwl/gaia/dic"
)

// MapListByFunc 对vals中的值应用f方法并将所有结果作为列表返回
func MapListByFunc[T, U any](vals []T, f func(val T) (ret U)) (res []U) {
	res = make([]U, len(vals))
	for i, v := range vals {
		res[i] = f(v)
	}
	return
}

// ReduceListByFunc 对vals中的值依次应用f方法，返回reduce的结果
//
// 比如给定的列表为 [1,2,3], 给定方法为 agg + itm 则返回值为 1 + 2 + 3 = 6
func ReduceListByFunc[T any](vals []T, f func(agg, itm T) T) (res T) {
	var agg T
	for _, v := range vals {
		agg = f(agg, v)
	}
	return agg
}

// FilterListByFunc 按指定方法对列表元素就地进行过滤
func FilterListByFunc[V any](vals []V, predicate func(val V, idx int) bool) []V {
	k := 0
	for i, item := range vals {
		if predicate(item, i) {
			vals[k] = vals[i]
			k++
		}
	}
	return vals[:k]
}

// UniqueList 返回不重复的列表
func UniqueList[T comparable](vals []T) (res []T) {
	seen := make(map[T]struct{}, len(vals))
	for _, itm := range vals {
		if _, ok := seen[itm]; ok {
			continue
		}
		seen[itm] = struct{}{}
		res = append(res, itm)
	}
	return
}

// UniqueListByFunc 根据指定方法过滤出不重复的列表
func UniqueListByFunc[T any, U comparable](vals []T, keyfunc func(T) U) (result []T) {
	seen := make(map[U]struct{}, len(vals))
	for _, item := range vals {
		key := keyfunc(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return
}

// GroupListByFunc 按照keyfunc获取唯一标识后，对给定列表进行分组, 返回键到子列表的map
func GroupListByFunc[T any, U comparable](vals []T, keyfunc func(T) U) (result map[U][]T) {
	result = make(map[U][]T)
	for _, item := range vals {
		key := keyfunc(item)
		result[key] = append(result[key], item)
	}
	return
}

// ListToMapKey 将一个slice列表，转换为map类型，其map的值为slice的下标值
// 此函数会将重复的列表函数收敛
func ListToMapKey[T comparable](vals []T) map[T]int {
	rets := make(map[T]int)
	if len(vals) > 0 {
		for i, itm := range vals {
			rets[itm] = i
		}
	}
	return rets
}

// InList 判断某个数据是否在列表内 O(N)
func InList[T comparable](needle T, vals []T) bool {
	if len(vals) == 0 {
		return false
	}
	for _, v := range vals {
		if v == needle {
			return true
		}
	}
	return false
}

// IntersectList 取列表交集, 即在 l1 且在 l2 中的数据, 注意这里并不会去重
func IntersectList[T comparable](l1, l2 []T) (res []T) {
	seen := make(map[T]struct{})
	for _, elem := range l1 {
		seen[elem] = struct{}{}
	}
	for _, elem := range l2 {
		if _, ok := seen[elem]; ok {
			res = append(res, elem)
		}
	}
	return res
}

// IntersectListMulti 取多个列表的交集, 注意这里不会去重
func IntersectListMulti[T comparable](lists ...[]T) (res []T) {
	if len(lists) == 0 {
		return
	}
	res = lists[0]
	for _, l := range lists {
		if len(l) == 0 {
			continue
		}
		if len(res) == 0 {
			return
		}
		res = IntersectList(res, l)
	}
	return
}

// UnionList 取列表并集, 注意这里并不会去重
func UnionList[T comparable](l1, l2 []T) (res []T) {
	seen := make(map[T]struct{})
	added := make(map[T]struct{})
	for _, elem := range l1 {
		seen[elem] = struct{}{}
	}
	for _, elem := range l2 {
		seen[elem] = struct{}{}
	}
	for _, elem := range l1 {
		if _, ok := seen[elem]; ok {
			res = append(res, elem)
			added[elem] = struct{}{}
		}
	}
	for _, elem := range l2 {
		if _, ok := added[elem]; ok {
			continue
		}
		if _, ok := seen[elem]; ok {
			res = append(res, elem)
		}
	}
	return
}

// DifferenceList 取列表差集, 注意不会去重
// 返回的第 1 个值是在 l1 中但不在 l2 中的数据
// 返回的第 2 个值是在 l2 中但不在 l1 中的数据
func DifferenceList[T comparable](l1, l2 []T) (left, right []T) {
	seenLeft := map[T]struct{}{}
	seenRight := map[T]struct{}{}

	for _, elem := range l1 {
		seenLeft[elem] = struct{}{}
	}
	for _, elem := range l2 {
		seenRight[elem] = struct{}{}
	}

	for _, elem := range l1 {
		if _, ok := seenRight[elem]; !ok {
			left = append(left, elem)
		}
	}
	for _, elem := range l2 {
		if _, ok := seenLeft[elem]; !ok {
			right = append(right, elem)
		}
	}

	return
}

// FindListIndex 从列表第一个元素开始，查找列表元素，返回返回第一个找到的元素所在的下标位置，如果存在多个相同元素，只返回第一个
// 如果没有找到对应的元素，则返回 -1
func FindListIndex[T comparable](elem T, list []T) int {
	if len(list) == 0 {
		return -1
	}
	for i, v := range list {
		if v == elem {
			return i
		}
	}
	return -1
}

// DelListValue 删除slice中的指定元素值，元素值可以是 string/number等可比较类型
func DelListValue[T comparable](list []T, delValue T) []T {
	return FilterListByFunc(list, func(val T, idx int) bool {
		return val != delValue
	})
}

// Join 将一个列表通过sep进行连接
// vlist 如 []string, []int64, []float32 等等
func Join[T any](vlist []T, sep string) string {
	if len(vlist) == 0 {
		return ""
	}
	slist := make([]string, len(vlist))
	for i, v := range vlist {
		slist[i] = fmt.Sprintf("%v", v)
	}
	return strings.Join(slist, sep)
}

// GetMapListById 根据列表节点中的某个key值，将列表形式的数据转为key-value的形式，其中value为列表中的节点
// 该逻辑旨在将一个列表数据中的节点中的某个key值作为列表数据的key值进行数据重组
func GetMapListById(list []map[string]string, id string) map[string]map[string]string {
	retval := make(map[string]map[string]string)
	if len(list) > 0 {
		for _, itm := range list {
			if kval, ok := itm[id]; ok {
				retval[kval] = itm
			}
		}
	}
	return retval
}

// GetMapInterfaceListById 根据列表节点中的某个key值，将列表形式的数据转为key-value的形式，其中value为列表中的节点
// 该逻辑旨在将一个列表数据中的节点中的某个key值作为列表数据的key值进行数据重组
func GetMapInterfaceListById(list []map[string]interface{}, id string) map[string]map[string]interface{} {
	retval := make(map[string]map[string]interface{})
	if len(list) > 0 {
		for _, itm := range list {
			kval := dic.S(itm, id)
			if len(kval) > 0 {
				retval[kval] = itm
			}
		}
	}
	return retval
}

// ListToStringIn 将一个list类型的数据，拼装成一个适合MYSQL IN查询内容的数据
// 如将[]string{"a", "b", "c"} 转成 `'a', 'b', 'c'`形式的串
func ListToStringIn(list []string) string {
	if len(list) == 0 {
		return ""
	}
	items := make([]string, len(list))
	for i, str := range list {
		items[i] = fmt.Sprintf(`'%s'`, str)
	}
	return strings.Join(items, ",")
}

// ByteToListWithNewline 将[]byte类型的内容一行一行读取，并以数组的形式返回
func ByteToListWithNewline(content []byte) ([]string, error) {
	content = bytes.Replace(content, []byte("\n"), []byte("\r"), -1)
	buf := bytes.NewBuffer(content)
	retval := make([]string, 0)
	for {
		bLine, bLineErr := buf.ReadBytes('\r')
		if bLineErr != nil {
			if bLineErr == io.EOF {
				// 处理最后一行（没有换行符的情况）
				if len(bLine) > 0 {
					line := string(bLine)
					line = strings.TrimSpace(line)
					if len(line) > 0 {
						retval = append(retval, line)
					}
				}
				break
			}
			return nil, bLineErr
		}
		line := string(bLine)
		line = strings.TrimSpace(line)
		if len(line) > 0 {
			retval = append(retval, line)
		}
	}
	return retval, nil
}

// StringListDiff 将list1中存在的而在list2中不存在的项返回，即取差集
func StringListDiff(list1, list2 []string) []string {
	left, _ := DifferenceList(list1, list2)
	return left
}

// StringListIntersection 将list1和list2的交集部分返回
func StringListIntersection(list1, list2 []string) []string {
	return IntersectList(list1, list2)
}

// IsStringListEqual 比较两个字符串列表中的元素是否一样，不考虑顺序 reflect.DeepEqual 会考虑顺序
// 如果元素一致返回true,否则返回false
func IsStringListEqual(list1, list2 []string) bool {
	if c := StringListDiff(list1, list2); len(c) > 0 {
		return false
	}
	if c := StringListDiff(list2, list1); len(c) > 0 {
		return false
	}
	return true
}

// GroupList 将传入的一个数据列表，按顺序进行分组，每组最多允许 groupSize 个元素
// list 待分割的数据列表
// groupSize 每组允许的最大元素数量，如果指定为0，则只分割成一个分组
func GroupList[T any](list []T, groupSize int) (res [][]T) {
	if groupSize <= 0 {
		groupSize = 1
	}
	if len(list) == 0 {
		return
	}

	n := len(list) / groupSize
	mod := len(list) % groupSize
	if mod > 0 {
		n += 1
	}

	// 避免内存分配
	res = make([][]T, n)
	for i := range res {
		res[i] = make([]T, groupSize)
	}
	for i := range list {
		res[i/groupSize][i%groupSize] = list[i]
	}

	// trim 一下
	if mod > 0 {
		res[n-1] = res[n-1][:mod]
	}

	return
}

// ListReverse 反转列表中的元素，比如[1, 2, 3, 4] -> [4, 3, 2, 1]
func ListReverse[T any](list []T) []T {
	if len(list) == 0 {
		return list
	}

	size := len(list)
	retlist := make([]T, size)
	j := 0
	for i := size - 1; i >= 0; i-- {
		retlist[j] = list[i]
		j++
	}
	return retlist
}

// RandList 从一个列表中随机的取出指定最大数量的元素列表
func RandList[T comparable](list []T, maxNum int) []T {
	if len(list) <= maxNum {
		return list
	}

	retlist := make([]T, maxNum)
	k := 0

	//将可随机获取的列表不断的装入tmplist中，对于获取过的元素，从tmplist中删除
	tmplist := make([]T, len(list))
	copy(tmplist, list)

	//临时列表总大小
	tmplen := len(tmplist)

	for i := 0; i < maxNum; i++ {
		idx := Rand(0, tmplen-1)
		v := tmplist[idx]
		retlist[k] = v
		k++

		//移除idx下标所在的元素，将列表向左收缩
		copy(tmplist[idx:], tmplist[idx+1:])
		tmplen -= 1
	}

	return retlist
}

// CopyList 以浅拷贝的方式克隆列表并返回
func CopyList[T any](vlist []T) []T {
	if len(vlist) == 0 {
		return vlist
	}
	retlist := make([]T, len(vlist))
	copy(retlist, vlist)
	return retlist
}
