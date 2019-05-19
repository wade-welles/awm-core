package multiconn

import (
	"encoding/binary"
	"errors"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/golang-lru"
)

var globalConnID uint32

func init() {
	rand.Seed(time.Now().UnixNano())
}

type Packet struct {
	Data []byte
	Addr net.Addr
}

type MultiUDPConnAddr struct {
	UDPAddrs []*net.UDPAddr
}

func (m *MultiUDPConnAddr) Network() string {
	return "udp"
}

func (m *MultiUDPConnAddr) String() string {
	var addrs []string
	for _, addr := range m.UDPAddrs {
		addrs = append(addrs, addr.String())
	}
	return strings.Join(addrs, ",")
}

type MultiUDPConnDial struct {
	connID    []byte
	udpConns  []*net.UDPConn
	sendBuf   chan *Packet
	recvBuf   chan *Packet
	closed    chan struct{}
	closeFunc func()
}

func NewMultiUDPConnDial(udpConns []*net.UDPConn) *MultiUDPConnDial {
	sendBuf := make(chan *Packet)
	recvBuf := make(chan *Packet)
	closed := make(chan struct{})

	closeOnce := sync.Once{}
	closeFunc := func() {
		closeOnce.Do(func() {
			close(closed)
		})
	}
	connID := make([]byte, 2, 2)
	binary.BigEndian.PutUint16(connID, uint16(atomic.AddUint32(&globalConnID, 1)))
	d := &MultiUDPConnDial{
		connID:    connID,
		udpConns:  udpConns,
		sendBuf:   sendBuf,
		recvBuf:   recvBuf,
		closed:    closed,
		closeFunc: closeFunc,
	}

	for _, connIter := range udpConns {
		// send
		conn := connIter
		go func() {
		L:
			for {
				select {
				case p := <-sendBuf:
					if _, err := conn.Write(p.Data); err != nil {
						break L
					}
				case <-closed:
					break L
				}
			}
			d.closeFunc()
		}()
		// recv
		go func() {
			buf := make([]byte, 2048)
		L:
			for {
				n, addr, err := conn.ReadFrom(buf)
				if err != nil {
					break L
				}
				data := append([]byte(nil), buf[:n]...)

				select {
				case recvBuf <- &Packet{Data: data, Addr: addr}:
				case <-closed:
					break L
				}
			}
			d.closeFunc()
		}()
	}
	return d
}

func (d *MultiUDPConnDial) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	select {
	case packet := <-d.recvBuf:
		n = copy(p, packet.Data)
		addr = packet.Addr
	case <-d.closed:
		err = errors.New("conn closed")
	}
	return
}

func (d *MultiUDPConnDial) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	data := append(d.connID, p...)
	packet := &Packet{Data: data, Addr: addr}
	select {
	case d.sendBuf <- packet:
		n = len(p)
	case <-d.closed:
		err = errors.New("conn closed")
	}
	return
}

func (d *MultiUDPConnDial) Close() error {
	for _, conn := range d.udpConns {
		if err := conn.Close(); err != nil {
			return err
		}
	}
	d.closeFunc()
	return nil
}

func (d *MultiUDPConnDial) LocalAddr() net.Addr {
	var udpAddrs []*net.UDPAddr
	for _, conn := range d.udpConns {
		udpAddrs = append(udpAddrs, conn.LocalAddr().(*net.UDPAddr))
	}
	return &MultiUDPConnAddr{UDPAddrs: udpAddrs}
}

func (d *MultiUDPConnDial) SetDeadline(t time.Time) error {
	for _, conn := range d.udpConns {
		if err := conn.SetDeadline(t); err != nil {
			return err
		}
	}
	return nil
}

func (d *MultiUDPConnDial) SetReadDeadline(t time.Time) error {
	for _, conn := range d.udpConns {
		if err := conn.SetReadDeadline(t); err != nil {
			return err
		}
	}
	return nil
}

func (d *MultiUDPConnDial) SetWriteDeadline(t time.Time) error {
	for _, conn := range d.udpConns {
		if err := conn.SetWriteDeadline(t); err != nil {
			return err
		}
	}
	return nil
}

type MultiUDPConnListener struct {
	udpConn   *net.UDPConn
	recvBuf   chan *Packet
	closed    chan struct{}
	closeFunc func()

	rw          sync.RWMutex
	connCache   *lru.Cache
	addr2ConnID map[string]uint16
}

func NewMultiUDPConnListener(udpConn *net.UDPConn) *MultiUDPConnListener {
	recvBuf := make(chan *Packet)
	closed := make(chan struct{})

	closeOnce := sync.Once{}
	closeFunc := func() {
		closeOnce.Do(func() {
			close(closed)
		})
	}

	addr2ConnID := make(map[string]uint16)
	connCache, err := lru.NewWithEvict(1024, func(key interface{}, value interface{}) {
		availAddrs := value.(map[string]*net.UDPAddr)
		for addr := range availAddrs {
			delete(addr2ConnID, addr)
		}
	})
	if err != nil {
		panic(err)
	}

	l := &MultiUDPConnListener{
		udpConn:   udpConn,
		recvBuf:   recvBuf,
		closed:    closed,
		closeFunc: closeFunc,

		connCache:   connCache,
		addr2ConnID: addr2ConnID,
	}
	go func() {
		buf := make([]byte, 2048)
	L:
		for {
			select {
			case <-l.closed:
				break L
			default:
			}
			n, addr, err := udpConn.ReadFrom(buf)
			if err != nil || n < 2 {
				break L
			}
			connID := binary.BigEndian.Uint16(buf[:2])

			func() {
				l.rw.RLock()
				oldConnID, ok := l.addr2ConnID[addr.String()]
				l.rw.RUnlock()
				if ok && oldConnID == connID {
					return
				}

				l.rw.Lock()
				defer l.rw.Unlock()

				var availAddrs map[string]*net.UDPAddr

				l.addr2ConnID[addr.String()] = connID
				availAddrsI, ok := l.connCache.Get(connID)
				if !ok {
					availAddrs = make(map[string]*net.UDPAddr)
					l.connCache.Add(connID, availAddrs)
				} else {
					availAddrs = availAddrsI.(map[string]*net.UDPAddr)
				}
				availAddrs[addr.String()] = addr.(*net.UDPAddr)
			}()

			data := append([]byte(nil), buf[2:n]...)
			recvBuf <- &Packet{Data: data, Addr: addr}
		}
		l.closeFunc()
	}()
	return l
}

func (l *MultiUDPConnListener) ReadFrom(p []byte) (n int, addr net.Addr, err error) {
	select {
	case packet := <-l.recvBuf:
		n = copy(p, packet.Data)
		addr = packet.Addr
	case <-l.closed:
		err = errors.New("conn closed")
	}
	return
}

func (l *MultiUDPConnListener) WriteTo(p []byte, addr net.Addr) (n int, err error) {
	l.rw.RLock()

	connID, ok := l.addr2ConnID[addr.String()]
	if !ok {
		l.rw.RUnlock()
		return 0, errors.New("invalid addr")
	}

	availAddrsI, ok := l.connCache.Get(connID)
	if !ok {
		l.rw.RUnlock()
		return 0, errors.New("invalid connection")
	}

	availAddrs := availAddrsI.(map[string]*net.UDPAddr)
	var udpAddrs []*net.UDPAddr
	for _, addr := range availAddrs {
		udpAddrs = append(udpAddrs, addr)
	}
	l.rw.RUnlock()

	if len(udpAddrs) == 0 {
		return 0, errors.New("no remote addr is available")
	}
	addr = udpAddrs[rand.Intn(len(udpAddrs))]
	return l.udpConn.WriteTo(p, addr)
}

func (l *MultiUDPConnListener) Close() error {
	l.closeFunc()
	return l.udpConn.Close()
}

func (l *MultiUDPConnListener) LocalAddr() net.Addr {
	return l.udpConn.LocalAddr()
}

func (l *MultiUDPConnListener) SetDeadline(t time.Time) error {
	return l.udpConn.SetDeadline(t)
}

func (l *MultiUDPConnListener) SetReadDeadline(t time.Time) error {
	return l.udpConn.SetReadDeadline(t)
}

func (l *MultiUDPConnListener) SetWriteDeadline(t time.Time) error {
	return l.udpConn.SetWriteDeadline(t)
}

func DialMultiUDPConn(remoteAddr string, localAddrs ...string) (net.PacketConn, error) {
	remoteUDPAddr, err := net.ResolveUDPAddr("udp", remoteAddr)
	if err != nil {
		return nil, err
	}

	var localUDPAddrs []*net.UDPAddr
	for _, localAddr := range localAddrs {
		localUDPAddr, err := net.ResolveUDPAddr("udp", localAddr+":0")
		if err != nil {
			return nil, err
		}
		localUDPAddrs = append(localUDPAddrs, localUDPAddr)
	}
	if len(localUDPAddrs) == 0 {
		localUDPAddrs = append(localUDPAddrs, &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	}
	var udpConns []*net.UDPConn
	for _, addr := range localUDPAddrs {
		udpConn, err := net.DialUDP("udp", addr, remoteUDPAddr)
		if err != nil {
			return nil, err
		}
		udpConns = append(udpConns, udpConn)
	}
	return NewMultiUDPConnDial(udpConns), nil
}

func ListenMultiUDPConn(addr string) (net.PacketConn, error) {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return nil, err
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return nil, err
	}
	return NewMultiUDPConnListener(udpConn), nil
}
