// Package redis 注释
// @author wanlizhan
// @created 2024/5/21
package redis

import (
	"context"
	"fmt"
	_ "github.com/xxzhwl/gaia/framework"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewFrameworkClient()
	ping := client.c.Ping(context.Background())

	if ping.Err() != nil {
		t.Fatal(ping.Err())
	}

	fmt.Println(ping.String())
	err := client.Set("name", "xxxxx", time.Second*30)
	if err != nil {
		t.Fatal(err)
	}

	x, err := client.Get("xxxxx")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(x))

}
