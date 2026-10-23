package counter

import (
	"net"
	"testing"

	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

func TestConnCounterAlsoUpdatesSite(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	col := sitestat.Enable("counter-site", sitestat.Options{Granularity: sitestat.GranularityHost, MaxSitesPerUser: 10})
	t.Cleanup(func() { sitestat.Disable("counter-site") })
	site := col.Open("user")
	site.Bind("example.com:443")
	storage := &TrafficStorage{}
	wrapped := NewConnCounter(client, storage, site).(*ConnCounter)
	go func() {
		buf := make([]byte, 4)
		_, _ = server.Read(buf)
		_, _ = server.Write([]byte("xyz"))
	}()
	if _, err := wrapped.Write([]byte("abcd")); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 3)
	if _, err := wrapped.Read(got); err != nil {
		t.Fatal(err)
	}
	if storage.DownCounter.Load() != 4 || site.Down.Load() != 4 {
		t.Fatalf("down user=%d site=%d", storage.DownCounter.Load(), site.Down.Load())
	}
	if storage.UpCounter.Load() != 3 || site.Up.Load() != 3 {
		t.Fatalf("up user=%d site=%d", storage.UpCounter.Load(), site.Up.Load())
	}
}

func TestConnCounterAccumulates(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	storage := &TrafficStorage{}
	wrapped := NewConnCounter(client, storage, nil).(*ConnCounter)

	errCh := make(chan error, 1)
	go func() {
		// Write first so the client's reads are not blocked on an empty pipe.
		if _, err := server.Write([]byte("abcdefg")); err != nil {
			errCh <- err
			return
		}
		buf := make([]byte, 8)
		if _, err := server.Read(buf[:2]); err != nil {
			errCh <- err
			return
		}
		if _, err := server.Read(buf[:4]); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	got := make([]byte, 3)
	if _, err := wrapped.Read(got); err != nil {
		t.Fatal(err)
	}
	got = make([]byte, 4)
	if _, err := wrapped.Read(got); err != nil {
		t.Fatal(err)
	}
	if storage.UpCounter.Load() != 7 {
		t.Fatalf("upload = %d, want 7", storage.UpCounter.Load())
	}
	if _, err := wrapped.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.Write([]byte("cdef")); err != nil {
		t.Fatal(err)
	}
	if storage.DownCounter.Load() != 6 {
		t.Fatalf("download = %d, want 6", storage.DownCounter.Load())
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}

	_, readFns := wrapped.UnwrapReader()
	_, writeFns := wrapped.UnwrapWriter()
	readFns[0](100)
	readFns[0](200)
	writeFns[0](10)
	if storage.UpCounter.Load() != 307 {
		t.Fatalf("upload after CountFunc = %d, want 307", storage.UpCounter.Load())
	}
	if storage.DownCounter.Load() != 16 {
		t.Fatalf("download after CountFunc = %d, want 16", storage.DownCounter.Load())
	}
}

func TestConnCounterBuffers(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	storage := &TrafficStorage{}
	wrapped := NewConnCounter(right, storage, nil).(N.ExtendedConn)

	done := make(chan struct{})
	go func() {
		defer close(done)
		b := buf.New()
		b.WriteString("hello")
		_ = bufio.NewExtendedConn(left).WriteBuffer(b)
		got := buf.New()
		_ = bufio.NewExtendedConn(left).ReadBuffer(got)
		got.Release()
	}()

	in := buf.New()
	defer in.Release()
	if err := wrapped.ReadBuffer(in); err != nil {
		t.Fatal(err)
	}
	if storage.UpCounter.Load() != 5 {
		t.Fatalf("upload = %d, want 5", storage.UpCounter.Load())
	}
	out := buf.New()
	out.WriteString("world!")
	if err := wrapped.WriteBuffer(out); err != nil {
		t.Fatal(err)
	}
	if storage.DownCounter.Load() != 6 {
		t.Fatalf("download = %d, want 6", storage.DownCounter.Load())
	}
	<-done
}

type stubPacket struct {
	N.PacketConn
	payload []byte
}

func (s *stubPacket) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	_, err := buffer.Write(s.payload)
	return M.Socksaddr{}, err
}

func (s *stubPacket) WritePacket(buffer *buf.Buffer, _ M.Socksaddr) error {
	buffer.Release()
	return nil
}

func (s *stubPacket) Close() error { return nil }

func TestPacketConnCounterAccumulates(t *testing.T) {
	col := sitestat.Enable("packet-site", sitestat.Options{Granularity: sitestat.GranularityHost, MaxSitesPerUser: 10})
	t.Cleanup(func() { sitestat.Disable("packet-site") })
	site := col.Open("user")
	site.Bind("1.2.3.4")
	storage := &TrafficStorage{}
	wrapped := NewPacketConnCounter(&stubPacket{payload: []byte("ping")}, storage, site).(*PacketConnCounter)
	b := buf.New()
	defer b.Release()
	if _, err := wrapped.ReadPacket(b); err != nil {
		t.Fatal(err)
	}
	if storage.UpCounter.Load() != 4 {
		t.Fatalf("upload = %d", storage.UpCounter.Load())
	}
	out := buf.New()
	out.WriteString("pong!")
	if err := wrapped.WritePacket(out, M.Socksaddr{}); err != nil {
		t.Fatal(err)
	}
	if storage.DownCounter.Load() != 5 {
		t.Fatalf("download = %d", storage.DownCounter.Load())
	}
	_, readFns := wrapped.UnwrapPacketReader()
	readFns[0](3)
	if storage.UpCounter.Load() != 7 || site.Up.Load() != 7 {
		t.Fatalf("upload user=%d site=%d", storage.UpCounter.Load(), site.Up.Load())
	}
	if site.Down.Load() != 5 {
		t.Fatalf("site download = %d, want 5", site.Down.Load())
	}
	got := col.Harvest()
	if len(got) != 1 || got[0].Site != "1.2.3.4" || got[0].Upload != 7 || got[0].Download != 5 {
		t.Fatalf("harvest = %+v", got)
	}
}
