// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package silk

import (
	"math"

	"github.com/pion/opus/internal/rangecoding"
)

// Encoder encodes PCM audio into SILK frames.
type Encoder struct {
	rangeEncoder *rangecoding.Encoder

	bandwidth Bandwidth

	// Previous frame state for prediction
	haveEncoded     bool
	previousLogGain int32
	previousNLSFQ15 []int16

	// LPC analysis state
	lpcOrder int
}

// NewEncoder creates a new SILK encoder.
func NewEncoder() *Encoder {
	return &Encoder{
		rangeEncoder: rangecoding.NewEncoder(),
		bandwidth:    BandwidthWideband,
		lpcOrder:     16, // WB uses 16, NB/MB use 10
	}
}

// SetBandwidth sets the encoding bandwidth.
func (e *Encoder) SetBandwidth(bandwidth Bandwidth) {
	e.bandwidth = bandwidth
	if bandwidth == BandwidthWideband {
		e.lpcOrder = 16
	} else {
		e.lpcOrder = 10
	}
}

// Encode encodes a frame of PCM samples into a SILK frame.
// samples should contain one frame worth of float32 samples (typically 320 for 20ms at 16kHz).
// Returns the encoded SILK frame bytes.
func (e *Encoder) Encode(samples []float32) ([]byte, error) {
	e.rangeEncoder.Reset()

	// Analyze input signal
	signalType, quantizationOffsetType := e.analyzeSignal(samples)

	// Encode header bits
	voiceActivityDetected := signalType != frameSignalTypeInactive
	e.encodeHeaderBits(voiceActivityDetected, false) // No LBRR

	// Encode frame type
	e.encodeFrameType(voiceActivityDetected, signalType, quantizationOffsetType)

	// Compute and encode gains
	gainQ16 := e.encodeSubframeQuantizations(samples, signalType)

	// Compute and encode LSF coefficients
	nlsfQ15 := e.encodeLSFCoefficients(samples, signalType)

	// Encode LSF interpolation weight (use 4 = no interpolation for simplicity)
	e.rangeEncoder.EncodeSymbolWithICDF(4, icdfNormalizedLSFInterpolationIndex)

	// For non-voiced frames, skip pitch/LTP parameters
	// Encode LCG seed (random)
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfLinearCongruentialGeneratorSeed)

	// Encode excitation
	e.encodeExcitation(samples, gainQ16, nlsfQ15, signalType, quantizationOffsetType)

	// Save state for next frame
	e.previousNLSFQ15 = nlsfQ15
	e.haveEncoded = true

	return e.rangeEncoder.Finalize(), nil
}

// analyzeSignal determines signal type based on energy analysis.
func (e *Encoder) analyzeSignal(samples []float32) (frameSignalType, frameQuantizationOffsetType) {
	// Compute signal energy
	energy := float32(0)
	for _, s := range samples {
		energy += s * s
	}
	energy /= float32(len(samples))

	// Simple classification based on energy
	// For minimal encoder, use unvoiced for simplicity (avoids pitch detection)
	if energy < 1e-8 {
		return frameSignalTypeInactive, frameQuantizationOffsetTypeLow
	}

	return frameSignalTypeUnvoiced, frameQuantizationOffsetTypeHigh
}

// encodeHeaderBits encodes VAD and LBRR header bits.
func (e *Encoder) encodeHeaderBits(voiceActivityDetected, lowBitRateRedundancy bool) {
	if voiceActivityDetected {
		e.rangeEncoder.EncodeSymbolLogP(1, 1)
	} else {
		e.rangeEncoder.EncodeSymbolLogP(0, 1)
	}

	if lowBitRateRedundancy {
		e.rangeEncoder.EncodeSymbolLogP(1, 1)
	} else {
		e.rangeEncoder.EncodeSymbolLogP(0, 1)
	}
}

// encodeFrameType encodes the frame type symbol.
func (e *Encoder) encodeFrameType(vad bool, signalType frameSignalType, quantizationOffsetType frameQuantizationOffsetType) {
	// Frame type encoding based on Table in decoder
	// VAD inactive: 0=inactive+low, 1=inactive+high
	// VAD active: 0=unvoiced+low, 1=unvoiced+high, 2=voiced+low, 3=voiced+high

	var symbol uint32
	if !vad {
		if quantizationOffsetType == frameQuantizationOffsetTypeLow {
			symbol = 0
		} else {
			symbol = 1
		}
		e.rangeEncoder.EncodeSymbolWithICDF(symbol, icdfFrameTypeVADInactive)
	} else {
		switch signalType {
		case frameSignalTypeUnvoiced:
			if quantizationOffsetType == frameQuantizationOffsetTypeLow {
				symbol = 0
			} else {
				symbol = 1
			}
		case frameSignalTypeVoiced:
			if quantizationOffsetType == frameQuantizationOffsetTypeLow {
				symbol = 2
			} else {
				symbol = 3
			}
		}
		e.rangeEncoder.EncodeSymbolWithICDF(symbol, icdfFrameTypeVADActive)
	}
}

// encodeSubframeQuantizations computes and encodes gain for each subframe.
func (e *Encoder) encodeSubframeQuantizations(samples []float32, signalType frameSignalType) []float32 {
	gainQ16 := make([]float32, subframeCount)
	subframeSize := len(samples) / subframeCount

	for sf := 0; sf < subframeCount; sf++ {
		// Compute subframe energy
		start := sf * subframeSize
		end := start + subframeSize
		energy := float32(0)
		for i := start; i < end; i++ {
			energy += samples[i] * samples[i]
		}
		energy /= float32(subframeSize)

		// Convert to log gain
		// gain_Q16 = silk_log2lin((0x1D1C71*log_gain>>16) + 2090)
		// We need to reverse this to find log_gain from energy
		var logGain int32
		if energy > 1e-10 {
			// Approximate: log_gain roughly maps to log2(sqrt(energy))
			// Scale to fit in 0-63 range
			logEnergy := float64(math.Log2(float64(energy) + 1e-10))
			logGain = int32((logEnergy + 20) * 1.5) // Heuristic scaling
			logGain = clamp(0, logGain, 63)
		} else {
			logGain = 0
		}

		if sf == 0 {
			// Independent coding
			gainIndex := logGain
			if e.haveEncoded {
				gainIndex = maxInt32(logGain, e.previousLogGain-16)
			}

			// Encode MSB (3 bits) and LSB (3 bits)
			msb := uint32(gainIndex >> 3)
			lsb := uint32(gainIndex & 7)

			var icdfMSB []uint
			switch signalType {
			case frameSignalTypeInactive:
				icdfMSB = icdfIndependentQuantizationGainMSBInactive
			case frameSignalTypeVoiced:
				icdfMSB = icdfIndependentQuantizationGainMSBVoiced
			default:
				icdfMSB = icdfIndependentQuantizationGainMSBUnvoiced
			}

			e.rangeEncoder.EncodeSymbolWithICDF(msb, icdfMSB)
			e.rangeEncoder.EncodeSymbolWithICDF(lsb, icdfIndependentQuantizationGainLSB)

			logGain = gainIndex
		} else {
			// Delta coding
			delta := logGain - e.previousLogGain + 4
			delta = clamp(0, delta, 40)
			e.rangeEncoder.EncodeSymbolWithICDF(uint32(delta), icdfDeltaQuantizationGain)

			logGain = clamp(0, maxInt32(2*delta-16, e.previousLogGain+delta-4), 63)
		}

		e.previousLogGain = logGain

		// Convert back to Q16 gain
		inLogQ7 := (0x1D1C71*logGain>>16 + 2090)
		i := inLogQ7 >> 7
		f := inLogQ7 & 127
		y := int32(46214)
		if (i & 1) != 0 {
			y = 32768
		}
		y >>= ((32 - i) >> 1)
		gainQ16[sf] = float32((1 << i) + ((-174*f*(128-f)>>16)+f)*((1<<i)>>7))
	}

	return gainQ16
}

// encodeLSFCoefficients computes and encodes LSF coefficients.
func (e *Encoder) encodeLSFCoefficients(samples []float32, signalType frameSignalType) []int16 {
	// Compute LPC coefficients using autocorrelation
	lpcCoeffs := e.computeLPC(samples)

	// Convert LPC to LSF
	nlsfQ15 := e.lpcToLSF(lpcCoeffs)

	// Quantize LSF using two-stage VQ
	voiced := signalType == frameSignalTypeVoiced

	// Stage 1: Find best codebook index
	I1 := e.findBestLSFStage1(nlsfQ15, voiced)

	// Encode stage 1
	var icdfStage1 []uint
	switch {
	case !voiced && (e.bandwidth == BandwidthNarrowband || e.bandwidth == BandwidthMediumband):
		icdfStage1 = icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandUnvoiced
	case voiced && (e.bandwidth == BandwidthNarrowband || e.bandwidth == BandwidthMediumband):
		icdfStage1 = icdfNormalizedLSFStageOneIndexNarrowbandOrMediumbandVoiced
	case !voiced && e.bandwidth == BandwidthWideband:
		icdfStage1 = icdfNormalizedLSFStageOneIndexWidebandUnvoiced
	case voiced && e.bandwidth == BandwidthWideband:
		icdfStage1 = icdfNormalizedLSFStageOneIndexWidebandVoiced
	}
	e.rangeEncoder.EncodeSymbolWithICDF(I1, icdfStage1)

	// Stage 2: Encode residuals
	e.encodeLSFStage2(nlsfQ15, I1)

	return nlsfQ15
}

// computeLPC computes LPC coefficients using Levinson-Durbin.
func (e *Encoder) computeLPC(samples []float32) []float32 {
	order := e.lpcOrder

	// Compute autocorrelation
	r := make([]float64, order+1)
	for i := 0; i <= order; i++ {
		for j := 0; j < len(samples)-i; j++ {
			r[i] += float64(samples[j]) * float64(samples[j+i])
		}
	}

	// Add small value to avoid division by zero
	r[0] += 1e-10

	// Levinson-Durbin recursion
	a := make([]float64, order+1)
	aTemp := make([]float64, order+1)
	a[0] = 1.0

	err := r[0]
	for i := 1; i <= order; i++ {
		lambda := float64(0)
		for j := 0; j < i; j++ {
			lambda += a[j] * r[i-j]
		}
		lambda = -lambda / err

		// Update coefficients
		for j := 0; j <= i; j++ {
			aTemp[j] = a[j] + lambda*a[i-j]
		}
		copy(a, aTemp)

		err *= (1.0 - lambda*lambda)
		if err <= 0 {
			break
		}
	}

	// Convert to float32 and skip a[0]
	lpc := make([]float32, order)
	for i := 0; i < order; i++ {
		lpc[i] = float32(a[i+1])
	}

	return lpc
}

// lpcToLSF converts LPC coefficients to LSF (Line Spectral Frequencies).
func (e *Encoder) lpcToLSF(lpc []float32) []int16 {
	order := len(lpc)
	nlsfQ15 := make([]int16, order)

	// Simplified LSF computation
	// For a minimal encoder, we use evenly spaced LSFs as fallback
	for i := 0; i < order; i++ {
		// Evenly space in range [0, 32768)
		nlsfQ15[i] = int16((i + 1) * 32768 / (order + 1))
	}

	// Try to refine based on LPC if possible
	// This is a simplified approach - full LSF computation requires polynomial root finding
	prevLSF := int16(0)
	for i := 0; i < order; i++ {
		// Adjust based on LPC coefficient magnitude
		adjustment := int16(float32(1000) * lpc[i])
		newLSF := nlsfQ15[i] + adjustment

		// Ensure monotonicity and bounds
		if newLSF <= prevLSF+100 {
			newLSF = prevLSF + 100
		}
		if newLSF >= 32700 {
			newLSF = 32700
		}
		nlsfQ15[i] = newLSF
		prevLSF = newLSF
	}

	return nlsfQ15
}

// findBestLSFStage1 finds the best stage-1 codebook index.
func (e *Encoder) findBestLSFStage1(nlsfQ15 []int16, voiced bool) uint32 {
	var codebook [][]uint
	if e.bandwidth == BandwidthWideband {
		codebook = codebookNormalizedLSFStageOneWideband
	} else {
		codebook = codebookNormalizedLSFStageOneNarrowbandOrMediumband
	}

	bestIndex := uint32(0)
	bestDist := float64(math.MaxFloat64)

	for i := uint32(0); i < uint32(len(codebook)); i++ {
		dist := float64(0)
		for k := 0; k < len(nlsfQ15) && k < len(codebook[i]); k++ {
			// Distance in Q15 space (codebook is Q8, so shift)
			cbVal := int16(codebook[i][k] << 7)
			diff := float64(nlsfQ15[k] - cbVal)
			dist += diff * diff
		}
		if dist < bestDist {
			bestDist = dist
			bestIndex = i
		}
	}

	return bestIndex
}

// encodeLSFStage2 encodes stage-2 LSF residuals.
func (e *Encoder) encodeLSFStage2(nlsfQ15 []int16, I1 uint32) {
	var codebook [][]uint
	if e.bandwidth == BandwidthWideband {
		codebook = codebookNormalizedLSFStageTwoIndexWideband
	} else {
		codebook = codebookNormalizedLSFStageTwoIndexNarrowbandOrMediumband
	}

	// Encode residual for each coefficient
	for k := 0; k < len(nlsfQ15) && k < len(codebook[0]); k++ {
		// Compute residual and quantize to [-4, 4]
		// For minimal encoder, just encode 0 (center value)
		quantized := int8(0)

		// Map to 0-8 range (add 4)
		symbol := uint32(quantized + 4)
		if symbol > 8 {
			symbol = 8
		}

		pdxIdx := codebook[I1][k]
		e.rangeEncoder.EncodeSymbolWithICDF(symbol, icdfNormalizedLSFStageTwoIndex[pdxIdx])
	}
}

// encodeExcitation encodes the excitation signal.
func (e *Encoder) encodeExcitation(samples []float32, gainQ16 []float32, nlsfQ15 []int16, signalType frameSignalType, quantOffsetType frameQuantizationOffsetType) {
	// Determine number of shell blocks based on bandwidth and frame size
	shellblocks := 20 // 20ms WB
	switch e.bandwidth {
	case BandwidthNarrowband:
		shellblocks = 10
	case BandwidthMediumband:
		shellblocks = 15
	}

	// Encode rate level (0 = lowest rate)
	rateLevel := uint32(0)
	if signalType == frameSignalTypeVoiced {
		e.rangeEncoder.EncodeSymbolWithICDF(rateLevel, icdfRateLevelVoiced)
	} else {
		e.rangeEncoder.EncodeSymbolWithICDF(rateLevel, icdfRateLevelUnvoiced)
	}

	// Encode pulse counts for each shell block
	// For minimal encoder, use 0 or 1 pulses per block
	pulsecounts := make([]uint8, shellblocks)
	for i := 0; i < shellblocks; i++ {
		// Simple energy-based pulse count
		pulsecounts[i] = 0 // Minimal: no pulses = silence/comfort noise

		e.rangeEncoder.EncodeSymbolWithICDF(uint32(pulsecounts[i]), icdfPulseCount[rateLevel])
	}

	// Since we're encoding 0 pulses, no pulse locations or signs need to be encoded
	// The decoder will generate comfort noise from the LCG seed
}

// EncodeSilence creates a minimal silent SILK frame.
func (e *Encoder) EncodeSilence() ([]byte, error) {
	e.rangeEncoder.Reset()

	// Inactive frame with no VAD
	e.encodeHeaderBits(false, false)

	// Frame type: inactive + low quantization offset
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfFrameTypeVADInactive)

	// Minimal gain (subframe 0 independent, rest delta)
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfIndependentQuantizationGainMSBInactive)
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfIndependentQuantizationGainLSB)
	for sf := 1; sf < subframeCount; sf++ {
		e.rangeEncoder.EncodeSymbolWithICDF(4, icdfDeltaQuantizationGain) // Delta of 0
	}

	// Use first codebook entry for LSF stage 1
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfNormalizedLSFStageOneIndexWidebandUnvoiced)

	// Encode LSF stage 2 residuals as zeros
	dLPC := 16
	if e.bandwidth != BandwidthWideband {
		dLPC = 10
	}
	for k := 0; k < dLPC; k++ {
		e.rangeEncoder.EncodeSymbolWithICDF(4, icdfNormalizedLSFStageTwoIndex[0]) // 4 = center (0 residual)
	}

	// LSF interpolation weight
	e.rangeEncoder.EncodeSymbolWithICDF(4, icdfNormalizedLSFInterpolationIndex)

	// LCG seed
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfLinearCongruentialGeneratorSeed)

	// Rate level
	e.rangeEncoder.EncodeSymbolWithICDF(0, icdfRateLevelUnvoiced)

	// Pulse counts: all zeros
	shellblocks := 20
	if e.bandwidth == BandwidthNarrowband {
		shellblocks = 10
	} else if e.bandwidth == BandwidthMediumband {
		shellblocks = 15
	}
	for i := 0; i < shellblocks; i++ {
		e.rangeEncoder.EncodeSymbolWithICDF(0, icdfPulseCount[0])
	}

	e.haveEncoded = true
	return e.rangeEncoder.Finalize(), nil
}
