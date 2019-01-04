package awm

import (
	"context"
	log "github.com/sirupsen/logrus"
	"github.com/xtaci/kcp-go"
	"io"
	"time"
)

const BufSize = 2048

type Tunnel struct {
	k      *kcp.KCP
	sendQ  chan []byte
	ctx    context.Context
	cancel context.CancelFunc
}

func NewTunnel(id int) *Tunnel {
	sendQ := make(chan []byte, 32)
	k := kcp.NewKCP(uint32(id), func(buf []byte, size int) {
		sendQ <- buf[:size]
	})
	k.WndSize(128, 128)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				k.Update()
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	return &Tunnel{k: k, sendQ: sendQ, ctx: ctx, cancel: cancel}
}

func (t *Tunnel) Read(p []byte) (int, error) {
	n := t.k.Recv(p)
	if n <= 0 {
		n = 0
	}
	return n, nil
}

func (t *Tunnel) Write(p []byte) (int, error) {
	n := t.k.Send(p)
	if n < 0 {
		n = 0
	}
	return n, nil
}

func (t *Tunnel) Close() error {
	t.cancel()
	return nil
}

func (t *Tunnel) AddReader(r io.Reader) {
	buf := make([]byte, BufSize)
	go func() {
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
				n, err := r.Read(buf)
				if n > 0 {
					t.k.Input(buf, true, true)
				}
				if err != nil {
					log.WithError(err).Error("read data failed")
					return
				}

			}
		}
	}()
}

func (t *Tunnel) AddWriter(w io.Writer) {
	go func() {
		for {
			select {
			case <-t.ctx.Done():
				return
			case p := <-t.sendQ:
				_, err := w.Write(p)
				if err != nil {
					log.WithError(err).Error("write data failed")
					return
				}
			}
		}
	}()
}
