package cloudflare

import (
	"context"
	"io"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"syscall/js"

	"github.com/syumai/workers/internal/jsutil"
)

// DurableObjectNamespace represents the namespace of the durable object.
type DurableObjectNamespace struct {
	instance js.Value
}

// NewDurableObjectNamespace returns the namespace for the `varName` binding.
//
// This binding must be defined in the `wrangler.toml` file. The method will
// return an `error` when there is no binding defined by `varName`.
func NewDurableObjectNamespace(ctx context.Context, varName string) (*DurableObjectNamespace, error) {
	inst := getRuntimeContextEnv(ctx).Get(varName)
	if inst.IsUndefined() {
		return nil, fmt.Errorf("%s is undefined", varName)
	}
	return &DurableObjectNamespace{instance: inst}, nil
}

// IdFromName returns a `DurableObjectId` for the given `name`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#deriving-ids-from-names
func (ns *DurableObjectNamespace) IdFromName(name string) *DurableObjectId {
	id := ns.instance.Call("idFromName", name)
	return &DurableObjectId{val: id}
}

// Get obtains the durable object stub for `id`.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#obtaining-an-object-stub
func (ns *DurableObjectNamespace) Get(id *DurableObjectId) (*DurableObjectStub, error) {
	if id == nil || id.val.IsUndefined() {
		return nil, fmt.Errorf("invalid UniqueGlobalId")
	}
	stub := ns.instance.Call("get", id.val)
	return &DurableObjectStub{val: stub}, nil
}

// DurableObjectId represents an identifier for a durable object.
type DurableObjectId struct {
	val js.Value
}

// DurableObjectStub represents the stub to communicate with the durable object.
type DurableObjectStub struct {
	val js.Value
}

// Fetch calls the durable objects `fetch()` method.
//
// https://developers.cloudflare.com/workers/runtime-apis/durable-objects/#sending-http-requests
func (s *DurableObjectStub) Fetch(req *http.Request) (*http.Response, error) {
	jsReq := toJSRequest(req)

	promise := s.val.Call("fetch", jsReq)
	jsRes, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return nil, err
	}

	return toResponse(jsRes)
}

// copied from workers#request.go
func toHeader(headers js.Value) http.Header {
	entries := jsutil.ArrayFrom(headers.Call("entries"))
	headerLen := entries.Length()
	h := http.Header{}
	for i := 0; i < headerLen; i++ {
		entry := entries.Index(i)
		key := entry.Index(0).String()
		values := entry.Index(1).String()
		for _, value := range strings.Split(values, ",") {
			h.Add(key, value)
		}
	}
	return h
}

// copied from workers#response.go
func toJSHeader(header http.Header) js.Value {
	h := jsutil.HeadersClass.New()
	for key, values := range header {
		for _, value := range values {
			h.Call("append", key, value)
		}
	}
	return h
}

func toJSRequest(req *http.Request) js.Value {
	jsReqOptions := jsutil.NewObject()
	jsReqOptions.Set("method", req.Method)
	jsReqOptions.Set("headers", toJSHeader(req.Header))
	jsReqBody := js.Undefined()
	if req.Body != nil {
		jsReqBody = jsutil.ConvertReaderToReadableStream(req.Body)
	}
	jsReqOptions.Set("body", jsReqBody)
	jsReq := jsutil.RequestClass.New(req.URL.String(), jsReqOptions)
	return jsReq
}

func toResponse(res js.Value) (*http.Response, error) {
	status := res.Get("status").Int()
	promise := res.Call("text")
	body, err := jsutil.AwaitPromise(promise)
	if err != nil {
		return nil, err
	}
	header := toHeader(res.Get("headers"))
	contentLength, _ := strconv.ParseInt(header.Get("Content-Length"), 10, 64)

	return &http.Response{
		Status: strconv.Itoa(status) + " " + res.Get("statusText").String(),
		StatusCode: status,
		Header: header,
		Body: io.NopCloser(strings.NewReader(body.String())),
		ContentLength: contentLength,
	}, nil
}