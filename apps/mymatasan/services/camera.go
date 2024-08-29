package services

import (
	"bytes"
	"context"
	"io"

	_ "github.com/lib/pq"
	camera "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
)

// cameraService struct
type cameraService struct {
	camffmpeg camera.INetCam
}

// Create new ICameraService
func NewCameraService(camffmpeg camera.INetCam) ICameraService {
	return &cameraService{
		camffmpeg: camffmpeg,
	}
}

func (m *cameraService) GetMjpegStream(ctx context.Context, vidStream chan []byte) error {

	_, readStream, err := m.camffmpeg.ReadStream()
	if err != nil {
		return err
	}

	defer readStream.Close()

	buf := make([]byte, 1024)
	res := make([]byte, 1024*64)

	for {
		n, err := readStream.Read(buf)
		if err == io.EOF {
			// there is no more data to read
			break
		}
		if err != nil {
			continue
		}
		if n > 0 {
			sbuff := buf[:n]
			res = append(res, sbuff...)
			endian := sbuff[len(sbuff)-1]

			if len(sbuff) < 1024 && endian == 0xD9 {
				res = bytes.Trim(res, "\x00")
				vidStream <- res
				res = res[:0]
			}
		}
	}

	close(vidStream)

	return nil
}
