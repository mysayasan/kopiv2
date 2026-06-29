package control

// Request is the application view of a parent→node tunneled HTTP request (a
// FrameReq). Path is the node-local path including the /api prefix; Role is the
// per-request asserted effective role ("viewer"|"admin"); Actor is the parent
// operator identity, carried so node-side audit attributes writes to a person.
type Request struct {
	Method  string
	Path    string
	Role    string
	Actor   string
	Headers map[string]string
	Body    []byte
}

// Response is the application view of the node's reply (a FrameRes).
type Response struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// ToFrame builds a FrameReq carrying this request, correlated by id.
func (r Request) ToFrame(id string) *Frame {
	return &Frame{
		Type:    FrameReq,
		ID:      id,
		Method:  r.Method,
		Path:    r.Path,
		Role:    r.Role,
		Actor:   r.Actor,
		Headers: r.Headers,
		Body:    r.Body,
	}
}

// RequestFromFrame extracts a Request from a FrameReq.
func RequestFromFrame(f *Frame) Request {
	return Request{
		Method:  f.Method,
		Path:    f.Path,
		Role:    f.Role,
		Actor:   f.Actor,
		Headers: f.Headers,
		Body:    f.Body,
	}
}

// ToFrame builds a FrameRes carrying this response, correlated by id.
func (r Response) ToFrame(id string) *Frame {
	return &Frame{
		Type:    FrameRes,
		ID:      id,
		Status:  r.Status,
		Headers: r.Headers,
		Body:    r.Body,
	}
}

// ResponseFromFrame extracts a Response from a FrameRes.
func ResponseFromFrame(f *Frame) Response {
	return Response{
		Status:  f.Status,
		Headers: f.Headers,
		Body:    f.Body,
	}
}
