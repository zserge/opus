# Opus Encoder Example

This example demonstrates encoding raw PCM audio into Opus frames.

## Usage

```bash
go run main.go <input.pcm> <output.opus>
```

### Input Format
- Raw S16LE (signed 16-bit little-endian) PCM
- 16 kHz sample rate
- Mono channel

### Output Format
- Raw Opus frames with simple length-prefixed framing
- Each frame is preceded by a 2-byte little-endian length field

### Creating Test Input

You can create a test PCM file from an audio file using FFmpeg:

```bash
ffmpeg -i input.wav -f s16le -acodec pcm_s16le -ar 16000 -ac 1 output.pcm
```

### Using in LiveKit / WebRTC

The Opus frames produced by this encoder are compatible with WebRTC
and can be sent directly via RTP. For LiveKit streaming:

```go
encoder := opus.NewEncoder()
encoder.SetBandwidth(opus.BandwidthWideband)

// In your audio capture loop:
opusFrame, err := encoder.Encode(pcmData)
if err != nil {
    // handle error
}

// Send opusFrame via your media track
track.WriteSample(media.Sample{
    Data:     opusFrame,
    Duration: 20 * time.Millisecond,
})
```
