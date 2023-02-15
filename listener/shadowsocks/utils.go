package shadowsocks

import (
	"bytes"
	"errors"
	"net"
	"net/url"

	"github.com/Dreamacro/clash/common/pool"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/transport/socks5"
)

type packet struct {
	pc      net.PacketConn
	rAddr   net.Addr
	payload []byte
	bufRef  []byte
}

func (c *packet) Data() []byte {
	return c.payload
}

// WriteBack wirtes UDP packet with source(ip, port) = `addr`
func (c *packet) WriteBack(b []byte, addr net.Addr) (n int, err error) {
	if addr == nil {
		err = errors.New("address is invalid")
		return
	}
	packet := bytes.Join([][]byte{socks5.ParseAddrToSocksAddr(addr), b}, []byte{})
	return c.pc.WriteTo(packet, c.rAddr)
}

// LocalAddr returns the source IP/Port of UDP Packet
func (c *packet) LocalAddr() net.Addr {
	return c.rAddr
}

func (c *packet) Drop() {
	pool.Put(c.bufRef)
}

func (c *packet) InAddr() net.Addr {
	return c.pc.LocalAddr()
}

func (c *packet) SetNatTable(natTable C.NatTable) {
	// no need
}

func (c *packet) SetUdpInChan(in chan<- C.PacketAdapter) {
	// no need
}
func ParseSSURL(s string) (addr, cipher, password string, err error) {
	u, err := url.Parse(s)
	if err != nil {
		return
	}

	addr = u.Host
	if u.User != nil {
		cipher = u.User.Username()
		password, _ = u.User.Password()
	}
	return
}
