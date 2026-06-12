package controllers

import (
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"strings"
)

type PagingResponse[T any] struct {
	Message    string `json:"message"`
	DurationMs int64  `json:"durationMs"`
	Data       struct {
		Result     T      `json:"result"`
		Limit      uint64 `json:"limit"`
		Offset     uint64 `json:"offset"`
		ResCnt     uint64 `json:"resCnt"`
		TotalCnt   uint64 `json:"totalCnt"`
		HasNext    bool   `json:"hasNext"`
		NextOffset uint64 `json:"nextOffset"`
	} `json:"data"`
}

type ErrResponse[T any] struct {
	StatsCode  int    `json:"statsCode"`
	Message    string `json:"message"`
	DurationMs int64  `json:"durationMs"`
	Details    T      `json:"details"`
}

type DefaultResponse[T any] struct {
	Message    string `json:"message"`
	DurationMs int64  `json:"durationMs"`
	Result     T      `json:"result"`
}

type requestTimer interface {
	RequestDurationMs() int64
}

func SendPagingResult(w http.ResponseWriter, data interface{}, limit uint64, offset uint64, totalCnt uint64, msgs ...string) error {
	var resp PagingResponse[interface{}]
	resp.Message = strings.Join(msgs, "\n")
	resp.DurationMs = responseDurationMs(w)
	resp.Data.Result = data
	resp.Data.Limit = limit
	resp.Data.Offset = offset
	resp.Data.ResCnt = pageResultCount(data)
	resp.Data.TotalCnt = totalCnt
	if resp.Data.ResCnt > 0 {
		resp.Data.NextOffset = offset + resp.Data.ResCnt
	} else if limit > 0 {
		resp.Data.NextOffset = offset + limit
	} else {
		resp.Data.NextOffset = offset
	}
	resp.Data.HasNext = resp.Data.NextOffset > offset && resp.Data.NextOffset < totalCnt

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}

func pageResultCount(data interface{}) uint64 {
	if data == nil {
		return 0
	}

	switch v := data.(type) {
	case []interface{}:
		return uint64(len(v))
	case []string:
		return uint64(len(v))
	}

	val := reflect.ValueOf(data)
	for val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return 0
		}
		val = val.Elem()
	}
	if val.Kind() == reflect.Slice || val.Kind() == reflect.Array {
		return uint64(val.Len())
	}
	return 1
}

func SendResult(w http.ResponseWriter, data interface{}, msgs ...string) error {
	var resp DefaultResponse[interface{}]
	resp.Message = strings.Join(msgs, "\n")
	resp.DurationMs = responseDurationMs(w)
	resp.Result = data

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}

func SendError(w http.ResponseWriter, err error, message string, data ...interface{}) error {
	msg := err.Error()
	if len(message) > 0 && os.Getenv("ENVIRONMENT") == "dev" {
		msg = message
	}
	stats := NewErrorUtils().GetHttpStatusCode(err)
	var resp ErrResponse[interface{}]
	resp.StatsCode = stats
	resp.Message = msg
	resp.DurationMs = responseDurationMs(w)
	resp.Details = data

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stats)
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}

func responseDurationMs(w http.ResponseWriter) int64 {
	if timer, ok := w.(requestTimer); ok {
		return timer.RequestDurationMs()
	}
	return 0
}
