package hy2

import (
	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/sitestat"
)

func (h *HookServer) harvest(uidOf map[string]int) []panel.SiteTraffic {
	if h == nil || !h.siteEnabled {
		return nil
	}
	type pair struct{ up, down int64 }
	tcp := make(map[string]pair)
	var entries []sitestat.Entry

	h.streams.Range(func(key, value any) bool {
		rec := value.(*hyStream)
		stats := rec.stats
		closed := rec.closed.Load()
		rec.mu.Lock()
		tx, rx := stats.Tx.Load(), stats.Rx.Load()
		var up, down int64
		if tx >= rec.lastTx {
			up = int64(tx - rec.lastTx)
		}
		if rx >= rec.lastRx {
			down = int64(rx - rec.lastRx)
		}
		rec.lastTx, rec.lastRx = tx, rx
		counted := rec.counted
		rec.mu.Unlock()

		site := stats.HookedReqAddr.Load()
		if site == "" {
			site = stats.ReqAddr.Load()
		}
		if site == "" && !closed {
			return true
		}
		site = sitestat.Normalize(site, h.granularity)
		if site == "" {
			site = sitestat.SiteUnknown
		}
		var conns int64
		if site != sitestat.SiteUnknown && !counted {
			rec.mu.Lock()
			if !rec.counted {
				rec.counted = true
				conns = 1
			}
			rec.mu.Unlock()
		}
		user := stats.AuthID
		entries = append(entries, sitestat.Entry{User: user, Site: site, Upload: up, Download: down, Conns: conns})
		prev := tcp[user]
		tcp[user] = pair{prev.up + up, prev.down + down}
		if closed {
			h.streams.Delete(key)
		}
		return true
	})

	h.userDelta.Range(func(key, value any) bool {
		id := key.(string)
		d := value.(*deltaPair)
		up, down := d.up.Swap(0), d.down.Swap(0)
		covered := tcp[id]
		udpUp, udpDown := up-covered.up, down-covered.down
		if udpUp < 0 {
			udpUp = 0
		}
		if udpDown < 0 {
			udpDown = 0
		}
		if udpUp == 0 && udpDown == 0 {
			return true
		}
		entries = append(entries, sitestat.Entry{
			User: id, Site: sitestat.SiteUDP, Upload: udpUp, Download: udpDown,
		})
		return true
	})

	maxSites := h.maxSites
	if maxSites <= 0 {
		maxSites = 200
	}
	entries = sitestat.LimitEntries(entries, maxSites)
	out := make([]panel.SiteTraffic, 0, len(entries))
	for _, e := range entries {
		uid := uidOf[e.User]
		if uid == 0 {
			continue
		}
		out = append(out, panel.SiteTraffic{
			UID: uid, Site: e.Site, Upload: e.Upload, Download: e.Download, Conns: int(e.Conns),
		})
	}
	return out
}
