package hy2

import (
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/limiter"
	"github.com/apernet/hysteria/core/v2/server"
	"go.uber.org/zap"
)

func TestHarvestTCPAndUDP(t *testing.T) {
	limiter.Init()
	const tag = "hy-site"
	limiter.AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{Id: 9, Uuid: "user-a"}}, map[int]int{})
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })

	h := &HookServer{
		Tag:         tag,
		logger:      zap.NewNop(),
		siteEnabled: true,
		granularity: sitestat.GranularitySite,
		maxSites:    200,
	}
	video := &server.StreamStats{AuthID: "user-a"}
	video.ReqAddr.Store("video.googlevideo.com:443")
	api := &server.StreamStats{AuthID: "user-a"}
	api.HookedReqAddr.Store("api.example.com:443")
	h.streams.Store(videoKey{1}, &hyStream{stats: video})
	apiRec := &hyStream{stats: api}
	h.streams.Store(videoKey{2}, apiRec)
	apiRec.closed.Store(true)

	video.Tx.Add(3_000_000)
	video.Rx.Add(4_000_000)
	api.Tx.Add(1_000_000)
	if !h.LogTraffic("user-a", 3_000_000, 4_000_000) ||
		!h.LogTraffic("user-a", 1_000_000, 0) ||
		!h.LogTraffic("user-a", 2_000_000, 500_000) {
		t.Fatal("LogTraffic rejected")
	}

	got := h.harvest(map[string]int{"user-a": 9})
	sum := map[string][2]int64{}
	for _, e := range got {
		if e.UID != 9 {
			t.Fatalf("uid = %d", e.UID)
		}
		cur := sum[e.Site]
		sum[e.Site] = [2]int64{cur[0] + e.Upload, cur[1] + e.Download}
	}
	if sum["googlevideo.com"] != [2]int64{3_000_000, 4_000_000} {
		t.Fatalf("googlevideo = %v (tx should be upload)", sum["googlevideo.com"])
	}
	if sum["example.com"] != [2]int64{1_000_000, 0} {
		t.Fatalf("example = %v", sum["example.com"])
	}
	if sum[sitestat.SiteUDP] != [2]int64{2_000_000, 500_000} {
		t.Fatalf("udp = %v", sum[sitestat.SiteUDP])
	}
}

type videoKey struct{ n int }
