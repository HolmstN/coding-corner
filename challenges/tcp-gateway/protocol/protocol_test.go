package protocol

import (
	"bytes"
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

type shortWriter struct{ bytes.Buffer }

func (w *shortWriter) Write(p []byte) (int, error) {
	return w.Buffer.Write(p[:1])
}

func TestFraming(t *testing.T) {
	var w shortWriter
	requests := []Request{
		{ID: "first", Target: "users", Body: "hello"},
		{ID: "second", Target: "orders", Body: "\u96ea"},
	}
	for _, request := range requests {
		if err := WriteJSON(&w, request); err != nil {
			t.Fatal(err)
		}
	}
	r := &oneByteReader{Reader: bytes.NewReader(w.Bytes())}
	for _, want := range requests {
		got, err := ReadRequest(r)
		if err != nil || got != want {
			t.Fatalf("got %+v, %v; want %+v", got, err, want)
		}
	}
	if _, err := ReadFrame(r); err != io.EOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

type oneByteReader struct{ io.Reader }

func (r *oneByteReader) Read(p []byte) (int, error) {
	return r.Reader.Read(p[:1])
}

func TestInvalidFrames(t *testing.T) {
	var oversized [4]byte
	binary.BigEndian.PutUint32(oversized[:], MaxPayload+1)
	for name, data := range map[string][]byte{
		"zero":        {0, 0, 0, 0},
		"oversized":   oversized[:],
		"header EOF":  {0, 0},
		"payload EOF": {0, 0, 0, 2, 'x'},
		"bad UTF-8":   {0, 0, 0, 1, 0xff},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ReadFrame(bytes.NewReader(data)); err == nil {
				t.Fatal("accepted invalid frame")
			}
		})
	}
}

func TestRequestValidation(t *testing.T) {
	for _, payload := range []string{
		`{"id":"a","target":"users"}`,
		`{"id":"a","target":"users","body":null}`,
		`{"id":"a","target":"users","body":1}`,
		`{"id":"","target":"users","body":""}`,
		`{"id":"a","target":"","body":""}`,
		`null`, `[]`, `{`,
	} {
		var frame bytes.Buffer
		if err := WriteFrame(&frame, []byte(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadRequest(&frame); err == nil {
			t.Fatalf("accepted %s", payload)
		}
	}
}

func TestPayloadLimit(t *testing.T) {
	for _, size := range []int{MaxPayload, MaxPayload + 1} {
		var frame bytes.Buffer
		err := WriteFrame(&frame, []byte(strings.Repeat("x", size)))
		if size > MaxPayload {
			if err == nil {
				t.Fatal("accepted oversized payload")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		payload, err := ReadFrame(&frame)
		if err != nil || len(payload) != size {
			t.Fatalf("boundary payload: length %d, error %v", len(payload), err)
		}
	}
}
