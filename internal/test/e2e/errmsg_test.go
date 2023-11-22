//go:build e2e

// Copyright 2023 Percona LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pmm-dump/internal/test/util"
	"pmm-dump/pkg/grafana"
)

func TestErrMsgCheckCompatibilityVersion(t *testing.T) {
	var b util.Binary
	tests := []struct {
		name          string
		internalError bool
		emptyJSON     bool
		returnVersion bool
		version       string
		expectedErr   string
	}{
		{
			name:        "no output",
			expectedErr: `failed to get PMM version error="failed to unmarshal response: unexpected end of JSON input`,
		},
		{
			name:          "internal error",
			expectedErr:   `failed to get PMM version error="non-ok status: 500`,
			internalError: true,
		},
		{
			name:        "return empty json",
			expectedErr: `Could not find server versio`,
			emptyJSON:   true,
		},
		{
			name:          "return json with low version",
			expectedErr:   `Your PMM-server version 2.11.0 is lower, than minimum required: 2.12.0!`,
			returnVersion: true,
		},
		{
			name:          "success",
			expectedErr:   `Opening dump file`,
			returnVersion: true,
			version:       "2.12.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.version == "" {
				tt.version = "2.11.0"
			}
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					defer r.Body.Close() //nolint:errcheck
					switch r.URL.Path {
					case "/graph/login":
						http.SetCookie(w, &http.Cookie{
							Name:  grafana.AuthCookieName,
							Value: "some-cookie",
						})
						w.WriteHeader(http.StatusOK)
					case "/v1/version":
						switch {
						case tt.internalError:
							w.WriteHeader(http.StatusInternalServerError)
						case tt.emptyJSON:
							w.WriteHeader(http.StatusOK)
							fmt.Fprint(w, `{}`)
						case tt.returnVersion:
							w.WriteHeader(http.StatusOK)
							fmt.Fprint(w, `{"server":{"version":"`+tt.version+`"}}`)
						}
					}
				},
			))
			defer server.Close()

			_, stderr, err := b.Run(
				"import",
				"-d", "some-dumppath",
				"--pmm-url", server.URL,
				"--pmm-user", "some-user",
				"--pmm-pass", "some-password")
			if err != nil && err.Error() != "exit status 1" {
				t.Fatal(err)
			}
			if !strings.Contains(stderr, tt.expectedErr) {
				t.Fatal("expected to contain", tt.expectedErr, "got", stderr)
			}
		})
	}
}
