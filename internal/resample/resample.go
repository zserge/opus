// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package resample provides tools to resample audio
package resample

// Up upsamples the requested amount.
func Up(in, out []float32, upsampleCount int) {
	currIndex := 0
	for i := range in {
		for j := 0; j < upsampleCount; j++ {
			out[currIndex] = in[i]
			currIndex++
		}
	}
}

// Down downsamples by the requested factor.
// Simple averaging decimation filter.
func Down(in, out []float32, downsampleFactor int) {
	outIndex := 0
	for i := 0; i+downsampleFactor <= len(in) && outIndex < len(out); i += downsampleFactor {
		sum := float32(0)
		for j := 0; j < downsampleFactor; j++ {
			sum += in[i+j]
		}
		out[outIndex] = sum / float32(downsampleFactor)
		outIndex++
	}
}

// Resample resamples from srcRate to dstRate.
// Uses simple linear interpolation for non-integer ratios.
func Resample(in []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate {
		out := make([]float32, len(in))
		copy(out, in)
		return out
	}

	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(in)) / ratio)
	out := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := float32(srcPos - float64(srcIdx))

		if srcIdx+1 < len(in) {
			out[i] = in[srcIdx]*(1-frac) + in[srcIdx+1]*frac
		} else if srcIdx < len(in) {
			out[i] = in[srcIdx]
		}
	}

	return out
}

// DownmixStereoToMono converts interleaved stereo to mono.
func DownmixStereoToMono(stereo []float32) []float32 {
	mono := make([]float32, len(stereo)/2)
	for i := 0; i < len(mono); i++ {
		mono[i] = (stereo[i*2] + stereo[i*2+1]) / 2
	}
	return mono
}

// InterleaveStereo interleaves separate L/R channels into stereo.
func InterleaveStereo(left, right []float32) []float32 {
	stereo := make([]float32, len(left)*2)
	for i := 0; i < len(left); i++ {
		stereo[i*2] = left[i]
		if i < len(right) {
			stereo[i*2+1] = right[i]
		}
	}
	return stereo
}

// DeinterleaveStereo splits interleaved stereo into L/R channels.
func DeinterleaveStereo(stereo []float32) (left, right []float32) {
	n := len(stereo) / 2
	left = make([]float32, n)
	right = make([]float32, n)
	for i := 0; i < n; i++ {
		left[i] = stereo[i*2]
		right[i] = stereo[i*2+1]
	}
	return
}
