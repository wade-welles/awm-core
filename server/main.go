package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"

	"github.com/armon/go-socks5"
	"github.com/goawm/awm-core/multiconn"
	"github.com/lucas-clemente/quic-go"
)

var bindAddr *net.TCPAddr

type streamConn struct {
	quic.Stream
}

func (streamConn) LocalAddr() net.Addr {
	return bindAddr
}

func (streamConn) RemoteAddr() net.Addr {
	return nil
}

func main() {
	flagBindAddr := flag.String("bind", "0.0.0.0:8901", "Bind address")
	flag.Parse()

	var err error
	bindAddr, err = net.ResolveTCPAddr("tcp", *flagBindAddr)
	if err != nil {
		log.Fatal(err)
	}

	s, err := socks5.New(&socks5.Config{})

	conn, err := multiconn.ListenMultiUDPConn(*flagBindAddr)
	if err != nil {
		log.Fatal(err)
	}

	listener, err := quic.Listen(conn, generateTLSConfig(), nil)
	if err != nil {
		log.Fatal(err)
	}
	for {
		sess, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		stream, err := sess.AcceptStream()
		if err != nil {
			panic(err)
		}

		go s.ServeConn(&streamConn{Stream: stream})
	}

}

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}
