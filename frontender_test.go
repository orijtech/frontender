// Copyright 2017 orijtech. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package frontender_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/orijtech/frontender"
)

func TestListen(t *testing.T) {
	t.Skipf("Tests not fully backed")

	theLog := new(bytes.Buffer)

	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(theLog, "Now here from: %q\n", req.URL)
	}))
	defer ts.Close()

	nonHTTPSBackend := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(theLog, "first redirected from %q\n", req.URL)
		http.Redirect(rw, req, ts.URL, http.StatusPermanentRedirect)
	}))
	defer nonHTTPSBackend.Close()

	domainsListener := func(domains ...string) net.Listener {
		return ts.Listener
	}

	lc, err := frontender.Listen(&frontender.Request{
		Domains: []string{
			"git.orijtech.com",
			"repo.orijtech.com",
		},

		DomainsListener:     domainsListener,
		NonHTTPSRedirectURL: nonHTTPSBackend.URL,
		ProxyAddress:        ts.URL,
	})
	if err != nil {
		t.Fatalf("listening err: %v", err)
	}
	defer lc.Close()

	tests := [...]struct {
		url     string
		wantLog string
		wantErr bool
	}{
		0: {
			url:     "http://git.orijtech.com",
			wantLog: fmt.Sprintf(`first redirected from "http://git.orijtech.com"\nNow at %q`, ts.URL),
		},
		1: {
			url:     "https://repo.orijtech.com",
			wantLog: fmt.Sprintf("Now at %q\n", ts.URL),
		},
	}

	for i, tt := range tests {
		res, err := ts.Client().Get(tt.url)
		if tt.wantErr {
			if err == nil {
				t.Errorf("#%d expected non-nil error", i)
			}
			continue
		}

		if err != nil {
			t.Errorf("#%d: err: %v", i, err)
			continue
		}

		if res == nil {
			t.Errorf("#%d: expected non-nil response", i)
			continue
		}
		gotBytes, _ := ioutil.ReadAll(theLog)
		theLog.Reset()

		want, got := tt.wantLog, string(gotBytes)
		if want != got {
			t.Errorf("#%d:\n\tgot:  %q\n\twant: %q", i, got, want)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	tests := [...]struct {
		req     *frontender.Request
		wantErr bool
	}{
		0: {req: nil, wantErr: true},
		1: {req: &frontender.Request{}, wantErr: true},
		2: {
			req: &frontender.Request{
				Domains:      []string{"golang.org/"},
				ProxyAddress: "http://192.168.1.104/",
			},
		},
		3: {
			req: &frontender.Request{
				Domains: []string{"orijtech.com/"},
			},
			// No proxy address specified.
			wantErr: true,
		},
	}

	for i, tt := range tests {
		err := tt.req.Validate()
		gotErr := err != nil
		wantErr := tt.wantErr
		if gotErr != wantErr {
			t.Errorf("#%d: gotErr=%v wantErr=%v; err=%v", i, gotErr, wantErr, err)
		}
	}
}

func TestRequestMakeDomains(t *testing.T) {
	tests := [...]struct {
		req  *frontender.Request
		want []string
	}{
		0: {
			req: &frontender.Request{
				Domains: []string{"foo", "www.foo", "", "foo", "FOO", "www.flux"},
			},
			want: []string{
				"foo",
				"www.foo",
				"FOO",
				"www.FOO",
				"www.flux",
			},
		},

		1: {
			req: &frontender.Request{
				Domains:   []string{"foo", "", "foo", "FOO", "www.flux"},
				NoAutoWWW: true,
			},
			want: []string{
				"foo",
				"FOO",
				"www.flux",
			},
		},
	}

	for i, tt := range tests {
		got := tt.req.SynthesizeDomains()
		want := tt.want

		// Sort them both for proper comparison
		sort.Strings(got)
		sort.Strings(want)

		if !reflect.DeepEqual(got, want) {
			t.Errorf("#%d:\ngot:  %#v\nwant: %#v\n", i, got, want)
		}
	}
}
