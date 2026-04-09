// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

// Encoder implements RFC 6716 Section 5.1
// The range encoder produces the same bitstream that the range decoder consumes.
// It maintains the state (low, rng) and outputs bytes as the range narrows.
type Encoder struct {
	low   uint32 // Lower bound of the current range
	rng   uint32 // Size of the current range
	rem   int    // Buffered byte (-1 if none)
	ext   uint32 // Number of 0xFF bytes buffered
	buf   []byte // Output buffer
	nbits uint32 // Total bits written (for tracking)
}

// NewEncoder creates a new range encoder.
func NewEncoder() *Encoder {
	return &Encoder{
		low: 0,
		rng: 0x80000000, // 2^31
		rem: -1,
		ext: 0,
		buf: make([]byte, 0, 256),
	}
}

// Reset clears the encoder state for reuse.
func (e *Encoder) Reset() {
	e.low = 0
	e.rng = 0x80000000
	e.rem = -1
	e.ext = 0
	e.buf = e.buf[:0]
	e.nbits = 0
}

// normalize outputs bytes and renormalizes the range.
// This is called after each symbol encoding to maintain rng >= 2^23.
func (e *Encoder) normalize() {
	for e.rng <= 0x800000 {
		e.outputByte()
		e.low = (e.low << 8) & 0x7FFFFFFF
		e.rng <<= 8
	}
}

// outputByte handles the carry propagation and byte output.
func (e *Encoder) outputByte() {
	// Check for carry
	if e.low < 0xFF000000 || (e.low>>31) != 0 {
		carry := e.low >> 31

		if e.rem >= 0 {
			e.buf = append(e.buf, byte(uint32(e.rem)+carry))
		}

		// Output any buffered 0xFF bytes (with carry propagation)
		for ; e.ext > 0; e.ext-- {
			e.buf = append(e.buf, byte(0xFF+carry))
		}

		// Buffer the current byte
		e.rem = int((e.low >> 23) & 0xFF)
	} else {
		// Buffer another 0xFF
		e.ext++
	}
}

// EncodeSymbolWithICDF encodes a symbol using an ICDF table.
// The ICDF table format is the same as used by DecodeSymbolWithICDF:
// cdf[0] = total, cdf[k+1] = cumulative frequency up to symbol k
//
// For encoding symbol k:
//
//	fl = cdf[k] (0 for k=0)
//	fh = cdf[k+1]
//	ft = cdf[0]
func (e *Encoder) EncodeSymbolWithICDF(symbol uint32, icdf []uint) {
	total := uint32(icdf[0])
	icdf = icdf[1:]

	var fl, fh uint32
	if symbol > 0 {
		fl = uint32(icdf[symbol-1])
	} else {
		fl = 0
	}
	fh = uint32(icdf[symbol])

	e.encodeFrequency(fl, fh, total)
}

// encodeFrequency encodes a symbol given its frequency range [fl, fh) out of total ft.
// This matches the Opus ICDF convention where the decoder interprets val as
// distance from the high end of the range.
func (e *Encoder) encodeFrequency(fl, fh, ft uint32) {
	scale := e.rng / ft

	// Update the range - Opus uses inverse CDF, so we add scale*fl to low
	e.low += scale * fl
	if fl > 0 {
		e.rng = scale * (fh - fl)
	} else {
		e.rng = scale * fh
	}

	e.normalize()
}

// EncodeSymbolLogP encodes a binary symbol with probability 2^(-logp) for value 1.
// This matches DecodeSymbolLogP in the decoder.
func (e *Encoder) EncodeSymbolLogP(symbol uint32, logp uint) {
	scale := e.rng >> logp

	if symbol == 0 {
		// Symbol 0: decoder expects val >= scale, so keep low unchanged
		// and reduce range by scale
		e.rng -= scale
	} else {
		// Symbol 1: decoder expects val < scale, so shift low up
		// to put us in the top part of the range
		e.low += e.rng - scale
		e.rng = scale
	}

	e.normalize()
}

// EncodeBit encodes a single bit with equal probability.
func (e *Encoder) EncodeBit(bit uint32) {
	e.EncodeSymbolLogP(bit, 1)
}

// Finalize completes the encoding and returns the encoded bytes.
// It flushes any remaining state to produce a complete bitstream.
func (e *Encoder) Finalize() []byte {
	// Compute the number of bits needed to finalize
	// We need to output the minimum number of bits to uniquely identify
	// a value in the current range [low, low+rng).

	// Flush with carry handling
	finalLow := e.low

	// Output any buffered bytes with proper carry propagation
	if finalLow >= 0x80000000 {
		// Carry occurred
		if e.rem >= 0 {
			e.buf = append(e.buf, byte(e.rem+1))
		}
		for ; e.ext > 0; e.ext-- {
			e.buf = append(e.buf, 0x00)
		}
	} else {
		if e.rem >= 0 {
			e.buf = append(e.buf, byte(e.rem))
		}
		for ; e.ext > 0; e.ext-- {
			e.buf = append(e.buf, 0xFF)
		}
	}

	// Determine how many more bytes we need to output
	// The final bytes come from the high bits of low
	finalLow &= 0x7FFFFFFF

	// We need to output bytes until rng would be > 2^23 after renormalization
	// At minimum, output the byte at position 23..30
	if e.rng > 0 {
		// Output enough precision
		for shift := 23; shift >= 0; shift -= 8 {
			b := byte((finalLow >> shift) & 0xFF)
			e.buf = append(e.buf, b)
			// Stop after we've output enough to represent the range
			if shift <= 23-8 && e.rng >= (1<<23) {
				break
			}
		}
	}

	// Ensure at least one byte of output
	if len(e.buf) == 0 {
		e.buf = append(e.buf, 0)
	}

	return e.buf
}

// Bytes returns the current encoded bytes without finalizing.
func (e *Encoder) Bytes() []byte {
	return e.buf
}
