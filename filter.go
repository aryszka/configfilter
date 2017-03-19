package configfilter

import (
	"io"
	"io/ioutil"
	"net/http"
	"strings"

	gdutil "github.com/golang/gddo/httputil/header"
	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/filters"
	"github.com/zalando/skipper/filters/serve"
	"github.com/zalando/skipper/logging"
)

type filter struct {
	request chan<- request
	log     logging.Logger
}

func validMethod(method string) bool {
	switch method {
	case "OPTIONS", "HEAD", "GET", "PUT", "POST", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

func trimTrailingSlash(path string) string {
	if len(path) > 1 && path[len(path)-1] == '/' {
		return path[:len(path)-1]
	}

	return path
}

func acceptedMime(method string, h http.Header) responseFormat {
	a := gdutil.ParseAccept(h, "Accept")

	var f responseFormat
	for _, ai := range a {
		switch ai.Value {
		case "text/json":
			f |= responseFormatJSON
		case "application/eskip":
			f |= responseFormatEskip
		}
	}

	if f == responseFormatNone {
		f = responseFormatText
	}

	return f
}

func requestPretty(pretty string) bool {
	pretty = strings.ToLower(pretty)
	switch pretty {
	case "false", "0":
		return false
	default:
		return true
	}
}

func canUseContent(method, id string) bool {
	switch method {
	case "PUT", "POST", "PATCH":
		return true
	case "DELETE":
		return id == ""
	default:
		return false
	}
}

func getContentType(method, id, contentType string) (string, error) {
	contentType = strings.Split(contentType, ";")[0]
	switch contentType {
	case "", "text/plain", "application/eskip":
		return contentType, nil
	default:
		return "", errUnsupportedMediaType
	}
}

func parseContent(method, id, contentType string, content io.Reader) ([]*eskip.Route, []string, error) {
	b, err := ioutil.ReadAll(content)
	if err != nil {
		return nil, nil, err
	}

	s := string(b)
	r, err := eskip.Parse(s)
	if err == nil || contentType == "application/eskip" || err != nil && method != "DELETE" {
		if err != nil {
			err = badRequest(err)
		}

		return r, nil, err
	}

	s = strings.Replace(s, " ", "", -1)
	return nil, strings.Split(s, ","), nil
}

func (f *filter) preprocessRequest(hreq *http.Request) (request, error) {
	var req request

	if !validMethod(hreq.Method) {
		return req, errMethodNotSupported
	}

	req.method = hreq.Method
	req.id = hreq.Header.Get("X-Config-RouteID")
	req.accept = acceptedMime(req.method, hreq.Header)
	req.pretty = requestPretty(hreq.URL.Query().Get("pretty"))

	if canUseContent(req.method, req.id) {
		contentType, err := getContentType(req.method, req.id, hreq.Header.Get("Content-Type"))
		if err != nil {
			return req, err
		}

		r, i, err := parseContent(req.method, req.id, contentType, hreq.Body)
		if err != nil {
			return req, err
		}

		if req.id == "" {
			for _, ri := range r {
				if ri.Id == "" {
					return req, badRequestString("route without id")
				}
			}
		} else {
			if len(r) > 1 {
				return req, badRequestString("no multiple routes allowed")
			}
		}

		req.routes = r
		req.ids = i
	}

	return req, nil
}

func (f *filter) serveError(w http.ResponseWriter, err error) {
	if berr, ok := err.(errBadRequest); ok {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(berr.Error()))
		return
	}

	switch err {
	case errMethodNotSupported:
		w.WriteHeader(http.StatusMethodNotAllowed)
	case errNotFound:
		w.WriteHeader(http.StatusNotFound)
	case errUnsupportedMediaType:
		w.WriteHeader(http.StatusUnsupportedMediaType)
	default:
		f.log.Error("server error", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

func decideContentType(f responseFormat) (responseFormat, string) {
	switch {
	case f&responseFormatJSON != 0:
		return responseFormatJSON, "text/json"
	case f&responseFormatEskip != 0:
		return responseFormatEskip, "application/eskip"
	default:
		return responseFormatText, "text/plain"
	}
}

func writeEskip(w io.Writer, req request, rsp response) error {
	var s string
	if req.id == "" {
		s = eskip.Print(req.pretty, rsp.routes...)
	} else {
		s = rsp.routes[0].Print(req.pretty)
	}

	_, err := w.Write([]byte(s))
	return err
}

func writeResponse(w http.ResponseWriter, req request, rsp response) error {
	f, ct := decideContentType(req.accept)
	switch f {
	case responseFormatJSON:
		w.WriteHeader(http.StatusNotImplemented)
		return nil
	default:
		w.Header().Set("Content-Type", ct)
		if req.method == "HEAD" {
			return nil
		}

		return writeEskip(w, req, rsp)
	}
}

func (f *filter) ServeHTTP(w http.ResponseWriter, hreq *http.Request) {
	req, err := f.preprocessRequest(hreq)
	if err != nil {
		f.serveError(w, err)
		return
	}

	switch req.method {
	case "OPTIONS":
		w.Header().Set("Allow", "HEAD, GET, PUT, POST, PATCH")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(APIDescription))
		return
	}

	rspChan := make(chan response)
	req.response = rspChan
	f.request <- req
	rsp := <-rspChan

	if rsp.err != nil {
		f.serveError(w, rsp.err)
	}

	if rsp.withContent {
		writeResponse(w, req, rsp)
	}
}

func (f *filter) Request(ctx filters.FilterContext) {
	id := ctx.PathParam("routeid")
	println(id)
	ctx.Request().Header.Set("X-Config-RouteID", id)
	serve.ServeHTTP(ctx, f)
}

func (f *filter) Response(filters.FilterContext) {}
