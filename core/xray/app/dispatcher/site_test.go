package dispatcher

import (
	"context"
	"testing"

	"github.com/InazumaV/V2bX/api/panel"
	"github.com/InazumaV/V2bX/common/format"
	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/InazumaV/V2bX/conf"
	"github.com/InazumaV/V2bX/limiter"
	xdispatch "github.com/xtls/xray-core/app/dispatcher"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/session"
)

func TestGetLinkUserCounterBaseline(t *testing.T) {
	limiter.Init()
	const tag = "baseline-node"
	const uuid = "11111111-1111-1111-1111-111111111111"
	email := format.UserTag(tag, uuid)
	limiter.AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{Id: 7, Uuid: uuid}}, map[int]int{})
	t.Cleanup(func() { limiter.DeleteLimiter(tag) })

	ctx := session.ContextWithInbound(context.Background(), &session.Inbound{
		Tag:    tag,
		User:   &protocol.MemoryUser{Email: email},
		Source: net.TCPDestination(net.ParseAddress("10.0.0.8"), 40000),
	})
	in, out, _, siteConn, err := (&DefaultDispatcher{}).getLink(ctx, net.Network_TCP)
	if err != nil {
		t.Fatal(err)
	}
	if siteConn != nil {
		t.Fatal("site conn created while collection is disabled")
	}
	sw, ok := in.Writer.(*xdispatch.SizeStatWriter)
	if !ok {
		t.Fatalf("inbound writer is %T, want SizeStatWriter", in.Writer)
	}
	if _, wrapped := sw.Writer.(*halfCloseWriter); wrapped {
		t.Fatal("writer wrapped while collection is disabled")
	}
	sw.Counter.Add(100)
	if sw.Counter.Value() != 100 {
		t.Fatalf("user counter = %d, want 100", sw.Counter.Value())
	}
	_ = out
}

func TestSiteWritersForwardInterrupt(t *testing.T) {
	col := sitestat.Enable("interrupt-node", sitestat.Options{Granularity: sitestat.GranularityHost, MaxSitesPerUser: 10})
	t.Cleanup(func() { sitestat.Disable("interrupt-node") })
	conn := col.Open("u")
	inner := &recordWriter{}
	w := halfWriter(inner, conn, sitestat.HalfUp)
	if _, ok := w.(common.Interruptible); !ok {
		t.Fatal("half writer does not forward Interrupt")
	}
	common.Interrupt(w)
	if !inner.interrupted || inner.closed {
		t.Fatalf("inner interrupted=%v closed=%v, want interrupt only", inner.interrupted, inner.closed)
	}
	common.Interrupt(halfWriter(&recordWriter{}, conn, sitestat.HalfDown))
	if got := col.Harvest(); len(got) != 0 {
		t.Fatalf("closed idle conn should be dropped silently, got %+v", got)
	}
	if again := col.Harvest(); len(again) != 0 {
		t.Fatalf("conn not removed after both halves interrupted: %+v", again)
	}
}

type recordWriter struct {
	interrupted, closed bool
}

func (w *recordWriter) WriteMultiBuffer(mb buf.MultiBuffer) error {
	buf.ReleaseMulti(mb)
	return nil
}

func (w *recordWriter) Close() error {
	w.closed = true
	return nil
}

func (w *recordWriter) Interrupt() {
	w.interrupted = true
}

func TestSplicePathCountsUserAndSite(t *testing.T) {
	limiter.Init()
	const tag = "site-node"
	const uuid = "22222222-2222-2222-2222-222222222222"
	email := format.UserTag(tag, uuid)
	limiter.AddLimiter(tag, &conf.LimitConfig{}, []panel.UserInfo{{Id: 9, Uuid: uuid}}, map[int]int{})
	t.Cleanup(func() {
		limiter.DeleteLimiter(tag)
		sitestat.Disable(tag)
	})
	sitestat.Enable(tag, sitestat.Options{Granularity: sitestat.GranularityHost, MaxSitesPerUser: 10})

	ctx := session.ContextWithInbound(context.Background(), &session.Inbound{
		Tag:    tag,
		User:   &protocol.MemoryUser{Email: email},
		Source: net.TCPDestination(net.ParseAddress("10.0.0.9"), 40001),
	})
	in, out, _, siteConn, err := (&DefaultDispatcher{}).getLink(ctx, net.Network_TCP)
	if err != nil {
		t.Fatal(err)
	}
	if siteConn == nil {
		t.Fatal("expected site conn")
	}
	sw := in.Writer.(*xdispatch.SizeStatWriter)
	sw.Counter.Add(100)
	if sw.Counter.Value() != 100 {
		t.Fatalf("user counter = %d, want 100", sw.Counter.Value())
	}
	if siteConn.Up.Load() != 100 {
		t.Fatalf("site upload = %d, want 100", siteConn.Up.Load())
	}

	common.Close(in.Writer)
	common.Close(out.Writer)
	got := sitestat.Get(tag).Harvest()
	if len(got) != 1 || got[0].Site != sitestat.SiteUnknown || got[0].Upload != 100 {
		t.Fatalf("harvest = %+v", got)
	}
	if again := sitestat.Get(tag).Harvest(); len(again) != 0 {
		t.Fatalf("connection was not removed: %+v", again)
	}
}
