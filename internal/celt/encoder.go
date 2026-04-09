// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package celt

import (
	"github.com/pion/opus/internal/rangecoding"
)

// Encoder encodes PCM audio into CELT frames.
type Encoder struct {
	rangeEncoder *rangecoding.Encoder

	bandwidth Bandwidth
	channels  int
	frameSize int

	// Previous frame state
	previousEnergies []float64
	prevSamplesLeft  []float64
	prevSamplesRight []float64

	// MDCT overlap buffer
	overlapLeft  []float64
	overlapRight []float64
}

// NewEncoder creates a new CELT encoder.
func NewEncoder(channels int) *Encoder {
	frameSize := FrameSize20ms
	return &Encoder{
		rangeEncoder:     rangecoding.NewEncoder(),
		bandwidth:        BandwidthFullband,
		channels:         channels,
		frameSize:        frameSize,
		previousEnergies: make([]float64, numBandsFB),
		prevSamplesLeft:  make([]float64, frameSize),
		prevSamplesRight: make([]float64, frameSize),
		overlapLeft:      make([]float64, frameSize),
		overlapRight:     make([]float64, frameSize),
	}
}

// SetBandwidth sets the encoding bandwidth.
func (e *Encoder) SetBandwidth(bw Bandwidth) {
	e.bandwidth = bw
}

// SetFrameSize sets the frame size in samples at 48kHz.
func (e *Encoder) SetFrameSize(size int) {
	e.frameSize = size
	e.prevSamplesLeft = make([]float64, size)
	e.prevSamplesRight = make([]float64, size)
	e.overlapLeft = make([]float64, size)
	e.overlapRight = make([]float64, size)
}

// FrameSize returns the current frame size.
func (e *Encoder) FrameSize() int {
	return e.frameSize
}

// Channels returns the number of channels.
func (e *Encoder) Channels() int {
	return e.channels
}

// Encode encodes a frame of float32 PCM samples.
// For stereo, samples should be interleaved [L, R, L, R, ...].
// For mono, just sequential samples.
func (e *Encoder) Encode(samples []float32) ([]byte, error) {
	e.rangeEncoder.Reset()

	// Convert to float64 for internal processing
	samplesF64 := make([]float64, len(samples))
	for i, s := range samples {
		samplesF64[i] = float64(s)
	}

	// Deinterleave stereo if needed
	var left, right []float64
	if e.channels == 2 {
		left = make([]float64, e.frameSize)
		right = make([]float64, e.frameSize)
		for i := 0; i < e.frameSize; i++ {
			if i*2 < len(samplesF64) {
				left[i] = samplesF64[i*2]
			}
			if i*2+1 < len(samplesF64) {
				right[i] = samplesF64[i*2+1]
			}
		}
	} else {
		left = samplesF64
		if len(left) > e.frameSize {
			left = left[:e.frameSize]
		}
	}

	// Build MDCT input (overlap-add with previous frame)
	mdctInput := make([]float64, e.frameSize*2)
	copy(mdctInput[:e.frameSize], e.overlapLeft)
	copy(mdctInput[e.frameSize:], left)
	copy(e.overlapLeft, left) // Save for next frame

	// Perform MDCT on left channel (or mono)
	spectrumLeft := make([]float64, e.frameSize)
	MDCT(mdctInput, spectrumLeft)

	// For stereo, also process right channel
	var spectrumRight []float64
	if e.channels == 2 {
		copy(mdctInput[:e.frameSize], e.overlapRight)
		copy(mdctInput[e.frameSize:], right)
		copy(e.overlapRight, right)

		spectrumRight = make([]float64, e.frameSize)
		MDCT(mdctInput, spectrumRight)
	}

	// Compute band energies
	energiesLeft := ComputeBandEnergies(spectrumLeft, e.bandwidth)

	var energiesRight []float64
	if e.channels == 2 {
		energiesRight = ComputeBandEnergies(spectrumRight, e.bandwidth)
	}

	// Encode the bitstream
	e.encodeFrame(spectrumLeft, spectrumRight, energiesLeft, energiesRight)

	return e.rangeEncoder.Finalize(), nil
}

// encodeFrame encodes a single CELT frame.
func (e *Encoder) encodeFrame(spectrumLeft, spectrumRight []float64, energiesLeft, energiesRight []float64) {
	numBands := e.numBands()

	// Encode silence flag (0 = not silence)
	e.rangeEncoder.EncodeSymbolLogP(0, 15)

	// Encode post-filter flag (0 = no post-filter)
	e.rangeEncoder.EncodeSymbolLogP(0, 1)

	// Encode transient flag (0 = no transient)
	e.rangeEncoder.EncodeSymbolLogP(0, 3)

	// Encode intra flag (1 = intra-frame, no prediction)
	e.rangeEncoder.EncodeSymbolLogP(1, 3)

	// Quantize and encode coarse energies
	coarseIndices, quantizedEnergies := QuantizeEnergies(energiesLeft, nil)
	e.encodeCoarseEnergies(coarseIndices, numBands)

	// For stereo, encode mid-side energy balance
	if e.channels == 2 && energiesRight != nil {
		e.encodeStereoBalance(energiesLeft, energiesRight, numBands)
	}

	// Encode fine energies (simplified - just encode zeros)
	e.encodeFineEnergies(numBands)

	// Encode spectral coefficients using PVQ
	normalized := NormalizeBands(spectrumLeft, quantizedEnergies, e.bandwidth)
	e.encodePVQ(normalized, numBands)

	// Update state
	e.previousEnergies = quantizedEnergies
}

// numBands returns the number of bands for current bandwidth.
func (e *Encoder) numBands() int {
	return NumBands(e.bandwidth)
}

// encodeCoarseEnergies encodes the coarse energy values.
func (e *Encoder) encodeCoarseEnergies(indices []int, numBands int) {
	// CELT coarse energy uses Laplace-like distribution
	// For simplicity, encode as uniform symbols
	for i := 0; i < numBands && i < len(indices); i++ {
		// Map index from [-15, 15] to [0, 30]
		symbol := uint32(indices[i] + 15)
		if symbol > 30 {
			symbol = 30
		}
		// Use a simple uniform distribution
		e.rangeEncoder.EncodeSymbolWithICDF(symbol, icdfCoarseEnergy)
	}
}

// encodeStereoBalance encodes the mid-side energy balance for stereo.
func (e *Encoder) encodeStereoBalance(energiesLeft, energiesRight []float64, numBands int) {
	for i := 0; i < numBands && i < len(energiesLeft) && i < len(energiesRight); i++ {
		// Simple balance encoding (0 = center)
		e.rangeEncoder.EncodeSymbolWithICDF(8, icdfStereoBalance) // 8 = center
	}
}

// encodeFineEnergies encodes fine energy corrections.
func (e *Encoder) encodeFineEnergies(numBands int) {
	// For minimal encoder, skip fine energy bits
	// This is done by not allocating bits to fine energy
}

// encodePVQ encodes the normalized spectral coefficients using PVQ.
func (e *Encoder) encodePVQ(normalized []float64, numBands int) {
	// Simplified PVQ encoding - just encode minimal pulses
	// Real CELT uses sophisticated bit allocation and PVQ
	for band := 0; band < numBands; band++ {
		// Encode number of pulses (0 = silent band)
		e.rangeEncoder.EncodeSymbolWithICDF(0, icdfPulseCount)
	}
}

// EncodeSilence creates a minimal silent CELT frame.
func (e *Encoder) EncodeSilence() ([]byte, error) {
	e.rangeEncoder.Reset()

	// Encode silence flag (1 = silence)
	e.rangeEncoder.EncodeSymbolLogP(1, 15)

	return e.rangeEncoder.Finalize(), nil
}

// ICDF tables for CELT encoding

// Coarse energy ICDF (31 symbols for range [-15, 15])
var icdfCoarseEnergy = []uint{
	256, 8, 16, 24, 32, 40, 48, 56, 64, 72, 80, 88, 96, 104, 112, 120,
	128, 136, 144, 152, 160, 168, 176, 184, 192, 200, 208, 216, 224, 232, 240, 256,
}

// Stereo balance ICDF (17 symbols)
var icdfStereoBalance = []uint{
	256, 15, 30, 45, 60, 75, 90, 105, 120, 135, 150, 165, 180, 195, 210, 225, 240, 256,
}

// Pulse count ICDF (simplified)
var icdfPulseCount = []uint{
	256, 200, 240, 256,
}
