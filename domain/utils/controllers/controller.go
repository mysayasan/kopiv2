package controllers

import (
	"math"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type PagingResponse[T any] struct {
	StatsCode int    `json:"status"`
	Message   string `json:"message"`
	Data      struct {
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
	StatsCode int    `json:"statsCode"`
	Message   string `json:"message"`
	Result    T      `json:"result"`
}

func SendPagingResult(c *fiber.Ctx, data interface{}, limit uint64, offset uint64, totalCnt uint64, message ...string) error {
	var resp PagingResponse[interface{}]
	resp.StatsCode = c.Response().StatusCode()
	resp.Message = strings.Join(message, "\n")
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

	err := c.JSON(resp)
	if err != nil {
		return err
	}

	return nil
}

func SendResult(c *fiber.Ctx, data interface{}, message ...string) error {
	var resp DefaultResponse[interface{}]
	resp.StatsCode = c.Response().StatusCode()
	resp.Message = strings.Join(message, "\n")
	resp.Result = data

	err := c.JSON(resp)
	if err != nil {
		return err
	}

	return nil
}

func SendError(c *fiber.Ctx, err error, message string, data ...interface{}) error {
	msg := err.Error()
	if len(message) > 0 && os.Getenv("ENVIRONMENT") == "dev" {
		msg = message
	}
	stats := NewErrorUtils().GetHttpStatusCode(err)
	var resp ErrResponse[interface{}]
	resp.StatsCode = stats
	resp.Message = msg
	resp.Details = data

	err = c.Status(stats).JSON(resp)
	if err != nil {
		return err
	}

	return nil
}
