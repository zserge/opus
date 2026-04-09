// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package celt implements the CELT (Constrained Energy Lapped Transform) codec
// as defined in RFC 6716 for the Opus audio codec.
package celt

import (
	"math"
)

// Bandwidth represents the audio bandwidth for CELT.
type Bandwidth int

// Bandwidth values.
const (
	BandwidthNarrowband Bandwidth = iota
	BandwidthWideband
	BandwidthSuperwideband
	BandwidthFullband
)

// Frame sizes in samples at 48kHz.
const (
	FrameSize2_5ms = 120
	FrameSize5ms   = 240
	FrameSize10ms  = 480
	FrameSize20ms  = 960
)

// Number of bands for each bandwidth.
const (
	numBandsNB  = 13
	numBandsWB  = 17
	numBandsSWB = 19
	numBandsFB  = 21
)

// CELT band boundaries (indices into MDCT spectrum at 48kHz, 20ms).
var bandBoundaries = []int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 14, 16,
	20, 24, 28, 36, 44, 52, 68, 84, 116,
}

// MDCT performs the Modified Discrete Cosine Transform.
// input should have 2N samples, output will have N coefficients.
func MDCT(input []float64, output []float64) {
	N := len(output)
	N2 := N * 2

	// Apply window (Kaiser-Bessel derived window approximation)
	windowed := make([]float64, N2)
	for n := 0; n < N2; n++ {
		// Simple sine window
		w := math.Sin(math.Pi * (float64(n) + 0.5) / float64(N2))
		windowed[n] = input[n] * w
	}

	// MDCT formula: X[k] = sum_{n=0}^{2N-1} x[n] * cos(pi/N * (n + 0.5 + N/2) * (k + 0.5))
	for k := 0; k < N; k++ {
		var sum float64
		for n := 0; n < N2; n++ {
			phase := math.Pi / float64(N) * (float64(n) + 0.5 + float64(N)/2.0) * (float64(k) + 0.5)
			sum += windowed[n] * math.Cos(phase)
		}
		output[k] = sum
	}
}

// ComputeBandEnergies computes the energy in each CELT band.
func ComputeBandEnergies(spectrum []float64, bandwidth Bandwidth) []float64 {
	numBands := NumBands(bandwidth)
	energies := make([]float64, numBands)

	specLen := len(spectrum)
	for band := 0; band < numBands; band++ {
		start := bandBoundaries[band]
		end := bandBoundaries[band+1]
		if start >= specLen {
			break
		}
		if end > specLen {
			end = specLen
		}

		var energy float64
		for i := start; i < end; i++ {
			energy += spectrum[i] * spectrum[i]
		}
		// Convert to dB-like scale
		if energy > 1e-10 {
			energies[band] = math.Log2(math.Sqrt(energy))
		} else {
			energies[band] = -15.0 // Minimum energy
		}
	}

	return energies
}

// QuantizeEnergies quantizes band energies for encoding.
// Returns quantization indices and reconstructed energies.
func QuantizeEnergies(energies []float64, prevEnergies []float64) ([]int, []float64) {
	indices := make([]int, len(energies))
	quantized := make([]float64, len(energies))

	for i, e := range energies {
		// Predict from previous
		var pred float64
		if prevEnergies != nil && i < len(prevEnergies) {
			pred = prevEnergies[i] * 0.5 // Simple prediction
		}

		// Compute residual and quantize
		residual := e - pred
		// Quantize to integer in range [-15, 15]
		idx := int(math.Round(residual))
		if idx < -15 {
			idx = -15
		}
		if idx > 15 {
			idx = 15
		}
		indices[i] = idx
		quantized[i] = pred + float64(idx)
	}

	return indices, quantized
}

// NormalizeBands normalizes spectral coefficients using band energies.
func NormalizeBands(spectrum []float64, energies []float64, bandwidth Bandwidth) []float64 {
	numBands := NumBands(bandwidth)
	normalized := make([]float64, len(spectrum))

	for band := 0; band < numBands && band < len(energies); band++ {
		start := bandBoundaries[band]
		end := bandBoundaries[band+1]
		if start >= len(spectrum) {
			break
		}
		if end > len(spectrum) {
			end = len(spectrum)
		}

		// Compute gain from quantized energy
		gain := math.Pow(2, energies[band])
		if gain < 1e-10 {
			gain = 1e-10
		}

		// Normalize
		for i := start; i < end; i++ {
			normalized[i] = spectrum[i] / gain
		}
	}

	return normalized
}

// NumBands returns the number of bands for a given bandwidth.
func NumBands(bandwidth Bandwidth) int {
	switch bandwidth {
	case BandwidthNarrowband:
		return numBandsNB
	case BandwidthWideband:
		return numBandsWB
	case BandwidthSuperwideband:
		return numBandsSWB
	default:
		return numBandsFB
	}
}


