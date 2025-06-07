//
// Copyright (c) 2019 Ted Unangst <tedu@tedunangst.com>
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

// The gate package provides rate limiters and serializers.
package gate

import (
	"sync"
)

// Limiter limits the number of concurrent outstanding operations.
// Typical usage: limiter.Start(); defer limiter.Finish()
type Limiter struct {
	maxout  int
	numout  int
	waiting int
	lock    sync.Mutex
	bell    *sync.Cond
	busy    map[interface{}]bool
}

// Create a new Limiter with maxout operations
func NewLimiter(maxout int) *Limiter {
	l := new(Limiter)
	l.maxout = maxout
	l.bell = sync.NewCond(&l.lock)
	l.busy = make(map[interface{}]bool)
	return l
}

// Wait for an opening, then return when ready.
func (l *Limiter) Start() {
	l.lock.Lock()
	for l.numout >= l.maxout {
		l.waiting++
		l.bell.Wait()
		l.waiting--
	}
	l.numout++
	l.lock.Unlock()
}

// Wait for an opening, then return when ready.
func (l *Limiter) StartKey(key interface{}) {
	l.lock.Lock()
	for l.numout >= l.maxout || l.busy[key] {
		l.waiting++
		l.bell.Wait()
		l.waiting--
	}
	l.busy[key] = true
	l.numout++
	l.lock.Unlock()
}

// Free an opening after finishing.
func (l *Limiter) Finish() {
	l.lock.Lock()
	l.numout--
	l.bell.Broadcast()
	l.lock.Unlock()
}

// Free an opening after finishing.
func (l *Limiter) FinishKey(key interface{}) {
	l.lock.Lock()
	delete(l.busy, key)
	l.numout--
	l.bell.Broadcast()
	l.lock.Unlock()
}

// Return current outstanding count
func (l *Limiter) Outstanding() int {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.numout
}

// Return current waiting count
func (l *Limiter) Waiting() int {
	l.lock.Lock()
	defer l.lock.Unlock()
	return l.waiting
}

type result struct {
	res interface{}
	err error
}
