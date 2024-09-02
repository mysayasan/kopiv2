package ffmpeg

import (
	"io"
	"path/filepath"
)

// netCam struct
type netCam struct {
	// uri string
}

// Create new INetCam
func NewNetCam(
// uri string,
) INetCam {
	return &netCam{
		// uri: uri,
	}
}

func (m *netCam) ReadMjpeg(uri string) (string, io.ReadCloser, error) {
	c := &Config{
		FFMPEG: filepath.Join(filepath.Dir(`C:\FFMpeg\bin\`), "ffmpeg.exe"),
		Copy:   true, // do not transcode
		// Audio:  true, // retain audio stream
		// Width:  1920,
		// Height: 1080,
		// CRF:    23,
		// Level:  "4.0",
		// Rate:   5,
		// Prof:   "baseline",
		Time: -1, // 10 seconds
	}

	encode := Get(c)
	return encode.GetVideo(uri, "SecuritySpyVideoTitle")
}
