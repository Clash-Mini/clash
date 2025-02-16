package outbound

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"runtime"
	"strconv"
	"time"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/component/dialer"
	"github.com/metacubex/mihomo/component/proxydialer"
	"github.com/metacubex/mihomo/component/resolver"
	tlsC "github.com/metacubex/mihomo/component/tls"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/transport/anytls"
	"github.com/sagernet/sing/common"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/uot"
)

type AnyTLS struct {
	*Base
	client *anytls.Client
	dialer proxydialer.SingDialer
	option *AnyTLSOption
}

type AnyTLSOption struct {
	BasicOption
	Name                     string   `proxy:"name"`
	Server                   string   `proxy:"server"`
	Port                     int      `proxy:"port"`
	Password                 string   `proxy:"password"`
	ALPN                     []string `proxy:"alpn,omitempty"`
	SNI                      string   `proxy:"sni,omitempty"`
	ClientFingerprint        string   `proxy:"client-fingerprint,omitempty"`
	SkipCertVerify           bool     `proxy:"skip-cert-verify,omitempty"`
	UDP                      bool     `proxy:"udp,omitempty"`
	IdleSessionCheckInterval int      `proxy:"idle-session-check-interval,omitempty"`
	IdleSessionTimeout       int      `proxy:"idle-session-timeout,omitempty"`
}

func (t *AnyTLS) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.Conn, err error) {
	options := t.Base.DialOptions(opts...)
	t.dialer.SetDialer(dialer.NewDialer(options...))
	c, err := t.client.CreateProxy(ctx, M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort))
	if err != nil {
		return nil, err
	}
	return NewConn(N.NewRefConn(c, t), t), nil
}

func (t *AnyTLS) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.PacketConn, err error) {
	// create tcp
	options := t.Base.DialOptions(opts...)
	t.dialer.SetDialer(dialer.NewDialer(options...))
	c, err := t.client.CreateProxy(ctx, M.ParseSocksaddrHostPort(metadata.String(), metadata.DstPort))
	if err != nil {
		return nil, err
	}

	// create uot on tcp
	if !metadata.Resolved() {
		ip, err := resolver.ResolveIP(ctx, metadata.Host)
		if err != nil {
			return nil, errors.New("can't resolve ip")
		}
		metadata.DstIP = ip
	}
	destination := M.SocksaddrFromNet(metadata.UDPAddr())
	return newPacketConn(N.NewThreadSafePacketConn(uot.NewLazyConn(c, uot.Request{Destination: destination})), t), nil
}

// SupportWithDialer implements C.ProxyAdapter
func (t *AnyTLS) SupportWithDialer() C.NetWork {
	return C.ALLNet
}

// SupportUOT implements C.ProxyAdapter
func (t *AnyTLS) SupportUOT() bool {
	return true
}

// ProxyInfo implements C.ProxyAdapter
func (t *AnyTLS) ProxyInfo() C.ProxyInfo {
	info := t.Base.ProxyInfo()
	info.DialerProxy = t.option.DialerProxy
	return info
}

func NewAnyTLS(option AnyTLSOption) (*AnyTLS, error) {
	addr := net.JoinHostPort(option.Server, strconv.Itoa(option.Port))

	singDialer := proxydialer.NewByNameSingDialer(option.DialerProxy, dialer.NewDialer())

	tOption := anytls.ClientConfig{
		Password:                 option.Password,
		Server:                   M.ParseSocksaddrHostPort(option.Server, uint16(option.Port)),
		Dialer:                   singDialer,
		IdleSessionCheckInterval: time.Duration(option.IdleSessionCheckInterval) * time.Second,
		IdleSessionTimeout:       time.Duration(option.IdleSessionTimeout) * time.Second,
		ClientFingerprint:        option.ClientFingerprint,
	}
	tlsConfig := &tls.Config{
		ServerName:         option.SNI,
		InsecureSkipVerify: option.SkipCertVerify,
		NextProtos:         option.ALPN,
	}
	if tlsConfig.ServerName == "" {
		tlsConfig.ServerName = "127.0.0.1"
	}
	tOption.TLSConfig = tlsConfig

	if tlsC.HaveGlobalFingerprint() && len(tOption.ClientFingerprint) == 0 {
		tOption.ClientFingerprint = tlsC.GetGlobalFingerprint()
	}

	outbound := &AnyTLS{
		Base: &Base{
			name:   option.Name,
			addr:   addr,
			tp:     C.AnyTLS,
			udp:    option.UDP,
			tfo:    option.TFO,
			mpTcp:  option.MPTCP,
			iface:  option.Interface,
			rmark:  option.RoutingMark,
			prefer: C.NewDNSPrefer(option.IPVersion),
		},
		client: anytls.NewClient(context.TODO(), tOption),
		option: &option,
		dialer: singDialer,
	}
	runtime.SetFinalizer(outbound, func(o *AnyTLS) {
		common.Close(o.client)
	})

	return outbound, nil
}
