package packet

import (
	"encoding/json"

	"eko-bug-repro/pkg/assert"
)

type Payload interface {
	Type() PacketType
}

type defaultPacketEncoder struct {
	data       []byte
	encoding   Encoding
	packetType PacketType
}

func (e defaultPacketEncoder) Encoding() Encoding {
	return e.encoding
}

func (e defaultPacketEncoder) Type() PacketType {
	return e.packetType
}

func (e defaultPacketEncoder) Payload() []byte {
	return e.data
}

func NewJsonEncoder(payload Payload) PacketEncoder {
	data, err := json.Marshal(payload)
	assert.NoError(err, "encoding a message with JSON should never fail")

	return defaultPacketEncoder{
		data:       data,
		encoding:   EncodingJson,
		packetType: payload.Type(),
	}
}
