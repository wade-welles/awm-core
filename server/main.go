package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"github.com/goawm/awm-core/tunnel"
)

func main() {
	flagBindAddr := flag.String("bind", "0.0.0.0:8901", "Bind address")
	flag.Parse()
	err := tunnel.ListenAndServe(*flagBindAddr, func(conn *tunnel.Conn) {
		buf := make([]byte, 1024)
		_, err := io.ReadFull(conn, buf[:3])
		if err != nil {
			log.Printf("err: %v", err)
			return
		}
		_, err = conn.Write([]byte{5, 0})
		if err != nil {
			log.Printf("err: %v", err)
			return
		}
		_, err = io.ReadFull(conn, buf[:4])
		if err != nil {
			log.Printf("err: %v", err)
			return
		}
		atyp := buf[3]
		var addr *net.TCPAddr
		switch atyp {
		case 1:
			_, err = io.ReadFull(conn, buf[:6])
			if err != nil {
				log.Printf("err: %v", err)
			}
			ipv4 := net.IPv4(buf[0], buf[1], buf[2], buf[3])
			port := int(binary.BigEndian.Uint16(buf[4:6]))
			addr = &net.TCPAddr{
				IP:   ipv4,
				Port: port,
			}
		case 3:
			_, err = io.ReadFull(conn, buf[:1])
			if err != nil {
				log.Printf("err: %v", err)
			}
			l := int(buf[0])
			_, err = io.ReadFull(conn, buf[:l+2])
			domain := string(buf[:l])
			port := int(binary.BigEndian.Uint16(buf[l : l+2]))
			addr, err = net.ResolveTCPAddr("tcp", domain+":"+fmt.Sprint(port))
			if err != nil {
				log.Printf("err: %v", err)
			}
		case 4:
			_, err = io.ReadFull(conn, buf[:18])
			if err != nil {
				log.Printf("err: %v", err)
			}
			p := make([]byte, net.IPv6len)
			ipv6 := net.IP(p)
			port := binary.BigEndian.Uint16(buf[16:18])
			addr = &net.TCPAddr{
				IP:   ipv6,
				Port: int(port),
			}
		default:
			return
		}
		conn2, err := net.DialTCP("tcp", nil, addr)
		if err != nil {
			log.Printf("err: %v", err)
			return
		}
		wg := sync.WaitGroup{}
		wg.Add(2)
		go func() {
			defer wg.Done()
			io.Copy(conn, conn2)
		}()
		go func() {
			defer wg.Done()
			io.Copy(conn2, conn)
		}()
		wg.Wait()
		conn2.Close()
	})
	if err != nil {
		log.Fatal(err)
	}
}
