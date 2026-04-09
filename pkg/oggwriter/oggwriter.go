// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package oggwriter implements the Ogg media container writer for Opus audio
package oggwriter

import (
	"encoding/binary"
	"errors"
	"io"
	"math/rand"
)

const (
	pageHeaderSignature  = "OggS"
	idPageSignature      = "OpusHead"
	commentPageSignature = "OpusTags"
	pageHeaderLen        = 27
	maxSegmentSize       = 255
	headerTypeBeginning  = 0x02
	headerTypeEnd        = 0x04
)

var (
	errNilWriter    = errors.New("writer is nil")
	errWriterClosed = errors.New("writer is closed")
)

// OggWriter writes Opus packets to an Ogg container.
type OggWriter struct {
	writer        io.Writer
	serial        uint32
	pageIndex     uint32
	granulePos    uint64
	checksumTable *[256]uint32
	closed        bool
	sampleRate    uint32
	channels      uint8
}

// Config contains the configuration for the OGG writer.
type Config struct {
	SampleRate uint32
	Channels   uint8
}

// New creates a new OggWriter that writes to the given io.Writer.
func New(w io.Writer, config Config) (*OggWriter, error) {
	if w == nil {
		return nil, errNilWriter
	}

	if config.SampleRate == 0 {
		config.SampleRate = 48000
	}
	if config.Channels == 0 {
		config.Channels = 1
	}

	writer := &OggWriter{
		writer:        w,
		serial:        rand.Uint32(), //nolint:gosec
		checksumTable: generateChecksumTable(),
		sampleRate:    config.SampleRate,
		channels:      config.Channels,
	}

	if err := writer.writeIDPage(); err != nil {
		return nil, err
	}

	if err := writer.writeCommentPage(); err != nil {
		return nil, err
	}

	return writer, nil
}

func (o *OggWriter) writeIDPage() error {
	payload := make([]byte, 19)
	copy(payload[0:8], idPageSignature)
	payload[8] = 1
	payload[9] = o.channels
	binary.LittleEndian.PutUint16(payload[10:12], 3840)
	binary.LittleEndian.PutUint32(payload[12:16], o.sampleRate)
	binary.LittleEndian.PutUint16(payload[16:18], 0)
	payload[18] = 0

	return o.writePage(payload, headerTypeBeginning, 0)
}

func (o *OggWriter) writeCommentPage() error {
	vendor := "pion/opus"
	payloadSize := 8 + 4 + len(vendor) + 4

	payload := make([]byte, payloadSize)
	copy(payload[0:8], commentPageSignature)
	binary.LittleEndian.PutUint32(payload[8:12], uint32(len(vendor)))
	copy(payload[12:12+len(vendor)], vendor)
	binary.LittleEndian.PutUint32(payload[12+len(vendor):], 0)

	return o.writePage(payload, 0, 0)
}

// WritePacket writes an Opus packet to the Ogg stream.
func (o *OggWriter) WritePacket(opusPacket []byte, samplesPerPacket uint32) error {
	if o.closed {
		return errWriterClosed
	}

	o.granulePos += uint64(samplesPerPacket)
	return o.writePage(opusPacket, 0, o.granulePos)
}

// Close finalizes the Ogg stream.
func (o *OggWriter) Close() error {
	if o.closed {
		return errWriterClosed
	}

	err := o.writePage(nil, headerTypeEnd, o.granulePos)
	o.closed = true
	return err
}

func (o *OggWriter) writePage(payload []byte, headerType uint8, granulePos uint64) error {
	segments := segmentPayload(payload)
	headerSize := pageHeaderLen + len(segments)
	header := make([]byte, headerSize)

	copy(header[0:4], pageHeaderSignature)
	header[4] = 0
	header[5] = headerType
	binary.LittleEndian.PutUint64(header[6:14], granulePos)
	binary.LittleEndian.PutUint32(header[14:18], o.serial)
	binary.LittleEndian.PutUint32(header[18:22], o.pageIndex)
	header[26] = uint8(len(segments))

	for i, seg := range segments {
		header[pageHeaderLen+i] = seg
	}

	checksum := o.calculateChecksum(header, payload)
	binary.LittleEndian.PutUint32(header[22:26], checksum)

	if _, err := o.writer.Write(header); err != nil {
		return err
	}

	if len(payload) > 0 {
		if _, err := o.writer.Write(payload); err != nil {
			return err
		}
	}

	o.pageIndex++
	return nil
}

func segmentPayload(payload []byte) []uint8 {
	if len(payload) == 0 {
		return []uint8{0}
	}

	segments := make([]uint8, 0, len(payload)/maxSegmentSize+1)
	remaining := len(payload)

	for remaining > 0 {
		if remaining >= maxSegmentSize {
			segments = append(segments, maxSegmentSize)
			remaining -= maxSegmentSize
		} else {
			segments = append(segments, uint8(remaining))
			remaining = 0
		}
	}

	if len(segments) > 0 && segments[len(segments)-1] == maxSegmentSize {
		segments = append(segments, 0)
	}

	return segments
}

func (o *OggWriter) calculateChecksum(header, payload []byte) uint32 {
	var checksum uint32

	updateChecksum := func(v byte) {
		checksum = (checksum << 8) ^ o.checksumTable[byte(checksum>>24)^v]
	}

	for i, b := range header {
		if i >= 22 && i < 26 {
			updateChecksum(0)
		} else {
			updateChecksum(b)
		}
	}

	for _, b := range payload {
		updateChecksum(b)
	}

	return checksum
}

func generateChecksumTable() *[256]uint32 {
	var table [256]uint32
	const poly = 0x04c11db7

	for i := range table {
		r := uint32(i) << 24 //nolint:gosec

		for j := 0; j < 8; j++ {
			if (r & 0x80000000) != 0 {
				r = (r << 1) ^ poly
			} else {
				r <<= 1
			}
			table[i] = (r & 0xffffffff)
		}
	}

	return &table
}
