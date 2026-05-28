package mongo

import (
	"context"
	"os"
	"testing"
	"time"
)

func getTestURI() string {
	if uri := os.Getenv("GAIA_TEST_MONGO_URI"); uri != "" {
		return uri
	}
	return "mongodb://localhost:27017"
}

func TestNewClient_MissingURI(t *testing.T) {
	_, err := NewClient(Config{Database: "test"})
	if err == nil {
		t.Fatal("缺少 URI 应该报错")
	}
}

func TestNewClient_MissingDatabase(t *testing.T) {
	_, err := NewClient(Config{URI: "mongodb://localhost:27017"})
	if err == nil {
		t.Fatal("缺少 Database 应该报错")
	}
}

func TestNewClient_DefaultTimeout(t *testing.T) {
	cfg := Config{URI: "mongodb://localhost:27017", Database: "test"}
	if cfg.ConnectTimeout != 0 {
		t.Fatal("未设置时应为零值")
	}
}

// TestIntegration_CRUD 集成测试：需要本地 MongoDB 运行
func TestIntegration_CRUD(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试（设置 GAIA_TEST_INTEGRATION=1 启用）")
	}

	cli, err := NewClient(Config{
		URI:            getTestURI(),
		Database:       "gaia_test",
		ConnectTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer cli.Close()

	ctx := context.Background()
	coll := "test_crud"

	// 清理
	cli.Collection(coll).Drop(ctx)

	// Insert
	res, err := cli.InsertOne(ctx, coll, map[string]any{"name": "alice", "age": 25})
	if err != nil {
		t.Fatalf("InsertOne 失败: %v", err)
	}
	if res.InsertedID == nil {
		t.Fatal("InsertedID 为 nil")
	}

	// Find
	var found map[string]any
	err = cli.FindOne(ctx, coll, map[string]any{"name": "alice"}, &found)
	if err != nil {
		t.Fatalf("FindOne 失败: %v", err)
	}
	if found["name"] != "alice" {
		t.Fatalf("期望 alice，得到 %v", found["name"])
	}

	// Count
	count, err := cli.CountDocuments(ctx, coll, map[string]any{})
	if err != nil {
		t.Fatalf("CountDocuments 失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("期望 1 条，得到 %d", count)
	}

	// Update
	_, err = cli.UpdateOne(ctx, coll, map[string]any{"name": "alice"}, map[string]any{"$set": map[string]any{"age": 26}})
	if err != nil {
		t.Fatalf("UpdateOne 失败: %v", err)
	}

	// Delete
	delRes, err := cli.DeleteOne(ctx, coll, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("DeleteOne 失败: %v", err)
	}
	if delRes.DeletedCount != 1 {
		t.Fatal("应该删除 1 条")
	}

	// 清理
	cli.Collection(coll).Drop(ctx)
}

func TestGetDB(t *testing.T) {
	if os.Getenv("GAIA_TEST_INTEGRATION") == "" {
		t.Skip("跳过集成测试")
	}
	cli, err := NewClient(Config{URI: getTestURI(), Database: "gaia_test"})
	if err != nil {
		t.Skip("MongoDB 不可用")
	}
	defer cli.Close()

	if cli.GetDB() == nil {
		t.Fatal("GetDB 返回 nil")
	}
	if cli.Database("other") == nil {
		t.Fatal("Database 返回 nil")
	}
	if cli.GetCli() == nil {
		t.Fatal("GetCli 返回 nil")
	}
}
