package lock

import (
	"testing"
)

func TestLockerInterface(t *testing.T) {
	// 确保 EtcdLocker 实现了 Locker 接口
	var _ Locker = &EtcdLocker{}
}

func TestNewEtcdLocker_DefaultPrefix(t *testing.T) {
	// 注意：这里不真的连 etcd，只测构造逻辑
	locker := NewEtcdLocker(nil, "")
	if locker.prefix != "/gaia/lock" {
		t.Fatalf("默认前缀应为 /gaia/lock，得到 %s", locker.prefix)
	}
}

func TestNewEtcdLocker_CustomPrefix(t *testing.T) {
	locker := NewEtcdLocker(nil, "/app/mylock")
	if locker.prefix != "/app/mylock" {
		t.Fatalf("前缀应为 /app/mylock，得到 %s", locker.prefix)
	}
}

func TestEtcdLocker_UnlockNotHeld(t *testing.T) {
	locker := NewEtcdLocker(nil, "/test")
	err := locker.Unlock(nil, "nonexist")
	if err == nil {
		t.Fatal("释放未持有的锁应报错")
	}
}
