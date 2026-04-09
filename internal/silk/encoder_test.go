// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderCreation(t *testing.T) {
	enc := NewEncoder()
	assert.NotNil(t, enc)
}

func TestEncoderEncodeSilence(t *testing.T) {
	enc := NewEncoder()

	frame, err := enc.EncodeSilence()
	require.NoError(t, err)
	assert.NotEmpty(t, frame)

	// The frame should be decodable
	dec := NewDecoder()
	out := make([]float32, 320) // 20ms at 16kHz

	err = dec.Decode(frame, out, false, 20000000, BandwidthWideband)
	require.NoError(t, err)
}

func TestEncoderEncodeQuietSignal(t *testing.T) {
	enc := NewEncoder()

	// Very quiet signal (effectively silence)
	samples := make([]float32, 320) // 20ms at 16kHz
	for i := range samples {
		samples[i] = 0.0001 * float32(math.Sin(2*math.Pi*100*float64(i)/16000))
	}

	frame, err := enc.Encode(samples)
	require.NoError(t, err)
	assert.NotEmpty(t, frame)
}

func TestEncoderEncodeSineWave(t *testing.T) {
	enc := NewEncoder()

	// Generate a 440 Hz sine wave
	samples := make([]float32, 320)
	for i := range samples {
		samples[i] = 0.5 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}

	frame, err := enc.Encode(samples)
	require.NoError(t, err)
	assert.NotEmpty(t, frame)
}

func TestEncoderMultipleFrames(t *testing.T) {
	enc := NewEncoder()

	// Encode multiple frames to test state persistence
	for i := 0; i < 10; i++ {
		samples := make([]float32, 320)
		freq := 200 + float64(i*50) // Varying frequency
		for j := range samples {
			samples[j] = 0.3 * float32(math.Sin(2*math.Pi*freq*float64(j)/16000))
		}

		frame, err := enc.Encode(samples)
		require.NoError(t, err)
		assert.NotEmpty(t, frame)
	}
}

func TestEncoderDecoderRoundTrip(t *testing.T) {
	enc := NewEncoder()
	dec := NewDecoder()

	// Test with silence
	silentFrame, err := enc.EncodeSilence()
	require.NoError(t, err)

	out := make([]float32, 320)
	err = dec.Decode(silentFrame, out, false, 20000000, BandwidthWideband)
	require.NoError(t, err)
}

func TestEncoderSetBandwidth(t *testing.T) {
	enc := NewEncoder()

	enc.SetBandwidth(BandwidthNarrowband)
	frame, err := enc.EncodeSilence()
	require.NoError(t, err)
	assert.NotEmpty(t, frame)

	enc.SetBandwidth(BandwidthMediumband)
	frame, err = enc.EncodeSilence()
	require.NoError(t, err)
	assert.NotEmpty(t, frame)

	enc.SetBandwidth(BandwidthWideband)
	frame, err = enc.EncodeSilence()
	require.NoError(t, err)
	assert.NotEmpty(t, frame)
}
