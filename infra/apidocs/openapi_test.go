package apidocs

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/mux"
)

type testProvider struct{}

func (p testProvider) APIDocs() SpecConfig {
	return SpecConfig{
		Metadata: Metadata{
			Title:       "test app",
			Version:     "2.0.0",
			Description: "test description",
		},
		Endpoints: map[string]EndpointDoc{
			"GET /api/items/{id}": {
				Summary:     "Get item",
				Description: "Returns item by ID",
				Tags:        []string{"items"},
			},
		},
	}
}

func TestBuildSpecIncludesRoutesAndProviderDocs(t *testing.T) {
	router := mux.NewRouter()
	api := router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/items/{id}", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	api.HandleFunc("/user-group", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	api.HandleFunc("/user-group", func(http.ResponseWriter, *http.Request) {}).Methods("POST")
	api.HandleFunc("/user-login", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	api.HandleFunc("/user-login/email", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	api.HandleFunc("/cache-service/wipe", func(http.ResponseWriter, *http.Request) {}).Methods("POST")
	api.HandleFunc("/login/default", func(http.ResponseWriter, *http.Request) {}).Methods("POST")
	api.HandleFunc("/login/default/register", func(http.ResponseWriter, *http.Request) {}).Methods("POST")
	api.HandleFunc("/login/google", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	api.HandleFunc("/camera/stream/mjpeg/{id}", func(http.ResponseWriter, *http.Request) {}).Methods("GET")
	router.HandleFunc("/health", func(http.ResponseWriter, *http.Request) {})
	router.PathPrefix("/").HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	b, err := buildSpec(router, "sample", testProvider{})
	if err != nil {
		t.Fatalf("buildSpec failed: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("invalid json: %v", err)
	}

	if doc["openapi"] != "3.0.3" {
		t.Fatalf("unexpected openapi version: %v", doc["openapi"])
	}

	paths, ok := doc["paths"].(map[string]any)
	if !ok {
		t.Fatalf("paths missing")
	}

	itemPath, ok := paths["/api/items/{id}"].(map[string]any)
	if !ok {
		t.Fatalf("/api/items/{id} path missing")
	}

	getOp, ok := itemPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing")
	}

	if getOp["summary"] != "Get item" {
		t.Fatalf("unexpected summary: %v", getOp["summary"])
	}

	userGroupPath, ok := paths["/api/user-group"].(map[string]any)
	if !ok {
		t.Fatalf("/api/user-group path missing")
	}

	postOp, ok := userGroupPath["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing for /api/user-group")
	}

	if _, ok := postOp["requestBody"].(map[string]any); !ok {
		t.Fatalf("requestBody missing for key endpoint /api/user-group POST")
	}
	postRB := postOp["requestBody"].(map[string]any)
	postRBContent, ok := postRB["content"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody content missing for /api/user-group POST")
	}
	postRBJSON, ok := postRBContent["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json requestBody missing for /api/user-group POST")
	}
	postRBSchema, ok := postRBJSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody schema missing for /api/user-group POST")
	}
	if postRBSchema["$ref"] != "#/components/schemas/UserGroupInputDto" {
		t.Fatalf("unexpected request schema for /api/user-group POST: %v", postRBSchema["$ref"])
	}

	responses, ok := postOp["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/user-group POST")
	}

	res200, ok := responses["200"].(map[string]any)
	if !ok {
		t.Fatalf("200 response missing for /api/user-group POST")
	}

	res200Content, ok := res200["content"].(map[string]any)
	if !ok {
		t.Fatalf("200 response content missing for /api/user-group POST")
	}

	res200JSON, ok := res200Content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json missing for /api/user-group POST")
	}

	res200Schema, ok := res200JSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing for /api/user-group POST")
	}

	if res200Schema["$ref"] != "#/components/schemas/DefaultUserGroupResponse" {
		t.Fatalf("unexpected 200 schema for /api/user-group POST: %v", res200Schema["$ref"])
	}

	getOpUG, ok := userGroupPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing for /api/user-group")
	}

	getResponses, ok := getOpUG["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/user-group GET")
	}

	getRes200, ok := getResponses["200"].(map[string]any)
	if !ok {
		t.Fatalf("200 response missing for /api/user-group GET")
	}

	getRes200Content, ok := getRes200["content"].(map[string]any)
	if !ok {
		t.Fatalf("content missing for /api/user-group GET")
	}

	getRes200JSON, ok := getRes200Content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json missing for /api/user-group GET")
	}

	getRes200Schema, ok := getRes200JSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing for /api/user-group GET")
	}

	if getRes200Schema["$ref"] != "#/components/schemas/PagingUserGroupResponse" {
		t.Fatalf("unexpected 200 schema for /api/user-group GET: %v", getRes200Schema["$ref"])
	}

	requireQueryParameters(t, getOpUG, "limit", "offset", "filters", "sorters")

	appUserLoginPath, ok := paths["/api/user-login"].(map[string]any)
	if !ok {
		t.Fatalf("/api/user-login path missing")
	}

	appUserLoginGet, ok := appUserLoginPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing for /api/user-login")
	}

	appUserLoginResponses, ok := appUserLoginGet["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/user-login GET")
	}

	appUserLogin200, ok := appUserLoginResponses["200"].(map[string]any)
	if !ok {
		t.Fatalf("200 response missing for /api/user-login GET")
	}

	appUserLogin200Content, ok := appUserLogin200["content"].(map[string]any)
	if !ok {
		t.Fatalf("content missing for /api/user-login GET")
	}

	appUserLogin200JSON, ok := appUserLogin200Content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json missing for /api/user-login GET")
	}

	appUserLogin200Schema, ok := appUserLogin200JSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing for /api/user-login GET")
	}

	if appUserLogin200Schema["$ref"] != "#/components/schemas/PagingAppUserLoginResponse" {
		t.Fatalf("unexpected 200 schema for /api/user-login GET: %v", appUserLogin200Schema["$ref"])
	}

	requireQueryParameters(t, appUserLoginGet, "limit", "offset", "filters", "sorters")

	components, ok := doc["components"].(map[string]any)
	if !ok {
		t.Fatalf("components missing")
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatalf("components.schemas missing")
	}

	if _, ok := schemas["UserGroupPayload"]; !ok {
		t.Fatalf("UserGroupPayload schema missing")
	}
	if _, ok := schemas["UserGroupInputDto"]; !ok {
		t.Fatalf("UserGroupInputDto schema missing")
	}
	if _, ok := schemas["UserGroupOutputDto"]; !ok {
		t.Fatalf("UserGroupOutputDto schema missing")
	}
	defaultUserGroupSchema, ok := schemas["DefaultUserGroupResponse"].(map[string]any)
	if !ok {
		t.Fatalf("DefaultUserGroupResponse schema missing")
	}
	defaultUserGroupProps, ok := defaultUserGroupSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("DefaultUserGroupResponse properties missing")
	}
	defaultUserGroupResult, ok := defaultUserGroupProps["result"].(map[string]any)
	if !ok {
		t.Fatalf("DefaultUserGroupResponse result missing")
	}
	if defaultUserGroupResult["$ref"] != "#/components/schemas/UserGroupOutputDto" {
		t.Fatalf("DefaultUserGroupResponse result should use UserGroupOutputDto, got %v", defaultUserGroupResult["$ref"])
	}

	if _, ok := schemas["PagingResponse"]; !ok {
		t.Fatalf("PagingResponse schema missing")
	}

	if _, ok := schemas["PagingUserGroupResponse"]; !ok {
		t.Fatalf("PagingUserGroupResponse schema missing")
	}
	pagingUserGroupSchema, ok := schemas["PagingUserGroupResponse"].(map[string]any)
	if !ok {
		t.Fatalf("PagingUserGroupResponse must be an object schema")
	}
	pagingUserGroupProps, ok := pagingUserGroupSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("PagingUserGroupResponse properties missing")
	}
	pagingUserGroupData, ok := pagingUserGroupProps["data"].(map[string]any)
	if !ok {
		t.Fatalf("PagingUserGroupResponse data property missing")
	}
	pagingUserGroupDataProps, ok := pagingUserGroupData["properties"].(map[string]any)
	if !ok {
		t.Fatalf("PagingUserGroupResponse data properties missing")
	}
	for _, field := range []string{"limit", "offset", "resCnt", "totalCnt", "hasNext", "nextOffset"} {
		if _, ok := pagingUserGroupDataProps[field]; !ok {
			t.Fatalf("PagingUserGroupResponse data field %q missing", field)
		}
	}
	if _, ok := pagingUserGroupDataProps["currentPage"]; ok {
		t.Fatalf("PagingUserGroupResponse must not expose currentPage")
	}
	if _, ok := pagingUserGroupDataProps["totalPage"]; ok {
		t.Fatalf("PagingUserGroupResponse must not expose totalPage")
	}

	if _, ok := schemas["DefaultUserGroupResponse"]; !ok {
		t.Fatalf("DefaultUserGroupResponse schema missing")
	}

	appUserLoginSchema, ok := schemas["AppUserLoginPayload"].(map[string]any)
	if !ok {
		t.Fatalf("AppUserLoginPayload schema missing")
	}

	appUserLoginProps, ok := appUserLoginSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("AppUserLoginPayload properties missing")
	}

	if _, ok := appUserLoginProps["userpwd"]; ok {
		t.Fatalf("AppUserLoginPayload must not expose userpwd")
	}

	if _, ok := schemas["CacheWipeRequest"]; !ok {
		t.Fatalf("CacheWipeRequest schema missing")
	}

	if _, ok := schemas["DefaultLoginRequest"]; !ok {
		t.Fatalf("DefaultLoginRequest schema missing")
	}

	if _, ok := schemas["DefaultRegisterRequest"]; !ok {
		t.Fatalf("DefaultRegisterRequest schema missing")
	}

	cacheWipePath, ok := paths["/api/cache-service/wipe"].(map[string]any)
	if !ok {
		t.Fatalf("/api/cache-service/wipe path missing")
	}

	cacheWipePost, ok := cacheWipePath["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing for /api/cache-service/wipe")
	}

	cacheWipeRB, ok := cacheWipePost["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody missing for /api/cache-service/wipe")
	}

	cacheWipeRBContent, ok := cacheWipeRB["content"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody content missing for /api/cache-service/wipe")
	}

	cacheWipeRBJSON, ok := cacheWipeRBContent["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json requestBody missing for /api/cache-service/wipe")
	}

	cacheWipeRBSchema, ok := cacheWipeRBJSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody schema missing for /api/cache-service/wipe")
	}

	if cacheWipeRBSchema["$ref"] != "#/components/schemas/CacheWipeRequest" {
		t.Fatalf("unexpected request schema for /api/cache-service/wipe: %v", cacheWipeRBSchema["$ref"])
	}

	loginDefaultPath, ok := paths["/api/login/default"].(map[string]any)
	if !ok {
		t.Fatalf("/api/login/default path missing")
	}

	loginDefaultPost, ok := loginDefaultPath["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing for /api/login/default")
	}

	loginDefaultRB, ok := loginDefaultPost["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody missing for /api/login/default")
	}

	loginDefaultRBContent, ok := loginDefaultRB["content"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody content missing for /api/login/default")
	}

	loginDefaultRBJSON, ok := loginDefaultRBContent["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json requestBody missing for /api/login/default")
	}

	loginDefaultRBSchema, ok := loginDefaultRBJSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody schema missing for /api/login/default")
	}

	if loginDefaultRBSchema["$ref"] != "#/components/schemas/DefaultLoginRequest" {
		t.Fatalf("unexpected request schema for /api/login/default: %v", loginDefaultRBSchema["$ref"])
	}

	loginDefaultResponses, ok := loginDefaultPost["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/login/default")
	}

	loginDefault200, ok := loginDefaultResponses["200"].(map[string]any)
	if !ok {
		t.Fatalf("200 response missing for /api/login/default")
	}

	loginDefault200Content, ok := loginDefault200["content"].(map[string]any)
	if !ok {
		t.Fatalf("200 response content missing for /api/login/default")
	}

	loginDefault200JSON, ok := loginDefault200Content["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json response missing for /api/login/default")
	}

	loginDefault200Schema, ok := loginDefault200JSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing for /api/login/default 200 response")
	}

	if loginDefault200Schema["$ref"] != "#/components/schemas/DefaultSessionResponse" {
		t.Fatalf("unexpected 200 schema for /api/login/default: %v", loginDefault200Schema["$ref"])
	}

	loginRegisterPath, ok := paths["/api/login/default/register"].(map[string]any)
	if !ok {
		t.Fatalf("/api/login/default/register path missing")
	}

	loginRegisterPost, ok := loginRegisterPath["post"].(map[string]any)
	if !ok {
		t.Fatalf("post operation missing for /api/login/default/register")
	}

	loginRegisterRB, ok := loginRegisterPost["requestBody"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody missing for /api/login/default/register")
	}

	loginRegisterRBContent, ok := loginRegisterRB["content"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody content missing for /api/login/default/register")
	}

	loginRegisterRBJSON, ok := loginRegisterRBContent["application/json"].(map[string]any)
	if !ok {
		t.Fatalf("application/json requestBody missing for /api/login/default/register")
	}

	loginRegisterRBSchema, ok := loginRegisterRBJSON["schema"].(map[string]any)
	if !ok {
		t.Fatalf("requestBody schema missing for /api/login/default/register")
	}

	if loginRegisterRBSchema["$ref"] != "#/components/schemas/DefaultRegisterRequest" {
		t.Fatalf("unexpected request schema for /api/login/default/register: %v", loginRegisterRBSchema["$ref"])
	}

	loginGooglePath, ok := paths["/api/login/google"].(map[string]any)
	if !ok {
		t.Fatalf("/api/login/google path missing")
	}

	loginGoogleGet, ok := loginGooglePath["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing for /api/login/google")
	}

	loginGoogleResponses, ok := loginGoogleGet["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/login/google")
	}

	if _, ok := loginGoogleResponses["302"].(map[string]any); !ok {
		t.Fatalf("302 response missing for /api/login/google")
	}

	mjpegPath, ok := paths["/api/camera/stream/mjpeg/{id}"].(map[string]any)
	if !ok {
		t.Fatalf("/api/camera/stream/mjpeg/{id} path missing")
	}

	mjpegGet, ok := mjpegPath["get"].(map[string]any)
	if !ok {
		t.Fatalf("get operation missing for /api/camera/stream/mjpeg/{id}")
	}

	mjpegResponses, ok := mjpegGet["responses"].(map[string]any)
	if !ok {
		t.Fatalf("responses missing for /api/camera/stream/mjpeg/{id}")
	}

	mjpeg206, ok := mjpegResponses["206"].(map[string]any)
	if !ok {
		t.Fatalf("206 response missing for /api/camera/stream/mjpeg/{id}")
	}

	mjpegContent, ok := mjpeg206["content"].(map[string]any)
	if !ok {
		t.Fatalf("content missing for 206 /api/camera/stream/mjpeg/{id}")
	}

	mjpegMedia, ok := mjpegContent["multipart/x-mixed-replace"].(map[string]any)
	if !ok {
		t.Fatalf("multipart/x-mixed-replace content missing for /api/camera/stream/mjpeg/{id}")
	}

	mjpegSchema, ok := mjpegMedia["schema"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing for MJPEG 206 content")
	}

	if mjpegSchema["type"] != "string" || mjpegSchema["format"] != "binary" {
		t.Fatalf("unexpected MJPEG schema: %v", mjpegSchema)
	}

	if _, ok := paths["/"]; ok {
		t.Fatalf("static catch-all path must not be included")
	}
}

func requireQueryParameters(t *testing.T, op map[string]any, names ...string) {
	t.Helper()

	params, ok := op["parameters"].([]any)
	if !ok {
		t.Fatalf("parameters missing")
	}

	found := map[string]bool{}
	for _, rawParam := range params {
		param, ok := rawParam.(map[string]any)
		if !ok {
			continue
		}
		if param["in"] != "query" {
			continue
		}
		name, ok := param["name"].(string)
		if ok {
			found[name] = true
		}
	}

	for _, name := range names {
		if !found[name] {
			t.Fatalf("query parameter %q missing from operation", name)
		}
	}
}
