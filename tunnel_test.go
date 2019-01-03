package awm

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

func ConnPair() (net.Conn, net.Conn, error) {
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, nil, err
	}
	defer l.Close()

	addr := l.Addr()
	done := make(chan struct{})

	var sConn, cConn net.Conn
	var err2 error

	go func() {
		defer close(done)
		cConn, err2 = net.Dial(addr.Network(), addr.String())
	}()
	sConn, err = l.Accept()
	<-done
	if err == nil {
		err = err2
	}
	if err != nil {
		if sConn != nil {
			sConn.Close()
		}
		if cConn != nil {
			cConn.Close()
		}
	}
	return sConn, cConn, nil
}

func TestNewTunnel(t *testing.T) {
	tun1 := NewTunnel(1)
	tun2 := NewTunnel(2)
	defer tun1.Close()
	defer tun2.Close()

	for i := 0; i < 20; i++ {
		r, w, err := ConnPair()
		if err != nil {
			t.Fatal(err)
		}
		tun1.AddReader(r)
		tun1.AddWriter(w)
		tun2.AddReader(w)
		tun2.AddWriter(r)
	}

	summary := "Go is a programming language designed by Google engineers Robert Griesemer, Rob Pike, and Ken " +
		"Thompson. Go is statically typed, compiled, and syntactically similar to C, with the added benefits of " +
		"memory safety, garbage collection, structural typing, and CSP-style concurrency."
	var sendData, recvData []byte
	var chunks [][]byte
	for i := 0; i < 1000; i++ {
		chunks = append(chunks, []byte(summary))
	}
	sendData = bytes.Join(chunks, []byte("//"))
	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		defer wg.Done()
		tun1.Write(sendData)
	}()
	go func() {
		defer wg.Done()
		var recvChunks [][]byte
		buf := make([]byte, 4096)
		for i := 0; i < 1000; i++ {
			n, _ := tun2.Read(buf)
			if n <= 0 {
				time.Sleep(time.Millisecond * 10)
			} else {
				recvChunks = append(recvChunks, buf[:n])
			}
		}
		recvData = bytes.Join(recvChunks, nil)
	}()
	wg.Wait()
	n := bytes.Compare(sendData, recvData)
	if n != 0 {
		t.Error("data corrupted")
	}
}
