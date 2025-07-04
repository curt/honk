//
// Copyright (c) 2023 Ted Unangst <tedu@tedunangst.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

// A simple in memory, in process cache
package gencache

import (
	"sync"
	"time"
)

type entry[V any] struct {
	value  V
	expiry time.Time
}

func (ent *entry[V]) expired(now time.Time) bool {
	return !ent.expiry.IsZero() && ent.expiry.Before(now)
}

type Cache[K comparable, V any] struct {
	mtx      sync.Mutex
	cache    map[K]entry[V]
	waiters  map[K][]chan bool
	fillfn   func(K) (V, bool)
	duration time.Duration
	limit    int
	serial   int
	needgc   bool
}

type Options[K comparable, V any] struct {
	Fill         func(K) (V, bool)
	Invalidator  *Invalidator[K]
	Invalidators []*Invalidator[K]
	Duration     time.Duration
	Limit        int
}

func New[K comparable, V any](opts Options[K, V]) *Cache[K, V] {
	c := new(Cache[K, V])
	c.cache = make(map[K]entry[V])
	c.waiters = make(map[K][]chan bool)
	c.fillfn = opts.Fill
	invalidators := opts.Invalidators
	if inv := opts.Invalidator; inv != nil {
		invalidators = append(invalidators, inv)
	}
	for _, inv := range invalidators {
		inv.flushes = append(inv.flushes, func() {
			c.Flush()
		})
		inv.clears = append(inv.clears, func(key K) {
			c.Clear(key)
		})
	}
	c.duration = opts.Duration
	if c.duration > 0 && c.duration < time.Millisecond {
		c.duration *= time.Second
	}
	c.limit = opts.Limit
	if c.duration > 0 {
		go c.collect()
	}
	return c
}

func (c *Cache[K, V]) collect() {
	time.Sleep(c.duration * 2)
	now := time.Now()
	c.mtx.Lock()
	for k, ent := range c.cache {
		if ent.expired(now) {
			delete(c.cache, k)
		}
	}
	c.needgc = true
	c.mtx.Unlock()
}

func (c *Cache[K, V]) cleanup() {
	if c.duration > 0 {
		now := time.Now()
		n := 0
		for k, ent := range c.cache {
			if ent.expired(now) {
				delete(c.cache, k)
				n++
			}
		}
		if n > 0 {
			return
		}
	}
	i := 0
	for k := range c.cache {
		if i%5 == 0 {
			delete(c.cache, k)
		}
		i++
	}
}

func (c *Cache[K, V]) set(key K, value V) {
	if c.limit > 0 && len(c.cache) >= c.limit {
		c.cleanup()
	}
	var ent entry[V]
	ent.value = value
	if c.duration > 0 {
		ent.expiry = time.Now().Add(c.duration)
	}
	c.cache[key] = ent
	if c.needgc {
		c.needgc = false
		go c.collect()
	}
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	return c.GetWith(key, c.fillfn)
}
func (c *Cache[K, V]) GetWith(key K, fillfn func(K) (V, bool)) (V, bool) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for {
		ent, ok := c.cache[key]
		if ok && c.duration > 0 && ent.expired(time.Now()) {
			delete(c.cache, key)
			ok = false
		}
		if ok {
			return ent.value, ok
		}
		ok = c.tryfill(key, fillfn)
		if !ok {
			var v V
			return v, false
		}
	}
}

func (c *Cache[K, V]) checkbusy(key K) bool {
	waiters, busy := c.waiters[key]
	if busy {
		ready := make(chan bool)
		c.waiters[key] = append(waiters, ready)
		c.mtx.Unlock()

		<-ready
		close(ready)

		c.mtx.Lock()
	}
	return busy
}

func (c *Cache[K, V]) tryfill(key K, fillfn func(K) (V, bool)) bool {
	if c.checkbusy(key) {
		return true
	}
	c.waiters[key] = nil
	serial := c.serial
	c.mtx.Unlock()
	relock := true
	defer func() {
		if relock {
			c.mtx.Lock()
		}
	}()

	value, ok := fillfn(key)

	c.mtx.Lock()
	relock = false
	if ok && serial == c.serial {
		c.set(key, value)
	}
	waiters, _ := c.waiters[key]
	delete(c.waiters, key)
	for _, done := range waiters {
		done <- true
	}
	return ok
}

func (c *Cache[K, V]) Clear(key K) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	delete(c.cache, key)
	_, busy := c.waiters[key]
	if busy {
		c.serial++
	}
}
func (c *Cache[K, V]) Flush() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	c.cache = make(map[K]entry[V])
	c.serial++
}

type Invalidator[K comparable] struct {
	flushes []func()
	clears  []func(K)
}

func (inv *Invalidator[K]) Flush() {
	for _, f := range inv.flushes {
		f()
	}
}
func (inv *Invalidator[K]) Clear(key K) {
	for _, c := range inv.clears {
		c(key)
	}
}
