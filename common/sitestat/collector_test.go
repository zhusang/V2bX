package sitestat

import (
	"sync"
	"testing"
)

func TestHarvestLongConnectionAcrossPeriods(t *testing.T) {
	c := &Collector{opt: Options{Granularity: GranularitySite, MaxSitesPerUser: 200}}
	conn := c.Open("user")
	conn.Bind("rr3.googlevideo.com:443")
	conn.Down.Add(1 << 30)
	first := c.Harvest()
	if len(first) != 1 || first[0].Download != 1<<30 || first[0].Conns != 1 || first[0].Site != "googlevideo.com" {
		t.Fatalf("period 1: %+v", first)
	}
	conn.Down.Add(2 << 30)
	second := c.Harvest()
	if len(second) != 1 || second[0].Download != 2<<30 || second[0].Conns != 0 {
		t.Fatalf("period 2: %+v", second)
	}
}

func TestHarvestSkipsUnboundUntilClose(t *testing.T) {
	c := &Collector{opt: Options{MaxSitesPerUser: 200}}
	conn := c.Open("user")
	conn.Up.Add(10)
	if got := c.Harvest(); len(got) != 0 {
		t.Fatalf("sniffing connection was harvested: %+v", got)
	}
	if conn.Up.Load() != 10 {
		t.Fatal("bytes were consumed while unbound")
	}
	conn.Close()
	got := c.Harvest()
	if len(got) != 1 || got[0].Site != SiteUnknown || got[0].Upload != 10 || got[0].Conns != 0 {
		t.Fatalf("closed unbound: %+v", got)
	}
	if again := c.Harvest(); len(again) != 0 {
		t.Fatalf("closed connection leaked: %+v", again)
	}
}

func TestCapSitesKeepsTotal(t *testing.T) {
	c := &Collector{opt: Options{Granularity: GranularityHost, MaxSitesPerUser: 200}}
	var want int64
	for i := 0; i < 250; i++ {
		conn := c.Open("user")
		conn.Bind("h" + itoa(i) + ".example.com")
		n := int64(i + 1)
		conn.Down.Add(n)
		want += n
	}
	got := c.Harvest()
	if len(got) != 200 {
		t.Fatalf("len = %d, want 200", len(got))
	}
	var sum int64
	var other int
	for _, e := range got {
		sum += e.Download
		if e.Site == SiteOther {
			other++
		}
	}
	if sum != want {
		t.Fatalf("total %d, want %d", sum, want)
	}
	if other != 1 {
		t.Fatalf("(other) entries = %d", other)
	}
}

func TestConcurrentOpenCloseHarvest(t *testing.T) {
	c := &Collector{opt: Options{MaxSitesPerUser: 50}}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				conn := c.Open("u")
				conn.Bind("example.com")
				conn.Up.Add(1)
				conn.Close()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 20; n++ {
				c.Harvest()
			}
		}()
	}
	wg.Wait()
	c.Harvest()
	for i := range c.shards {
		c.shards[i].Lock()
		left := len(c.shards[i].live)
		c.shards[i].Unlock()
		if left != 0 {
			t.Fatalf("shard %d still has %d conns", i, left)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
