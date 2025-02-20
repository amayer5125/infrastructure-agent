// Copyright 2021 NewServer Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package httpapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"github.com/newrelic/infrastructure-agent/pkg/integrations/execution/v4/fixtures"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/newrelic/infrastructure-agent/internal/agent/id"
	"github.com/newrelic/infrastructure-agent/internal/agent/status"
	"github.com/newrelic/infrastructure-agent/pkg/entity"
	network_helpers "github.com/newrelic/infrastructure-agent/pkg/helpers/network"
	"github.com/newrelic/infrastructure-agent/pkg/integrations/execution/v4/testhelp/testemit"
	"github.com/newrelic/infrastructure-agent/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServe_Status(t *testing.T) {
	t.Parallel()

	// Given a running HTTP endpoint
	port, err := network_helpers.TCPPort()
	require.NoError(t, err)

	serverOk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer serverOk.Close()

	// And a status reporter monitoring it
	endpoints := []string{serverOk.URL}
	l := log.WithComponent(t.Name())
	timeout := 100 * time.Millisecond
	transport := &http.Transport{}
	emptyIDProvide := func() entity.Identity {
		return entity.EmptyIdentity
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := status.NewReporter(ctx, l, endpoints, timeout, transport, emptyIDProvide, "user-agent", "agent-key")

	// When agent status API server is ready
	em := &testemit.RecordEmitter{}
	s, err := NewServer(r, em)
	require.NoError(t, err)
	s.Status.Enable("localhost", port)
	defer cancel()

	go s.Serve(ctx)

	s.WaitUntilReady()

	// And a request to the status API is sent
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d%s", port, statusAPIPath), nil)
	require.NoError(t, err)
	client := http.Client{}

	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	// Then response contains a report for the monitored endpoint
	require.Equal(t, http.StatusOK, res.StatusCode)

	var gotReport status.Report
	json.NewDecoder(res.Body).Decode(&gotReport)
	require.Len(t, gotReport.Checks.Endpoints, 1)
	e := gotReport.Checks.Endpoints[0]
	assert.Empty(t, e.Error)
	assert.True(t, e.Reachable)
	assert.Equal(t, serverOk.URL, e.URL)
}

func TestServe_OnlyErrors(t *testing.T) {
	t.Parallel()

	// Given a running HTTP endpoint and an errored one (which times out)
	port, err := network_helpers.TCPPort()
	require.NoError(t, err)

	serverOk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer serverOk.Close()
	serverTimeout := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second)
	}))
	defer serverTimeout.Close()

	// And a status reporter monitoring these endpoints
	endpoints := []string{serverOk.URL, serverTimeout.URL}
	l := log.WithComponent(t.Name())
	timeout := 100 * time.Millisecond
	transport := &http.Transport{}
	emptyIDProvide := func() entity.Identity {
		return entity.EmptyIdentity
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := status.NewReporter(ctx, l, endpoints, timeout, transport, emptyIDProvide, "user-agent", "agent-key")

	// When agent status API server is ready
	em := &testemit.RecordEmitter{}

	s, err := NewServer(r, em)
	require.NoError(t, err)
	s.Status.Enable("localhost", port)
	defer cancel()

	go s.Serve(ctx)

	s.WaitUntilReady()

	// And a request to the status API is sent
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d%s", port, statusOnlyErrorsAPIPath), nil)
	require.NoError(t, err)
	client := http.Client{}

	res, err := client.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()

	// Then response contains a report for the monitored endpoint
	require.Equal(t, http.StatusOK, res.StatusCode)

	var gotReport status.Report
	json.NewDecoder(res.Body).Decode(&gotReport)
	require.Len(t, gotReport.Checks.Endpoints, 1, "only errored endpoint should be reported")
	e := gotReport.Checks.Endpoints[0]
	assert.NotEmpty(t, e.Error)
	assert.False(t, e.Reachable)
	assert.Equal(t, serverTimeout.URL, e.URL)
}

func TestServe_Entity(t *testing.T) {
	t.Parallel()

	l := log.WithComponent(t.Name())
	timeout := 100 * time.Millisecond
	transport := &http.Transport{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	emptyIDProvide := func() entity.Identity {
		return entity.Identity{}
	}
	fooIDProvide := func() entity.Identity {
		return entity.Identity{
			GUID: "foo",
		}
	}

	tests := []struct {
		name         string
		idProvide    id.Provide
		expectedCode int
	}{
		{"empty id", emptyIDProvide, http.StatusNoContent},
		{"pinned id", fooIDProvide, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given a running HTTP endpoint and an errored one (which times out)
			port, err := network_helpers.TCPPort()
			require.NoError(t, err)

			r := status.NewReporter(ctx, l, []string{}, timeout, transport, tt.idProvide, "user-agent", "agent-key")
			// When agent status API server is ready
			em := &testemit.RecordEmitter{}
			s, err := NewServer(r, em)
			require.NoError(t, err)
			s.Status.Enable("localhost", port)
			defer cancel()

			go s.Serve(ctx)

			s.WaitUntilReady()

			// And a request to the status API is sent
			req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%d%s", port, statusEntityAPIPath), nil)
			require.NoError(t, err)
			client := http.Client{}

			res, err := client.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()

			// Then response contains an entity report
			require.Equal(t, tt.expectedCode, res.StatusCode)

			if tt.expectedCode != http.StatusNoContent {
				var gotReport status.ReportEntity
				json.NewDecoder(res.Body).Decode(&gotReport)
				assert.Equal(t, tt.idProvide().GUID.String(), gotReport.GUID)
			}
		})
	}
}

func TestServe_IngestData(t *testing.T) {
	t.Parallel()

	port, err := network_helpers.TCPPort()
	require.NoError(t, err)

	em := &testemit.RecordEmitter{}
	s, err := NewServer(&noopReporter{}, em)
	require.NoError(t, err)
	s.Ingest.Enable("localhost", port)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Serve(ctx)

	payloadWritten := make(chan struct{})
	go func() {
		s.WaitUntilReady()
		client := http.Client{}
		postReq, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%d%s", port, ingestAPIPath), bytes.NewReader(fixtures.FooBytes))
		resp, err := client.Do(postReq)
		require.NoError(t, err)
		require.Equal(t, 20, resp.StatusCode/10, "status code: %v", resp.StatusCode)
		close(payloadWritten)
	}()

	select {
	case <-time.NewTimer(1000 * time.Millisecond).C:
		t.Error("timeout waiting for HTTP request to be submitted")
	case <-payloadWritten:
	}

	t.Log("receiving from integration...\n")
	d, err := em.ReceiveFrom(IntegrationName)
	require.NoError(t, err)
	assert.Equal(t, "unique foo", d.DataSet.PluginDataSet.Entity.Name)
}

func TestServe_IngestData_mTLS(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		validateClient bool
		sendCert       bool
		shouldFail     bool
	}{
		{
			name:           "without_client_validation",
			validateClient: false,
		},
		{
			name:           "rejects_unauthenticated_client",
			validateClient: true,
			shouldFail:     true,
		},
		{
			name:           "accepts_valid_client",
			validateClient: true,
			sendCert:       true,
		},
	}

	caCertFile, err := ioutil.ReadFile("testdata/rootCA.pem")
	if err != nil {
		t.Fatalf("internal error: cannot load testdata CA: %v", err)
	}

	certPool := x509.NewCertPool()
	certPool.AppendCertsFromPEM(caCertFile)

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			port, err := network_helpers.TCPPort()
			require.NoError(t, err)

			em := &testemit.RecordEmitter{}
			s, err := NewServer(&noopReporter{}, em)
			require.NoError(t, err)
			s.Ingest.Enable("localhost", port)
			s.Ingest.TLS("testdata/localhost.pem", "testdata/localhost-key.pem")
			if testCase.validateClient {
				s.Ingest.VerifyTLSClient("testdata/rootCA.pem")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go s.Serve(ctx)

			payloadWritten := make(chan struct{})
			go func() {
				s.WaitUntilReady()
				if testCase.sendCert {
					// WaitUntilReady() is a no-op when mTLS is enabled, which causes the test to race sometimes.
					// Sleeping one second is a dirty workaround to wait for the server to be ready.
					time.Sleep(1 * time.Second)
				}

				client := http.Client{}
				transport := &http.Transport{
					TLSClientConfig: &tls.Config{
						RootCAs: certPool,
					},
				}

				if testCase.sendCert {
					cert, err := tls.LoadX509KeyPair("testdata/client-client.pem", "testdata/client-client-key.pem")
					if err != nil {
						// We cannot t.Fatal if we're not the main goroutine of the test.
						t.Logf("internal error: loading testdata certs: %v", err)
						t.Fail()
						return
					}

					transport.TLSClientConfig.Certificates = []tls.Certificate{cert}
				}

				client.Transport = transport

				postReq, err := http.NewRequest("POST", fmt.Sprintf("https://localhost:%d%s", port, ingestAPIPath), bytes.NewReader(fixtures.FooBytes))
				resp, err := client.Do(postReq)
				if testCase.shouldFail {
					// If we are expecting this request to fail, we won't check for errors.
					return
				}
				require.NoError(t, err)
				require.Equal(t, 20, resp.StatusCode/10, "status code: %v", resp.StatusCode)
				close(payloadWritten)
			}()

			select {
			case <-time.NewTimer(2 * time.Second).C:
				if testCase.shouldFail {
					// Payload not received and test should fail, return.
					return
				}

				t.Fatal("timeout waiting for HTTP request to be submitted")
			case <-payloadWritten:
			}

			t.Log("receiving from integration...\n")
			d, err := em.ReceiveFrom(IntegrationName)
			require.NoError(t, err)
			assert.Equal(t, "unique foo", d.DataSet.PluginDataSet.Entity.Name)
		})
	}
}

type noopReporter struct{}

func (r *noopReporter) Report() (status.Report, error) {
	return status.Report{}, nil
}

func (r *noopReporter) ReportErrors() (status.Report, error) {
	return status.Report{}, nil
}

func (r *noopReporter) ReportEntity() (re status.ReportEntity, err error) {
	return status.ReportEntity{}, nil
}
