package api

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mook/mockesphome/api/pb"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

func TestConnectionReadVarInt(t *testing.T) {
	cases := []struct {
		input    []byte
		expected uint64
	}{
		{[]byte{0}, 0},
		{[]byte{0x96, 0x01}, 150},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%02x", c.input), func(t *testing.T) {
			it := &server{conn: bytes.NewBuffer(c.input)}
			actual, err := it.readVarInt()
			assert.NilError(t, err)
			assert.Equal(t, c.expected, actual)
		})
	}
}

func TestConnectionReadMessage(t *testing.T) {
	expected := &pb.ConnectResponse{}
	expected.SetInvalidPassword(true)
	input, err := proto.Marshal(expected)
	assert.NilError(t, err)
	header := []byte{
		0x00,             // header byte
		byte(len(input)), // one byte of data
		0x04,             // id 4 = ConnectResponse
	}
	it := &server{conn: bytes.NewBuffer(append(header, input...))}
	actual, err := it.readMessage(t.Context())
	assert.NilError(t, err)
	assert.Assert(t, proto.Equal(expected, actual))
}
