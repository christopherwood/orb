/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package resthandler

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/trustbloc/sidetree-svc-go/pkg/restapi/common"

	logfields "github.com/trustbloc/orb/internal/pkg/log"
	"github.com/trustbloc/orb/pkg/activitypub/store/spi"
	"github.com/trustbloc/orb/pkg/activitypub/vocab"
	orberrors "github.com/trustbloc/orb/pkg/errors"
)

const loggerModule = "activitypub_resthandler"

const (
	// PublicKeysPath specifies the service's "keys" endpoint.
	PublicKeysPath = "/keys/{id}"
	// FollowersPath specifies the service's 'followers' endpoint.
	FollowersPath = "/followers"
	// FollowingPath specifies the service's 'following' endpoint.
	FollowingPath = "/following"
	// OutboxPath specifies the service's 'outbox' endpoint.
	OutboxPath = "/outbox"
	// InboxPath specifies the service's 'inbox' endpoint.
	InboxPath = "/inbox"
	// WitnessesPath specifies the service's 'witnesses' endpoint.
	WitnessesPath = "/witnesses"
	// WitnessingPath specifies the service's 'witnessing' endpoint.
	WitnessingPath = "/witnessing"
	// LikedPath specifies the service's 'liked' endpoint.
	LikedPath = "/liked"
	// SharesPath specifies the object's 'shares' endpoint.
	SharesPath = "/shares"
	// LikesPath specifies the object's 'likes' endpoint.
	LikesPath = "/likes"
	// ActivitiesPath specifies the object's 'activities' endpoint.
	ActivitiesPath = "/activities/{id}"
	// AcceptListPath specifies the endpoint to manage an "accept list" for a service.
	AcceptListPath = "/acceptlist"
)

const (
	pageParam    = "page"
	pageNumParam = "page-num"
	idParam      = "id"
	typeParam    = "type"

	authHeader  = "Authorization"
	tokenPrefix = "Bearer "

	notFoundResponse            = "Not Found.\n"
	unauthorizedResponse        = "Unauthorized.\n"
	badRequestResponse          = "Bad Request.\n"
	internalServerErrorResponse = "Internal Server Error.\n"
)

// Config contains configuration parameters for the handler.
type Config struct {
	BasePath               string
	ObjectIRI              *url.URL
	ServiceEndpointURL     *url.URL
	PageSize               int
	VerifyActorInSignature bool
}

type handler struct {
	*Config
	*AuthHandler

	params    map[string]string
	handler   common.HTTPRequestHandler
	marshal   func(v interface{}) ([]byte, error)
	getParams func(req *http.Request) map[string][]string
	sortOrder spi.SortOrder
}

func newHandler(endpoint string, cfg *Config, s spi.Store, rh common.HTTPRequestHandler,
	verifier signatureVerifier, sortOrder spi.SortOrder, tm authTokenManager, params ...string,
) *handler {
	h := &handler{
		Config:  cfg,
		params:  paramsBuilder(params).build(),
		handler: rh,
		marshal: vocab.Marshal,
		getParams: func(req *http.Request) map[string][]string {
			return req.URL.Query()
		},
		sortOrder: sortOrder,
	}

	h.AuthHandler = NewAuthHandler(cfg, endpoint, http.MethodGet, s, verifier, tm, nil)

	return h
}

// Path returns the base path of the target URL for this handler.
func (h *handler) Path() string {
	return h.endpoint
}

// Params returns the accepted parameters.
func (h *handler) Params() map[string]string {
	return h.params
}

// Method returns the HTTP method, which is always GET.
func (h *handler) Method() string {
	return http.MethodGet
}

// Handler returns the handler that should be invoked when an HTTP GET is requested to the target endpoint.
// This handler must be registered with an HTTP server.
func (h *handler) Handler() common.HTTPRequestHandler {
	return h.handler
}

func (h *handler) getPageID(objectIRI fmt.Stringer, pageNum int) string {
	var delimiter string

	if strings.Contains(objectIRI.String(), "?") {
		delimiter = "&"
	} else {
		delimiter = "?"
	}

	if pageNum >= 0 {
		return fmt.Sprintf("%s%s%s=true&%s=%d", objectIRI, delimiter, pageParam, pageNumParam, pageNum)
	}

	return fmt.Sprintf("%s%s%s=true", objectIRI, delimiter, pageParam)
}

func (h *handler) getPageURL(objectIRI fmt.Stringer, pageNum int) (*url.URL, error) {
	pageID := h.getPageID(objectIRI, pageNum)

	pageURL, err := url.Parse(pageID)
	if err != nil {
		return nil, fmt.Errorf("invalid 'page' URL [%s]: %w", pageID, err)
	}

	return pageURL, nil
}

func (h *handler) getCurrentPrevNext(totalItems int, options *spi.QueryOptions) (int, int, int) {
	first := getFirstPageNum(totalItems, options.PageSize, options.SortOrder)
	last := getLastPageNum(totalItems, options.PageSize, options.SortOrder)

	var current int
	if options.PageNumber >= 0 {
		current = options.PageNumber
	} else {
		current = first
	}

	var prev, next int

	if options.SortOrder == spi.SortDescending {
		prev, next = getPrevNextDescending(current, first, last)
	} else {
		prev, next = getPrevNextAscending(current, first, last)
	}

	return current, prev, next
}

func (h *handler) getIDPrevNextURL(objectIRI fmt.Stringer, totalItems int,
	options *spi.QueryOptions,
) (*url.URL, *url.URL, *url.URL, error) {
	current, prev, next := h.getCurrentPrevNext(totalItems, options)

	var err error

	var nextURL *url.URL

	var prevURL *url.URL

	if prev >= 0 {
		prevURL, err = h.getPageURL(objectIRI, prev)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	if next >= 0 {
		nextURL, err = h.getPageURL(objectIRI, next)
		if err != nil {
			return nil, nil, nil, err
		}
	}

	pageURI, err := h.getPageURL(objectIRI, current)
	if err != nil {
		return nil, nil, nil, err
	}

	return pageURI, prevURL, nextURL, nil
}

func (h *handler) isPaging(req *http.Request) bool {
	return h.paramAsBool(req, pageParam)
}

func (h *handler) getPageNum(req *http.Request) (int, bool) {
	return h.paramAsInt(req, pageNumParam)
}

func (h *handler) paramAsInt(req *http.Request, param string) (int, bool) {
	params := h.getParams(req)

	values := params[param]
	if len(values) == 0 || values[0] == "" {
		return 0, false
	}

	size, err := strconv.Atoi(values[0])
	if err != nil {
		logfields.InvalidParameterValue(h.logger, param, err)

		return 0, false
	}

	return size, true
}

func (h *handler) paramAsBool(req *http.Request, param string) bool {
	params := h.getParams(req)

	values := params[param]
	if len(values) == 0 || values[0] == "" {
		return false
	}

	b, err := strconv.ParseBool(values[0])
	if err != nil {
		logfields.InvalidParameterValue(h.logger, param, err)

		return false
	}

	return b
}

func getPrevNextAscending(current, first, last int) (int, int) {
	prev := -1
	next := -1

	if current < last {
		next = current + 1
	}

	if current > first {
		if current > last {
			prev = last
		} else {
			prev = current - 1
		}
	}

	return prev, next
}

func getPrevNextDescending(current, first, last int) (int, int) {
	prev := -1
	next := -1

	if current > last {
		if current > first {
			next = first
		} else {
			next = current - 1
		}
	}

	if current < first {
		prev = current + 1
	}

	return prev, next
}

func getFirstPageNum(totalItems, pageSize int, sortOrder spi.SortOrder) int {
	if sortOrder == spi.SortAscending {
		return 0
	}

	if totalItems%pageSize > 0 {
		return totalItems / pageSize
	}

	return totalItems/pageSize - 1
}

func getLastPageNum(totalItems, pageSize int, sortOrder spi.SortOrder) int {
	if sortOrder == spi.SortDescending {
		return 0
	}

	if totalItems%pageSize > 0 {
		return totalItems / pageSize
	}

	return totalItems/pageSize - 1
}

type paramsBuilder []string

func (p paramsBuilder) build() map[string]string {
	m := make(map[string]string)

	for _, p := range p {
		m[p] = fmt.Sprintf("{%s}", p)
	}

	return m
}

func getID(path string) getIDFunc {
	return func(objectIRI *url.URL, _ *http.Request) (*url.URL, error) {
		return url.Parse(fmt.Sprintf("%s/%s", objectIRI, path))
	}
}

func getObjectIRI(baseObjectIRI *url.URL) getObjectIRIFunc {
	return func(*http.Request) (*url.URL, error) {
		return baseObjectIRI, nil
	}
}

func getIDFromParam(objectIRI *url.URL, path string) getIDFunc {
	return func(_ *url.URL, req *http.Request) (*url.URL, error) {
		id := getIDParam(req)
		if id == "" {
			return nil, orberrors.NewBadRequest(errors.New("id not specified in URL"))
		}

		return url.Parse(fmt.Sprintf("%s%s/%s", objectIRI, path, url.QueryEscape(id)))
	}
}

var getIDParam = func(req *http.Request) string {
	return getParam(req, idParam)
}

var getTypeParam = func(req *http.Request) string {
	return getParam(req, typeParam)
}

func getParam(req *http.Request, name string) string {
	id, ok := mux.Vars(req)[name]
	if ok {
		return id
	}

	values := req.URL.Query()[name]

	if len(values) == 0 {
		return ""
	}

	return values[0]
}
