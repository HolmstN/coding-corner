package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

const MaxPayload = 1 << 20

type Request struct {
	ID     string `json:"id"`
	Target string `json:"target"`
	Body   string `json:"body"`
}

type Response struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Body   string `json:"body"`
}

func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 || length > MaxPayload {
		return nil, fmt.Errorf("invalid frame length: %d", length)
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("payload is not UTF-8")
	}
	return payload, nil
}

func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 || len(payload) > MaxPayload || !utf8.Valid(payload) {
		return fmt.Errorf("invalid payload")
	}
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame, uint32(len(payload)))
	copy(frame[4:], payload)
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

func WriteJSON(w io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return WriteFrame(w, payload)
}

func ReadRequest(r io.Reader) (Request, error) {
	payload, err := ReadFrame(r)
	if err != nil {
		return Request{}, err
	}
	var wire struct {
		ID     *string `json:"id"`
		Target *string `json:"target"`
		Body   *string `json:"body"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Request{}, err
	}
	if wire.ID == nil || *wire.ID == "" || wire.Target == nil || *wire.Target == "" || wire.Body == nil {
		return Request{}, fmt.Errorf("id and target must be nonempty strings; body must be a string")
	}
	return Request{ID: *wire.ID, Target: *wire.Target, Body: *wire.Body}, nil
}

func ReadResponse(r io.Reader) (Response, error) {
	payload, err := ReadFrame(r)
	if err != nil {
		return Response{}, err
	}
	var wire struct {
		ID     *string `json:"id"`
		Status *string `json:"status"`
		Body   *string `json:"body"`
	}
	if err := json.Unmarshal(payload, &wire); err != nil {
		return Response{}, err
	}
	if wire.ID == nil || *wire.ID == "" || wire.Status == nil || wire.Body == nil {
		return Response{}, fmt.Errorf("response requires string id, status, and body")
	}
	if *wire.Status != "ok" && *wire.Status != "error" {
		return Response{}, fmt.Errorf("invalid response status: %q", *wire.Status)
	}
	if *wire.Status == "error" && *wire.Body == "" {
		return Response{}, fmt.Errorf("error response must explain the failure")
	}
	return Response{ID: *wire.ID, Status: *wire.Status, Body: *wire.Body}, nil
}
