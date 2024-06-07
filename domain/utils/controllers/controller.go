package controllers

import (
	"math"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
)

type Response[T any] struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Result      T      `json:"result"`
		ResCnt      uint64 `json:"resCnt"`
		CurrentPage int    `json:"currentPage"`
		TotalPage   int    `json:"totalPage"`
	} `json:"data"`
}

type ErrResponse[T any] struct {
	Status  int    `json:"status"`
	Message string `json:"message"`
	Details T      `json:"details"`
}

func SendPagingResult(c *fiber.Ctx, data interface{}, limit uint64, offset uint64, totalCnt uint64, message ...string) error {
	var resp Response[interface{}]
	resp.Status = 1
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

func SendError(c *fiber.Ctx, err error, data interface{}, message ...string) error {
	msg := err.Error()
	if len(message) > 0 && os.Getenv("ENVIRONMENT") == "dev" {
		msg = strings.Join(message, "\n")
	}
	var resp ErrResponse[interface{}]
	resp.Status = 1
	resp.Message = msg
	resp.Details = data

	err = c.Status(NewErrorUtils().GetHttpStatusCode(err)).JSON(resp)
	if err != nil {
		return err
	}

	return nil
}
