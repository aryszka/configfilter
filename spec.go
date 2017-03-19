package configfilter

import (
	"errors"
	"github.com/zalando/skipper/eskip"
	"github.com/zalando/skipper/filters"
	"github.com/zalando/skipper/logging"
)

const (
	// Name is the name of the config filter in eskip documents.
	Name = "config"

	// DefaultSelfID is used as the default route ID for the API root endpoint.
	DefaultSelfID = "__config"

	// DefaultRoot is the default path of the API root endpoint.
	DefaultRoot = "/" + DefaultSelfID
)

type responseFormat int

const (
	responseFormatNone responseFormat = 0
	responseFormatText responseFormat = 1 << iota
	responseFormatEskip
	responseFormatJSON
)

// Options is used to provide initialization options for the config filter.
type Options struct {

	// DefaultRoutes contains routes that the data client will always include
	// in the routing. They cannot be changed or deleted through the API.
	//
	// It is a good practice to include two routes to the API endpoint: an API root
	// endpoint with all the routes and an endpoint for the individual routes. The
	// route for the individual routes is expected to have a path predicate with a
	// wildcard called routeid, e.g. Path("/__config/:routeid").
	DefaultRoutes []*eskip.Route

	log logging.Logger
}

// Spec implements a Skipper data client and a filter specification, where the
// data client for the routing table accepts route updates through an API served
// by itself as a filter.
type Spec struct {
	defaults []*eskip.Route
	log      logging.Logger
	routes   []*eskip.Route
	request  chan request
	getAll   chan (chan<- updateMessage)
	update   chan updateMessage
	stop     chan struct{}
}

type response struct {
	withContent bool
	routes      []*eskip.Route
	err         error
}

type request struct {
	id       string
	method   string
	routes   []*eskip.Route
	ids      []string
	accept   responseFormat
	pretty   bool
	response chan<- response
}

type updateMessage struct {
	routes     []*eskip.Route
	deletedIDs []string
	err        error
}

type errBadRequest struct{ err error }

// SelfRoutes contain route specifications that can be used in the Options as API
// endpoints for the data client.
var SelfRoutes = []*eskip.Route{{
	Id:      DefaultSelfID,
	Path:    DefaultRoot,
	Filters: []*eskip.Filter{{Name: Name}},
	Shunt:   true,
}, {
	Id:      DefaultSelfID + "__singleRoute",
	Path:    DefaultRoot + "/:routeid",
	Filters: []*eskip.Filter{{Name: Name}},
	Shunt:   true,
}}

var (
	errMethodNotSupported   = errors.New("method not supported")
	errNotFound             = errors.New("not found")
	errUnsupportedMediaType = errors.New("unsupported media type")
	errMissedUpdate         = errors.New("missed update")
)

func (m updateMessage) hasData() bool {
	return len(m.routes) != 0 ||
		len(m.deletedIDs) != 0 ||
		m.err != nil
}

func badRequest(err error) error {
	return errBadRequest{err}
}

func badRequestString(s string) error {
	return errBadRequest{errors.New(s)}
}

func (e errBadRequest) Error() string { return e.err.Error() }

// New initializes a data client/filter specification for Skipper route
// configurations.
func New(o Options) *Spec {
	if len(o.DefaultRoutes) == 0 {
		o.DefaultRoutes = SelfRoutes
	}

	if o.log == nil {
		o.log = &logging.DefaultLog{}
	}

	s := &Spec{
		defaults: uniqueRoutes(o.DefaultRoutes),
		log:      o.log,
		request:  make(chan request),
		getAll:   make(chan (chan<- updateMessage)),
		update:   make(chan updateMessage),
		stop:     make(chan struct{}),
	}

	go s.run()
	return s
}

func (s *Spec) getRoot(req request) response {
	return response{
		withContent: true,
		routes:      append(s.routes, s.defaults...),
	}
}

func (s *Spec) putRoot(req request) updateMessage {
	var update updateMessage
	routes := uniqueRoutes(req.routes)
	routes = removeRoutes(routes, s.defaults)
	s.routes, update.routes, update.deletedIDs = replaceRoutes(s.routes, routes)
	return update
}

func (s *Spec) patchInRoot(req request) updateMessage {
	var update updateMessage
	routes := uniqueRoutes(req.routes)
	routes = removeRoutes(routes, s.defaults)
	s.routes, update.routes = upsertRoutes(s.routes, routes)
	return update
}

func (s *Spec) deleteFromRoot(req request) updateMessage {
	var update updateMessage
	routes := idsToRoutes(req.ids, s.routes)
	routes = append(routes, req.routes...)
	routes = uniqueRoutes(routes)
	routes = removeRoutes(routes, s.defaults)
	routes = removeRoutes(routes, removeRoutes(routes, s.routes))
	s.routes = removeRoutes(s.routes, routes)
	update.deletedIDs = routesToIDs(routes)
	return update
}

func (s *Spec) get(req request) response {
	routes := idsToRoutes([]string{req.id}, append(s.defaults, s.routes...))
	if len(routes) == 0 {
		return response{err: errNotFound}
	}

	return response{
		routes:      routes,
		withContent: true,
	}
}

func (s *Spec) put(req request) (rsp response, update updateMessage) {
	if len(req.routes) != 1 {
		rsp = response{err: badRequestString("exactly one route expected")}
		return
	}

	req.routes[0].Id = req.id
	routes := removeRoutes(req.routes, s.defaults)
	if len(routes) == 0 {
		return
	}

	s.routes, update.routes = upsertRoutes(s.routes, routes)
	return
}

func (s *Spec) patch(req request) (rsp response, update updateMessage) {
	routes := idsToRoutes([]string{req.id}, append(s.defaults, s.routes...))
	if len(routes) == 0 {
		rsp.err = errNotFound
		return
	}

	if len(req.routes) != 1 {
		rsp.err = badRequestString("exactly one route expected")
		return
	}

	routes = removeRoutes(routes, s.defaults)
	if len(routes) == 0 {
		return
	}

	req.routes[0].Id = req.id
	s.routes, update.routes = upsertRoutes(s.routes, req.routes)
	return
}

func (s *Spec) del(req request) (rsp response, update updateMessage) {
	routes := idsToRoutes([]string{req.id}, s.routes)
	if len(routes) == 0 {
		rsp.err = errNotFound
		return
	}

	s.routes = removeRoutes(s.routes, routes)
	update.deletedIDs = routesToIDs(routes)
	return
}

func (s *Spec) handleRoot(req request) (rsp response, update updateMessage) {
	switch req.method {
	case "HEAD", "GET":
		rsp = s.getRoot(req)
	case "PUT", "POST":
		update = s.putRoot(req)
	case "PATCH":
		update = s.patchInRoot(req)
	case "DELETE":
		update = s.deleteFromRoot(req)
	}

	return
}

func (s *Spec) handleIndividual(req request) (response, updateMessage) {
	var (
		rsp    response
		update updateMessage
	)

	switch req.method {
	case "HEAD", "GET":
		rsp = s.get(req)
	case "PUT", "POST":
		rsp, update = s.put(req)
	case "PATCH":
		rsp, update = s.patch(req)
	case "DELETE":
		rsp, update = s.del(req)
	}

	return rsp, update
}

func (s *Spec) handle(req request) (response, updateMessage) {
	if req.id == "" {
		return s.handleRoot(req)
	}

	return s.handleIndividual(req)
}

func (s *Spec) run() {
	var (
		updateRelay  chan<- updateMessage
		updateToSend updateMessage
	)

	for {
		select {
		case all := <-s.getAll:
			all <- updateMessage{routes: s.routes}
		case updateRelay <- updateToSend:
			updateRelay = nil
		case req := <-s.request:
			rsp, update := s.handle(req)
			if update.hasData() {
				if updateRelay == nil {
					updateRelay = s.update
					updateToSend = update
				} else {
					updateToSend = updateMessage{err: errMissedUpdate}
				}
			}

			req.response <- rsp
		case <-s.stop:
			return
		}
	}
}

// LoadAll returns all the current routes. (Skipper's routing.DataClient
// implementation.)
func (s *Spec) LoadAll() ([]*eskip.Route, error) {
	c := make(chan updateMessage)
	s.getAll <- c
	m := <-c
	return append(s.defaults, m.routes...), m.err
}

// LoadUpdate returns all changes since the last call to LoadAll or LoadUpdate.
// (Skipper's routing.DataClient implementation.)
func (s *Spec) LoadUpdate() ([]*eskip.Route, []string, error) {
	u := <-s.update
	return u.routes, u.deletedIDs, u.err
}

// Name returns the name of the filter in eskip documents ("config").
// (Skipper's filters.Spec implementation.)
func (s *Spec) Name() string { return Name }

// CreateFilter creates a config filter. It iscalled by the routing package.
// (Skipper's filters.Spec implementation.)
func (s *Spec) CreateFilter(_ []interface{}) (filters.Filter, error) {
	return &filter{
		request: s.request,
		log:     s.log,
	}, nil
}

// Close releases the resource taken by the data client.
func (s *Spec) Close() {
	close(s.stop)
}
