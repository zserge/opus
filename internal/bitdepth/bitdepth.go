// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package bitdepth provides utilities to convert between different audio bitdepths
package bitdepth

import (
	"math"
)

// ConvertFloat32LittleEndianToSigned16LittleEndian converts a f32le to s16le.
func ConvertFloat32LittleEndianToSigned16LittleEndian(in []float32, out []byte, resampleCount int) error {
	currIndex := 0
	for i := range in {
		res := int16(math.Floor(float64(in[i] * 32767)))

		for j := resampleCount; j > 0; j-- {
			out[currIndex] = byte(res & 0b11111111)
			currIndex++

			out[currIndex] = (byte(res >> 8))
			currIndex++
		}
	}

	return nil
}

// ConvertSigned16LittleEndianToFloat32LittleEndian converts s16le to f32le.
func ConvertSigned16LittleEndianToFloat32LittleEndian(in []byte, out []float32) error {
	for i := 0; i < len(out); i++ {
		byteIndex := i * 2
		if byteIndex+1 >= len(in) {
			break
		}
		// Read little-endian int16
		sample := int16(in[byteIndex]) | (int16(in[byteIndex+1]) << 8)
		// Convert to float32 in range [-1, 1]
		out[i] = float32(sample) / 32768.0
	}
	return nil
}
