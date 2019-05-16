package tunnel

import (
	"errors"
	"net"
	"time"

	"v2ray.com/core/transport/internet/kcp"
)

var ErrClosedListener = errors.New("listener closed")

type session struct {
	conn  *Conn
	addrs map[string]struct{}
}

type Listener struct {
	sessions map[uint16]session
	udpConn  *net.UDPConn
	handler  ConnHandler
	done     chan struct{}
}

func NewListener(addr string, handler ConnHandler) (*Listener, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		sessions: make(map[uint16]session),
		udpConn:  udpConn,
		handler:  handler,
		done:     make(chan struct{}, 1),
	}
	return l, nil
}

func (l *Listener) Serve() error {
	buf := make([]byte, 2048)
	segReader := kcp.KCPPacketReader{}
	for {
		select {
		case <-l.done:
			return ErrClosedListener
		default:
		}
		n, addr, err := l.udpConn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		segments := segReader.Read(buf[:n])
		for _, seg := range segments {
			if _, ok := l.sessions[seg.Conversation()]; !ok {
				conn := newConn(seg.Conversation())
				s := session{conn: conn, addrs: make(map[string]struct{})}
				l.sessions[seg.Conversation()] = s
				go func() {
					l.handler(conn)
					conn.Close()
					for {
						time.Sleep(time.Millisecond * 10)
						if conn.kcpConn.State() == kcp.StateTerminated {
							break
						}
					}
					delete(l.sessions, seg.Conversation())
				}()
			}
			s := l.sessions[seg.Conversation()]

			if _, ok := s.addrs[addr.String()]; !ok {
				s.conn.addLink(&UDPWriter{conn: l.udpConn, addr: addr})
			}
			s.conn.input(seg)
		}
	}
}

func (l *Listener) Close() error {
	l.done <- struct{}{}
	return l.udpConn.Close()
}

func ListenAndServe(addr string, handler ConnHandler) error {
	l, err := NewListener(addr, handler)
	if err != nil {
		return err
	}
	return l.Serve()
}
