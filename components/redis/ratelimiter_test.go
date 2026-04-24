package redis

import (
	"os"
	"testing"
	"time"
)

func TestIntegration_RateLimitAllow(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试（设置 GAIA_TEST_INTEGRATION=1 启用）")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:ratelimit:" + time.Now().Format("150405")
	defer cli.RateLimitReset(key)

	// 限制每秒 5 次
	for i := 0; i < 5; i++ {
		allowed, err := cli.RateLimitAllow(key, 5, time.Second)
		if err != nil {
			t.Fatalf("RateLimitAllow 报错: %v", err)
		}
		if !allowed {
			t.Fatalf("第 %d 次请求应该放行", i+1)
		}
	}

	// 第 6 次应该被限流
	allowed, err := cli.RateLimitAllow(key, 5, time.Second)
	if err != nil {
		t.Fatalf("RateLimitAllow 报错: %v", err)
	}
	if allowed {
		t.Fatal("第 6 次请求应该被限流")
	}
}

func TestIntegration_RateLimitRemaining(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:remaining:" + time.Now().Format("150405")
	defer cli.RateLimitReset(key)

	// 先发 3 次
	for i := 0; i < 3; i++ {
		cli.RateLimitAllow(key, 10, time.Second)
	}

	remaining, err := cli.RateLimitRemaining(key, 10, time.Second)
	if err != nil {
		t.Fatalf("RateLimitRemaining 报错: %v", err)
	}
	if remaining != 7 {
		t.Fatalf("期望剩余 7，得到 %d", remaining)
	}
}

func TestIntegration_FixedWindowAllow(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}

	cli := NewClient("127.0.0.1:6379", "", "")

	key := "test:fixedwindow:" + time.Now().Format("150405")
	defer cli.c.Del(cli.ctx, "ratelimit:fw:"+key)

	for i := 0; i < 3; i++ {
		allowed, err := cli.FixedWindowAllow(key, 3, time.Second)
		if err != nil {
			t.Fatalf("FixedWindowAllow 报错: %v", err)
		}
		if !allowed {
			t.Fatalf("第 %d 次应该放行", i+1)
		}
	}

	allowed, _ := cli.FixedWindowAllow(key, 3, time.Second)
	if allowed {
		t.Fatal("超出限额应被限流")
	}
}
