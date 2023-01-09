package shadowtls

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"hash"
	"io"
	"net"

	"github.com/Dreamacro/clash/common/pool"
	"github.com/Dreamacro/clash/transport/trojan"
)

const (
	chunkSize             = 1 << 13
	MODE           string = "shadow-tls"
	HASH_LEN       int    = 8
	TLS_HEADER_LEN int    = 5
)

// TLSObfs is shadowsocks tls simple-obfs implementation
type ShadowTls struct {
	net.Conn
	server       string
	password     []byte
	remain       int
	firstRequest bool
}

type HashedConn struct {
	net.Conn
	hasher hash.Hash
}

func newHashedStream(conn net.Conn, password []byte) HashedConn {
	return HashedConn{
		Conn:   conn,
		hasher: hmac.New(sha1.New, password),
	}
}

func (h HashedConn) Read(b []byte) (n int, err error) {
	n, err = h.Conn.Read(b)
	h.hasher.Write(b[:n])
	return
}

func (to *ShadowTls) read(b []byte) (int, error) {
	buf := pool.Get(TLS_HEADER_LEN)
	_, err := io.ReadFull(to.Conn, buf)
	if err != nil {
		return 0, fmt.Errorf("shadowtls read failed %w", err)
	}
	if buf[0] != 0x17 || buf[1] != 0x3 || buf[2] != 0x3 {
		return 0, fmt.Errorf("invalid shadowtls header %v", buf)
	}
	length := int(binary.BigEndian.Uint16(buf[3:]))
	pool.Put(buf)

	if length > len(b) {
		n, err := to.Conn.Read(b)
		if err != nil {
			return n, err
		}
		to.remain = length - n
		return n, nil
	}

	return io.ReadFull(to.Conn, b[:length])
}

func (to *ShadowTls) Read(b []byte) (int, error) {
	if to.remain > 0 {
		length := to.remain
		if length > len(b) {
			length = len(b)
		}

		n, err := io.ReadFull(to.Conn, b[:length])
		if err != nil {
			return n, fmt.Errorf("shadowtls Read failed with %w", err)
		}
		to.remain -= n
		return n, nil
	}

	return to.read(b)
}

func (to *ShadowTls) Write(b []byte) (int, error) {
	length := len(b)
	for i := 0; i < length; i += chunkSize {
		end := i + chunkSize
		if end > length {
			end = length
		}

		n, err := to.write(b[i:end])
		if err != nil {
			return n, fmt.Errorf("shadowtls Write failed with %w, i=%d, end=%d, n=%d", err, i, end, n)
		}
	}
	return length, nil
}

func (s *ShadowTls) write(b []byte) (int, error) {
	var hashVal []byte
	if s.firstRequest {
		tlsConn := trojan.New(&trojan.Option{ServerName: s.server, SkipCertVerify: false})
		hashedConn := newHashedStream(s.Conn, s.password)
		if _, err := tlsConn.StreamConn(hashedConn); err != nil {
			return 0, fmt.Errorf("tls connect failed with %w", err)
		}
		hashVal = hashedConn.hasher.Sum(nil)[:HASH_LEN]
		s.firstRequest = false
	}

	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)
	buf.Write([]byte{0x17, 0x03, 0x03})
	binary.Write(buf, binary.BigEndian, uint16(len(b)+len(hashVal)))
	buf.Write(hashVal)
	buf.Write(b)
	_, err := s.Conn.Write(buf.Bytes())
	return len(b), err
}

// NewShadowTls return a ShadowTls
func NewShadowTls(conn net.Conn, server string, password string) net.Conn {
	return &ShadowTls{
		Conn:         conn,
		password:     []byte(password),
		server:       server,
		firstRequest: true,
	}
}
