// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package opus

import (
	"github.com/pion/opus/internal/bitdepth"
	"github.com/pion/opus/internal/celt"
	"github.com/pion/opus/internal/resample"
	"github.com/pion/opus/internal/silk"
)

// Mode specifies the encoding mode.
type Mode int

const (
	// ModeSILKOnly uses SILK encoding (speech optimized, up to 16kHz).
	ModeSILKOnly Mode = iota
	// ModeCELTOnly uses CELT encoding (music/stereo optimized, up to 48kHz).
	ModeCELTOnly
	// ModeAuto automatically selects based on bandwidth and channels.
	ModeAuto
)

// Encoder encodes PCM audio into Opus packets.
// Supports both SILK mode (speech, mono, up to 16kHz) and
// CELT mode (music/stereo, up to 48kHz).
type Encoder struct {
	silkEncoder *silk.Encoder
	celtEncoder *celt.Encoder

	// Buffers
	silkBuffer  []float32
	celtBuffer  []float32
	resampleBuf []float32

	// Configuration
	mode          Mode
	bandwidth     Bandwidth
	sampleRate    int
	frameDuration frameDuration
	channels      int
}

// NewEncoder creates a new Opus encoder.
// By default, it encodes mono wideband audio at 20ms frame duration using SILK.
func NewEncoder() *Encoder {
	enc := &Encoder{
		silkEncoder:   silk.NewEncoder(),
		celtEncoder:   nil,                   // Created on demand
		silkBuffer:    make([]float32, 320),  // 20ms at 16kHz
		celtBuffer:    make([]float32, 1920), // 20ms at 48kHz stereo
		resampleBuf:   make([]float32, 1920),
		mode:          ModeAuto,
		bandwidth:     BandwidthWideband,
		sampleRate:    16000,
		frameDuration: frameDuration20ms,
		channels:      1,
	}
	enc.silkEncoder.SetBandwidth(silk.BandwidthWideband)
	return enc
}

// SetMode sets the encoding mode (SILK, CELT, or Auto).
func (e *Encoder) SetMode(mode Mode) {
	e.mode = mode
}

// SetChannels sets the number of audio channels (1 for mono, 2 for stereo).
func (e *Encoder) SetChannels(channels int) error {
	if channels != 1 && channels != 2 {
		return errUnsupportedConfigurationMode
	}
	e.channels = channels

	// CELT is required for stereo
	if channels == 2 && e.celtEncoder == nil {
		e.celtEncoder = celt.NewEncoder(channels)
	}
	return nil
}

// SetSampleRate sets the input sample rate.
// Supported: 8000, 12000, 16000, 24000, 48000 Hz.
func (e *Encoder) SetSampleRate(rate int) error {
	switch rate {
	case 8000:
		e.sampleRate = rate
		e.bandwidth = BandwidthNarrowband
	case 12000:
		e.sampleRate = rate
		e.bandwidth = BandwidthMediumband
	case 16000:
		e.sampleRate = rate
		e.bandwidth = BandwidthWideband
	case 24000:
		e.sampleRate = rate
		e.bandwidth = BandwidthSuperwideband
	case 48000:
		e.sampleRate = rate
		e.bandwidth = BandwidthFullband
	default:
		return errUnsupportedConfigurationMode
	}

	// Update internal encoders
	if e.sampleRate <= 16000 {
		switch e.bandwidth {
		case BandwidthNarrowband:
			e.silkEncoder.SetBandwidth(silk.BandwidthNarrowband)
		case BandwidthMediumband:
			e.silkEncoder.SetBandwidth(silk.BandwidthMediumband)
		case BandwidthWideband:
			e.silkEncoder.SetBandwidth(silk.BandwidthWideband)
		}
	}

	return nil
}

// Channels returns the number of channels.
func (e *Encoder) Channels() int {
	return e.channels
}

// SampleRate returns the configured sample rate.
func (e *Encoder) SampleRate() int {
	return e.sampleRate
}

// SetBandwidth sets the encoding bandwidth.
// This also affects the sample rate expectation.
func (e *Encoder) SetBandwidth(b Bandwidth) error {
	e.bandwidth = b
	switch b {
	case BandwidthNarrowband:
		e.sampleRate = 8000
		e.silkEncoder.SetBandwidth(silk.BandwidthNarrowband)
	case BandwidthMediumband:
		e.sampleRate = 12000
		e.silkEncoder.SetBandwidth(silk.BandwidthMediumband)
	case BandwidthWideband:
		e.sampleRate = 16000
		e.silkEncoder.SetBandwidth(silk.BandwidthWideband)
	case BandwidthSuperwideband:
		e.sampleRate = 24000
	case BandwidthFullband:
		e.sampleRate = 48000
	default:
		return errUnsupportedConfigurationMode
	}
	return nil
}

// Bandwidth returns the current encoding bandwidth.
func (e *Encoder) Bandwidth() Bandwidth {
	return e.bandwidth
}

// SamplesPerFrame returns the number of samples expected per frame
// for the current configuration (per channel).
func (e *Encoder) SamplesPerFrame() int {
	switch e.frameDuration {
	case frameDuration10ms:
		return e.sampleRate / 100
	case frameDuration20ms:
		return e.sampleRate / 50
	default:
		return e.sampleRate / 50 // default to 20ms
	}
}

// effectiveMode determines the actual encoding mode to use.
func (e *Encoder) effectiveMode() Mode {
	if e.mode != ModeAuto {
		return e.mode
	}

	// Auto mode selection:
	// - Use CELT for stereo or high sample rates
	// - Use SILK for mono speech at lower rates
	if e.channels == 2 || e.sampleRate > 16000 {
		return ModeCELTOnly
	}
	return ModeSILKOnly
}

// Encode encodes a frame of S16LE PCM audio into an Opus packet.
// For stereo, samples should be interleaved.
// The input should contain SamplesPerFrame() * channels * 2 bytes.
func (e *Encoder) Encode(pcm []byte) ([]byte, error) {
	samplesNeeded := e.SamplesPerFrame() * e.channels

	if len(pcm) < samplesNeeded*2 {
		return nil, errTooShortForTableOfContentsHeader
	}

	// Ensure buffer is large enough
	if len(e.celtBuffer) < samplesNeeded {
		e.celtBuffer = make([]float32, samplesNeeded)
	}

	// Convert S16LE to float32
	if err := bitdepth.ConvertSigned16LittleEndianToFloat32LittleEndian(pcm[:samplesNeeded*2], e.celtBuffer[:samplesNeeded]); err != nil {
		return nil, err
	}

	return e.encodeFloat32Internal(e.celtBuffer[:samplesNeeded])
}

// EncodeFloat32 encodes a frame of float32 PCM audio into an Opus packet.
// For stereo, samples should be interleaved [L, R, L, R, ...].
// The input should contain SamplesPerFrame() * channels float32 samples.
func (e *Encoder) EncodeFloat32(pcm []float32) ([]byte, error) {
	samplesNeeded := e.SamplesPerFrame() * e.channels

	if len(pcm) < samplesNeeded {
		return nil, errTooShortForTableOfContentsHeader
	}

	return e.encodeFloat32Internal(pcm[:samplesNeeded])
}

// encodeFloat32Internal handles the actual encoding logic.
func (e *Encoder) encodeFloat32Internal(samples []float32) ([]byte, error) {
	mode := e.effectiveMode()

	switch mode {
	case ModeCELTOnly:
		return e.encodeCELT(samples)
	default:
		return e.encodeSILK(samples)
	}
}

// encodeSILK encodes using SILK mode.
func (e *Encoder) encodeSILK(samples []float32) ([]byte, error) {
	// Downmix stereo to mono if needed
	monoSamples := samples
	if e.channels == 2 {
		monoSamples = resample.DownmixStereoToMono(samples)
	}

	// Resample to SILK-compatible rate if needed
	silkRate := e.sampleRate
	if silkRate > 16000 {
		silkRate = 16000
	}

	encodeSamples := monoSamples
	if e.sampleRate != silkRate {
		encodeSamples = resample.Resample(monoSamples, e.sampleRate, silkRate)
	}

	// Ensure buffer size
	silkFrameSize := silkRate / 50 // 20ms
	if len(encodeSamples) < silkFrameSize {
		// Pad if needed
		padded := make([]float32, silkFrameSize)
		copy(padded, encodeSamples)
		encodeSamples = padded
	}

	// Encode with SILK
	silkFrame, err := e.silkEncoder.Encode(encodeSamples[:silkFrameSize])
	if err != nil {
		return nil, err
	}

	// Determine bandwidth for TOC
	silkBandwidth := e.bandwidth
	if silkBandwidth > BandwidthWideband {
		silkBandwidth = BandwidthWideband
	}

	return e.buildSILKPacket(silkFrame, silkBandwidth, e.channels == 2), nil
}

// encodeCELT encodes using CELT mode.
func (e *Encoder) encodeCELT(samples []float32) ([]byte, error) {
	// Initialize CELT encoder if needed
	if e.celtEncoder == nil {
		e.celtEncoder = celt.NewEncoder(e.channels)
	}

	// CELT always operates at 48kHz internally
	celtSamples := samples
	if e.sampleRate != 48000 {
		// Resample to 48kHz
		if e.channels == 2 {
			// Deinterleave, resample each channel, re-interleave
			left, right := resample.DeinterleaveStereo(samples)
			leftResampled := resample.Resample(left, e.sampleRate, 48000)
			rightResampled := resample.Resample(right, e.sampleRate, 48000)
			celtSamples = resample.InterleaveStereo(leftResampled, rightResampled)
		} else {
			celtSamples = resample.Resample(samples, e.sampleRate, 48000)
		}
	}

	// Encode with CELT
	celtFrame, err := e.celtEncoder.Encode(celtSamples)
	if err != nil {
		return nil, err
	}

	return e.buildCELTPacket(celtFrame, e.bandwidth, e.channels == 2), nil
}

// EncodeSilence creates a minimal Opus packet representing silence.
func (e *Encoder) EncodeSilence() ([]byte, error) {
	mode := e.effectiveMode()

	if mode == ModeCELTOnly {
		if e.celtEncoder == nil {
			e.celtEncoder = celt.NewEncoder(e.channels)
		}
		celtFrame, err := e.celtEncoder.EncodeSilence()
		if err != nil {
			return nil, err
		}
		return e.buildCELTPacket(celtFrame, e.bandwidth, e.channels == 2), nil
	}

	silkFrame, err := e.silkEncoder.EncodeSilence()
	if err != nil {
		return nil, err
	}

	silkBandwidth := e.bandwidth
	if silkBandwidth > BandwidthWideband {
		silkBandwidth = BandwidthWideband
	}
	return e.buildSILKPacket(silkFrame, silkBandwidth, e.channels == 2), nil
}

// buildSILKPacket builds an Opus packet with SILK mode.
func (e *Encoder) buildSILKPacket(silkFrame []byte, bw Bandwidth, isStereo bool) []byte {
	var config Configuration

	// SILK-only mode configurations:
	// 0-3: NB, 10/20/40/60ms
	// 4-7: MB, 10/20/40/60ms
	// 8-11: WB, 10/20/40/60ms
	switch bw {
	case BandwidthNarrowband:
		switch e.frameDuration {
		case frameDuration10ms:
			config = 0
		default:
			config = 1
		}
	case BandwidthMediumband:
		switch e.frameDuration {
		case frameDuration10ms:
			config = 4
		default:
			config = 5
		}
	default: // Wideband
		switch e.frameDuration {
		case frameDuration10ms:
			config = 8
		default:
			config = 9
		}
	}

	// TOC byte: config (5 bits) | stereo (1 bit) | frame code (2 bits)
	toc := byte(config<<3) | byte(frameCodeOneFrame)
	if isStereo {
		toc |= 0x04
	}

	packet := make([]byte, 1+len(silkFrame))
	packet[0] = toc
	copy(packet[1:], silkFrame)

	return packet
}

// buildCELTPacket builds an Opus packet with CELT mode.
func (e *Encoder) buildCELTPacket(celtFrame []byte, bw Bandwidth, isStereo bool) []byte {
	var config Configuration

	// CELT-only mode configurations:
	// 16-19: NB, 2.5/5/10/20ms
	// 20-23: WB, 2.5/5/10/20ms
	// 24-27: SWB, 2.5/5/10/20ms
	// 28-31: FB, 2.5/5/10/20ms
	switch bw {
	case BandwidthNarrowband:
		switch e.frameDuration {
		case frameDuration10ms:
			config = 18
		default:
			config = 19
		}
	case BandwidthWideband:
		switch e.frameDuration {
		case frameDuration10ms:
			config = 22
		default:
			config = 23
		}
	case BandwidthSuperwideband:
		switch e.frameDuration {
		case frameDuration10ms:
			config = 26
		default:
			config = 27
		}
	default: // Fullband
		switch e.frameDuration {
		case frameDuration10ms:
			config = 30
		default:
			config = 31
		}
	}

	// TOC byte: config (5 bits) | stereo (1 bit) | frame code (2 bits)
	toc := byte(config<<3) | byte(frameCodeOneFrame)
	if isStereo {
		toc |= 0x04
	}

	packet := make([]byte, 1+len(celtFrame))
	packet[0] = toc
	copy(packet[1:], celtFrame)

	return packet
}
