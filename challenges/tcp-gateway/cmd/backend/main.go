package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/HolmstN/coding-corner/challenges/tcp-gateway/protocol"
)

func main() {
	name := flag.String("name", "users", "backend name")
	port := flag.Int("port", 9001, "listening port")
	delay := flag.Duration("delay", 0, "delay each response, for timeout experiments")
	flag.Parse()
	if *name == "" || *port < 1 || *port > 65535 || *delay < 0 {
		log.Fatal("name must be nonempty; port must be 1..65535; delay must be nonnegative")
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()
	log.Printf("%s listening on %s", *name, listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go serve(conn, *name, *delay)
	}
}

func serve(conn net.Conn, name string, delay time.Duration) {
	defer conn.Close()
	for {
		request, err := protocol.ReadRequest(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("%s: invalid or incomplete request: %v", conn.RemoteAddr(), err)
			}
			return
		}
		time.Sleep(delay)
		response := protocol.Response{ID: request.ID, Status: "ok", Body: name + " got: " + request.Body}
		if err := protocol.WriteJSON(conn, response); err != nil {
			log.Printf("%s: response write failed: %v", conn.RemoteAddr(), err)
			return
		}
	}
}
