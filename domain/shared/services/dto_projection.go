package services

import dtoutils "github.com/mysayasan/kopiv2/domain/utils/dtos"

func ProjectSliceResult[TDto any](src any, totalCnt uint64, err error) ([]*TDto, uint64, error) {
	if err != nil {
		return nil, 0, err
	}

	res, err := dtoutils.ProjectSlice[TDto](src)
	if err != nil {
		return nil, 0, err
	}
	return res, totalCnt, nil
}

func ProjectSlice[TDto any](src any, err error) ([]*TDto, error) {
	if err != nil {
		return nil, err
	}

	return dtoutils.ProjectSlice[TDto](src)
}

func ProjectOne[TDto any](src any, err error) (*TDto, error) {
	if err != nil {
		return nil, err
	}

	return dtoutils.Project[TDto](src)
}

func projectSliceResult[TDto any](src any, totalCnt uint64, err error) ([]*TDto, uint64, error) {
	return ProjectSliceResult[TDto](src, totalCnt, err)
}

func projectSlice[TDto any](src any, err error) ([]*TDto, error) {
	return ProjectSlice[TDto](src, err)
}

func projectOne[TDto any](src any, err error) (*TDto, error) {
	return ProjectOne[TDto](src, err)
}
