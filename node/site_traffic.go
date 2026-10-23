package node

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/InazumaV/V2bX/api/panel"
	log "github.com/sirupsen/logrus"
)

const (
	siteBatchLimit = 5000
	siteQueueLimit = 12
)

type siteReportState struct {
	queue    []panel.SiteTrafficBatch
	disabled bool
	warned   bool
}

func (s *siteReportState) reset() {
	s.disabled = false
	s.warned = false
}

func (s *siteReportState) run(
	period func() (start, end int64),
	harvest func() ([]panel.SiteTraffic, error),
	send func(panel.SiteTrafficBatch) error,
	warn func(string),
) {
	if s.disabled {
		return
	}
	pending := append([]panel.SiteTrafficBatch(nil), s.queue...)
	kept, stop := sendAll(pending, send, warn, s)
	if stop {
		s.queue = nil
		return
	}
	if len(kept) > 0 {
		s.queue = trimSiteQueue(kept)
		return
	}

	items, err := harvest()
	if err != nil || len(items) == 0 {
		s.queue = nil
		return
	}
	start, end := period()
	fresh := splitSiteBatches(items, start, end)
	queued, stop := sendAll(fresh, send, warn, s)
	if stop {
		s.queue = nil
		return
	}
	s.queue = trimSiteQueue(queued)
}

func sendAll(batches []panel.SiteTrafficBatch, send func(panel.SiteTrafficBatch) error, warn func(string), s *siteReportState) (kept []panel.SiteTrafficBatch, stop bool) {
	for i, batch := range batches {
		err := send(batch)
		if err == nil {
			continue
		}
		if errors.Is(err, panel.ErrSiteTrafficRejected) {
			if warn != nil {
				warn("site traffic batch " + batch.BatchID + " dropped: " + err.Error())
			}
			continue
		}
		if errors.Is(err, panel.ErrSiteTrafficUnsupported) {
			s.disabled = true
			s.queue = nil
			if !s.warned && warn != nil {
				s.warned = true
				warn("site traffic endpoint not found, reporting disabled until reload")
			}
			return nil, true
		}
		return append(kept, batches[i:]...), false
	}
	return nil, false
}

func trimSiteQueue(q []panel.SiteTrafficBatch) []panel.SiteTrafficBatch {
	if len(q) <= siteQueueLimit {
		return q
	}
	drop := len(q) - siteQueueLimit
	log.Warnf("site traffic queue full, dropping %d oldest batches", drop)
	return q[drop:]
}

func splitSiteBatches(items []panel.SiteTraffic, start, end int64) []panel.SiteTrafficBatch {
	var out []panel.SiteTrafficBatch
	for len(items) > 0 {
		n := len(items)
		if n > siteBatchLimit {
			n = siteBatchLimit
		}
		out = append(out, panel.SiteTrafficBatch{
			BatchID:     newBatchID(),
			PeriodStart: start,
			PeriodEnd:   end,
			Items:       items[:n],
		})
		items = items[n:]
	}
	return out
}

func newBatchID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
