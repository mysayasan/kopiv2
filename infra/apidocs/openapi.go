package apidocs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gorilla/mux"
)

// Metadata controls top-level OpenAPI info fields.
type Metadata struct {
	Title       string
	Version     string
	Description string
}

// EndpointDoc adds optional human descriptions for an auto-discovered endpoint.
type EndpointDoc struct {
	Summary     string
	Description string
	Tags        []string
}

// SpecConfig configures app-level API docs.
type SpecConfig struct {
	Metadata  Metadata
	Endpoints map[string]EndpointDoc
}

// Provider can be implemented by app modules to enrich endpoint descriptions.
type Provider interface {
	APIDocs() SpecConfig
}

type openAPISpec struct {
	OpenAPI    string                 `json:"openapi"`
	Info       openAPIInfo            `json:"info"`
	Servers    []openAPIServer        `json:"servers,omitempty"`
	Paths      map[string]openAPIPath `json:"paths"`
	Components openAPIComponents      `json:"components,omitempty"`
	Tags       []openAPITag           `json:"tags,omitempty"`
}

type openAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type openAPIServer struct {
	URL string `json:"url"`
}

type openAPIPath map[string]openAPIOperation

type openAPIOperation struct {
	Summary     string                     `json:"summary,omitempty"`
	Description string                     `json:"description,omitempty"`
	Tags        []string                   `json:"tags,omitempty"`
	Responses   map[string]openAPIResponse `json:"responses"`
	Security    []map[string][]string      `json:"security,omitempty"`
	Parameters  []openAPIParameter         `json:"parameters,omitempty"`
	RequestBody *openAPIRequestBody        `json:"requestBody,omitempty"`
}

type openAPIResponse struct {
	Description string                      `json:"description"`
	Content     map[string]openAPIMediaType `json:"content,omitempty"`
}

type openAPIComponents struct {
	SecuritySchemes map[string]openAPISecurityScheme `json:"securitySchemes,omitempty"`
	Schemas         map[string]openAPISchema         `json:"schemas,omitempty"`
}

type openAPISecurityScheme struct {
	Type   string `json:"type"`
	Scheme string `json:"scheme,omitempty"`
	In     string `json:"in,omitempty"`
	Name   string `json:"name,omitempty"`
}

type openAPITag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type openAPIParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Schema      struct {
		Type string `json:"type"`
	} `json:"schema"`
}

type openAPIRequestBody struct {
	Required bool                        `json:"required,omitempty"`
	Content  map[string]openAPIMediaType `json:"content"`
}

type openAPIMediaType struct {
	Schema openAPISchema `json:"schema"`
}

type openAPISchema struct {
	Ref                  string                   `json:"$ref,omitempty"`
	Type                 string                   `json:"type,omitempty"`
	Format               string                   `json:"format,omitempty"`
	Description          string                   `json:"description,omitempty"`
	Nullable             bool                     `json:"nullable,omitempty"`
	Properties           map[string]openAPISchema `json:"properties,omitempty"`
	Items                *openAPISchema           `json:"items,omitempty"`
	Required             []string                 `json:"required,omitempty"`
	AdditionalProperties any                      `json:"additionalProperties,omitempty"`
	OneOf                []openAPISchema          `json:"oneOf,omitempty"`
}

// Register mounts shared swagger routes for all app modules.
func Register(router *mux.Router, appName string, provider Provider) {
	router.HandleFunc("/swagger/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		spec, err := buildSpec(router, appName, provider)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(spec)
	}).Methods("GET")

	router.HandleFunc("/swagger", swaggerUIHandler).Methods("GET")
	router.HandleFunc("/swagger/", swaggerUIHandler).Methods("GET")
}

func buildSpec(router *mux.Router, appName string, provider Provider) ([]byte, error) {
	meta := Metadata{
		Title:       fmt.Sprintf("%s API", appName),
		Version:     "1.0.0",
		Description: "Auto-generated endpoint list from runtime router registration.",
	}
	endpointDocs := map[string]EndpointDoc{}

	if provider != nil {
		cfg := provider.APIDocs()
		if strings.TrimSpace(cfg.Metadata.Title) != "" {
			meta.Title = cfg.Metadata.Title
		}
		if strings.TrimSpace(cfg.Metadata.Version) != "" {
			meta.Version = cfg.Metadata.Version
		}
		if strings.TrimSpace(cfg.Metadata.Description) != "" {
			meta.Description = cfg.Metadata.Description
		}
		for k, v := range cfg.Endpoints {
			endpointDocs[normalizeEndpointKey(k)] = v
		}
	}

	paths := map[string]openAPIPath{}
	tagSet := map[string]struct{}{}

	err := router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err != nil || path == "" {
			return nil
		}

		if !includePath(path) {
			return nil
		}

		path = normalizePath(path)

		methods, err := route.GetMethods()
		if err != nil || len(methods) == 0 {
			methods = []string{"GET"}
		}

		if _, ok := paths[path]; !ok {
			paths[path] = openAPIPath{}
		}

		params := extractPathParams(path)

		for _, method := range methods {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				continue
			}

			lookupKey := endpointKey(method, path)
			doc, found := endpointDocs[lookupKey]
			if !found {
				doc = defaultDoc(method, path)
			}
			if len(doc.Tags) == 0 {
				doc.Tags = []string{tagFromPath(path)}
			}

			tagSet[doc.Tags[0]] = struct{}{}

			op := openAPIOperation{
				Summary:     strings.TrimSpace(doc.Summary),
				Description: strings.TrimSpace(doc.Description),
				Tags:        doc.Tags,
				Responses:   defaultResponses(method, path),
				Parameters:  params,
			}
			enrichOperationWithSchemas(method, path, &op)
			if requiresCookieAuth(path) {
				op.Security = []map[string][]string{{"cookieAuth": {}}}
				if requiresCSRFHeader(method) {
					op.Parameters = append(op.Parameters, csrfHeaderParameter())
				}
			}
			paths[path][strings.ToLower(method)] = op
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	tags := make([]openAPITag, 0, len(tagSet))
	for tag := range tagSet {
		tags = append(tags, openAPITag{Name: tag})
	}
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})

	spec := openAPISpec{
		OpenAPI: "3.0.3",
		Info: openAPIInfo{
			Title:       meta.Title,
			Version:     meta.Version,
			Description: meta.Description,
		},
		Servers: []openAPIServer{{URL: "/"}},
		Paths:   paths,
		Components: openAPIComponents{
			SecuritySchemes: map[string]openAPISecurityScheme{
				"cookieAuth": {
					Type: "apiKey",
					In:   "cookie",
					Name: "__Host-kopiv2_access",
				},
			},
			Schemas: baseComponentSchemas(),
		},
		Tags: tags,
	}

	return json.MarshalIndent(spec, "", "  ")
}

func includePath(path string) bool {
	if path == "/" {
		return false
	}
	if path == "/api" {
		return false
	}
	if strings.HasPrefix(path, "/swagger") {
		return false
	}
	return strings.HasPrefix(path, "/api") || path == "/health" || path == "/ready" || strings.HasPrefix(path, "/setup")
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return strings.ReplaceAll(path, "//", "/")
}

func defaultDoc(method string, path string) EndpointDoc {
	tag := tagFromPath(path)
	return EndpointDoc{
		Summary:     fmt.Sprintf("%s %s", method, path),
		Description: "Auto-discovered endpoint. Add an APIDocs entry in the app module for richer descriptions.",
		Tags:        []string{tag},
	}
}

func tagFromPath(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) == 0 || segments[0] == "" {
		return "general"
	}
	if segments[0] != "api" {
		return segments[0]
	}
	if len(segments) >= 2 {
		return segments[1]
	}
	return "api"
}

func requiresCookieAuth(path string) bool {
	if !strings.HasPrefix(path, "/api") {
		return false
	}
	if strings.HasPrefix(path, "/api/login") || strings.HasPrefix(path, "/api/callback") || path == "/api/health" {
		return false
	}
	if path == "/api/file-storage/download" {
		return false
	}
	if path == "/api/version" {
		return false
	}
	return true
}

func requiresCSRFHeader(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func extractPathParams(path string) []openAPIParameter {
	segments := strings.Split(path, "/")
	params := make([]openAPIParameter, 0)
	for _, segment := range segments {
		if len(segment) < 3 {
			continue
		}
		if !strings.HasPrefix(segment, "{") || !strings.HasSuffix(segment, "}") {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		if i := strings.Index(name, ":"); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		var param openAPIParameter
		param.Name = name
		param.In = "path"
		param.Required = true
		param.Description = "Path parameter"
		param.Schema.Type = "string"
		params = append(params, param)
	}
	return params
}

func csrfHeaderParameter() openAPIParameter {
	var param openAPIParameter
	param.Name = "X-CSRF-Token"
	param.In = "header"
	param.Required = true
	param.Description = "CSRF token copied from the readable CSRF cookie for unsafe cookie-authenticated requests."
	param.Schema.Type = "string"
	return param
}

func queryParameter(name string, required bool, description string) openAPIParameter {
	return typedQueryParameter(name, required, description, "integer")
}

func stringQueryParameter(name string, required bool, description string) openAPIParameter {
	return typedQueryParameter(name, required, description, "string")
}

func typedQueryParameter(name string, required bool, description string, schemaType string) openAPIParameter {
	var param openAPIParameter
	param.Name = name
	param.In = "query"
	param.Required = required
	param.Description = description
	param.Schema.Type = schemaType
	return param
}

func endpointKey(method string, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + normalizePath(path)
}

func normalizeEndpointKey(key string) string {
	parts := strings.Fields(strings.TrimSpace(key))
	if len(parts) < 2 {
		return strings.ToUpper(strings.TrimSpace(key))
	}
	method := strings.ToUpper(parts[0])
	path := normalizePath(strings.Join(parts[1:], " "))
	return method + " " + path
}

func defaultResponses(method string, path string) map[string]openAPIResponse {
	key := endpointKey(method, path)

	switch key {
	case "GET /health":
		return map[string]openAPIResponse{"200": jsonResponse("Service liveness", "HealthResponse")}
	case "GET /ready":
		return map[string]openAPIResponse{"200": jsonResponse("Readiness check", "ReadyResponse")}
	case "GET /setup/status":
		return map[string]openAPIResponse{"200": jsonResponse("Bootstrap status", "BootstrapStatusResponse")}
	case "GET /api/health":
		return map[string]openAPIResponse{"200": jsonResponse("API health", "ApiHealthResponse")}
	case "GET /api/version":
		return map[string]openAPIResponse{"200": jsonResponse("Runtime version", "DefaultVersionInfoResponse")}
	case "POST /api/login/default", "POST /api/login/default/register":
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Default login result", "DefaultSessionResponse")})
	case "POST /api/login/default/logout":
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Default logout result", "DefaultSessionResponse")})
	case "GET /api/login/google", "GET /api/login/github":
		return map[string]openAPIResponse{
			"302": {Description: "Redirect to OAuth provider"},
			"500": jsonResponse("Server error", "ErrorResponse"),
		}
	case "GET /api/callback/google":
		resp := withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("OAuth callback session result", "DefaultSessionResponse")})
		resp["422"] = jsonResponse("OAuth callback validation failed", "ErrorResponse")
		return resp
	case "GET /api/callback/github":
		resp := withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("OAuth callback session result", "DefaultSessionResponse")})
		resp["422"] = jsonResponse("OAuth callback validation failed", "ErrorResponse")
		return resp
	case "GET /api/file-storage/download":
		return withRateLimitResponse(map[string]openAPIResponse{"200": binaryResponse("File content")})
	case "GET /api/camera/stream/mjpeg/{id}":
		resp := map[string]openAPIResponse{"206": mjpegResponse("Multipart MJPEG stream")}
		resp["401"] = jsonResponse("Unauthorized", "ErrorResponse")
		resp["429"] = jsonResponse("Too many requests", "ErrorResponse")
		resp["500"] = jsonResponse("Server error", "ErrorResponse")
		return resp
	}

	if successSchema := endpointSuccessSchema(method, path); successSchema != "" {
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Success response", successSchema)})
	}

	if isPagingEndpoint(method, path) {
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Paged response", "PagingResponse")})
	}

	if strings.EqualFold(method, "GET") {
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Default response", "DefaultResponse")})
	}

	if strings.EqualFold(method, "POST") || strings.EqualFold(method, "PUT") || strings.EqualFold(method, "DELETE") {
		return withErrorResponse(map[string]openAPIResponse{"200": jsonResponse("Default response", "DefaultResponse")})
	}

	return map[string]openAPIResponse{"200": {Description: "OK"}}
}

func endpointSuccessSchema(method string, path string) string {
	switch endpointKey(method, path) {
	case "POST /api/login/default", "POST /api/login/default/register":
		return "DefaultSessionResponse"
	case "GET /api/user-group":
		return "PagingUserGroupResponse"
	case "POST /api/user-group", "PUT /api/user-group":
		return "DefaultUserGroupResponse"
	case "GET /api/endpoint":
		return "PagingApiEndpointResponse"
	case "POST /api/endpoint", "PUT /api/endpoint":
		return "DefaultApiEndpointResponse"
	case "GET /api/endpoint-rbac":
		return "PagingApiEndpointRbacResponse"
	case "GET /api/endpoint-rbac/ep/me":
		return "DefaultApiEndpointRbacJoinListResponse"
	case "GET /api/endpoint-rbac/validate/me":
		return "DefaultApiEndpointRbacResponse"
	case "POST /api/endpoint-rbac", "PUT /api/endpoint-rbac":
		return "DefaultApiEndpointRbacResponse"
	case "GET /api/user-credential":
		return "PagingUserCredentialResponse"
	case "GET /api/user-credential/email":
		return "DefaultUserLoginResponse"
	case "GET /api/user-login":
		return "PagingAppUserLoginResponse"
	case "GET /api/user-login/email":
		return "DefaultAppUserLoginResponse"
	case "GET /api/user-credential/group/{id}":
		return "DefaultUserRoleListResponse"
	case "POST /api/user-credential":
		return "DefaultUserRoleResponse"
	case "PUT /api/user-credential":
		return "DefaultUserCredentialResponse"
	case "GET /api/camera/stream":
		return "PagingCameraStreamResponse"
	case "POST /api/camera/stream", "PUT /api/camera/stream":
		return "DefaultCameraStreamResponse"
	case "GET /api/home/latest":
		return "PagingHomeLatestResponse"
	case "GET /api/log":
		return "PagingApiLogResponse"
	case "DELETE /api/log":
		return "DefaultLogDeleteResponse"
	case "GET /api/log-service":
		return "PagingRuntimeLogResponse"
	case "DELETE /api/log-service":
		return "DefaultLogDeleteResponse"
	case "GET /api/cache-service":
		return "PagingStringResponse"
	case "GET /api/cache-service/health", "DELETE /api/cache-service", "POST /api/cache-service/wipe":
		return "DefaultBoolResponse"
	case "GET /api/version":
		return "DefaultVersionInfoResponse"
	case "GET /api/admin/test":
		return "DefaultStringResponse"
	case "POST /api/home/new":
		return "PagingStringResponse"
	case "POST /api/file-storage/upload":
		return "PagingFileStorageResponse"
	case "POST /api/file-storage/upload-async", "GET /api/file-storage/job":
		return "DefaultOperationJobResponse"
	}

	return ""
}

func withErrorResponse(in map[string]openAPIResponse) map[string]openAPIResponse {
	in["400"] = jsonResponse("Bad request", "ErrorResponse")
	in["401"] = jsonResponse("Unauthorized", "ErrorResponse")
	in["429"] = jsonResponse("Too many requests", "ErrorResponse")
	in["500"] = jsonResponse("Server error", "ErrorResponse")
	return in
}

func withRateLimitResponse(in map[string]openAPIResponse) map[string]openAPIResponse {
	in["429"] = jsonResponse("Too many requests", "ErrorResponse")
	return in
}

func jsonResponse(description string, schemaName string) openAPIResponse {
	return openAPIResponse{
		Description: description,
		Content: map[string]openAPIMediaType{
			"application/json": {
				Schema: openAPISchema{Ref: schemaRef(schemaName)},
			},
		},
	}
}

func binaryResponse(description string) openAPIResponse {
	return openAPIResponse{
		Description: description,
		Content: map[string]openAPIMediaType{
			"application/octet-stream": {
				Schema: openAPISchema{Type: "string", Format: "binary"},
			},
		},
	}
}

func mjpegResponse(description string) openAPIResponse {
	return openAPIResponse{
		Description: description,
		Content: map[string]openAPIMediaType{
			"multipart/x-mixed-replace": {
				Schema: openAPISchema{Type: "string", Format: "binary"},
			},
		},
	}
}

func enrichOperationWithSchemas(method string, path string, op *openAPIOperation) {
	key := endpointKey(method, path)

	switch key {
	case "POST /api/login/default":
		op.RequestBody = jsonRequestBody("DefaultLoginRequest", true)
	case "POST /api/login/default/register":
		op.RequestBody = jsonRequestBody("DefaultRegisterRequest", true)
	case "POST /api/user-group", "PUT /api/user-group":
		op.RequestBody = jsonRequestBody("UserGroupInputDto", true)
	case "POST /api/endpoint", "PUT /api/endpoint":
		op.RequestBody = jsonRequestBody("ApiEndpointInputDto", true)
	case "POST /api/endpoint-rbac", "PUT /api/endpoint-rbac":
		op.RequestBody = jsonRequestBody("ApiEndpointRbacInputDto", true)
	case "POST /api/camera/stream", "PUT /api/camera/stream":
		op.RequestBody = jsonRequestBody("CameraStreamPayload", true)
	case "POST /api/user-credential":
		op.RequestBody = jsonRequestBody("UserRoleInputDto", true)
	case "PUT /api/user-credential":
		op.RequestBody = jsonRequestBody("UserCredentialInputDto", true)
	case "POST /api/file-storage/upload", "POST /api/file-storage/upload-async":
		op.RequestBody = multipartRequestBody("FileUploadRequest", true)
	case "GET /api/file-storage/download":
		op.Parameters = append(op.Parameters, queryParameter("id", false, "File storage metadata ID for a single file"))
		op.Parameters = append(op.Parameters, stringQueryParameter("ids", false, "Comma-separated file storage metadata IDs for a ZIP download"))
		op.Parameters = append(op.Parameters, typedQueryParameter("view", false, "Set true for inline browser rendering on single-file downloads", "boolean"))
	case "GET /api/file-storage/job":
		op.Parameters = append(op.Parameters, queryParameter("id", true, "Operation job ID"))
	case "POST /api/cache-service/wipe":
		op.RequestBody = jsonRequestBody("CacheWipeRequest", true)
	}

	if isPagingEndpoint(method, path) {
		op.Parameters = append(op.Parameters,
			queryParameter("limit", false, "Maximum number of rows to return. Omit or set 0 for the endpoint default/unbounded behavior."),
			queryParameter("offset", false, "Number of rows to skip before returning results."),
		)
	}

	if isSharedDBPagingEndpoint(method, path) {
		op.Parameters = append(op.Parameters,
			stringQueryParameter("filters", false, `JSON filter object or array. Fields use the shared sqldata shape: {"fieldName":"createdAt","compare":5,"value":1700000000}. Compare values: 1 eq, 2 neq, 3 gt, 4 lt, 5 gte, 6 lte.`),
			stringQueryParameter("sorters", false, `JSON sorter object or array. Fields use the shared sqldata shape: {"fieldName":"createdAt","sort":2}. Sort values: 1 asc, 2 desc.`),
		)
	}

	if key == "DELETE /api/log" || key == "DELETE /api/log-service" {
		op.Parameters = append(op.Parameters, queryParameter("year", true, "Log year, for example 2026"))
		op.Parameters = append(op.Parameters, queryParameter("month", true, "Log month from 1 to 12"))
	}
}

func jsonRequestBody(schemaName string, required bool) *openAPIRequestBody {
	return &openAPIRequestBody{
		Required: required,
		Content: map[string]openAPIMediaType{
			"application/json": {
				Schema: openAPISchema{Ref: schemaRef(schemaName)},
			},
		},
	}
}

func multipartRequestBody(schemaName string, required bool) *openAPIRequestBody {
	return &openAPIRequestBody{
		Required: required,
		Content: map[string]openAPIMediaType{
			"multipart/form-data": {
				Schema: openAPISchema{Ref: schemaRef(schemaName)},
			},
		},
	}
}

func schemaRef(name string) string {
	return "#/components/schemas/" + name
}

func isPagingEndpoint(method string, path string) bool {
	if !strings.EqualFold(method, "GET") {
		return false
	}

	pagingPaths := map[string]struct{}{
		"/api/user-group":      {},
		"/api/user-credential": {},
		"/api/user-login":      {},
		"/api/endpoint":        {},
		"/api/endpoint-rbac":   {},
		"/api/cache-service":   {},
		"/api/camera/stream":   {},
		"/api/home/latest":     {},
		"/api/log":             {},
	}

	_, ok := pagingPaths[path]
	return ok
}

func isSharedDBPagingEndpoint(method string, path string) bool {
	if !strings.EqualFold(method, "GET") {
		return false
	}

	pagingPaths := map[string]struct{}{
		"/api/user-group":      {},
		"/api/user-credential": {},
		"/api/user-login":      {},
		"/api/endpoint":        {},
		"/api/endpoint-rbac":   {},
		"/api/log":             {},
	}

	_, ok := pagingPaths[path]
	return ok
}

func baseComponentSchemas() map[string]openAPISchema {
	schemas := map[string]openAPISchema{
		"HealthResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"alive": {Type: "boolean"},
			},
			Required: []string{"alive"},
		},
		"ReadyResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"ok": {Type: "boolean"},
				"db": {Type: "string"},
			},
			Required: []string{"ok", "db"},
		},
		"ApiHealthResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"ok": {Type: "boolean"},
			},
			Required: []string{"ok"},
		},
		"VersionInfoPayload": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"app":         {Type: "string"},
				"appVersion":  {Type: "string"},
				"coreVersion": {Type: "string"},
				"commit":      {Type: "string"},
				"updatedAt":   {Type: "string"},
			},
			Required: []string{"app", "appVersion", "coreVersion"},
		},
		"RuntimeLogOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"timestamp": {Type: "integer", Format: "int64"},
				"time":      {Type: "string"},
				"level":     {Type: "string"},
				"source":    {Type: "string"},
				"message":   {Type: "string"},
				"os":        {Type: "string"},
			},
		},
		"BootstrapStatusResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"appName":         {Type: "string"},
				"databaseName":    {Type: "string"},
				"databaseCreated": {Type: "boolean"},
				"schemaCreated":   {Type: "boolean"},
				"schemaUpdated":   {Type: "boolean"},
				"driftDetected":   {Type: "boolean"},
				"seeded":          {Type: "boolean"},
				"ready":           {Type: "boolean"},
				"manifestHash":    {Type: "string"},
				"message":         {Type: "string"},
			},
		},
		"DefaultResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"message":    {Type: "string"},
				"durationMs": {Type: "integer", Format: "int64"},
				"result": {
					Type:                 "object",
					AdditionalProperties: true,
				},
			},
		},
		"DefaultSessionResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"message":    {Type: "string"},
				"durationMs": {Type: "integer", Format: "int64"},
				"result": {
					Type: "object",
					Properties: map[string]openAPISchema{
						"ok": {Type: "boolean"},
					},
					Required: []string{"ok"},
				},
			},
		},
		"DefaultLogDeleteResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"message":    {Type: "string"},
				"durationMs": {Type: "integer", Format: "int64"},
				"result": {
					Type: "object",
					Properties: map[string]openAPISchema{
						"deleted": {Type: "integer", Format: "int64"},
					},
					Required: []string{"deleted"},
				},
			},
		},
		"PagingResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"message":    {Type: "string"},
				"durationMs": {Type: "integer", Format: "int64"},
				"data": {
					Type: "object",
					Properties: map[string]openAPISchema{
						"result": {
							Type:        "array",
							Items:       &openAPISchema{Type: "object", AdditionalProperties: true},
							Description: "Endpoint-specific items",
						},
						"limit":      {Type: "integer", Format: "int64"},
						"offset":     {Type: "integer", Format: "int64"},
						"resCnt":     {Type: "integer", Format: "int64"},
						"totalCnt":   {Type: "integer", Format: "int64"},
						"hasNext":    {Type: "boolean"},
						"nextOffset": {Type: "integer", Format: "int64"},
					},
				},
			},
		},
		"ErrorResponse": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"statsCode":  {Type: "integer", Format: "int32"},
				"message":    {Type: "string"},
				"durationMs": {Type: "integer", Format: "int64"},
				"details": {
					Type:        "array",
					Items:       &openAPISchema{Type: "object", AdditionalProperties: true},
					Description: "Additional error details",
				},
			},
		},
		"UserGroupOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":          {Type: "integer", Format: "int64"},
				"title":       {Type: "string"},
				"description": {Type: "string"},
				"parentId":    {Type: "integer", Format: "int64"},
				"isActive":    {Type: "boolean"},
				"createdBy":   {Type: "integer", Format: "int64"},
				"createdAt":   {Type: "integer", Format: "int64"},
				"updatedBy":   {Type: "integer", Format: "int64"},
				"updatedAt":   {Type: "integer", Format: "int64"},
			},
			Required: []string{"title", "parentId"},
		},
		"UserRoleOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":          {Type: "integer", Format: "int64"},
				"title":       {Type: "string"},
				"description": {Type: "string", Nullable: true},
				"parentId":    {Type: "integer", Format: "int64"},
				"groupId":     {Type: "integer", Format: "int64"},
				"isActive":    {Type: "boolean"},
				"createdBy":   {Type: "integer", Format: "int64"},
				"createdAt":   {Type: "integer", Format: "int64"},
				"updatedBy":   {Type: "integer", Format: "int64"},
				"updatedAt":   {Type: "integer", Format: "int64"},
			},
			Required: []string{"title", "parentId", "groupId"},
		},
		"UserLoginOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":         {Type: "integer", Format: "int64"},
				"email":      {Type: "string", Format: "email"},
				"userpwd":    {Type: "string"},
				"firstName":  {Type: "string"},
				"lastName":   {Type: "string"},
				"picUrl":     {Type: "string"},
				"userRoleId": {Type: "integer", Format: "int64"},
				"isActive":   {Type: "boolean"},
				"createdBy":  {Type: "integer", Format: "int64"},
				"createdAt":  {Type: "integer", Format: "int64"},
				"updatedBy":  {Type: "integer", Format: "int64"},
				"updatedAt":  {Type: "integer", Format: "int64"},
			},
			Required: []string{"email"},
		},
		"AppUserLoginPayload": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":         {Type: "integer", Format: "int64"},
				"email":      {Type: "string", Format: "email"},
				"firstName":  {Type: "string"},
				"lastName":   {Type: "string"},
				"picUrl":     {Type: "string"},
				"userRoleId": {Type: "integer", Format: "int64"},
				"isActive":   {Type: "boolean"},
				"createdBy":  {Type: "integer", Format: "int64"},
				"createdAt":  {Type: "integer", Format: "int64"},
				"updatedBy":  {Type: "integer", Format: "int64"},
				"updatedAt":  {Type: "integer", Format: "int64"},
			},
			Required: []string{"email"},
		},
		"UserCredentialOutputDto": {
			OneOf: []openAPISchema{{Ref: schemaRef("UserLoginOutputDto")}, {Ref: schemaRef("UserRoleOutputDto")}},
		},
		"ApiEndpointOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":          {Type: "integer", Format: "int64"},
				"title":       {Type: "string"},
				"description": {Type: "string"},
				"host":        {Type: "string"},
				"path":        {Type: "string"},
				"accessTier": {
					Type:        "integer",
					Format:      "int32",
					Description: "0=DevOnly, 1=AuthOnly, 2=Public.",
				},
				"isActive":  {Type: "boolean"},
				"createdBy": {Type: "integer", Format: "int64"},
				"createdAt": {Type: "integer", Format: "int64"},
				"updatedBy": {Type: "integer", Format: "int64"},
				"updatedAt": {Type: "integer", Format: "int64"},
			},
			Required: []string{"title", "host", "path"},
		},
		"ApiEndpointRbacOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":            {Type: "integer", Format: "int64"},
				"apiEndpointId": {Type: "integer", Format: "int64"},
				"userRoleId":    {Type: "integer", Format: "int64"},
				"canGet":        {Type: "boolean"},
				"canPost":       {Type: "boolean"},
				"canPut":        {Type: "boolean"},
				"canDelete":     {Type: "boolean"},
				"isActive":      {Type: "boolean"},
				"createdBy":     {Type: "integer", Format: "int64"},
				"createdAt":     {Type: "integer", Format: "int64"},
				"updatedBy":     {Type: "integer", Format: "int64"},
				"updatedAt":     {Type: "integer", Format: "int64"},
			},
			Required: []string{"apiEndpointId", "userRoleId"},
		},
		"CameraStreamPayload": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":              {Type: "integer", Format: "int64"},
				"url":             {Type: "string"},
				"stream_protocol": {Type: "integer", Format: "int32"},
				"out_stream_fmt":  {Type: "integer", Format: "int32"},
				"autoStart":       {Type: "boolean"},
				"isActive":        {Type: "boolean"},
				"createdBy":       {Type: "integer", Format: "int64"},
				"createdAt":       {Type: "integer", Format: "int64"},
				"updatedBy":       {Type: "integer", Format: "int64"},
				"updatedAt":       {Type: "integer", Format: "int64"},
			},
			Required: []string{"url"},
		},
		"FileUploadRequest": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"documents": {
					Type:  "array",
					Items: &openAPISchema{Type: "string", Format: "binary"},
				},
				"securityLvl": {
					Type:        "integer",
					Format:      "int32",
					Description: "0=SystemOnly, 1=Group, 2=Role, 3=Public. Defaults to 0 when omitted.",
				},
				"expiredAt": {
					Type:        "integer",
					Format:      "int64",
					Description: "Optional absolute Unix timestamp in seconds. 0 or empty means no expiry. Do not combine with expiresIn.",
				},
				"expiresIn": {
					Type:        "integer",
					Format:      "int64",
					Description: "Optional positive countdown amount. Requires expiresInUnit and is converted to expiredAt by the endpoint.",
				},
				"expiresInUnit": {
					Type:        "string",
					Description: "Countdown unit: second, minute, hour, day, week, month, or year. Plural and short aliases are accepted.",
				},
			},
			Required: []string{"documents"},
		},
		"CacheWipeRequest": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"key":     {Type: "string"},
				"prefix":  {Type: "string"},
				"wipeAll": {Type: "boolean"},
			},
		},
		"DefaultLoginRequest": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"username": {Type: "string"},
				"password": {Type: "string"},
			},
			Required: []string{"username", "password"},
		},
		"DefaultRegisterRequest": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"username":  {Type: "string"},
				"password":  {Type: "string"},
				"firstName": {Type: "string"},
				"lastName":  {Type: "string"},
			},
			Required: []string{"username", "password"},
		},
		"ApiEndpointRbacJoinOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":            {Type: "integer", Format: "int64"},
				"apiEndpointId": {Type: "integer", Format: "int64"},
				"userRoleId":    {Type: "integer", Format: "int64"},
				"host":          {Type: "string"},
				"path":          {Type: "string"},
				"accessTier": {
					Type:        "integer",
					Format:      "int32",
					Description: "0=DevOnly, 1=AuthOnly, 2=Public.",
				},
				"canGet":    {Type: "boolean"},
				"canPost":   {Type: "boolean"},
				"canPut":    {Type: "boolean"},
				"canDelete": {Type: "boolean"},
				"isActive":  {Type: "boolean"},
				"createdAt": {Type: "integer", Format: "int64"},
			},
		},
		"ApiLogOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":             {Type: "integer", Format: "int64"},
				"statsCode":      {Type: "integer", Format: "int32"},
				"durationMs":     {Type: "integer", Format: "int64"},
				"logMsg":         {Type: "string"},
				"clientIpAddrV4": {Type: "string"},
				"clientIpAddrV6": {Type: "string"},
				"requestUrl":     {Type: "string"},
				"createdBy":      {Type: "integer", Format: "int64"},
				"createdAt":      {Type: "integer", Format: "int64"},
				"updatedBy":      {Type: "integer", Format: "int64"},
				"updatedAt":      {Type: "integer", Format: "int64"},
			},
		},
		"FileStorageOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":          {Type: "integer", Format: "int64"},
				"title":       {Type: "string"},
				"description": {Type: "string"},
				"guid":        {Type: "string"},
				"mimeType":    {Type: "string"},
				"vrPath":      {Type: "string"},
				"sha1Chksum":  {Type: "string"},
				"securityLvl": {Type: "integer", Format: "int32"},
				"expiredAt":   {Type: "integer", Format: "int64"},
				"createdBy":   {Type: "integer", Format: "int64"},
				"createdAt":   {Type: "integer", Format: "int64"},
				"updatedBy":   {Type: "integer", Format: "int64"},
				"updatedAt":   {Type: "integer", Format: "int64"},
			},
		},
		"OperationJobOutputDto": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":             {Type: "integer", Format: "int64"},
				"type":           {Type: "string"},
				"resourceKey":    {Type: "string"},
				"idempotencyKey": {Type: "string"},
				"status":         {Type: "string"},
				"attempt":        {Type: "integer", Format: "int64"},
				"maxAttempts":    {Type: "integer", Format: "int64"},
				"result":         {Type: "string"},
				"lastError":      {Type: "string"},
				"startedAt":      {Type: "integer", Format: "int64"},
				"deadlineAt":     {Type: "integer", Format: "int64"},
				"completedAt":    {Type: "integer", Format: "int64"},
				"createdAt":      {Type: "integer", Format: "int64"},
				"updatedAt":      {Type: "integer", Format: "int64"},
			},
		},
		"ResidentPropPicPayload": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":             {Type: "integer", Format: "int64"},
				"residentPropId": {Type: "integer", Format: "int64"},
				"picUrl":         {Type: "string"},
			},
		},
		"ResidentPropPayload": {
			Type: "object",
			Properties: map[string]openAPISchema{
				"id":            {Type: "integer", Format: "int64"},
				"title":         {Type: "string"},
				"description":   {Type: "string"},
				"currencyCode":  {Type: "string"},
				"price":         {Type: "number", Format: "double"},
				"propType":      {Type: "integer", Format: "int32"},
				"propTitle":     {Type: "integer", Format: "int32"},
				"landTitle":     {Type: "integer", Format: "int32"},
				"landTenure":    {Type: "integer", Format: "int32"},
				"builtUpSize":   {Type: "number", Format: "float"},
				"landAreaSize":  {Type: "number", Format: "float"},
				"bedroomCount":  {Type: "integer", Format: "int32"},
				"bathroomCount": {Type: "integer", Format: "int32"},
				"countryAbbrev": {Type: "string"},
				"stateAbbrev":   {Type: "string"},
				"locode":        {Type: "string"},
				"postcode":      {Type: "integer", Format: "int32"},
				"lat":           {Type: "number", Format: "double"},
				"lon":           {Type: "number", Format: "double"},
				"postedAt":      {Type: "integer", Format: "int64"},
				"expiredAt":     {Type: "integer", Format: "int64"},
				"pics": {
					Type:  "array",
					Items: &openAPISchema{Ref: schemaRef("ResidentPropPicPayload")},
				},
			},
		},
	}

	schemas["UserGroupInputDto"] = schemas["UserGroupOutputDto"]
	schemas["UserRoleInputDto"] = schemas["UserRoleOutputDto"]
	schemas["UserLoginInputDto"] = schemas["UserLoginOutputDto"]
	schemas["UserCredentialInputDto"] = openAPISchema{
		OneOf: []openAPISchema{{Ref: schemaRef("UserLoginInputDto")}, {Ref: schemaRef("UserRoleInputDto")}},
	}
	schemas["ApiEndpointInputDto"] = schemas["ApiEndpointOutputDto"]
	schemas["ApiEndpointRbacInputDto"] = schemas["ApiEndpointRbacOutputDto"]
	schemas["ApiLogInputDto"] = schemas["ApiLogOutputDto"]
	schemas["FileStorageInputDto"] = schemas["FileStorageOutputDto"]
	schemas["OperationJobInputDto"] = schemas["OperationJobOutputDto"]

	schemas["RuntimeLogEntry"] = schemas["RuntimeLogOutputDto"]
	schemas["UserGroupPayload"] = schemas["UserGroupOutputDto"]
	schemas["UserRolePayload"] = schemas["UserRoleOutputDto"]
	schemas["UserLoginPayload"] = schemas["UserLoginOutputDto"]
	schemas["UserCredentialPayload"] = schemas["UserCredentialOutputDto"]
	schemas["ApiEndpointPayload"] = schemas["ApiEndpointOutputDto"]
	schemas["ApiEndpointRbacPayload"] = schemas["ApiEndpointRbacOutputDto"]
	schemas["ApiEndpointRbacJoinPayload"] = schemas["ApiEndpointRbacJoinOutputDto"]
	schemas["ApiLogPayload"] = schemas["ApiLogOutputDto"]
	schemas["FileStoragePayload"] = schemas["FileStorageOutputDto"]
	schemas["OperationJobPayload"] = schemas["OperationJobOutputDto"]

	schemas["DefaultStringResponse"] = defaultResponseSchema(openAPISchema{Type: "string"})
	schemas["DefaultBoolResponse"] = defaultResponseSchema(openAPISchema{Type: "boolean"})
	schemas["DefaultVersionInfoResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("VersionInfoPayload")})
	schemas["DefaultUserGroupResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("UserGroupOutputDto")})
	schemas["DefaultUserLoginResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("UserLoginOutputDto")})
	schemas["DefaultAppUserLoginResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("AppUserLoginPayload")})
	schemas["DefaultUserRoleResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("UserRoleOutputDto")})
	schemas["DefaultUserCredentialResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("UserCredentialOutputDto")})
	schemas["DefaultApiEndpointResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("ApiEndpointOutputDto")})
	schemas["DefaultApiEndpointRbacResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("ApiEndpointRbacOutputDto")})
	schemas["DefaultCameraStreamResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("CameraStreamPayload")})
	schemas["DefaultOperationJobResponse"] = defaultResponseSchema(openAPISchema{Ref: schemaRef("OperationJobOutputDto")})
	schemas["DefaultUserRoleListResponse"] = defaultResponseSchema(openAPISchema{Type: "array", Items: &openAPISchema{Ref: schemaRef("UserRoleOutputDto")}})
	schemas["DefaultApiEndpointRbacJoinListResponse"] = defaultResponseSchema(openAPISchema{Type: "array", Items: &openAPISchema{Ref: schemaRef("ApiEndpointRbacJoinOutputDto")}})

	schemas["PagingUserGroupResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("UserGroupOutputDto")})
	schemas["PagingUserCredentialResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("UserCredentialOutputDto")})
	schemas["PagingAppUserLoginResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("AppUserLoginPayload")})
	schemas["PagingApiEndpointResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("ApiEndpointOutputDto")})
	schemas["PagingApiEndpointRbacResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("ApiEndpointRbacOutputDto")})
	schemas["PagingCameraStreamResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("CameraStreamPayload")})
	schemas["PagingHomeLatestResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("ResidentPropPayload")})
	schemas["PagingApiLogResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("ApiLogOutputDto")})
	schemas["PagingRuntimeLogResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("RuntimeLogOutputDto")})
	schemas["PagingFileStorageResponse"] = pagingResponseSchema(openAPISchema{Ref: schemaRef("FileStorageOutputDto")})
	schemas["PagingStringResponse"] = pagingResponseSchema(openAPISchema{Type: "string"})

	return schemas
}

func defaultResponseSchema(resultSchema openAPISchema) openAPISchema {
	return openAPISchema{
		Type: "object",
		Properties: map[string]openAPISchema{
			"message":    {Type: "string"},
			"durationMs": {Type: "integer", Format: "int64"},
			"result":     resultSchema,
		},
	}
}

func pagingResponseSchema(itemSchema openAPISchema) openAPISchema {
	return openAPISchema{
		Type: "object",
		Properties: map[string]openAPISchema{
			"message":    {Type: "string"},
			"durationMs": {Type: "integer", Format: "int64"},
			"data": {
				Type: "object",
				Properties: map[string]openAPISchema{
					"result": {
						Type:  "array",
						Items: &itemSchema,
					},
					"limit":      {Type: "integer", Format: "int64"},
					"offset":     {Type: "integer", Format: "int64"},
					"resCnt":     {Type: "integer", Format: "int64"},
					"totalCnt":   {Type: "integer", Format: "int64"},
					"hasNext":    {Type: "boolean"},
					"nextOffset": {Type: "integer", Format: "int64"},
				},
			},
		},
	}
}

func swaggerUIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>API Docs</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
  <style>
    body { margin: 0; background: #f4f7f9; }
    #swagger-ui { max-width: 1200px; margin: 0 auto; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: '/swagger/openapi.json',
      dom_id: '#swagger-ui',
      deepLinking: true,
      displayRequestDuration: true,
      docExpansion: 'none',
      withCredentials: true,
      requestInterceptor: (req) => {
        if (typeof FormData !== 'undefined' && req.body instanceof FormData && req.headers) {
          delete req.headers['Content-Type'];
          delete req.headers['content-type'];
        }
        return req;
      },
    });
  </script>
</body>
</html>`))
}
