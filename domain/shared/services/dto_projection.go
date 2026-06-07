package services

import dtoutils "github.com/mysayasan/kopiv2/domain/utils/dtos"

func projectSliceResult[TDto any](src any, totalCnt uint64, err error) ([]*TDto, uint64, error) {
	if err != nil {
		return nil, 0, err
	}

	res, err := dtoutils.ProjectSlice[TDto](src)
	if err != nil {
		return nil, 0, err
	}
	return res, totalCnt, nil
}

func projectSlice[TDto any](src any, err error) ([]*TDto, error) {
	if err != nil {
		return nil, err
	}

	return dtoutils.ProjectSlice[TDto](src)
}

func projectOne[TDto any](src any, err error) (*TDto, error) {
	if err != nil {
		return nil, err
	}

	return dtoutils.Project[TDto](src)
}
