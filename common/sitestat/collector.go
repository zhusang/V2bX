package sitestat

import (
	"sort"
	"sync"
	"sync/atomic"
	"unsafe"
)

const (
	HalfUp   uint32 = 1
	HalfDown uint32 = 2

	flagClosed uint32 = 4
	shardCount        = 16
)

// Options controls how a collector attributes traffic.
type Options struct {
	Granularity     string
	MaxSitesPerUser int
}

// Entry is one user's traffic on one site since the previous harvest.
type Entry struct {
	User     string
	Site     string
	Upload   int64
	Download int64
	Conns    int64
}

// Conn counts bytes for a single connection. A nil *Conn is safe to use.
type Conn struct {
	Up   atomic.Int64
	Down atomic.Int64

	user    string
	site    atomic.Pointer[string]
	counted atomic.Bool
	state   atomic.Uint32
	stop    atomic.Pointer[stopFunc]
	col     *Collector
}

type stopFunc struct {
	fn func() bool
}

// Bind attributes this connection, including bytes already counted, to target.
func (x *Conn) Bind(target string) {
	if x == nil {
		return
	}
	site := Normalize(target, x.col.opt.Granularity)
	if site == "" {
		site = SiteUnknown
	}
	x.site.Store(&site)
}

// CloseHalf records that one direction has closed. Both directions closing
// closes the connection.
func (x *Conn) CloseHalf(dir uint32) {
	if x == nil {
		return
	}
	for {
		old := x.state.Load()
		if old&flagClosed != 0 {
			return
		}
		next := old | dir
		if x.state.CompareAndSwap(old, next) {
			if next&HalfUp != 0 && next&HalfDown != 0 {
				x.Close()
			}
			return
		}
	}
}

// Close marks the connection finished. Further calls are no-ops.
// Bytes stay readable until the next harvest.
func (x *Conn) Close() {
	if x == nil {
		return
	}
	for {
		old := x.state.Load()
		if old&flagClosed != 0 {
			return
		}
		if x.state.CompareAndSwap(old, old|flagClosed) {
			x.cancelStop()
			return
		}
	}
}

// SetStop stores the function that releases a context callback.
// If the connection is already closed, stop is invoked immediately.
func (x *Conn) SetStop(stop func() bool) {
	if x == nil || stop == nil {
		return
	}
	x.stop.Store(&stopFunc{fn: stop})
	if x.state.Load()&flagClosed != 0 {
		x.cancelStop()
	}
}

func (x *Conn) cancelStop() {
	p := x.stop.Swap(nil)
	if p != nil && p.fn != nil {
		p.fn()
	}
}

func (x *Conn) closed() bool {
	return x.state.Load()&flagClosed != 0
}

// Collector tracks live connections for one node.
type Collector struct {
	opt    Options
	shards [shardCount]struct {
		sync.Mutex
		live map[*Conn]struct{}
	}
}

// Open registers a connection for user. user is the core's user key
// (Xray email or sing/hy2 uuid).
func (c *Collector) Open(user string) *Conn {
	if c == nil {
		return nil
	}
	conn := &Conn{user: user, col: c}
	shard := &c.shards[shardOf(conn)]
	shard.Lock()
	if shard.live == nil {
		shard.live = make(map[*Conn]struct{})
	}
	shard.live[conn] = struct{}{}
	shard.Unlock()
	return conn
}

func shardOf(conn *Conn) uintptr {
	return uintptr(unsafe.Pointer(conn)) % shardCount
}

type acc struct {
	up, down, conns int64
}

// Harvest returns deltas since the previous call and drops closed connections.
func (c *Collector) Harvest() []Entry {
	if c == nil {
		return nil
	}
	byUser := make(map[string]map[string]*acc)
	add := func(user, site string, up, down, conns int64) {
		if up == 0 && down == 0 && conns == 0 {
			return
		}
		sites := byUser[user]
		if sites == nil {
			sites = make(map[string]*acc)
			byUser[user] = sites
		}
		a := sites[site]
		if a == nil {
			a = &acc{}
			sites[site] = a
		}
		a.up += up
		a.down += down
		a.conns += conns
	}

	for i := range c.shards {
		shard := &c.shards[i]
		shard.Lock()
		for conn := range shard.live {
			// Read closed before site and counters so a connection that closes
			// mid-harvest is kept for the next round instead of losing bytes.
			done := conn.closed()
			sitePtr := conn.site.Load()
			if sitePtr == nil && !done {
				continue
			}
			site := SiteUnknown
			if sitePtr != nil {
				site = *sitePtr
			}
			up := conn.Up.Swap(0)
			down := conn.Down.Swap(0)
			var conns int64
			if sitePtr != nil && conn.counted.CompareAndSwap(false, true) {
				conns = 1
			}
			add(conn.user, site, up, down, conns)
			if done {
				delete(shard.live, conn)
			}
		}
		shard.Unlock()
	}

	maxSites := c.opt.MaxSitesPerUser
	if maxSites <= 0 {
		maxSites = 200
	}
	var out []Entry
	for user, sites := range byUser {
		capSites(sites, maxSites)
		for site, a := range sites {
			out = append(out, Entry{
				User:     user,
				Site:     site,
				Upload:   a.up,
				Download: a.down,
				Conns:    a.conns,
			})
		}
	}
	return out
}

// capSites keeps the largest max-1 sites and folds the rest into (other),
// preserving the user's byte totals.
func capSites(sites map[string]*acc, max int) {
	if max < 2 || len(sites) <= max {
		return
	}
	other := sites[SiteOther]
	delete(sites, SiteOther)
	type pair struct {
		site  string
		total int64
	}
	ps := make([]pair, 0, len(sites))
	for site, a := range sites {
		ps = append(ps, pair{site, a.up + a.down})
	}
	sort.Slice(ps, func(i, j int) bool {
		if ps[i].total == ps[j].total {
			return ps[i].site < ps[j].site
		}
		return ps[i].total < ps[j].total
	})
	keep := max - 1
	overflow := len(ps) - keep
	if overflow < 0 {
		overflow = 0
	}
	if other == nil {
		other = &acc{}
	}
	for i := 0; i < overflow; i++ {
		a := sites[ps[i].site]
		other.up += a.up
		other.down += a.down
		other.conns += a.conns
		delete(sites, ps[i].site)
	}
	if other.up != 0 || other.down != 0 || other.conns != 0 {
		sites[SiteOther] = other
	}
}

var registry sync.Map // tag -> *Collector

// LimitEntries caps each user's sites the same way Collector.Harvest does.
func LimitEntries(entries []Entry, max int) []Entry {
	if max < 2 {
		max = 200
	}
	byUser := make(map[string]map[string]*acc)
	for _, e := range entries {
		sites := byUser[e.User]
		if sites == nil {
			sites = make(map[string]*acc)
			byUser[e.User] = sites
		}
		a := sites[e.Site]
		if a == nil {
			a = &acc{}
			sites[e.Site] = a
		}
		a.up += e.Upload
		a.down += e.Download
		a.conns += e.Conns
	}
	var out []Entry
	for user, sites := range byUser {
		capSites(sites, max)
		for site, a := range sites {
			out = append(out, Entry{User: user, Site: site, Upload: a.up, Download: a.down, Conns: a.conns})
		}
	}
	return out
}

// Enable registers a collector for tag, replacing any previous one.
func Enable(tag string, opt Options) *Collector {
	c := &Collector{opt: opt}
	registry.Store(tag, c)
	return c
}

// Get returns the collector for tag, or nil when collection is off.
func Get(tag string) *Collector {
	v, ok := registry.Load(tag)
	if !ok {
		return nil
	}
	return v.(*Collector)
}

// Disable removes the collector for tag.
func Disable(tag string) {
	registry.Delete(tag)
}
