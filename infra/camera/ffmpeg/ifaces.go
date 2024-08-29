package ffmpeg

import "io"

// INetCam interface
type INetCam interface {
	ReadStream() (string, io.ReadCloser, error)
}
