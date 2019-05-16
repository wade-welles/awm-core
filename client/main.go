package main

import (
	"flag"
	"io"
	"log"
	"net"
	"strings"

	"github.com/goawm/awm-core/tunnel"
)

func main() {
	flagBind := flag.String("bind", "localhost:1080", "Server address")
	flagServerAddr := flag.String("server-addr", "localhost:8901", "Server address")
	flagLocalAddrs := flag.String("local-addrs", "", "Local addresses")
	flag.Parse()

	var localAddrs []string
	for _, addr := range strings.Split(*flagLocalAddrs, ";") {
		addr = strings.TrimSpace(addr)
		if len(addr) > 0 {
			localAddrs = append(localAddrs, addr+":0")
		}
	}

	l, err := net.Listen("tcp", *flagBind)
	if err != nil {
		log.Fatal(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}
		conn2 := conn
		go func() {
			tun, err := tunnel.Dial(*flagServerAddr, localAddrs...)
			if err != nil {
				log.Println(err)
				return
			}
			go func() {
				io.Copy(conn2, tun)
			}()
			go func() {
				io.Copy(tun, conn2)
			}()
		}()
	}
}
