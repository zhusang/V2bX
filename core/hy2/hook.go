package hy2

import (
	"sync"
	"sync/atomic"

	"github.com/InazumaV/V2bX/common/counter"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/apernet/hysteria/core/v2/server"
	"go.uber.org/zap"
)

var _ server.TrafficLogger = (*HookServer)(nil)

type HookServer struct {
	Tag                   string
	logger                *zap.Logger
	Counter               sync.Map
	ReportMinTrafficBytes int64
	siteEnabled           bool
	granularity           string
	maxSites              int
	streams               sync.Map // server.HyStream -> *hyStream
	userDelta             sync.Map // user id -> *deltaPair
}

type hyStream struct {
	mu      sync.Mutex
	stats   *server.StreamStats
	lastTx  uint64
	lastRx  uint64
	closed  atomic.Bool
	counted bool
}

type deltaPair struct {
	up   atomic.Int64
	down atomic.Int64
}

func (h *HookServer) TraceStream(stream server.HyStream, stats *server.StreamStats) {
	if !h.siteEnabled || stats == nil {
		return
	}
	h.streams.Store(stream, &hyStream{stats: stats})
}

func (h *HookServer) UntraceStream(stream server.HyStream) {
	if v, ok := h.streams.Load(stream); ok {
		v.(*hyStream).closed.Store(true)
	}
}

func (h *HookServer) LogTraffic(id string, tx, rx uint64) (ok bool) {
	var c interface{}
	var exists bool

	limiterinfo, err := limiter.GetLimiter(h.Tag)
	if err != nil {
		h.logger.Error("Get limiter error", zap.String("tag", h.Tag), zap.Error(err))
		return false
	}

	userLimit, ok := limiterinfo.UserLimitInfo.Load(format.UserTag(h.Tag, id))
	if ok {
		userlimitInfo := userLimit.(*limiter.UserLimitInfo)
		if userlimitInfo.OverLimit {
			userlimitInfo.OverLimit = false
			return false
		}
	}

	if c, exists = h.Counter.Load(h.Tag); !exists {
		c = counter.NewTrafficCounter()
		h.Counter.Store(h.Tag, c)
	}

	if tc, ok := c.(*counter.TrafficCounter); ok {
		tc.Rx(id, int(rx))
		tc.Tx(id, int(tx))
		if h.siteEnabled {
			d := h.delta(id)
			if tx > 0 {
				d.up.Add(int64(tx))
			}
			if rx > 0 {
				d.down.Add(int64(rx))
			}
		}
		return true
	}

	return false
}

func (s *HookServer) LogOnlineState(id string, online bool) {
}

func (h *HookServer) delta(id string) *deltaPair {
	if v, ok := h.userDelta.Load(id); ok {
		return v.(*deltaPair)
	}
	d := &deltaPair{}
	if v, loaded := h.userDelta.LoadOrStore(id, d); loaded {
		return v.(*deltaPair)
	}
	return d
}
