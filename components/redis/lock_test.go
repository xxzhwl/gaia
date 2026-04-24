package redis

import (
	"os"
	"testing"
	"time"
)

func TestIntegration_LockUnlock(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试（设置 GAIA_TEST_INTEGRATION=1 启用）")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:lock:" + time.Now().Format("150405")

	// 加锁
	lockVal, err := cli.Lock(key, 5*time.Second, 3*time.Second)
	if err != nil {
		t.Fatalf("Lock 失败: %v", err)
	}
	if lockVal == "" {
		t.Fatal("lockVal 不应为空")
	}

	// 解锁
	err = cli.UnLock(key, lockVal)
	if err != nil {
		t.Fatalf("UnLock 失败: %v", err)
	}
}

func TestIntegration_TryLock(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:trylock:" + time.Now().Format("150405")

	// 第一次 TryLock 应该成功
	val, ok, err := cli.TryLock(key, 5*time.Second)
	if err != nil {
		t.Fatalf("TryLock 报错: %v", err)
	}
	if !ok {
		t.Fatal("第一次 TryLock 应该成功")
	}

	// 第二次 TryLock 应该失败（锁已持有）
	_, ok2, err := cli.TryLock(key, 5*time.Second)
	if err != nil {
		t.Fatalf("TryLock 报错: %v", err)
	}
	if ok2 {
		t.Fatal("第二次 TryLock 应该失败")
	}

	// 解锁
	cli.UnLock(key, val)
}

func TestIntegration_UnLockByKey(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:unlockbykey:" + time.Now().Format("150405")

	_, err := cli.Lock(key, 5*time.Second, 3*time.Second)
	if err != nil {
		t.Fatalf("Lock 失败: %v", err)
	}

	err = cli.UnLockByKey(key)
	if err != nil {
		t.Fatalf("UnLockByKey 失败: %v", err)
	}
}

func TestIntegration_UnLockByKey_NotHeld(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	err := cli.UnLockByKey("nonexist-key")
	if err == nil {
		t.Fatal("释放未持有的锁应报错")
	}
}

func TestIntegration_UnLock_WrongValue(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:wrongval:" + time.Now().Format("150405")

	lockVal, _ := cli.Lock(key, 5*time.Second, 3*time.Second)

	// 用错误的 value 解锁
	err := cli.UnLock(key, "wrong-value-xxx")
	if err == nil {
		t.Fatal("错误 value 解锁应报错")
	}

	// 正确解锁
	cli.UnLock(key, lockVal)
}
