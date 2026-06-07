package dtos

import (
	"encoding/json"
	"testing"
)

type sourceUser struct {
	Id         int64  `json:"id"`
	Email      string `json:"email"`
	Userpwd    string `json:"userpwd"`
	FirstName  string `json:"firstName"`
	UserRoleId int64  `json:"userRoleId"`
}

type targetUserDTO struct {
	Id         int64  `json:"id"`
	Email      string `json:"email"`
	FirstName  string `json:"firstName"`
	UserRoleId int64  `json:"userRoleId"`
}

func TestProjectCopiesMatchingFieldsAndOmitsMissingFields(t *testing.T) {
	res, err := Project[targetUserDTO](&sourceUser{
		Id:         7,
		Email:      "admin@example.test",
		Userpwd:    "hashed-secret",
		FirstName:  "Admin",
		UserRoleId: 3,
	})
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}
	if res.Id != 7 || res.Email != "admin@example.test" || res.FirstName != "Admin" || res.UserRoleId != 3 {
		t.Fatalf("unexpected dto: %+v", res)
	}

	body, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if containsJSONField(body, "userpwd") {
		t.Fatalf("projection leaked omitted source field: %s", body)
	}
}

func TestProjectSliceCopiesMatchingFields(t *testing.T) {
	res, err := ProjectSlice[targetUserDTO]([]*sourceUser{
		{Id: 1, Email: "one@example.test", Userpwd: "one"},
		{Id: 2, Email: "two@example.test", Userpwd: "two"},
	})
	if err != nil {
		t.Fatalf("ProjectSlice failed: %v", err)
	}
	if len(res) != 2 || res[0].Email != "one@example.test" || res[1].Email != "two@example.test" {
		t.Fatalf("unexpected projected slice: %+v", res)
	}
}

func TestProjectReadsMapKeys(t *testing.T) {
	res, err := Project[targetUserDTO](map[string]any{
		"id":         int64(9),
		"email":      "map@example.test",
		"userRoleId": int64(4),
		"userpwd":    "hashed-secret",
	})
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}
	if res.Id != 9 || res.Email != "map@example.test" || res.UserRoleId != 4 {
		t.Fatalf("unexpected dto from map: %+v", res)
	}
}

func containsJSONField(body []byte, field string) bool {
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		return false
	}
	return containsJSONFieldValue(decoded, field)
}

func containsJSONFieldValue(value any, field string) bool {
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v[field]; ok {
			return true
		}
		for _, child := range v {
			if containsJSONFieldValue(child, field) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if containsJSONFieldValue(child, field) {
				return true
			}
		}
	}
	return false
}
