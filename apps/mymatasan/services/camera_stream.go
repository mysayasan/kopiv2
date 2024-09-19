package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	camera "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
	memCache "github.com/patrickmn/go-cache"
	"gocv.io/x/gocv"
)

// cameraStreamService struct
type cameraStreamService struct {
	repo         dbsql.IGenericRepo[entities.CameraStream]
	memCache     *memCache.Cache
	camffmpeg    camera.INetCam
	nosignalgif  []byte
	startstreams map[int64](chan []byte)
	mu           sync.Mutex
}

// Create new ICameraStreamService
func NewCameraStreamService(
	repo dbsql.IGenericRepo[entities.CameraStream],
	memCache *memCache.Cache,
	camffmpeg camera.INetCam,
) ICameraStreamService {
	// dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	// if err != nil {
	// 	log.Fatal(err)
	// }

	startstreams := make(map[int64](chan []byte))

	fi, err := os.Open(filepath.Join("./nosignal.gif"))
	if err != nil {
		log.Fatal(err)
	}
	// close fi on exit and check for its returned error
	defer func() {
		if err := fi.Close(); err != nil {
			return
		}
	}()

	content, err := io.ReadAll(fi)
	if err != nil {
		log.Fatal(err)
	}

	return &cameraStreamService{
		repo:         repo,
		memCache:     memCache,
		camffmpeg:    camffmpeg,
		nosignalgif:  content,
		startstreams: startstreams,
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

func (m *cameraStreamService) startMjpegStream(uri string, vidStream chan<- []byte) error {
	rescnt := 0
restart:
	_, readStream, err := m.camffmpeg.ReadMjpeg(uri)
	if err != nil {
		return err
	}
	defer readStream.Close()

	fmt.Printf("stream from [%s] is online\n", uri)

	buf := make([]byte, 1024)
	res := make([]byte, 1024*64)

	// color for the rect when faces detected
	blue := color.RGBA{0, 0, 255, 0}

	// load classifier to recognize faces
	classifier := gocv.NewCascadeClassifier()
	defer classifier.Close()

	xmlFile := filepath.Join("./haarcascade_frontalface_alt.xml")
	if !classifier.Load(xmlFile) {
		fmt.Printf("Error reading cascade file: %v\n", xmlFile)
		return fmt.Errorf("error reading cascade file : %v", xmlFile)
	}

	for {
		n, err := readStream.Read(buf)
		if err == io.EOF {
			if rescnt < 30 {
				vidStream <- m.nosignalgif
				fmt.Printf("stream disruption on [%s], restarting in 10secs\n", uri)
				time.Sleep(10 * time.Second)
				rescnt += 1
				goto restart
			}
			return errors.New("failed to stream")
		}
		if err != nil {
			continue
		}
		if n > 0 {
			sbuff := buf[:n]
			if len(res) < 1 {
				startByte := sbuff[0]
				if len(sbuff) > 1024 && startByte != 0xD8 {
					continue
				}
			}
			res = append(res, sbuff...)
			endian := sbuff[len(sbuff)-1]

			facedetect := false

			if len(sbuff) < 1024 && endian == 0xD9 {
				res = bytes.Trim(res, "\x00")

				if facedetect {
					// prepare image matrix
					mat := gocv.NewMat()
					defer mat.Close()

					gocv.IMDecodeIntoMat(res, gocv.IMReadAnyColor, &mat)

					if mat.Empty() {
						continue
					}

					// detect faces
					rects := classifier.DetectMultiScale(mat)
					fmt.Printf("found %d faces\n", len(rects))

					// draw a rectangle around each face on the original image,
					// along with text identifying as "Human"
					for _, r := range rects {
						gocv.Rectangle(&mat, r, blue, 3)

						size := gocv.GetTextSize("Human", gocv.FontHersheyPlain, 1.2, 2)
						pt := image.Pt(r.Min.X+(r.Min.X/2)-(size.X/2), r.Min.Y-2)
						gocv.PutText(&mat, "Human", pt, gocv.FontHersheyPlain, 1.2, blue, 2)
					}

					buff, err := gocv.IMEncode(gocv.JPEGFileExt, mat)
					if err != nil {
						continue
					}

					// vidStream <- res
					vidStream <- buff.GetBytes()
					res = res[:0]
					continue
				}

				// vidStream <- res
				vidStream <- res
				res = res[:0]
			}
		}
	}
}

func (m *cameraStreamService) StartAllMjpegStream() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	filters := []sqldataenums.Filter{
		{
			FieldName: "AutoStart",
			Compare:   sqldataenums.Equal,
			Value:     true,
		},
	}
	streams, _, err := m.repo.Get(ctx, "", 0, 0, filters, nil)
	if err != nil {
		return err
	}

	for _, stream := range streams {
		if _, ok := m.startstreams[stream.Id]; !ok {
			m.startstreams[stream.Id] = make(chan []byte)

			go func(ctx context.Context, startStream chan<- []byte) {
				m.startMjpegStream(stream.Url, startStream)
			}(ctx, m.startstreams[stream.Id])
		}
	}

	return nil
}

func (m *cameraStreamService) ReadMjpeg(ctx context.Context, id int64) <-chan []byte {
	return m.startstreams[id]
}
