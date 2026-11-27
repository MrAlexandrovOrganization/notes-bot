package mobilevoice

import "bytes"

// WebAudioFormat reads container signatures. A browser may supply a blank or
// generic MIME type, or label the same recording differently across platforms.
func WebAudioFormat(audio []byte) string {
	if len(audio) < 12 {
		return ""
	}
	switch {
	case bytes.Equal(audio[4:8], []byte("ftyp")):
		return "mp4"
	case bytes.HasPrefix(audio, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "webm"
	case bytes.HasPrefix(audio, []byte("OggS")):
		return "ogg"
	case bytes.HasPrefix(audio, []byte("RIFF")) && bytes.Equal(audio[8:12], []byte("WAVE")):
		return "wav"
	case bytes.HasPrefix(audio, []byte("fLaC")):
		return "flac"
	case audio[0] == 0xff && audio[1]&0xf6 == 0xf0:
		return "aac"
	case bytes.HasPrefix(audio, []byte("ID3")) || audio[0] == 0xff && audio[1]&0xe6 == 0xe2:
		return "mp3"
	default:
		return ""
	}
}
