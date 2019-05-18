package main

import (
	"crypto/tls"
	"flag"
	"io"
	"log"
	"net"
	"strings"

	"github.com/goawm/awm-core/multiconn"
	"github.com/lucas-clemente/quic-go"
)

func main() {
	flagBind := flag.String("bind", "localhost:1080", "Server address")
	flagRemoteAddr := flag.String("remote-addr", "localhost:8901", "Server address")
	flagLocalAddrs := flag.String("local-addrs", "", "Local addresses")
	flag.Parse()

	udpAddr, err := net.ResolveUDPAddr("udp", *flagRemoteAddr)
	if err != nil {
		log.Fatal(err)
	}

	l, err := net.Listen("tcp", *flagBind)
	if err != nil {
		log.Fatal(err)
	}
	for {
		clientConn, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		remoteConn, err := multiconn.DialMultiUDPConn(*flagRemoteAddr, strings.Split(*flagLocalAddrs, ",")...)
		if err != nil {
			log.Println(err)
			continue
		}

		session, err := quic.Dial(remoteConn, udpAddr, *flagRemoteAddr, &tls.Config{InsecureSkipVerify: true}, nil)
		if err != nil {
			log.Println(err)
			continue
		}

		stream, err := session.OpenStreamSync()
		go func() {
			if _, err := io.Copy(clientConn, stream); err != nil {
				log.Printf("copy: %v", err)
			}
		}()
		go func() {
			if _, err := io.Copy(stream, clientConn); err != nil {
				log.Printf("copy: %v", err)
			}
		}()
	}
}
