package counter

import (
	"io"
	"net"

	"github.com/InazumaV/V2bX/common/sitestat"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/network"
)

type ConnCounter struct {
	network.ExtendedConn
	storage   *TrafficStorage
	site      *sitestat.Conn
	readFunc  network.CountFunc
	writeFunc network.CountFunc
}

func NewConnCounter(conn net.Conn, s *TrafficStorage, site *sitestat.Conn) net.Conn {
	c := &ConnCounter{
		ExtendedConn: bufio.NewExtendedConn(conn),
		storage:      s,
		site:         site,
	}
	c.readFunc = func(n int64) {
		c.addUp(n)
	}
	c.writeFunc = func(n int64) {
		c.addDown(n)
	}
	return c
}

func (c *ConnCounter) addUp(n int64) {
	if n <= 0 {
		return
	}
	c.storage.UpCounter.Add(n)
	if c.site != nil {
		c.site.Up.Add(n)
	}
}

func (c *ConnCounter) addDown(n int64) {
	if n <= 0 {
		return
	}
	c.storage.DownCounter.Add(n)
	if c.site != nil {
		c.site.Down.Add(n)
	}
}

func (c *ConnCounter) Read(b []byte) (n int, err error) {
	n, err = c.ExtendedConn.Read(b)
	c.addUp(int64(n))
	return
}

func (c *ConnCounter) Write(b []byte) (n int, err error) {
	n, err = c.ExtendedConn.Write(b)
	c.addDown(int64(n))
	return
}

func (c *ConnCounter) ReadBuffer(buffer *buf.Buffer) error {
	err := c.ExtendedConn.ReadBuffer(buffer)
	if err != nil {
		return err
	}
	c.addUp(int64(buffer.Len()))
	return nil
}

func (c *ConnCounter) WriteBuffer(buffer *buf.Buffer) error {
	dataLen := int64(buffer.Len())
	err := c.ExtendedConn.WriteBuffer(buffer)
	if err != nil {
		return err
	}
	c.addDown(dataLen)
	return nil
}

func (c *ConnCounter) Close() error {
	if c.site != nil {
		c.site.Close()
	}
	return c.ExtendedConn.Close()
}

func (c *ConnCounter) UnwrapReader() (io.Reader, []network.CountFunc) {
	return c.ExtendedConn, []network.CountFunc{c.readFunc}
}

func (c *ConnCounter) UnwrapWriter() (io.Writer, []network.CountFunc) {
	return c.ExtendedConn, []network.CountFunc{c.writeFunc}
}

func (c *ConnCounter) Upstream() any {
	return c.ExtendedConn
}

type PacketConnCounter struct {
	network.PacketConn
	storage   *TrafficStorage
	site      *sitestat.Conn
	readFunc  network.CountFunc
	writeFunc network.CountFunc
}

func NewPacketConnCounter(conn network.PacketConn, s *TrafficStorage, site *sitestat.Conn) network.PacketConn {
	p := &PacketConnCounter{
		PacketConn: conn,
		storage:    s,
		site:       site,
	}
	p.readFunc = func(n int64) {
		p.addUp(n)
	}
	p.writeFunc = func(n int64) {
		p.addDown(n)
	}
	return p
}

func (p *PacketConnCounter) addUp(n int64) {
	if n <= 0 {
		return
	}
	p.storage.UpCounter.Add(n)
	if p.site != nil {
		p.site.Up.Add(n)
	}
}

func (p *PacketConnCounter) addDown(n int64) {
	if n <= 0 {
		return
	}
	p.storage.DownCounter.Add(n)
	if p.site != nil {
		p.site.Down.Add(n)
	}
}

func (p *PacketConnCounter) ReadPacket(buff *buf.Buffer) (destination M.Socksaddr, err error) {
	destination, err = p.PacketConn.ReadPacket(buff)
	if err != nil {
		return
	}
	p.addUp(int64(buff.Len()))
	return
}

func (p *PacketConnCounter) WritePacket(buff *buf.Buffer, destination M.Socksaddr) (err error) {
	n := int64(buff.Len())
	err = p.PacketConn.WritePacket(buff, destination)
	if err != nil {
		return
	}
	p.addDown(n)
	return
}

func (p *PacketConnCounter) Close() error {
	if p.site != nil {
		p.site.Close()
	}
	return p.PacketConn.Close()
}

func (p *PacketConnCounter) UnwrapPacketReader() (network.PacketReader, []network.CountFunc) {
	return p.PacketConn, []network.CountFunc{p.readFunc}
}

func (p *PacketConnCounter) UnwrapPacketWriter() (network.PacketWriter, []network.CountFunc) {
	return p.PacketConn, []network.CountFunc{p.writeFunc}
}

func (p *PacketConnCounter) Upstream() any {
	return p.PacketConn
}
