package rtsp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
)

// Track describes one media track announced by a RTSP server.
type Track struct {
	MediaType   string `json:"mediaType"`
	Control     string `json:"control"`
	Codec       string `json:"codec"`
	ClockRate   int    `json:"clockRate"`
	PayloadType uint8  `json:"payloadType"`
}

// ProbeResult describes a successful RTSP DESCRIBE/SETUP probe.
type ProbeResult struct {
	URI       string  `json:"uri"`
	Transport string  `json:"transport"`
	Tracks    []Track `json:"tracks"`
	CheckedAt int64   `json:"checkedAt"`
}

// OpenOptions controls RTSP probe/open behavior.
type OpenOptions struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Client is the cross-platform RTSP boundary used by app services.
type Client interface {
	Probe(ctx context.Context, uri string, opts OpenOptions) (*ProbeResult, error)
}

type client struct{}

// NewClient creates a RTSP client backed by gortsplib.
func NewClient() Client {
	return &client{}
}

func (c *client) Probe(ctx context.Context, uri string, opts OpenOptions) (*ProbeResult, error) {
	parsed, err := base.ParseURL(strings.TrimSpace(uri))
	if err != nil {
		return nil, fmt.Errorf("parse RTSP URL failed: %w", err)
	}
	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return nil, fmt.Errorf("unsupported RTSP scheme %q", parsed.Scheme)
	}

	readTimeout := opts.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 10 * time.Second
	}
	writeTimeout := opts.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = 10 * time.Second
	}

	gclient := &gortsplib.Client{
		Scheme:       parsed.Scheme,
		Host:         parsed.Host,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}
	if deadline, ok := ctx.Deadline(); ok {
		timeout := time.Until(deadline)
		if timeout > 0 && timeout < readTimeout {
			gclient.ReadTimeout = timeout
		}
		if timeout > 0 && timeout < writeTimeout {
			gclient.WriteTimeout = timeout
		}
	}

	if err := gclient.Start(); err != nil {
		return nil, fmt.Errorf("start RTSP client failed: %w", err)
	}
	defer gclient.Close()

	desc, _, err := gclient.Describe(parsed)
	if err != nil {
		return nil, fmt.Errorf("describe RTSP stream failed: %w", err)
	}

	if len(desc.Medias) > 0 {
		if err := gclient.SetupAll(desc.BaseURL, desc.Medias); err != nil {
			return nil, fmt.Errorf("setup RTSP medias failed: %w", err)
		}
	}

	transport := ""
	if gclient.Transport() != nil {
		transport = fmt.Sprint(gclient.Transport().Conn)
	}

	result := &ProbeResult{
		URI:       parsed.String(),
		Transport: transport,
		CheckedAt: time.Now().UTC().Unix(),
	}
	for _, media := range desc.Medias {
		for _, format := range media.Formats {
			result.Tracks = append(result.Tracks, Track{
				MediaType:   string(media.Type),
				Control:     media.Control,
				Codec:       format.Codec(),
				ClockRate:   format.ClockRate(),
				PayloadType: format.PayloadType(),
			})
		}
	}

	return result, nil
}
