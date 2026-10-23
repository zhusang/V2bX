package dispatcher

import (
	"context"
	"sync/atomic"

	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/common/session"
)

func openSiteConn(tag, email string) *sitestat.Conn {
	col := sitestat.Get(tag)
	if col == nil {
		return nil
	}
	return col.Open(email)
}

func siteExtras(conn *sitestat.Conn) (up, down *atomic.Int64) {
	if conn == nil {
		return nil, nil
	}
	return &conn.Up, &conn.Down
}

func watchSiteConn(ctx context.Context, conn *sitestat.Conn) {
	if conn == nil {
		return
	}
	conn.SetStop(context.AfterFunc(ctx, conn.Close))
}

func bindSite(conn *sitestat.Conn, ob *session.Outbound, destination net.Destination) {
	if conn == nil {
		return
	}
	target := destination
	if ob != nil && ob.RouteTarget.IsValid() {
		target = ob.RouteTarget
	}
	conn.Bind(destHost(target))
}

func destHost(dest net.Destination) string {
	if dest.Address == nil {
		return ""
	}
	if dest.Address.Family().IsDomain() {
		return dest.Address.Domain()
	}
	if ip := dest.Address.IP(); ip != nil {
		return ip.String()
	}
	return ""
}

// halfWriter wraps w so closing or interrupting it ends one direction of conn.
// With collection off it returns w unchanged.
func halfWriter(w buf.Writer, conn *sitestat.Conn, dir uint32) buf.Writer {
	if conn == nil {
		return w
	}
	return &halfCloseWriter{Writer: w, conn: conn, dir: dir}
}

// fullWriter wraps w so closing or interrupting it ends conn. Used by
// DispatchLink, which has a single outbound writer.
func fullWriter(w buf.Writer, conn *sitestat.Conn) buf.Writer {
	if conn == nil {
		return w
	}
	return &fullCloseWriter{Writer: w, conn: conn}
}

// halfCloseWriter sits inside SizeStatWriter. Interrupt must be forwarded:
// common.Interrupt falls back to Close for writers that lack it.
type halfCloseWriter struct {
	buf.Writer
	conn *sitestat.Conn
	dir  uint32
}

func (w *halfCloseWriter) Close() error {
	w.conn.CloseHalf(w.dir)
	return common.Close(w.Writer)
}

func (w *halfCloseWriter) Interrupt() {
	w.conn.CloseHalf(w.dir)
	common.Interrupt(w.Writer)
}

type fullCloseWriter struct {
	buf.Writer
	conn *sitestat.Conn
}

func (w *fullCloseWriter) Close() error {
	w.conn.Close()
	return common.Close(w.Writer)
}

func (w *fullCloseWriter) Interrupt() {
	w.conn.Close()
	common.Interrupt(w.Writer)
}
