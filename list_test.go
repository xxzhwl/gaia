package gaia

import (
	"reflect"
	"testing"
)

// TestMapListByFunc tests the MapListByFunc function
func TestMapListByFunc(t *testing.T) {
	// Test with int to string mapping
	intList := []int{1, 2, 3, 4, 5}
	strList := MapListByFunc(intList, func(val int) string {
		return string(rune(val + 64)) // Convert to uppercase letters
	})
	expected := []string{"A", "B", "C", "D", "E"}
	if !reflect.DeepEqual(strList, expected) {
		t.Errorf("Expected %v, got %v", expected, strList)
	}

	// Test with string to int mapping
	strList2 := []string{"1", "2", "3"}
	intList2 := MapListByFunc(strList2, func(val string) int {
		return len(val)
	})
	expected2 := []int{1, 1, 1}
	if !reflect.DeepEqual(intList2, expected2) {
		t.Errorf("Expected %v, got %v", expected2, intList2)
	}
}

// TestReduceListByFunc tests the ReduceListByFunc function
func TestReduceListByFunc(t *testing.T) {
	// Test sum reduction
	intList := []int{1, 2, 3, 4, 5}
	sum := ReduceListByFunc(intList, func(agg, itm int) int {
		return agg + itm
	})
	expected := 15
	if sum != expected {
		t.Errorf("Expected %d, got %d", expected, sum)
	}

	// Test concatenation reduction
	strList := []string{"a", "b", "c"}
	concat := ReduceListByFunc(strList, func(agg, itm string) string {
		return agg + itm
	})
	expected2 := "abc"
	if concat != expected2 {
		t.Errorf("Expected %s, got %s", expected2, concat)
	}
}

// TestFilterListByFunc tests the FilterListByFunc function
func TestFilterListByFunc(t *testing.T) {
	// Test filtering even numbers
	intList := []int{1, 2, 3, 4, 5, 6}
	evenList := FilterListByFunc(intList, func(val int, idx int) bool {
		return val%2 == 0
	})
	expected := []int{2, 4, 6}
	if !reflect.DeepEqual(evenList, expected) {
		t.Errorf("Expected %v, got %v", expected, evenList)
	}

	// Test filtering strings by length
	strList := []string{"a", "ab", "abc", "abcd"}
	longStrList := FilterListByFunc(strList, func(val string, idx int) bool {
		return len(val) > 2
	})
	expected2 := []string{"abc", "abcd"}
	if !reflect.DeepEqual(longStrList, expected2) {
		t.Errorf("Expected %v, got %v", expected2, longStrList)
	}
}

// TestUniqueList tests the UniqueList function
func TestUniqueList(t *testing.T) {
	// Test with duplicates
	intList := []int{1, 2, 2, 3, 3, 3, 4}
	uniqueList := UniqueList(intList)
	expected := []int{1, 2, 3, 4}
	if !reflect.DeepEqual(uniqueList, expected) {
		t.Errorf("Expected %v, got %v", expected, uniqueList)
	}

	// Test with no duplicates
	strList := []string{"a", "b", "c"}
	uniqueStrList := UniqueList(strList)
	expected2 := []string{"a", "b", "c"}
	if !reflect.DeepEqual(uniqueStrList, expected2) {
		t.Errorf("Expected %v, got %v", expected2, uniqueStrList)
	}

	// Test with empty list
	emptyList := UniqueList([]int{})
	if len(emptyList) != 0 {
		t.Errorf("Expected empty list, got %v", emptyList)
	}
}

// TestUniqueListByFunc tests the UniqueListByFunc function
func TestUniqueListByFunc(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	// Test with custom key function
	people := []Person{
		{Name: "Alice", Age: 20},
		{Name: "Bob", Age: 25},
		{Name: "Alice", Age: 30}, // Duplicate name
	}

	uniquePeople := UniqueListByFunc(people, func(p Person) string {
		return p.Name
	})

	expected := []Person{
		{Name: "Alice", Age: 20},
		{Name: "Bob", Age: 25},
	}

	if !reflect.DeepEqual(uniquePeople, expected) {
		t.Errorf("Expected %v, got %v", expected, uniquePeople)
	}
}

// TestGroupListByFunc tests the GroupListByFunc function
func TestGroupListByFunc(t *testing.T) {
	type Person struct {
		Name string
		Age  int
	}

	// Test with custom key function
	people := []Person{
		{Name: "Alice", Age: 20},
		{Name: "Bob", Age: 25},
		{Name: "Charlie", Age: 20},
	}

	grouped := GroupListByFunc(people, func(p Person) int {
		return p.Age
	})

	expected := map[int][]Person{
		20: {{Name: "Alice", Age: 20}, {Name: "Charlie", Age: 20}},
		25: {{Name: "Bob", Age: 25}},
	}

	if !reflect.DeepEqual(grouped, expected) {
		t.Errorf("Expected %v, got %v", expected, grouped)
	}
}

// TestListToMapKey tests the ListToMapKey function
func TestListToMapKey(t *testing.T) {
	// Test with string list
	strList := []string{"a", "b", "c"}
	mapKey := ListToMapKey(strList)
	expected := map[string]int{"a": 0, "b": 1, "c": 2}
	if !reflect.DeepEqual(mapKey, expected) {
		t.Errorf("Expected %v, got %v", expected, mapKey)
	}

	// Test with duplicate values (should keep last index)
	dupList := []string{"a", "b", "a"}
	mapKey2 := ListToMapKey(dupList)
	expected2 := map[string]int{"a": 2, "b": 1}
	if !reflect.DeepEqual(mapKey2, expected2) {
		t.Errorf("Expected %v, got %v", expected2, mapKey2)
	}

	// Test with empty list
	emptyMap := ListToMapKey([]string{})
	if len(emptyMap) != 0 {
		t.Errorf("Expected empty map, got %v", emptyMap)
	}
}

// TestInList tests the InList function
func TestInList(t *testing.T) {
	// Test with existing element
	intList := []int{1, 2, 3, 4, 5}
	if !InList(3, intList) {
		t.Errorf("Expected 3 to be in list %v", intList)
	}

	// Test with non-existing element
	if InList(6, intList) {
		t.Errorf("Expected 6 not to be in list %v", intList)
	}

	// Test with empty list
	if InList(1, []int{}) {
		t.Errorf("Expected 1 not to be in empty list")
	}

	// Test with string list
	strList := []string{"a", "b", "c"}
	if !InList("b", strList) {
		t.Errorf("Expected 'b' to be in list %v", strList)
	}
}

// TestIntersectList tests the IntersectList function
func TestIntersectList(t *testing.T) {
	// Test with common elements
	l1 := []int{1, 2, 3, 4}
	l2 := []int{3, 4, 5, 6}
	intersect := IntersectList(l1, l2)
	expected := []int{3, 4}
	if !reflect.DeepEqual(intersect, expected) {
		t.Errorf("Expected %v, got %v", expected, intersect)
	}

	// Test with no common elements
	l3 := []int{1, 2}
	l4 := []int{3, 4}
	intersect2 := IntersectList(l3, l4)
	if len(intersect2) != 0 {
		t.Errorf("Expected empty list, got %v", intersect2)
	}

	// Test with empty list
	l5 := []int{1, 2}
	l6 := []int{}
	intersect3 := IntersectList(l5, l6)
	if len(intersect3) != 0 {
		t.Errorf("Expected empty list, got %v", intersect3)
	}
}

// TestIntersectListMulti tests the IntersectListMulti function
func TestIntersectListMulti(t *testing.T) {
	// Test with multiple lists
	lists := [][]int{{1, 2, 3}, {2, 3, 4}, {3, 4, 5}}
	intersect := IntersectListMulti(lists...)
	expected := []int{3}
	if !reflect.DeepEqual(intersect, expected) {
		t.Errorf("Expected %v, got %v", expected, intersect)
	}

	// Test with no common elements
	lists2 := [][]int{{1, 2}, {3, 4}, {5, 6}}
	intersect2 := IntersectListMulti(lists2...)
	if len(intersect2) != 0 {
		t.Errorf("Expected empty list, got %v", intersect2)
	}

	// Test with single list
	lists3 := [][]int{{1, 2, 3}}
	intersect3 := IntersectListMulti(lists3...)
	if !reflect.DeepEqual(intersect3, []int{1, 2, 3}) {
		t.Errorf("Expected %v, got %v", []int{1, 2, 3}, intersect3)
	}
}

// TestUnionList tests the UnionList function
func TestUnionList(t *testing.T) {
	// Test with common elements
	l1 := []int{1, 2, 3}
	l2 := []int{3, 4, 5}
	union := UnionList(l1, l2)
	// Note: UnionList doesn't guarantee order, so we need to check all elements are present
	expected := []int{1, 2, 3, 4, 5}
	if len(union) != len(expected) {
		t.Errorf("Expected %d elements, got %d", len(expected), len(union))
	}
	for _, e := range expected {
		if !InList(e, union) {
			t.Errorf("Expected %d to be in union %v", e, union)
		}
	}
}

// TestDifferenceList tests the DifferenceList function
func TestDifferenceList(t *testing.T) {
	// Test with common elements
	l1 := []int{1, 2, 3, 4}
	l2 := []int{3, 4, 5, 6}
	left, right := DifferenceList(l1, l2)
	expectedLeft := []int{1, 2}
	expectedRight := []int{5, 6}
	if !reflect.DeepEqual(left, expectedLeft) {
		t.Errorf("Expected left %v, got %v", expectedLeft, left)
	}
	if !reflect.DeepEqual(right, expectedRight) {
		t.Errorf("Expected right %v, got %v", expectedRight, right)
	}

	// Test with identical lists
	l3 := []int{1, 2, 3}
	l4 := []int{1, 2, 3}
	left2, right2 := DifferenceList(l3, l4)
	if len(left2) != 0 || len(right2) != 0 {
		t.Errorf("Expected empty difference, got left: %v, right: %v", left2, right2)
	}
}

// TestFindListIndex tests the FindListIndex function
func TestFindListIndex(t *testing.T) {
	// Test with existing element
	intList := []int{1, 2, 3, 4, 5}
	index := FindListIndex(3, intList)
	expected := 2
	if index != expected {
		t.Errorf("Expected index %d, got %d", expected, index)
	}

	// Test with first element
	index2 := FindListIndex(1, intList)
	expected2 := 0
	if index2 != expected2 {
		t.Errorf("Expected index %d, got %d", expected2, index2)
	}

	// Test with last element
	index3 := FindListIndex(5, intList)
	expected3 := 4
	if index3 != expected3 {
		t.Errorf("Expected index %d, got %d", expected3, index3)
	}

	// Test with non-existing element
	index4 := FindListIndex(6, intList)
	expected4 := -1
	if index4 != expected4 {
		t.Errorf("Expected index %d, got %d", expected4, index4)
	}

	// Test with empty list
	index5 := FindListIndex(1, []int{})
	expected5 := -1
	if index5 != expected5 {
		t.Errorf("Expected index %d, got %d", expected5, index5)
	}
}

// TestDelListValue tests the DelListValue function
func TestDelListValue(t *testing.T) {
	// Test with existing value
	intList := []int{1, 2, 3, 4, 5}
	result := DelListValue(intList, 3)
	expected := []int{1, 2, 4, 5}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with multiple occurrences
	intList2 := []int{1, 2, 2, 3, 2}
	result2 := DelListValue(intList2, 2)
	expected2 := []int{1, 3}
	if !reflect.DeepEqual(result2, expected2) {
		t.Errorf("Expected %v, got %v", expected2, result2)
	}

	// Test with non-existing value
	intList3 := []int{1, 2, 3}
	result3 := DelListValue(intList3, 4)
	expected3 := []int{1, 2, 3}
	if !reflect.DeepEqual(result3, expected3) {
		t.Errorf("Expected %v, got %v", expected3, result3)
	}

	// Test with empty list
	result4 := DelListValue([]int{}, 1)
	if len(result4) != 0 {
		t.Errorf("Expected empty list, got %v", result4)
	}
}

// TestJoin tests the Join function
func TestJoin(t *testing.T) {
	// Test with int list
	intList := []int{1, 2, 3, 4, 5}
	result := Join(intList, ",")
	expected := "1,2,3,4,5"
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with string list
	strList := []string{"a", "b", "c"}
	result2 := Join(strList, "-")
	expected2 := "a-b-c"
	if result2 != expected2 {
		t.Errorf("Expected %s, got %s", expected2, result2)
	}

	// Test with empty list
	result3 := Join([]int{}, ",")
	expected3 := ""
	if result3 != expected3 {
		t.Errorf("Expected %s, got %s", expected3, result3)
	}

	// Test with single element
	result4 := Join([]int{42}, ",")
	expected4 := "42"
	if result4 != expected4 {
		t.Errorf("Expected %s, got %s", expected4, result4)
	}
}

// TestGetMapListById tests the GetMapListById function
func TestGetMapListById(t *testing.T) {
	// Test with valid data
	list := []map[string]string{
		{"id": "1", "name": "Alice"},
		{"id": "2", "name": "Bob"},
		{"id": "3", "name": "Charlie"},
	}

	result := GetMapListById(list, "id")
	expected := map[string]map[string]string{
		"1": {"id": "1", "name": "Alice"},
		"2": {"id": "2", "name": "Bob"},
		"3": {"id": "3", "name": "Charlie"},
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with missing id field
	list2 := []map[string]string{
		{"name": "Alice"}, // Missing id
		{"id": "2", "name": "Bob"},
	}

	result2 := GetMapListById(list2, "id")
	expected2 := map[string]map[string]string{
		"2": {"id": "2", "name": "Bob"},
	}

	if !reflect.DeepEqual(result2, expected2) {
		t.Errorf("Expected %v, got %v", expected2, result2)
	}

	// Test with empty list
	result3 := GetMapListById([]map[string]string{}, "id")
	if len(result3) != 0 {
		t.Errorf("Expected empty map, got %v", result3)
	}
}

// TestGetMapInterfaceListById tests the GetMapInterfaceListById function
func TestGetMapInterfaceListById(t *testing.T) {
	// Test with valid data
	list := []map[string]interface{}{
		{"id": "1", "name": "Alice", "age": 20},
		{"id": "2", "name": "Bob", "age": 25},
		{"id": "3", "name": "Charlie", "age": 30},
	}

	result := GetMapInterfaceListById(list, "id")
	expected := map[string]map[string]interface{}{
		"1": {"id": "1", "name": "Alice", "age": 20},
		"2": {"id": "2", "name": "Bob", "age": 25},
		"3": {"id": "3", "name": "Charlie", "age": 30},
	}

	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with empty list
	result2 := GetMapInterfaceListById([]map[string]interface{}{}, "id")
	if len(result2) != 0 {
		t.Errorf("Expected empty map, got %v", result2)
	}
}

// TestListToStringIn tests the ListToStringIn function
func TestListToStringIn(t *testing.T) {
	// Test with valid data
	strList := []string{"a", "b", "c"}
	result := ListToStringIn(strList)
	expected := `'a','b','c'`
	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}

	// Test with empty list
	result2 := ListToStringIn([]string{})
	expected2 := ""
	if result2 != expected2 {
		t.Errorf("Expected %s, got %s", expected2, result2)
	}

	// Test with single element
	result3 := ListToStringIn([]string{"single"})
	expected3 := `'single'`
	if result3 != expected3 {
		t.Errorf("Expected %s, got %s", expected3, result3)
	}
}

// TestByteToListWithNewline tests the ByteToListWithNewline function
func TestByteToListWithNewline(t *testing.T) {
	// Test with CRLF line endings
	content := []byte("line1\r\nline2\r\nline3")
	result, err := ByteToListWithNewline(content)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	expected := []string{"line1", "line2", "line3"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with LF line endings (should be converted to CRLF)
	content2 := []byte("line1\nline2\nline3")
	result2, err2 := ByteToListWithNewline(content2)
	if err2 != nil {
		t.Errorf("Unexpected error: %v", err2)
	}
	if !reflect.DeepEqual(result2, expected) {
		t.Errorf("Expected %v, got %v", expected, result2)
	}

	// Test with empty content
	content3 := []byte("")
	result3, err3 := ByteToListWithNewline(content3)
	if err3 != nil {
		t.Errorf("Unexpected error: %v", err3)
	}
	if len(result3) != 0 {
		t.Errorf("Expected empty list, got %v", result3)
	}

	// Test with whitespace-only lines (should be filtered out)
	content4 := []byte("line1\r\n   \r\nline2")
	result4, err4 := ByteToListWithNewline(content4)
	if err4 != nil {
		t.Errorf("Unexpected error: %v", err4)
	}
	expected4 := []string{"line1", "line2"}
	if !reflect.DeepEqual(result4, expected4) {
		t.Errorf("Expected %v, got %v", expected4, result4)
	}
}

// TestStringListDiff tests the StringListDiff function
func TestStringListDiff(t *testing.T) {
	// Test with common elements
	list1 := []string{"a", "b", "c", "d"}
	list2 := []string{"c", "d", "e", "f"}
	result := StringListDiff(list1, list2)
	expected := []string{"a", "b"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with no difference
	list3 := []string{"a", "b", "c"}
	list4 := []string{"a", "b", "c"}
	result2 := StringListDiff(list3, list4)
	if len(result2) != 0 {
		t.Errorf("Expected empty list, got %v", result2)
	}

	// Test with list1 empty
	result3 := StringListDiff([]string{}, list2)
	if len(result3) != 0 {
		t.Errorf("Expected empty list, got %v", result3)
	}
}

// TestStringListIntersection tests the StringListIntersection function
func TestStringListIntersection(t *testing.T) {
	// Test with common elements
	list1 := []string{"a", "b", "c", "d"}
	list2 := []string{"c", "d", "e", "f"}
	result := StringListIntersection(list1, list2)
	expected := []string{"c", "d"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with no common elements
	list3 := []string{"a", "b"}
	list4 := []string{"c", "d"}
	result2 := StringListIntersection(list3, list4)
	if len(result2) != 0 {
		t.Errorf("Expected empty list, got %v", result2)
	}

	// Test with one empty list
	result3 := StringListIntersection([]string{}, list1)
	if len(result3) != 0 {
		t.Errorf("Expected empty list, got %v", result3)
	}
}

// TestIsStringListEqual tests the IsStringListEqual function
func TestIsStringListEqual(t *testing.T) {
	// Test with identical lists (different order)
	list1 := []string{"a", "b", "c"}
	list2 := []string{"c", "a", "b"}
	if !IsStringListEqual(list1, list2) {
		t.Errorf("Expected lists %v and %v to be equal", list1, list2)
	}

	// Test with different lists
	list3 := []string{"a", "b", "c"}
	list4 := []string{"a", "b", "d"}
	if IsStringListEqual(list3, list4) {
		t.Errorf("Expected lists %v and %v to be different", list3, list4)
	}

	// Test with empty lists
	if !IsStringListEqual([]string{}, []string{}) {
		t.Errorf("Expected empty lists to be equal")
	}

	// Test with lists of different lengths
	list5 := []string{"a", "b"}
	list6 := []string{"a", "b", "c"}
	if IsStringListEqual(list5, list6) {
		t.Errorf("Expected lists %v and %v to be different (different lengths)", list5, list6)
	}
}

// TestGroupList tests the GroupList function
func TestGroupList(t *testing.T) {
	// Test with normal grouping
	intList := []int{1, 2, 3, 4, 5, 6, 7}
	result := GroupList(intList, 3)
	expected := [][]int{{1, 2, 3}, {4, 5, 6}, {7}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with exact group size
	intList2 := []int{1, 2, 3, 4, 5, 6}
	result2 := GroupList(intList2, 3)
	expected2 := [][]int{{1, 2, 3}, {4, 5, 6}}
	if !reflect.DeepEqual(result2, expected2) {
		t.Errorf("Expected %v, got %v", expected2, result2)
	}

	// Test with groupSize = 1
	intList3 := []int{1, 2, 3}
	result3 := GroupList(intList3, 1)
	expected3 := [][]int{{1}, {2}, {3}}
	if !reflect.DeepEqual(result3, expected3) {
		t.Errorf("Expected %v, got %v", expected3, result3)
	}

	// Test with groupSize = 0 (should use default 1)
	result4 := GroupList(intList3, 0)
	if !reflect.DeepEqual(result4, expected3) {
		t.Errorf("Expected %v, got %v", expected3, result4)
	}

	// Test with empty list
	result5 := GroupList([]int{}, 3)
	if len(result5) != 0 {
		t.Errorf("Expected empty list, got %v", result5)
	}

	// Test with single element
	result6 := GroupList([]int{42}, 3)
	expected6 := [][]int{{42}}
	if !reflect.DeepEqual(result6, expected6) {
		t.Errorf("Expected %v, got %v", expected6, result6)
	}
}

// TestListReverse tests the ListReverse function
func TestListReverse(t *testing.T) {
	// Test with normal list
	intList := []int{1, 2, 3, 4, 5}
	result := ListReverse(intList)
	expected := []int{5, 4, 3, 2, 1}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}

	// Test with empty list
	result2 := ListReverse([]int{})
	if len(result2) != 0 {
		t.Errorf("Expected empty list, got %v", result2)
	}

	// Test with single element
	result3 := ListReverse([]int{42})
	expected3 := []int{42}
	if !reflect.DeepEqual(result3, expected3) {
		t.Errorf("Expected %v, got %v", expected3, result3)
	}

	// Test with string list
	strList := []string{"a", "b", "c"}
	result4 := ListReverse(strList)
	expected4 := []string{"c", "b", "a"}
	if !reflect.DeepEqual(result4, expected4) {
		t.Errorf("Expected %v, got %v", expected4, result4)
	}
}

// TestCopyList tests the CopyList function
func TestCopyList(t *testing.T) {
	// Test with normal list
	intList := []int{1, 2, 3, 4, 5}
	result := CopyList(intList)
	if !reflect.DeepEqual(result, intList) {
		t.Errorf("Expected %v, got %v", intList, result)
	}

	// Test that it's a copy (not reference)
	result[0] = 100
	if intList[0] == result[0] {
		t.Errorf("Expected original list to not be modified, but got %v", intList)
	}

	// Test with empty list
	result2 := CopyList([]int{})
	if len(result2) != 0 {
		t.Errorf("Expected empty list, got %v", result2)
	}

	// Test with single element
	result3 := CopyList([]int{42})
	expected3 := []int{42}
	if !reflect.DeepEqual(result3, expected3) {
		t.Errorf("Expected %v, got %v", expected3, result3)
	}
}

// TestRandList tests the RandList function
func TestRandList(t *testing.T) {
	// Test with maxNum less than list length
	intList := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	result := RandList(intList, 5)
	if len(result) != 5 {
		t.Errorf("Expected list of length 5, got %d", len(result))
	}

	// Test with all unique elements
	seen := make(map[int]bool)
	for _, v := range result {
		seen[v] = true
	}
	if len(seen) != 5 {
		t.Errorf("Expected all elements to be unique, got duplicates: %v", result)
	}

	// Test with maxNum greater than list length (should return entire list)
	result2 := RandList(intList, 20)
	if len(result2) != len(intList) {
		t.Errorf("Expected list of length %d, got %d", len(intList), len(result2))
	}

	// Test with maxNum equal to list length (should return entire list)
	result3 := RandList(intList, 10)
	if len(result3) != 10 {
		t.Errorf("Expected list of length 10, got %d", len(result3))
	}

	// Test with empty list
	result4 := RandList([]int{}, 5)
	if len(result4) != 0 {
		t.Errorf("Expected empty list, got %v", result4)
	}

	// Test with single element
	result5 := RandList([]int{42}, 1)
	expected5 := []int{42}
	if !reflect.DeepEqual(result5, expected5) {
		t.Errorf("Expected %v, got %v", expected5, result5)
	}
}
