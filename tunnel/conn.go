package tunnel

import (
	"io"
	"net"
	"sync"

	"v2ray.com/core/common/errors"
	"v2ray.com/core/transport/internet/kcp"
)

var ErrClosedWriter = errors.New("writer closed")

// High Availability MultiWriter
type HAMultiWriter struct {
	p    chan []byte
	once sync.Once
	done chan struct{}
}

func NewHAMultiWriter(writers ...io.Writer) *HAMultiWriter {
	h := &HAMultiWriter{p: make(chan []byte), done: make(chan struct{})}
	for _, w := range writers {
		h.AddWriter(w)
	}
	return h
}

func (h *HAMultiWriter) AddWriter(w io.Writer) {
	go func() {
		for {
			select {
			case <-h.done:
				return
			case data := <-h.p:
				if _, err := w.Write(data); err != nil {
					h.once.Do(func() {
						h.done <- struct{}{}
					})
					return
				}
			}
		}
	}()
}

func (h *HAMultiWriter) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	select {
	case h.p <- buf:
		return len(p), nil
	case <-h.done:
		return 0, ErrClosedWriter
	}
}

func (h *HAMultiWriter) Close() error {
	h.once.Do(func() {
		h.done <- struct{}{}
	})
	return nil
}

type UDPWriter struct {
	conn *net.UDPConn
	addr *net.UDPAddr
}

func (u *UDPWriter) Write(p []byte) (int, error) {
	return u.conn.WriteToUDP(p, u.addr)
}

func (u *UDPWriter) Close() error {
	return nil
}

type ConnHandler func(*Conn)

type Conn struct {
	kcpConn *kcp.Connection
	writer  *HAMultiWriter
	closers []io.Closer
}

func newConn(conv uint16) *Conn {
	w := NewHAMultiWriter()
	kcpConn := kcp.NewConnection(kcp.ConnMetadata{Conversation: conv},
		&kcp.KCPPacketWriter{Writer: w}, w, &kcp.Config{})
	return &Conn{kcpConn: kcpConn, writer: w}
}

func (c *Conn) addLink(wc io.WriteCloser) {
	c.writer.AddWriter(wc)
	c.closers = append(c.closers, wc)
}

func (c *Conn) input(seg kcp.Segment) {
	c.kcpConn.Input([]kcp.Segment{seg})
}

func (c *Conn) Read(p []byte) (int, error) {
	return c.kcpConn.Read(p)
}

func (c *Conn) Write(p []byte) (int, error) {
	return c.kcpConn.Write(p)
}

func (c *Conn) Close() error {
	for _, c := range c.closers {
		c.Close()
	}
	return c.kcpConn.Close()
}
