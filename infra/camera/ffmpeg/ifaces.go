package ffmpeg

import "io"

// INetCam interface
type INetCam interface {
	ReadMjpeg(uri string) (string, io.ReadCloser, error)
}
