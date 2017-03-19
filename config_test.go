package configfilter

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/filters/builtin"
	"github.com/zalando/skipper/filters/filtertest"
	"github.com/zalando/skipper/logging"
	"github.com/zalando/skipper/logging/loggingtest"
	"github.com/zalando/skipper/proxy"
	"github.com/zalando/skipper/routing"
)

type testProxy struct {
	config  *Spec
	log     *loggingtest.Logger
	routing *routing.Routing
	proxy   *proxy.Proxy
	server  *httptest.Server
}

type failingRW struct {
	log logging.Logger
}

type responseWriter struct {
	status int
	header http.Header
	writer io.Writer
}

var (
	defaultRoutes       = eskip.String(SelfRoutes...)
	defaultRoutesPretty = eskip.Print(true, SelfRoutes...)
)

func (rw failingRW) Read([]byte) (int, error) {
	if rw.log != nil {
		rw.log.Error("read failed")
	}

	return 0, errors.New("read failed")
}

func (rw failingRW) Write([]byte) (int, error) {
	if rw.log != nil {
		rw.log.Error("write failed")
	}

	return 0, errors.New("write failed")
}

func (w *responseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}

	return w.header
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *responseWriter) Write(p []byte) (int, error) {
	return w.writer.Write(p)
}

func (w *responseWriter) Flush() {}

func newTeapot() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
}

func newTestSpec(routes []*eskip.Route, l logging.Logger) *Spec {
	return New(Options{
		DefaultRoutes: routes,
		log:           l,
	})
}

func newTestRouting(l logging.Logger, s *Spec) *routing.Routing {
	fr := builtin.MakeRegistry()
	fr.Register(s)

	rt := routing.New(routing.Options{
		FilterRegistry:  fr,
		DataClients:     []routing.DataClient{s},
		Log:             l,
		MatchingOptions: routing.IgnoreTrailingSlash,
	})

	return rt
}

func newTestProxyHandler(rt *routing.Routing) *proxy.Proxy {
	return proxy.WithParams(proxy.Params{Routing: rt})
}

func newTestProxy(routes []*eskip.Route) *testProxy {
	l := loggingtest.New()
	spec := newTestSpec(routes, l)
	rt := newTestRouting(l, spec)
	l.WaitFor("route settings applied", 120*time.Millisecond)
	p := newTestProxyHandler(rt)
	s := httptest.NewServer(p)
	return &testProxy{
		config:  spec,
		log:     l,
		routing: rt,
		proxy:   p,
		server:  s,
	}
}

func (p *testProxy) close() {
	p.config.Close()
	p.log.Close()
	p.routing.Close()
	p.proxy.Close()
	p.server.Close()
}

func makeRequest(method, u, contentType, content, accept string) (string, *http.Response, error) {
	var body io.ReadCloser
	if content != "" {
		body = ioutil.NopCloser(bytes.NewBufferString(content))
	}

	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return "", nil, err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	rsp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", nil, err
	}

	defer rsp.Body.Close()
	b, err := ioutil.ReadAll(rsp.Body)
	return string(b), rsp, err
}

func get(u, accept string) (string, *http.Response, error) {
	return makeRequest("GET", u, "", "", accept)
}

func getText(u string) (string, *http.Response, error) {
	return get(u, "")
}

func put(u, contentType, content string) (*http.Response, error) {
	_, rsp, err := makeRequest("PUT", u, contentType, content, "")
	return rsp, err
}

func putText(u, content string) (*http.Response, error) {
	return put(u, "", content)
}

func post(u, contentType, content string) (*http.Response, error) {
	_, rsp, err := makeRequest("POST", u, contentType, content, "")
	return rsp, err
}

func postText(u, content string) (*http.Response, error) {
	return post(u, "", content)
}

func patch(u, contentType, content string) (*http.Response, error) {
	_, rsp, err := makeRequest("PATCH", u, contentType, content, "")
	return rsp, err
}

func patchText(u, content string) (*http.Response, error) {
	return patch(u, "", content)
}

func del(u, contentType, content string) (*http.Response, error) {
	_, rsp, err := makeRequest("DELETE", u, contentType, content, "")
	return rsp, err
}

func delText(u, content string) (*http.Response, error) {
	return del(u, "text/plain", content)
}

func delURL(u string) (*http.Response, error) {
	return del(u, "", "")
}

func checkRoutesParsed(got, expected []*eskip.Route) bool {
	if len(got) != len(expected) {
		return false
	}

	for _, ei := range got {
		var found bool
		for _, gi := range expected {
			if gi.Id != ei.Id {
				continue
			}

			found = true
			if gi.String() != ei.String() {
				return false
			}

			break
		}

		if !found {
			return false
		}
	}

	return true
}

func checkRoutes(got, expected string) (bool, error) {
	pgot, err := eskip.Parse(got)
	if err != nil {
		return false, err
	}

	pexpected, err := eskip.Parse(expected)
	if err != nil {
		return false, err
	}

	return checkRoutesParsed(pgot, pexpected), nil
}

func TestAppliesDefaults(t *testing.T) {
	s := New(Options{})

	if len(s.defaults) != len(SelfRoutes) {
		t.Error("failed to apply default routes")
	}

	for i, r := range s.defaults {
		if r.Id != SelfRoutes[i].Id {
			t.Error("failed to apply default routes")
		}
	}

	if _, ok := s.log.(*logging.DefaultLog); !ok {
		t.Error("failed to apply default log")
	}
}

func TestMethodNotAllowed(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, rsp, err := makeRequest("TRACE", p.server.URL+DefaultRoot, "", "", "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusMethodNotAllowed {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestInvalidPath(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := http.Get(p.server.URL + DefaultRoot + "/" + DefaultSelfID + "/some")
	if err != nil {
		t.Error(err)
		return
	}

	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestIgnoreTrailingSlash(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, _, err := getText(p.server.URL + DefaultRoot + "/")
	if err != nil {
		t.Error(err)
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestNotImplementedFormat(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, rsp, err := get(p.server.URL+DefaultRoot, "text/json")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotImplemented {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestUnsupportedMediaType(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := put(p.server.URL+DefaultRoot, "application/yaml", "foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusUnsupportedMediaType {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestBadRequestFormat(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot, "foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDeleteIDsWithEskipContentType(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := del(p.server.URL+DefaultRoot, "application/eskip", "foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDoNotCreateRouteWithoutID(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot, `Path("/foo") -> "https://www.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDoNotAcceptMultipleRoutesForIndividualPath(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot+"/foo", `
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestIndividualRouteNotFoundOnGet(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, rsp, err := getText(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status")
	}
}

func TestIndividualRouteNotFoundOnDelete(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := delURL(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status")
	}
}

func TestOptions(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, rsp, err := makeRequest("OPTIONS", p.server.URL+DefaultRoot, "", "", "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
	}

	if rsp.Header.Get("Allow") != "HEAD, GET, PUT, POST, PATCH" {
		t.Error("unexpected Allow header")
	}
}

func TestHead(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, rsp, err := makeRequest("HEAD", p.server.URL+DefaultRoot, "", "", "application/eskip")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
		return
	}

	if rsp.Header.Get("Content-Type") != "application/eskip" {
		t.Error("unexpected content type", rsp.Header.Get("Content-Type"))
		return
	}

	if s != "" {
		t.Error("unexpected content")
	}
}

func TestGetInitial(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestAcceptEskip(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, rsp, err := get(p.server.URL+DefaultRoot, "application/eskip")
	if err != nil {
		t.Error(err)
	}

	if rsp.Header.Get("Content-Type") != "application/eskip" {
		t.Error("unexpected content type")
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestAcceptFallback(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, rsp, err := get(p.server.URL+DefaultRoot, "application/yaml")
	if err != nil {
		t.Error(err)
	}

	if rsp.Header.Get("Content-Type") != "text/plain" {
		t.Error("unexpected content type")
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestNoPrettyPrint(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, _, err := getText(p.server.URL + DefaultRoot + "?pretty=false")
	if err != nil {
		t.Error(err)
		return
	}

	if s != defaultRoutes {
		t.Error("failed to return routes without pretty printing", s)
	}
}

func TestUpdateRouting(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
	}

	foo := newTeapot()
	defer foo.Close()

	s += fmt.Sprintf(`;
		foo: Path("/foo") -> "%s"
	`, foo.URL)
	_, err = putText(p.server.URL+DefaultRoot, s)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err = getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
	}

	if match, err := checkRoutes(s, defaultRoutes+fmt.Sprintf(`;
		foo: Path("/foo") -> "%s"
	`, foo.URL)); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestApplyRouting(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	foo := newTeapot()
	defer foo.Close()

	p.log.Reset()

	s := fmt.Sprintf(`;
		foo: Path("/foo") -> "%s"
	`, foo.URL)
	_, err := putText(p.server.URL+DefaultRoot, s)
	if err != nil {
		t.Error(err)
		return
	}

	if err := p.log.WaitFor("route settings applied", 120*time.Millisecond); err != nil {
		t.Error(err)
		return
	}

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusTeapot {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestKeepDefaultsOnUpdate(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	foo := newTeapot()
	defer foo.Close()

	s := fmt.Sprintf(`;
		foo: Path("/foo") -> "%s"
	`, foo.URL)
	_, err := putText(p.server.URL+DefaultRoot, s)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err = getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
	}

	if match, err := checkRoutes(s, defaultRoutes+fmt.Sprintf(`;
		foo: Path("/foo") -> "%s"
	`, foo.URL)); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routing doesn't match")
	}
}

func TestUpdateExistingRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	foo := newTeapot()
	defer foo.Close()

	_, err := putText(p.server.URL+DefaultRoot, fmt.Sprintf(`foo: Path("/foo") -> "%s"`, foo.URL))
	if err != nil {
		t.Error(err)
		return
	}

	p.log.WaitFor("route settings applied", 120*time.Millisecond)
	p.log.Reset()

	_, err = putText(p.server.URL+DefaultRoot, fmt.Sprintf(`foo: Path("/bar") -> "%s"`, foo.URL))
	if err != nil {
		t.Error(err)
		return
	}

	p.log.WaitFor("route settings applied", 120*time.Millisecond)

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status code", rsp.StatusCode)
	}

	rsp, err = http.Get(p.server.URL + "/bar")
	if err != nil {
		t.Error(err)
		return
	}

	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusTeapot {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDeleteExistingRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	foo := newTeapot()
	defer foo.Close()

	_, err := putText(p.server.URL+DefaultRoot, fmt.Sprintf(`foo: Path("/foo") -> "%s"`, foo.URL))
	if err != nil {
		t.Error(err)
		return
	}

	p.log.WaitFor("route settings applied", 120*time.Millisecond)
	p.log.Reset()

	_, err = putText(p.server.URL+DefaultRoot, "")
	if err != nil {
		t.Error(err)
		return
	}

	p.log.WaitFor("route settings applied", 120*time.Millisecond)

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	defer rsp.Body.Close()

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDoNotUpdateDefaults(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `foo: Path("/foo") -> "https://www.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = putText(p.server.URL+DefaultRoot,
		SelfRoutes[0].Id+`: Path("/bar") -> "https://bar";
		foo: Path("/foo") -> "https://baz"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+`;
		foo: Path("/foo") -> "https://baz"
	`); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("routes don't match")
	}
}

func TestReadFailed(t *testing.T) {
	l := loggingtest.New()
	defer l.Close()

	spec := New(Options{
		DefaultRoutes: SelfRoutes,
		log:           l,
	})
	f, err := spec.CreateFilter(nil)
	if err != nil {
		t.Error(err)
		return
	}

	ctx := &filtertest.Context{
		FRequest: &http.Request{
			Method: "PUT",
			URL:    &url.URL{Path: DefaultRoot},
			Header: make(http.Header),
			Body:   ioutil.NopCloser(failingRW{}),
		},
		FParams: make(map[string]string),
	}

	f.Request(ctx)
	if ctx.FResponse.StatusCode != http.StatusInternalServerError {
		t.Error("failed to fail")
	}
}

func TestMultipleUpdatesDeletes(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo1.example.org";
		bar: Path("/bar") -> "https://bar1.example.org";
		baz: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+`;
		foo: Path("/foo") -> "https://foo1.example.org";
		bar: Path("/bar") -> "https://bar1.example.org";
		baz: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes")
	}
}

func TestPost(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	ts := newTeapot()
	defer ts.Close()

	p.log.Reset()
	_, err = postText(p.server.URL+DefaultRoot, fmt.Sprintf(`
		foo: Path("/foo") -> "%s";
		bar: Path("/bar") -> "https://bar1.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`, ts.URL))
	if err != nil {
		t.Error(err)
		return
	}

	if err := p.log.WaitFor("route settings applied", 120*time.Millisecond); err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+fmt.Sprintf(`;
		foo: Path("/foo") -> "%s";
		bar: Path("/bar") -> "https://bar1.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`, ts.URL)); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes")
	}

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusTeapot {
		t.Error("failed to apply routes")
	}
}

func TestPatch(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	ts := newTeapot()
	defer ts.Close()

	p.log.Reset()
	_, err = patchText(p.server.URL+DefaultRoot, fmt.Sprintf(`
		__config: * -> status(200) -> <shunt>;
		foo: Path("/foo") -> "%s";
		bar: Path("/bar") -> "https://bar1.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`, ts.URL))
	if err != nil {
		t.Error(err)
		return
	}

	if err := p.log.WaitFor("route settings applied", 120*time.Millisecond); err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+fmt.Sprintf(`;
		foo: Path("/foo") -> "%s";
		bar: Path("/bar") -> "https://bar1.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`, ts.URL)); err != nil {
		t.Error(err)
		return
	} else if !match {
		t.Error("failed to match routes", s, defaultRoutes+fmt.Sprintf(`;
		foo: Path("/foo") -> "%s";
		bar: Path("/bar") -> "https://bar1.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
		quux: Path("/quux") -> "https://quux.example.org";
		quuz: Path("/quuz") -> "https://quuz.example.org";
		corge: Path("/corge") -> "https://corge.example.org";
	`, ts.URL))
		return
	}

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusTeapot {
		t.Error("failed to apply routes")
	}
}

func TestDeleteAsRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	rsp, err := delText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("invalid status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes")
	}
}

func TestDeleteAsID(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	rsp, err := delText(p.server.URL+DefaultRoot, "foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("invalid status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes")
	}
}

func TestApplyDelete(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	p.log.Reset()
	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if err = p.log.WaitFor("route settings applied", 120*time.Millisecond); err != nil {
		t.Error(err)
		return
	}

	p.log.Reset()
	_, err = delText(p.server.URL+DefaultRoot, "foo")
	if err != nil {
		t.Error(err)
		return
	}

	if err := p.log.WaitFor("route settings applied", 120*time.Millisecond); err != nil {
		t.Error(err)
		return
	}

	rsp, err := http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("failed to delete route", rsp.StatusCode)
	}
}

func TestDeleteAsMultipleIDs(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo.example.org";
		bar: Path("/bar") -> "https://bar.example.org";
		baz: Path("/baz") -> "https://baz.example.org";
		qux: Path("/qux") -> "https://qux.example.org";
	`)
	if err != nil {
		t.Error(err)
		return
	}

	rsp, err := delText(p.server.URL+DefaultRoot, "foo, baz, qux")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("invalid status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+`;
		bar: Path("/bar") -> "https://bar.example.org";
	`); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestDoNotDeleteDefaultRoutesAsEskip(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := delText(p.server.URL+DefaultRoot, ";"+DefaultSelfID+`:
		Path("/__config") -> config() -> <shunt>;
	`)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestDoNotDeleteDefaultRoutesAsID(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := delText(p.server.URL+DefaultRoot, DefaultSelfID)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestOptionsIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, rsp, err := makeRequest("OPTIONS", p.server.URL+DefaultRoot+"/foo", "", "", "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
	}

	if rsp.Header.Get("Allow") != "HEAD, GET, PUT, POST, PATCH" {
		t.Error("unexpected Allow header")
	}
}

func TestHeadIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot+"/foo",
		`Path("/foo") -> "https://www.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	s, rsp, err := makeRequest("HEAD", p.server.URL+DefaultRoot+"/foo", "", "", "application/eskip")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
		return
	}

	if rsp.Header.Get("Content-Type") != "application/eskip" {
		t.Error("unexpected content type", rsp.Header.Get("Content-Type"))
		return
	}

	if s != "" {
		t.Error("unexpected content")
	}
}

func TestInsertGetIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	ts := newTeapot()
	defer ts.Close()

	rsp, err := putText(p.server.URL+DefaultRoot+"/foo",
		fmt.Sprintf(`Path("/foo") -> "%s"`, ts.URL))
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code", rsp.StatusCode)
		return
	}

	s, rsp, err := getText(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code", rsp.StatusCode)
		return
	}

	if match, err := checkRoutes(s, fmt.Sprintf(`Path("/foo") -> "%s"`, ts.URL)); err != nil {
		t.Error(err)
		return
	} else if !match {
		t.Error("failed to match routes", s)
		return
	}

	rsp, err = http.Get(p.server.URL + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusTeapot {
		t.Error("unexpected status code")
	}
}

func TestUpdateIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot+"/foo",
		`Path("/foo") -> "https://foo.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = putText(p.server.URL+DefaultRoot+"/foo",
		`Path("/foo") -> "https://foo1.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, `Path("/foo") -> "https://foo1.example.org"`); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestDeleteIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot+"/foo",
		`Path("/foo") -> "https://foo.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = delURL(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	_, rsp, err := getText(p.server.URL + DefaultRoot + "/foo")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status code")
	}
}

func TestPutEmptyIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot+"/foo", "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code")
	}
}

func TestPatchEmptyIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo1.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	rsp, err := patchText(p.server.URL+DefaultRoot+"/foo", "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code")
	}
}

func TestIndividualPatchNotFound(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := patchText(p.server.URL+DefaultRoot+"/foo",
		`Path("/foo") -> "https://foo.example.org"`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusNotFound {
		t.Error("unexpected status code", rsp.StatusCode)
	}
}

func TestDedupeRoutes(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo1.example.org";
		foo: Path("/foo") -> "https://foo2.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	r, err := eskip.Parse(s)
	if err != nil {
		t.Error(err)
		return
	}

	if len(r) != len(SelfRoutes)+1 {
		t.Error("unexpected count of routes")
	}
}

func TestPutDefaultRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot+"/"+SelfRoutes[0].Id, `
		Path("/foo") -> "https://foo1.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestPatchDefaultRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := patchText(p.server.URL+DefaultRoot+"/"+SelfRoutes[0].Id, `
		Path("/foo") -> "https://foo1.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusOK {
		t.Error("unexpected status code")
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes); err != nil {
		t.Error(err, s)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestMissingPatchRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	rsp, err := putText(p.server.URL+DefaultRoot+"/"+SelfRoutes[0].Id, "")
	if err != nil {
		t.Error(err)
		return
	}

	if rsp.StatusCode != http.StatusBadRequest {
		t.Error("unexpected status code")
		return
	}
}

func TestPatchIndividualRoute(t *testing.T) {
	p := newTestProxy(SelfRoutes)
	defer p.close()

	_, err := putText(p.server.URL+DefaultRoot, `
		foo: Path("/foo") -> "https://foo1.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	_, err = patchText(p.server.URL+DefaultRoot+"/foo", `
		foo: Path("/foo") -> "https://foo2.example.org"
	`)
	if err != nil {
		t.Error(err)
		return
	}

	s, _, err := getText(p.server.URL + DefaultRoot)
	if err != nil {
		t.Error(err)
		return
	}

	if match, err := checkRoutes(s, defaultRoutes+`;
		foo: Path("/foo") -> "https://foo2.example.org"
	`); err != nil {
		t.Error(err)
	} else if !match {
		t.Error("failed to match routes", s)
	}
}

func TestMissNoUpdate(t *testing.T) {
	l := loggingtest.New()
	defer l.Close()

	spec := New(Options{log: l})
	defer spec.Close()

	r, err := spec.LoadAll()
	if err != nil {
		t.Error(err)
		return
	}

	if !checkRoutesParsed(r, SelfRoutes) {
		t.Error("unexpected routes received")
		return
	}

	f, err := spec.CreateFilter(nil)
	if err != nil {
		t.Error(err)
		return
	}

	putRoute := func(r string) {
		ctx := &filtertest.Context{
			FRequest: &http.Request{
				Method: "PUT",
				URL:    &url.URL{Path: DefaultRoot},
				Header: make(http.Header),
				Body:   ioutil.NopCloser(bytes.NewBufferString(r)),
			},
			FParams: make(map[string]string),
		}

		f.Request(ctx)
	}

	putRoute(`foo: Path("/foo") -> "https://foo.example.org"`)
	putRoute(`bar: Path("/bar") -> "https://bar.example.org"`)

	r, _, err = spec.LoadUpdate()
	if err == nil && len(r) != 2 {
		t.Error("missing update")
		return
	}
}
