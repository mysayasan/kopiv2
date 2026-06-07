package services

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/mysayasan/kopiv2/apps/mymatasan/entities"
	sqldataenums "github.com/mysayasan/kopiv2/domain/enums/sqldata"
	"github.com/mysayasan/kopiv2/infra/cache"
	camera "github.com/mysayasan/kopiv2/infra/camera/ffmpeg"
	dbsql "github.com/mysayasan/kopiv2/infra/db/sql"
)

const streamBufferSize = 8

type streamWorker struct {
	uri    string
	stream chan []byte
	cancel context.CancelFunc
}

// cameraStreamService struct
type cameraStreamService struct {
	repo        dbsql.IGenericRepo[entities.CameraStream]
	cache       cache.Store
	camffmpeg   camera.INetCam
	logger      serviceLogger
	nosignalgif []byte
	workers     map[int64]*streamWorker
	mu          sync.Mutex
	wg          sync.WaitGroup
}

type serviceLogger interface {
	Infof(source string, format string, args ...any)
	Warnf(source string, format string, args ...any)
	Errorf(source string, format string, args ...any)
}

// Create new ICameraStreamService
func NewCameraStreamService(
	repo dbsql.IGenericRepo[entities.CameraStream],
	cacheStore cache.Store,
	camffmpeg camera.INetCam,
	logger ...serviceLogger,
) ICameraStreamService {
	workers := make(map[int64]*streamWorker)
	var serviceLog serviceLogger
	if len(logger) > 0 {
		serviceLog = logger[0]
	}

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
		repo:        repo,
		cache:       cacheStore,
		camffmpeg:   camffmpeg,
		logger:      serviceLog,
		nosignalgif: content,
		workers:     workers,
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

func (m *cameraStreamService) startMjpegStream(ctx context.Context, id int64, uri string, vidStream chan<- []byte) error {
	rescnt := 0

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, readStream, err := m.camffmpeg.ReadMjpeg(uri)
		if err != nil {
			return fmt.Errorf("open stream [%d] failed: %w", id, err)
		}

		m.infof("stream from [%s] is online", uri)

		buf := make([]byte, 1024)
		res := make([]byte, 0, 1024*64)

		for {
			select {
			case <-ctx.Done():
				readStream.Close()
				return nil
			default:
			}

			n, err := readStream.Read(buf)
			if err == io.EOF {
				readStream.Close()
				if rescnt < 30 {
					m.pushFrame(vidStream, m.nosignalgif)
					m.warnf("stream disruption on [%s], restarting in 10secs", uri)

					timer := time.NewTimer(10 * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return nil
					case <-timer.C:
					}

					rescnt += 1
					break
				}
				return errors.New("failed to stream")
			}

			if err != nil {
				continue
			}

			if n < 1 {
				continue
			}

			sbuff := buf[:n]
			if len(res) < 1 {
				startByte := sbuff[0]
				if len(sbuff) > 1024 && startByte != 0xD8 {
					continue
				}
			}

			res = append(res, sbuff...)
			endian := sbuff[len(sbuff)-1]
			if len(sbuff) < 1024 && endian == 0xD9 {
				res = bytes.Trim(res, "\x00")
				m.pushFrame(vidStream, res)
				res = res[:0]
			}
		}
	}
}

func (m *cameraStreamService) StartAllMjpegStream() error {
	filters := []sqldataenums.Filter{
		{
			FieldName: "AutoStart",
			Compare:   sqldataenums.Equal,
			Value:     true,
		},
	}
	streams, _, err := m.repo.Get(context.Background(), "", 0, 0, filters, nil)
	if err != nil {
		if isNoResultFoundErr(err) {
			return nil
		}
		return err
	}

	for _, stream := range streams {
		if _, err := m.ensureWorker(stream.Id, stream.Url); err != nil {
			m.errorf("failed to autostart stream [%d]: %s", stream.Id, err.Error())
		}
	}

	return nil
}

func (m *cameraStreamService) ReadMjpeg(ctx context.Context, id int64) <-chan []byte {
	m.mu.Lock()
	if worker, ok := m.workers[id]; ok {
		stream := worker.stream
		m.mu.Unlock()
		return stream
	}
	m.mu.Unlock()

	cam, err := m.repo.GetById(ctx, "", uint64(id))
	if err != nil || cam == nil || cam.Url == "" {
		return m.closedFrameChan()
	}

	worker, err := m.ensureWorker(id, cam.Url)
	if err != nil {
		m.errorf("failed to start stream [%d]: %s", id, err.Error())
		return m.closedFrameChan()
	}

	return worker.stream
}

func (m *cameraStreamService) pushFrame(stream chan<- []byte, frame []byte) {
	if len(frame) < 1 {
		return
	}

	copyFrame := append([]byte(nil), frame...)
	select {
	case stream <- copyFrame:
	default:
		// Drop frame when the channel is full to keep stream latency stable.
	}
}

func isNoResultFoundErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no result found")
}

func (m *cameraStreamService) ensureWorker(id int64, uri string) (*streamWorker, error) {
	m.mu.Lock()
	if worker, ok := m.workers[id]; ok {
		m.mu.Unlock()
		return worker, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	worker := &streamWorker{
		uri:    uri,
		stream: make(chan []byte, streamBufferSize),
		cancel: cancel,
	}
	m.workers[id] = worker
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()

		err := m.startMjpegStream(ctx, id, uri, worker.stream)
		if err != nil {
			m.warnf("stream [%d] stopped: %s", id, err.Error())
		}

		m.mu.Lock()
		if curr, ok := m.workers[id]; ok && curr == worker {
			delete(m.workers, id)
		}
		close(worker.stream)
		m.mu.Unlock()
	}()

	return worker, nil
}

func (m *cameraStreamService) infof(format string, args ...any) {
	if m.logger != nil {
		m.logger.Infof("camera-stream", format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}

func (m *cameraStreamService) warnf(format string, args ...any) {
	if m.logger != nil {
		m.logger.Warnf("camera-stream", format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}

func (m *cameraStreamService) errorf(format string, args ...any) {
	if m.logger != nil {
		m.logger.Errorf("camera-stream", format, args...)
		return
	}
	fmt.Printf(format+"\n", args...)
}

func (m *cameraStreamService) closedFrameChan() <-chan []byte {
	ch := make(chan []byte)
	close(ch)
	return ch
}

func (m *cameraStreamService) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	workers := make([]*streamWorker, 0, len(m.workers))
	for _, worker := range m.workers {
		workers = append(workers, worker)
	}
	m.mu.Unlock()

	for _, worker := range workers {
		worker.cancel()
	}

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}
