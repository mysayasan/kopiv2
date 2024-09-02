package services

import (
	"bytes"
	"context"
	"fmt"
	"io"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	camera "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
)

// cameraStreamService struct
type cameraStreamService struct {
	repo         dbsql.IGenericRepo[entities.CameraStream]
	memCache     *memCache.Cache
	camffmpeg    camera.INetCam
	mjpegstreams map[string](chan []byte)
}

// Create new ICameraStreamService
func NewCameraStreamService(
	repo dbsql.IGenericRepo[entities.CameraStream],
	memCache *memCache.Cache,
	camffmpeg camera.INetCam,
) ICameraStreamService {
	mjpegstreams := make(map[string](chan []byte))
	return &cameraStreamService{
		repo:         repo,
		memCache:     memCache,
		camffmpeg:    camffmpeg,
		mjpegstreams: mjpegstreams,
	}
}

func (m *cameraStreamService) Get(ctx context.Context, limit uint64, offset uint64) ([]*entities.CameraStream, uint64, error) {
	sorters := []sqldataenums.Sorter{
		{
			FieldName: "CreatedAt",
			Sort:      2,
		},
	}

	return m.repo.Get(ctx, "", limit, offset, nil, sorters)
}

// GetByGroup implements IUserRoleService.
func (m *cameraStreamService) GetById(ctx context.Context, groupId uint64) (*entities.CameraStream, error) {
	return m.repo.GetById(ctx, "", groupId)
}

func (m *cameraStreamService) Create(ctx context.Context, model entities.CameraStream) (uint64, error) {
	return m.repo.Create(ctx, "", model)
}

func (m *cameraStreamService) Update(ctx context.Context, model entities.CameraStream) (uint64, error) {
	return m.repo.UpdateById(ctx, "", model)
}

func (m *cameraStreamService) Delete(ctx context.Context, id uint64) (uint64, error) {
	return m.repo.DeleteById(ctx, "", id)
}

func (m *cameraStreamService) ReadMjpeg(ctx context.Context, uri string, vidStream chan []byte) error {

	// create channel
	if _, ok := m.mjpegstreams[uri]; !ok {
		m.mjpegstreams[uri] = make(chan []byte)
		go func() {
			_, readStream, err := m.camffmpeg.ReadMjpeg(uri)
			if err != nil {
				return
			}

			defer readStream.Close()

			buf := make([]byte, 1024)
			res := make([]byte, 1024*64)

			for {
				n, err := readStream.Read(buf)
				if err == io.EOF {
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
						m.mjpegstreams[uri] <- res
						res = res[:0]
					}
				}
			}

			close(m.mjpegstreams[uri])
			fmt.Printf("closed camera stream from uri : %s", uri)
		}()
	}

	for {
		select {
		case <-ctx.Done():
			{
				return nil
			}
		case v, ok := <-m.mjpegstreams[uri]:
			if !ok {
				break
			}
			vidStream <- v
		}
	}
}
