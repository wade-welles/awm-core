package tunnel

import (
	"net"
	"sync/atomic"

	"v2ray.com/core/transport/internet/kcp"
)

var globalConv uint32

func Dial(serverAddr string, localAddrs ...string) (*Conn, error) {
	raddr, err := net.ResolveUDPAddr("udp", serverAddr)
	if err != nil {
		return nil, err
	}
	conv := uint16(atomic.AddUint32(&globalConv, 1))
	conn := newConn(conv)
	if len(localAddrs) == 0 {
		localAddrs = append(localAddrs, "0.0.0.0:0")
	}
	for _, addr := range localAddrs {
		laddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, err
		}
		udpConn, err := net.DialUDP("udp", laddr, raddr)
		if err != nil {
			return nil, err
		}
		conn.addLink(udpConn)
		go func() {
			buf := make([]byte, 2048)
			segReader := kcp.KCPPacketReader{}
			for {
				n, err := udpConn.Read(buf)
				if err != nil {
					return
				}
				segments := segReader.Read(buf[:n])
				for _, seg := range segments {
					conn.input(seg)
				}
			}
		}()
	}
	return conn, nil
}
