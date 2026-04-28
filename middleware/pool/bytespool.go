// Package pool provides reusable sync.Pool helpers to minimise allocations
// in hot middleware paths.
package pool

import (
	"bytes"
	"sync"
)

var bufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}

// GetBuffer returns a reset *bytes.Buffer from the pool.
func GetBuffer() *bytes.Buffer {
	b := bufPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

// PutBuffer returns a buffer to the pool for reuse.
func PutBuffer(b *bytes.Buffer) { bufPool.Put(b) }
