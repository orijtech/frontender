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

package frontender

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/crypto/acme/autocert"

	"github.com/orijtech/otils"
)

type Request struct {
	// HTTP1 signifies that this server should
	// not be ran as an HTTP/2<-->HTTPS server.
	// This variable is useful for testing purposes.
	HTTP1 bool `json:"http1"`

	Domains []string `json:"domains"`

	NoAutoWWW bool `json:"no_auto_www"`

	ProxyAddress string `json:"proxy_address"`

	NonHTTPSRedirectURL string `json:"non_https_redirect_url"`
	NonHTTPSAddr        string `json:"non_https_addr"`

	DomainsListener func(domains ...string) net.Listener

	Environ    []string `json:"environ"`
	TargetGOOS string   `json:"target_goos"`
}

var (
	errEmptyDomains  = errors.New("expecting at least one non-empty domain")
	errAlreadyClosed = errors.New("already closed")

	errEmptyProxyAddress = errors.New("expecting a non-empty proxy server address")
)

func (req *Request) Validate() error {
	if req == nil || strings.TrimSpace(req.ProxyAddress) == "" {
		return errEmptyProxyAddress
	}
	if req.needsDomains() && strings.TrimSpace(otils.FirstNonEmptyString(req.Domains...)) == "" {
		return errEmptyDomains
	}
	return nil
}

type Server struct {
	Domains []string `json:"domains"`

	ProxyAddress string `json:"proxy_address"`

	NonHTTPSRedirectURL string `json:"non_https_redirect_url"`
}

// Synthesizes domains removing duplicates
// and if NoAutoWWW if not set, will automatically make
// the corresponding www.domain domain.
func (req *Request) SynthesizeDomains() []string {
	var finalList []string
	uniqs := make(map[string]bool)
	for _, domain := range req.Domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}

		toAdd := []string{domain}
		if !req.NoAutoWWW && !strings.HasPrefix(domain, "www") {
			toAdd = append(toAdd, fmt.Sprintf("www.%s", domain))
		}

		for _, curDomain := range toAdd {
			if _, seen := uniqs[curDomain]; seen {
				continue
			}

			finalList = append(finalList, curDomain)
			uniqs[curDomain] = true
		}
	}

	return finalList
}

type proxy struct {
	proxyAddress string
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, p.proxyAddress, http.StatusPermanentRedirect)
}

var _ http.Handler = (*proxy)(nil)

func (req *Request) runNonHTTPSRedirector() error {
	if req.HTTP1 {
		return nil
	}

	redirectURL := strings.TrimSpace(req.NonHTTPSRedirectURL)
	if redirectURL == "" {
		return nil
	}
	nonHTTPSAddr := strings.TrimSpace(req.NonHTTPSAddr)
	if nonHTTPSAddr == "" {
		nonHTTPSAddr = ":80"
	}

	return http.ListenAndServe(nonHTTPSAddr, otils.RedirectAllTrafficTo(redirectURL))
}

type ListenConfirmation struct {
	closeFn  func() error
	errsChan <-chan error
}

func (lc *ListenConfirmation) Close() error {
	return lc.closeFn()
}

func (lc *ListenConfirmation) Wait() error {
	return <-lc.errsChan
}

func (req *Request) needsDomains() bool {
	return req.HTTP1 == false
}

func Listen(req *Request) (*ListenConfirmation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	proxyURL, err := url.Parse(req.ProxyAddress)
	if err != nil {
		return nil, err
	}

	madeDomains := req.SynthesizeDomains()
	if req.needsDomains() && len(madeDomains) == 0 {
		return nil, errEmptyDomains
	}

	domainsListener := req.DomainsListener
	if domainsListener == nil {
		if !req.HTTP1 {
			domainsListener = autocert.NewListener
		} else {
			listener, err := net.Listen("tcp", ":80")
			if err != nil {
				return nil, err
			}
			domainsListener = func(domains ...string) net.Listener { return listener }
		}
	}
	listener := domainsListener(madeDomains...)

	return req.runAndCreateListener(listener, proxyURL)
}

func (req *Request) runAndCreateListener(listener net.Listener, proxyURL *url.URL) (*ListenConfirmation, error) {
	var closeOnce sync.Once
	errsChan := make(chan error)
	closeFn := func() error {
		err := errAlreadyClosed
		closeOnce.Do(func() {
			err = listener.Close()
		})
		return err
	}

	lc := &ListenConfirmation{closeFn: closeFn, errsChan: errsChan}

	// Run the nonHTTPS redirector.
	go req.runNonHTTPSRedirector()

	// Now run the domain listener
	go func() {
		defer close(errsChan)

		proxy := httputil.NewSingleHostReverseProxy(proxyURL)
		errsChan <- http.Serve(listener, proxy)
	}()

	return lc, nil
}
