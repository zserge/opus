// SPDX-FileCopyrightText: 2026 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

// Package main demonstrates encoding raw PCM audio into Opus frames
package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/pion/opus"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: encode <input.pcm> <output.opus>")
		fmt.Println("")
		fmt.Println("Input should be raw S16LE PCM at 16kHz mono")
		fmt.Println("Output is raw Opus frames (no container)")
		os.Exit(1)
	}

	inputFile, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer inputFile.Close()

	outputFile, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}
	defer outputFile.Close()

	encoder := opus.NewEncoder()

	// Configure for wideband speech (16kHz)
	if err := encoder.SetBandwidth(opus.BandwidthWideband); err != nil {
		panic(err)
	}

	samplesPerFrame := encoder.SamplesPerFrame()
	bytesPerFrame := samplesPerFrame * 2 // 16-bit samples

	pcmBuffer := make([]byte, bytesPerFrame)
	frameCount := 0
	totalBytes := 0

	for {
		n, err := io.ReadFull(inputFile, pcmBuffer)
		if err == io.EOF {
			break
		}
		if err == io.ErrUnexpectedEOF {
			// Pad with zeros for the last partial frame
			for i := n; i < len(pcmBuffer); i++ {
				pcmBuffer[i] = 0
			}
		} else if err != nil {
			panic(err)
		}

		// Encode the frame
		opusFrame, err := encoder.Encode(pcmBuffer)
		if err != nil {
			panic(err)
		}

		// Write frame length (2 bytes, little-endian) followed by frame data
		// This simple framing allows reading back individual frames
		lenBuf := make([]byte, 2)
		binary.LittleEndian.PutUint16(lenBuf, uint16(len(opusFrame)))
		if _, err := outputFile.Write(lenBuf); err != nil {
			panic(err)
		}
		if _, err := outputFile.Write(opusFrame); err != nil {
			panic(err)
		}

		frameCount++
		totalBytes += len(opusFrame)
	}

	fmt.Printf("Encoded %d frames (%d ms)\n", frameCount, frameCount*20)
	fmt.Printf("Total encoded size: %d bytes\n", totalBytes)
	fmt.Printf("Average frame size: %.1f bytes\n", float64(totalBytes)/float64(frameCount))
}
