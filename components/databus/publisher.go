// Package databus 包注释
// @author wanlizhan
// @created 2024/7/16
package databus

import (
	"errors"
	"github.com/xxzhwl/gaia/framework/logImpl"
	"github.com/xxzhwl/gaia/g"
	"sync"
	"time"
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

func (p *Publisher) Publish() {
	p.once.Do(func() {
		p.publish()
	})
}

func (p *Publisher) publish() {
	for {
		data, err := p.publishFunc()
		if err != nil {
			return
		}
		if len(data) == 0 {
			return
		}
		p.locker.Lock()
		for _, subscriber := range p.subscribers {
			g.Go(func() {
				for _, datum := range data {
					if len(subscriber.dataChan) == subscriber.chanSize {
						p.logger.WarnF("订阅者%s已阻塞", subscriber.theme)
						continue
					}
					subscriber.dataChan <- datum
				}
			})
		}
		p.locker.Unlock()
		time.Sleep(p.publicInterval)
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
