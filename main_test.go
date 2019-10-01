package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
)

const redisAddr = "127.0.0.1:6379"

func getConn() redis.Conn {
	conn, err := redis.Dial("tcp", redisAddr)
	if err != nil {
		panic(err)
	}
	return conn
}

func getPool() *redis.Pool {
	return &redis.Pool{
		MaxIdle:     128,
		MaxActive:   128,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			return redis.Dial("tcp", redisAddr)
		},
	}
}

func BenchmarkPublish01(b *testing.B) {
	pool := getPool()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := pool.Get()
		_, err := c.Do("publish", "test", "boom")
		if err != nil {
			c.Close()
			b.Fatal(err)
		}
		c.Close()
	}
	b.ReportAllocs()
}

func BenchmarkPublish02(b *testing.B) {
	c := getConn()
	defer c.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := c.Do("publish", "test", "boom")
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
}

func BenchmarkPublish03(b *testing.B) {
	pool := getPool()
	b.ResetTimer()
	b.SetParallelism(32)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c := pool.Get()
			_, err := c.Do("publish", "test", "boom")
			if err != nil {
				c.Close()
				b.Fatal(err)
			}
			c.Close()
		}
	})
	b.ReportAllocs()
}

func BenchmarkPublish04(b *testing.B) {
	pool := getPool()
	defer pool.Close()
	b.ResetTimer()
	b.SetParallelism(32)
	b.RunParallel(func(pb *testing.PB) {
		c := pool.Get()
		defer c.Close()
		for pb.Next() {
			_, err := c.Do("publish", "test", "boom")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportAllocs()
}

type publication struct {
	channel string
	data    string
	errCh   chan error
}

type publisher struct {
	b     *testing.B
	pubCh chan publication
	pubs  []publication
}

func newPublisher(b *testing.B, useSlice bool) *publisher {
	p := &publisher{
		b:     b,
		pubCh: make(chan publication),
	}
	go func() {
		for {
			for i := 0; i < 1; i++ {
				if useSlice {
					p.runPublishRoutineSlice(make([]publication, 512))
				} else {
					p.runPublishRoutine()
				}
			}
		}
	}()
	return p
}

func (p *publisher) runPublishRoutine() {
	conn := getConn()
	defer conn.Close()
	for {
		select {
		case pub := <-p.pubCh:
			pubs := []publication{pub}
			conn.Send("publish", pub.channel, pub.data)
		loop:
			for i := 0; i < 512; i++ {
				select {
				case pub := <-p.pubCh:
					pubs = append(pubs, pub)
					conn.Send("publish", pub.channel, pub.data)
				default:
					break loop
				}
			}
			err := conn.Flush()
			if err != nil {
				for i := 0; i < len(pubs); i++ {
					pubs[i].errCh <- err
				}
				continue
			}
			for i := 0; i < len(pubs); i++ {
				_, err := conn.Receive()
				pubs[i].errCh <- err
			}
		}
	}
}

func (p *publisher) runPublishRoutineSlice(pubs []publication) {
	conn := getConn()
	defer conn.Close()
	for {
		n := 0
		select {
		case pub := <-p.pubCh:
			pubs[0] = pub
			n++
			conn.Send("publish", pub.channel, pub.data)
		loop:
			for i := 0; i < cap(pubs)-1; i++ {
				select {
				case pub := <-p.pubCh:
					pubs[n] = pub
					n++
					conn.Send("publish", pub.channel, pub.data)
				default:
					break loop
				}
			}
			err := conn.Flush()
			if err != nil {
				for i := 0; i < n; i++ {
					pubs[i].errCh <- err
				}
				continue
			}
			for i := 0; i < n; i++ {
				_, err := conn.Receive()
				pubs[i].errCh <- err
			}
		}
	}
}

var chPool = sync.Pool{
	New: func() interface{} {
		ch := make(chan error, 1)
		return ch
	},
}

func getErrCh() chan error {
	return chPool.Get().(chan error)
}

func putErrCh(ch chan error) {
	select {
	case <-ch:
	default:
	}
	chPool.Put(ch)
}

func (p *publisher) publish(channel, data string) error {
	errCh := make(chan error, 1)
	pub := publication{
		channel: channel,
		data:    data,
		errCh:   errCh,
	}
	select {
	case p.pubCh <- pub:
	default:
		select {
		case p.pubCh <- pub:
		case <-time.After(time.Second):
			return errors.New("timeout")
		}
	}
	return <-errCh
}

func (p *publisher) publishPool(channel, data string) error {
	errCh := getErrCh()
	defer putErrCh(errCh)
	pub := publication{
		channel: channel,
		data:    data,
		errCh:   errCh,
	}
	select {
	case p.pubCh <- pub:
	default:
		select {
		case p.pubCh <- pub:
		case <-time.After(time.Second):
			return errors.New("timeout")
		}
	}
	return <-errCh
}

var timerPool sync.Pool

func getTimer(d time.Duration) *time.Timer {
	v := timerPool.Get()
	if v == nil {
		return time.NewTimer(d)
	}
	tm := v.(*time.Timer)
	if tm.Reset(d) {
		panic("Received an active timer from the pool!")
	}
	return tm
}

func putTimer(tm *time.Timer) {
	if !tm.Stop() {
		// Do not reuse timer that has been already stopped.
		// See https://groups.google.com/forum/#!topic/golang-nuts/-8O3AknKpwk
		return
	}
	timerPool.Put(tm)
}

func (p *publisher) publishPoolTimer(channel, data string) error {
	errCh := getErrCh()
	pub := publication{
		channel: channel,
		data:    data,
		errCh:   errCh,
	}
	select {
	case p.pubCh <- pub:
	default:
		timer := getTimer(time.Second)
		defer putTimer(timer)
		select {
		case p.pubCh <- pub:
		case <-timer.C:
			putErrCh(errCh)
			return errors.New("timeout")
		}
	}
	val := <-errCh
	putErrCh(errCh)
	return val
}

func BenchmarkPublish05(b *testing.B) {
	publisher := newPublisher(b, false)
	b.ResetTimer()
	b.SetParallelism(128)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := publisher.publish("test", "boom")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportAllocs()
}

func BenchmarkPublish06(b *testing.B) {
	publisher := newPublisher(b, false)
	b.ResetTimer()
	b.SetParallelism(128)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := publisher.publishPool("test", "boom")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportAllocs()
}

func BenchmarkPublish07(b *testing.B) {
	publisher := newPublisher(b, false)
	b.ResetTimer()
	b.SetParallelism(128)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := publisher.publishPoolTimer("test", "boom")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportAllocs()
}

func BenchmarkPublish08(b *testing.B) {
	publisher := newPublisher(b, true)
	b.ResetTimer()
	b.SetParallelism(128)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			err := publisher.publishPoolTimer("test", "boom")
			if err != nil {
				b.Fatal(err)
			}
		}
	})
	b.ReportAllocs()
}
