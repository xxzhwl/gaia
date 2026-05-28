// Package redis 注释
// @author wanlizhan
// @created 2024/5/21
package redis

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	ctx := context.Background()
	client := NewClient("127.0.0.1:6379", "", "").WithCtx(ctx)

	if err := client.c.Ping(ctx).Err(); err != nil {
		t.Skipf("skip integration test: redis unavailable: %v", err)
	}

	err := client.Set("name", "xxxxx", time.Second*30)
	if err != nil {
		t.Fatal(err)
	}

	x, err := client.Get("name")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(x))
}
