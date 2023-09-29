/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package httpserver

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/trustbloc/sidetree-svc-go/pkg/restapi/common"
)

const (
	url       = "localhost:8080"
	clientURL = "http://" + url

	samplePath = "/sample"
)

func TestServer_Start(t *testing.T) {
	s := New(url, WithHandlers(&mockUpdateHandler{}, &mockResolveHandler{}))
	require.NoError(t, s.Start())
	require.Error(t, s.Start())

	// Wait for the service to start
	time.Sleep(time.Second)

	t.Run("success - sample operation ", func(t *testing.T) {
		resp, err := httpPut(t, clientURL+samplePath, []byte(""))
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("success - sample resolution", func(t *testing.T) {
		resp, err := httpGet(t, clientURL+samplePath+"/id")
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("Stop", func(t *testing.T) {
		require.NoError(t, s.Stop(context.Background()))
		require.Error(t, s.Stop(context.Background()))
	})
}

// httpPut sends a regular POST request to the sidetree-node
// - If post request has operation "create" then return sidetree document else no response.
func httpPut(t *testing.T, url string, req []byte) ([]byte, error) {
	t.Helper()

	client := &http.Client{}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(req))
	require.NoError(t, err)

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := invokeWithRetry(
		func() (response *http.Response, e error) {
			return client.Do(httpReq)
		},
	)
	require.NoError(t, err)

	return handleHTTPResp(resp)
}

// httpGet send a regular GET request to the sidetree-node and expects 'side tree document' argument as a response.
func httpGet(t *testing.T, url string) ([]byte, error) {
	t.Helper()

	client := &http.Client{}

	httpReq, err := http.NewRequest(http.MethodGet, url, http.NoBody)
	require.NoError(t, err)

	resp, err := invokeWithRetry(
		func() (response *http.Response, e error) {
			return client.Do(httpReq)
		},
	)
	require.NoError(t, err)

	return handleHTTPResp(resp)
}

func handleHTTPResp(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body failed: %w", err)
	}

	if status := resp.StatusCode; status != http.StatusOK {
		return nil, errors.New(string(body))
	}

	return body, nil
}

func invokeWithRetry(invoke func() (*http.Response, error)) (*http.Response, error) {
	remainingAttempts := 20

	for {
		resp, err := invoke()
		if err == nil {
			return resp, nil
		}

		remainingAttempts--
		if remainingAttempts == 0 {
			return nil, err
		}

		time.Sleep(100 * time.Millisecond)
	}
}

type mockUpdateHandler struct{}

// Path returns the context path.
func (h *mockUpdateHandler) Path() string {
	return samplePath
}

// Method returns the HTTP method.
func (h *mockUpdateHandler) Method() string {
	return http.MethodPost
}

// Handler returns the handler.
func (h *mockUpdateHandler) Handler() common.HTTPRequestHandler {
	return func(writer http.ResponseWriter, request *http.Request) {
	}
}

type mockResolveHandler struct{}

// Path returns the context path.
func (h *mockResolveHandler) Path() string {
	return samplePath + "/{id}"
}

// Method returns the HTTP method.
func (h *mockResolveHandler) Method() string {
	return http.MethodGet
}

// Handler returns the handler.
func (h *mockResolveHandler) Handler() common.HTTPRequestHandler {
	return func(writer http.ResponseWriter, request *http.Request) {
	}
}
