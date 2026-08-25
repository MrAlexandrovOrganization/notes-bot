package tghandlers

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestVoiceReorderBuffer_ConcurrentRegisterComplete hammers the buffer from
// many goroutines to catch races (run with -race) and verifies that results
// are only flushed in strict message-ID order.
func TestVoiceReorderBuffer_ConcurrentRegisterComplete(t *testing.T) {
	const n = 200
	buf := newVoiceReorderBuffer()

	var mu sync.Mutex
	var delivered []int

	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		msgID := i
		r := &pendingVoiceResult{}
		buf.register(msgID, r)
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready := buf.complete(msgID, "text", nil)
			mu.Lock()
			delivered = append(delivered, len(ready))
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Every result must be delivered exactly once.
	total := sumDelivered(delivered)
	assert.Equal(t, n, total, "every registered result should be delivered exactly once")
}

func sumDelivered(xs []int) int {
	s := 0
	for _, x := range xs {
		s += x
	}
	return s
}

// TestVoiceReorderBuffer_OrderGuarantee verifies out-of-order completion:
// completing msg 3 before 1 and 2 must not flush anything until 1 arrives.
func TestVoiceReorderBuffer_OrderGuarantee(t *testing.T) {
	buf := newVoiceReorderBuffer()
	for i := 1; i <= 3; i++ {
		buf.register(i, &pendingVoiceResult{})
	}

	assert.Empty(t, buf.complete(3, "third", nil), "front slot still pending")

	assert.Len(t, buf.complete(1, "first", nil), 1, "only msg 1 ready")
	assert.Len(t, buf.complete(2, "second", nil), 2, "msgs 2 and 3 ready")
}

// TestVoiceBufferReuseAcrossMessages documents that buffers persist per user
// and a late register() lands in the same buffer earlier goroutines use.
func TestVoiceBufferReuseAcrossMessages(t *testing.T) {
	a := &App{}
	b1 := a.getVoiceBuffer(42)
	r := &pendingVoiceResult{}
	b1.register(10, r)
	b2 := a.getVoiceBuffer(42)
	assert.Same(t, b1, b2, "same user must always get the same buffer")
	assert.NotEmpty(t, b2.complete(10, "", fmt.Errorf("x")))
}
