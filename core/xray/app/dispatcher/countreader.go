package dispatcher

import (
	"sync/atomic"
	"time"

	"github.com/xtls/xray-core/common/buf"
)

var _ buf.TimeoutReader = (*CounterReader)(nil)

type CounterReader struct {
	Reader  buf.TimeoutReader
	Counter *atomic.Int64
	Extra   *atomic.Int64
}

func (c *CounterReader) add(n int32) {
	if n <= 0 {
		return
	}
	c.Counter.Add(int64(n))
	if c.Extra != nil {
		c.Extra.Add(int64(n))
	}
}

func (c *CounterReader) ReadMultiBufferTimeout(time.Duration) (buf.MultiBuffer, error) {
	mb, err := c.Reader.ReadMultiBufferTimeout(time.Second)
	if err != nil {
		return nil, err
	}
	c.add(mb.Len())
	return mb, nil
}

func (c *CounterReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	mb, err := c.Reader.ReadMultiBuffer()
	if err != nil {
		return nil, err
	}
	c.add(mb.Len())
	return mb, nil
}
