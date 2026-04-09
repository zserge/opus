// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package rangecoding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncoderDecoderRoundTrip(t *testing.T) {
	// Test ICDF encoding/decoding round trip
	icdf := []uint{256, 64, 128, 192, 256} // 4 symbols, uniform distribution

	t.Run("SingleSymbol", func(t *testing.T) {
		for symbol := uint32(0); symbol < 4; symbol++ {
			enc := NewEncoder()
			enc.EncodeSymbolWithICDF(symbol, icdf)
			data := enc.Finalize()

			var dec Decoder
			dec.Init(data)
			decoded := dec.DecodeSymbolWithICDF(icdf)

			assert.Equal(t, symbol, decoded, "Symbol mismatch for symbol %d", symbol)
		}
	})

	t.Run("MultipleSymbols", func(t *testing.T) {
		symbols := []uint32{0, 1, 2, 3, 1, 2, 0, 3}

		enc := NewEncoder()
		for _, sym := range symbols {
			enc.EncodeSymbolWithICDF(sym, icdf)
		}
		data := enc.Finalize()

		var dec Decoder
		dec.Init(data)
		for i, expected := range symbols {
			decoded := dec.DecodeSymbolWithICDF(icdf)
			assert.Equal(t, expected, decoded, "Symbol mismatch at position %d", i)
		}
	})
}

func TestEncoderLogPRoundTrip(t *testing.T) {
	t.Run("SingleBit", func(t *testing.T) {
		for _, bit := range []uint32{0, 1} {
			enc := NewEncoder()
			enc.EncodeSymbolLogP(bit, 1)
			data := enc.Finalize()

			var dec Decoder
			dec.Init(data)
			decoded := dec.DecodeSymbolLogP(1)

			assert.Equal(t, bit, decoded, "Bit mismatch for bit %d", bit)
		}
	})

	t.Run("MultipleBits", func(t *testing.T) {
		bits := []uint32{1, 0, 1, 1, 0, 0, 1, 0}

		enc := NewEncoder()
		for _, bit := range bits {
			enc.EncodeSymbolLogP(bit, 1)
		}
		data := enc.Finalize()

		var dec Decoder
		dec.Init(data)
		for i, expected := range bits {
			decoded := dec.DecodeSymbolLogP(1)
			assert.Equal(t, expected, decoded, "Bit mismatch at position %d", i)
		}
	})
}

func TestEncoderMixedSymbols(t *testing.T) {
	// Test mixing ICDF and LogP encoding
	icdf := []uint{256, 50, 100, 200, 256}

	enc := NewEncoder()
	enc.EncodeSymbolLogP(1, 1)        // VAD bit
	enc.EncodeSymbolLogP(0, 1)        // LBRR bit
	enc.EncodeSymbolWithICDF(2, icdf) // Frame type
	enc.EncodeSymbolWithICDF(1, icdf) // Another symbol

	data := enc.Finalize()

	var dec Decoder
	dec.Init(data)

	assert.Equal(t, uint32(1), dec.DecodeSymbolLogP(1))
	assert.Equal(t, uint32(0), dec.DecodeSymbolLogP(1))
	assert.Equal(t, uint32(2), dec.DecodeSymbolWithICDF(icdf))
	assert.Equal(t, uint32(1), dec.DecodeSymbolWithICDF(icdf))
}

func TestEncoderSilkGainSymbols(t *testing.T) {
	// Use actual SILK probability tables
	gainHighbits := []uint{256, 32, 144, 212, 241, 253, 254, 255, 256}
	gainLowbits := []uint{256, 32, 64, 96, 128, 160, 192, 224, 256}

	enc := NewEncoder()
	enc.EncodeSymbolWithICDF(3, gainHighbits) // MSB
	enc.EncodeSymbolWithICDF(5, gainLowbits)  // LSB

	data := enc.Finalize()

	var dec Decoder
	dec.Init(data)

	assert.Equal(t, uint32(3), dec.DecodeSymbolWithICDF(gainHighbits))
	assert.Equal(t, uint32(5), dec.DecodeSymbolWithICDF(gainLowbits))
}
