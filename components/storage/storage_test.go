package storage

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// memoryStore 内存实现（测试用）
type memoryStore struct {
	data map[string][]byte
}

func newMemoryStore() *memoryStore {
	return &memoryStore{data: make(map[string][]byte)}
}

func (m *memoryStore) Put(_ context.Context, key string, data []byte) error {
	m.data[key] = data
	return nil
}

func (m *memoryStore) Get(_ context.Context, key string) ([]byte, error) {
	d, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("not found: %s", key)
	}
	return d, nil
}

func (m *memoryStore) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *memoryStore) Exists(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *memoryStore) List(_ context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	var result []ObjectInfo
	for k, v := range m.data {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			result = append(result, ObjectInfo{Key: k, Size: int64(len(v))})
			if len(result) >= maxKeys {
				break
			}
		}
	}
	return result, nil
}

func (m *memoryStore) SignURL(_ context.Context, key string, _ time.Duration) (string, error) {
	return fmt.Sprintf("https://memory.local/%s?signed=true", key), nil
}

func TestObjectStoreInterface(t *testing.T) {
	// memoryStore 实现了 ObjectStore 接口
	var store ObjectStore = newMemoryStore()

	ctx := context.Background()

	// Put
	if err := store.Put(ctx, "file.txt", []byte("hello")); err != nil {
		t.Fatalf("Put 失败: %v", err)
	}

	// Exists
	exists, _ := store.Exists(ctx, "file.txt")
	if !exists {
		t.Fatal("应该存在")
	}

	// Get
	data, err := store.Get(ctx, "file.txt")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("期望 hello，得到 %s", data)
	}

	// List
	store.Put(ctx, "dir/a.txt", []byte("a"))
	store.Put(ctx, "dir/b.txt", []byte("b"))
	list, _ := store.List(ctx, "dir/", 10)
	if len(list) != 2 {
		t.Fatalf("期望 2 个对象，得到 %d", len(list))
	}

	// SignURL
	url, _ := store.SignURL(ctx, "file.txt", time.Hour)
	if url == "" {
		t.Fatal("SignURL 不应返回空")
	}

	// Delete
	store.Delete(ctx, "file.txt")
	exists, _ = store.Exists(ctx, "file.txt")
	if exists {
		t.Fatal("删除后不应存在")
	}

	// Get not found
	_, err = store.Get(ctx, "file.txt")
	if err == nil {
		t.Fatal("不存在的对象应报错")
	}
}
