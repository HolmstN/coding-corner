package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"time"

	"github.com/HolmstN/coding-corner/challenges/tcp-gateway/protocol"
)

func main() {
	address := flag.String("address", "127.0.0.1:7000", "gateway address")
	target := flag.String("target", "users", "backend target")
	body := flag.String("body", "hello", "request body")
	count := flag.Int("count", 1, "number of sequential requests")
	timeout := flag.Duration("timeout", 5*time.Second, "deadline per request")
	flag.Parse()
	if *count < 1 || *timeout <= 0 || *target == "" {
		log.Fatal("count and timeout must be positive; target must be nonempty")
	}
	conn, err := net.DialTimeout("tcp", *address, *timeout)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < *count; i++ {
		if err := conn.SetDeadline(time.Now().Add(*timeout)); err != nil {
			log.Fatal(err)
		}
		id := fmt.Sprintf("request-%d", i+1)
		if err := protocol.WriteJSON(conn, protocol.Request{ID: id, Target: *target, Body: *body}); err != nil {
			log.Fatal(err)
		}
		response, err := protocol.ReadResponse(conn)
		if err != nil {
			log.Fatal(err)
		}
		if response.ID != id {
			log.Fatalf("response ID %q does not match %q", response.ID, id)
		}
		fmt.Printf("%s %s: %s\n", response.ID, response.Status, response.Body)
	}
}
