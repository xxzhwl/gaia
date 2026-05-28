// @author wanlizhan
// @created 2026-05-28
package ratelimiter

import (
	"context"
	"errors"
	"testing"
)

// stubLimiter 测试用 Limiter
type stubLimiter struct {
	allow bool
	err   error
	calls int
}

func (s *stubLimiter) AllowCtx(_ context.Context) (bool, error) {
	s.calls++
	return s.allow, s.err
}

func TestMultiLimiter_AllPass(t *testing.T) {
	a := &stubLimiter{allow: true}
	b := &stubLimiter{allow: true}
	m := NewMultiLimiter(a, b)
	ok, err := m.AllowCtx(context.Background())
	if err != nil || !ok {
		t.Fatalf("全部通过应放行, ok=%v err=%v", ok, err)
	}
	if a.calls != 1 || b.calls != 1 {
		t.Fatalf("应全部评估，calls=%d/%d", a.calls, b.calls)
	}
}

func TestMultiLimiter_OneDeny(t *testing.T) {
	a := &stubLimiter{allow: true}
	b := &stubLimiter{allow: false}
	c := &stubLimiter{allow: true}
	m := NewMultiLimiter(a, b, c)
	ok, err := m.AllowCtx(context.Background())
	if err != nil {
		t.Fatalf("拒绝不应携带 error，实际 %v", err)
	}
	if ok {
		t.Fatal("有一个拒绝应整体拒绝")
	}
	if a.calls != 1 || b.calls != 1 {
		t.Fatal("a/b 应被评估")
	}
	if c.calls != 0 {
		t.Fatal("b 拒绝后 c 不应被评估（短路）")
	}
}

func TestMultiLimiter_Error(t *testing.T) {
	wantErr := errors.New("backend down")
	a := &stubLimiter{allow: true}
	b := &stubLimiter{err: wantErr}
	c := &stubLimiter{allow: true}
	m := NewMultiLimiter(a, b, c)
	ok, err := m.AllowCtx(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("应返回首个 error，实际 %v", err)
	}
	if ok {
		t.Fatal("出错应返回 false")
	}
	if c.calls != 0 {
		t.Fatal("出错应短路")
	}
}

func TestMultiLimiter_Empty(t *testing.T) {
	m := NewMultiLimiter()
	ok, err := m.AllowCtx(context.Background())
	if err != nil || !ok {
		t.Fatal("空 MultiLimiter 应直接放行")
	}
	if m.Len() != 0 {
		t.Fatal("Len 应为 0")
	}
}

func TestMultiLimiter_NilFiltered(t *testing.T) {
	a := &stubLimiter{allow: true}
	m := NewMultiLimiter(nil, a, nil)
	if m.Len() != 1 {
		t.Fatalf("nil 应被过滤，Len=%d", m.Len())
	}
	ok, _ := m.AllowCtx(context.Background())
	if !ok {
		t.Fatal("应放行")
	}
}

func TestMultiLimiter_Add(t *testing.T) {
	m := NewMultiLimiter()
	m.Add(&stubLimiter{allow: true}).Add(nil).Add(&stubLimiter{allow: true})
	if m.Len() != 2 {
		t.Fatalf("Add 应忽略 nil，Len=%d", m.Len())
	}
}

func TestMultiLimiter_RealCombination(t *testing.T) {
	// 单用户每秒 1 次 + 全局每秒 1000 次
	user := NewLocalLimiter(1, 1)
	global := NewLocalLimiter(1000, 1000)
	m := NewMultiLimiter(user, global)

	// 第一次应放行
	ok, _ := m.AllowCtx(context.Background())
	if !ok {
		t.Fatal("第一次应放行")
	}
	// 第二次紧接着，user 被拒绝，整体拒绝
	ok, _ = m.AllowCtx(context.Background())
	if ok {
		t.Fatal("用户限流命中，整体应拒绝")
	}
}
