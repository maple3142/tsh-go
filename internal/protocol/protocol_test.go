package protocol

import (
	"bytes"
	"testing"
)

func TestRequestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	want := Request{Mode: RunShellNoTTY}

	if err := WriteRequest(&buf, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRequest(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ReadRequest() = %+v, want %+v", got, want)
	}
}

func TestRejectsUnknownMode(t *testing.T) {
	if _, err := ParseMode(255); err == nil {
		t.Fatal("ParseMode accepted unknown mode")
	}
	if err := WriteRequest(&bytes.Buffer{}, Request{Mode: Mode(255)}); err == nil {
		t.Fatal("WriteRequest accepted unknown mode")
	}
	if _, err := ReadRequest(bytes.NewReader([]byte{255})); err == nil {
		t.Fatal("ReadRequest accepted unknown mode")
	}
}
