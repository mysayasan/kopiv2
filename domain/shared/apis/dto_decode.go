package apis

import (
	"encoding/json"
	"net/http"

	dtoutils "github.com/mysayasan/kopiv2/domain/utils/dtos"
)

func DecodeRequestDto[TDto any, TEntity any](w http.ResponseWriter, r *http.Request) (*TEntity, error) {
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	body := new(TDto)
	if err := dec.Decode(body); err != nil {
		return nil, err
	}

	return dtoutils.Project[TEntity](body)
}

func decodeRequestDto[TDto any, TEntity any](w http.ResponseWriter, r *http.Request) (*TEntity, error) {
	return DecodeRequestDto[TDto, TEntity](w, r)
}
