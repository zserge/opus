// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package oggwriter

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOggWriterCreation(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{
		SampleRate: 48000,
		Channels:   2,
	})
	require.NoError(t, err)
	require.NotNil(t, writer)

	err = writer.Close()
	require.NoError(t, err)

	data := buf.Bytes()
	assert.GreaterOrEqual(t, len(data), 4)
	assert.Equal(t, "OggS", string(data[0:4]))
}

func TestOggWriterNilWriter(t *testing.T) {
	_, err := New(nil, Config{})
	assert.Error(t, err)
	assert.Equal(t, errNilWriter, err)
}

func TestOggWriterDefaultConfig(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{})
	require.NoError(t, err)
	require.NotNil(t, writer)

	assert.Equal(t, uint32(48000), writer.sampleRate)
	assert.Equal(t, uint8(1), writer.channels)

	err = writer.Close()
	require.NoError(t, err)
}

func TestOggWriterWritePacket(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{
		SampleRate: 48000,
		Channels:   1,
	})
	require.NoError(t, err)

	opusPacket := []byte{0x48, 0x0, 0x0, 0x0}
	for i := 0; i < 5; i++ {
		err = writer.WritePacket(opusPacket, 960)
		require.NoError(t, err)
	}

	err = writer.Close()
	require.NoError(t, err)
}

func TestOggWriterCloseTwice(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{})
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	err = writer.Close()
	assert.Error(t, err)
	assert.Equal(t, errWriterClosed, err)
}

func TestOggWriterWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{})
	require.NoError(t, err)

	err = writer.Close()
	require.NoError(t, err)

	err = writer.WritePacket([]byte{0x00}, 960)
	assert.Error(t, err)
	assert.Equal(t, errWriterClosed, err)
}

func TestSegmentPayload(t *testing.T) {
	tests := []struct {
		name     string
		size     int
		expected int
	}{
		{"empty", 0, 1},
		{"small", 100, 1},
		{"exact255", 255, 2},
		{"256", 256, 2},
		{"large", 1000, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload := make([]byte, tt.size)
			segments := segmentPayload(payload)
			assert.Equal(t, tt.expected, len(segments))

			var sum int
			for _, s := range segments {
				sum += int(s)
			}
			assert.Equal(t, tt.size, sum)
		})
	}
}

func TestOggHeaderFormat(t *testing.T) {
	var buf bytes.Buffer

	writer, err := New(&buf, Config{
		SampleRate: 48000,
		Channels:   2,
	})
	require.NoError(t, err)
	require.NotNil(t, writer)

	err = writer.Close()
	require.NoError(t, err)

	data := buf.Bytes()

	opusHeadIdx := bytes.Index(data, []byte("OpusHead"))
	require.NotEqual(t, -1, opusHeadIdx, "OpusHead not found")

	channelIdx := opusHeadIdx + 9
	assert.Equal(t, uint8(2), data[channelIdx], "channel count should be 2")

	opusTagsIdx := bytes.Index(data, []byte("OpusTags"))
	require.NotEqual(t, -1, opusTagsIdx, "OpusTags not found")
}
