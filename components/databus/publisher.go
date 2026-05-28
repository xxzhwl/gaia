// Package databus 包注释
// @author wanlizhan
// @created 2024/7/16
// @updated 2026-05-28  增加 Stop 防止 goroutine 泄漏 + 移除每次 publish 都 spawn 临时 goroutine
package databus

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xxzhwl/gaia"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/g"
)

var publishers map[string]*Publisher

var publisherLock sync.RWMutex

func init() {
	publishers = make(map[string]*Publisher)
}

type Publisher struct {
	theme string

	publishFunc func() ([][]byte, error)

	publicInterval time.Duration

	subscribers map[string]*Subscriber

	locker sync.RWMutex

	once sync.Once

	// stop 用于通知 publish goroutine 退出，避免常驻 goroutine 泄漏
	stop chan struct{}
	// done 在 publish goroutine 退出后被 close，便于 Stop 同步等待
	done chan struct{}

	logger *logImpl.DefaultLogger
}

func NewPublisher(theme string, pubFunc func() ([][]byte, error)) (*Publisher, error) {
	if len(theme) == 0 {
		return nil, errors.New("发布者不允许无theme")
	}
	p := &Publisher{
		theme:          theme,
		publicInterval: time.Second,
		publishFunc:    pubFunc,
		subscribers:    map[string]*Subscriber{},
		stop:           make(chan struct{}),
		done:           make(chan struct{}),
		logger:         logImpl.NewDefaultLogger().SetTitle("databus_pub_" + theme)}

	publisherLock.Lock()
	defer publisherLock.Unlock()
	if _, ok := publishers[theme]; ok {
		return nil, errors.New("已有同theme的发布者存在")
	}
	publishers[theme] = p
	return p, nil
}

func GetPublisher(theme string) *Publisher {
	publisherLock.RLock()
	defer publisherLock.RUnlock()
	if v, ok := publishers[theme]; ok {
		return v
	}
	return nil
}

// Publish 启动后台分发循环（幂等，多次调用只会启动一次）
func (p *Publisher) Publish() {
	p.once.Do(func() {
		g.Go(func() {
			p.publish()
			close(p.done)
		})
	})
}

// Stop 停止发布并从全局注册表中移除该 publisher，避免 goroutine 与注册表泄漏
func (p *Publisher) Stop(ctx context.Context) error {
	select {
	case <-p.stop:
		// already stopped
	default:
		close(p.stop)
	}

	// 从全局注册表移除
	publisherLock.Lock()
	delete(publishers, p.theme)
	publisherLock.Unlock()

	// 等 publish goroutine 真正退出
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sleepOrStop 在等待 d 期间响应 stop 信号，避免无谓的 1 秒延迟
func (p *Publisher) sleepOrStop(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-p.stop:
		return true
	case <-t.C:
		return false
	}
}

func (p *Publisher) publish() {
	for {
		select {
		case <-p.stop:
			return
		default:
		}

		data, err := p.publishFunc()
		if err != nil {
			p.emitMqLog("produce", "", "", 0, time.Now(), err, gaia.LogErrorLevel)
			if p.sleepOrStop(p.publicInterval) {
				return
			}
			continue
		}
		if len(data) == 0 {
			if p.sleepOrStop(p.publicInterval) {
				return
			}
			continue
		}

		// 直接同步分发：每个订阅者通道写入是 select-default 非阻塞操作，
		// 不再为每次 publish 都 spawn N 个一次性 goroutine。
		start := time.Now()
		bodySize := 0
		delivered := 0
		dropped := 0
		p.locker.RLock()
		for _, sub := range p.subscribers {
			for _, datum := range data {
				bodySize += len(datum)
				select {
				case sub.dataChan <- datum:
					delivered++
				default:
					dropped++
				}
			}
		}
		p.locker.RUnlock()
		if dropped > 0 {
			p.emitMqLog("produce", "", "", bodySize, start, fmt.Errorf("%d subscriber messages dropped", dropped), gaia.LogWarnLevel)
		}
		if delivered > 0 || dropped > 0 {
			p.emitMqLog("produce", "", "", bodySize, start, nil, gaia.LogInfoLevel)
		}

		if p.sleepOrStop(p.publicInterval) {
			return
		}
	}
}

func (p *Publisher) AddSubscriber(subscriber *Subscriber) {
	p.locker.Lock()
	p.subscribers[subscriber.theme] = subscriber
	p.logger.InfoF("订阅者%s加入", subscriber.theme)
	p.locker.Unlock()
}

func (p *Publisher) RemoveSubscriber(subTheme string) {
	p.locker.Lock()
	delete(p.subscribers, subTheme)
	p.logger.InfoF("订阅者%s移除", subTheme)
	p.locker.Unlock()
}

func (p *Publisher) emitMqLog(direction, key, consumerGroup string, bodySize int, start time.Time, err error, level gaia.LogLevel) {
	if p.logger == nil {
		return
	}
	end := time.Now()
	body := logImpl.MqLogBaseModel{
		Backend:        "databus",
		Direction:      direction,
		Topic:          p.theme,
		Key:            key,
		ConsumerGroup:  consumerGroup,
		BodySize:       bodySize,
		StartTime:      start.Format(gaia.DateTimeMillsFormat),
		EndTime:        end.Format(gaia.DateTimeMillsFormat),
		Duration:       float64(end.Sub(start).Milliseconds()),
		StartTimeStamp: start.UnixMilli(),
		EndTimeStamp:   end.UnixMilli(),
	}
	content := fmt.Sprintf("%s %s", direction, p.theme)
	if err != nil {
		body.Err = err.Error()
		content += " err=" + err.Error()
	}
	p.logger.MqLog(level, content)
	p.logger.MqLogBody(level, content, body)
}
