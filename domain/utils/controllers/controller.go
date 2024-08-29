package controllers

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"
)

type PagingResponse[T any] struct {
	Message string `json:"message"`
	Data    struct {
		Result      T      `json:"result"`
		ResCnt      uint64 `json:"resCnt"`
		CurrentPage int    `json:"currentPage"`
		TotalPage   int    `json:"totalPage"`
	} `json:"data"`
}

type ErrResponse[T any] struct {
	StatsCode int    `json:"statsCode"`
	Message   string `json:"message"`
	Details   T      `json:"details"`
}

type DefaultResponse[T any] struct {
	Message string `json:"message"`
	Result  T      `json:"result"`
}

func SendPagingResult(w http.ResponseWriter, data interface{}, limit uint64, offset uint64, totalCnt uint64, msgs ...string) error {
	var resp PagingResponse[interface{}]
	resp.Message = strings.Join(msgs, "\n")
	resp.Data.Result = data
	resp.Data.ResCnt = totalCnt
	resp.Data.CurrentPage = 1
	resp.Data.TotalPage = 1

	if limit > 0 {
		resp.Data.TotalPage = int(math.Ceil(float64(totalCnt) / float64(limit)))
		if offset > 0 {
			resp.Data.CurrentPage = int((offset / limit) + 1)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}

func SendResult(w http.ResponseWriter, data interface{}, msgs ...string) error {
	var resp DefaultResponse[interface{}]
	resp.Message = strings.Join(msgs, "\n")
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
	resp.Details = data

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stats)
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		return err
	}

	return nil
}
