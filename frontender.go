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
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme/autocert"

	"github.com/orijtech/frontender/lively"
	"github.com/orijtech/otils"

	"github.com/odeke-em/go-uuid"
)

type Request struct {
	// HTTP1 signifies that this server should
	// not be ran as an HTTP/2<-->HTTPS server.
	// This variable is useful for testing purposes.
	HTTP1 bool `json:"http1"`

	Domains []string `json:"domains"`

	NoAutoWWW bool `json:"no_auto_www"`

	ProxyAddresses []string `json:"proxy_addresses"`

	NonHTTPSRedirectURL string `json:"non_https_redirect_url"`
	NonHTTPSAddr        string `json:"non_https_addr"`

	DomainsListener func(domains ...string) net.Listener

	Environ    []string `json:"environ"`
	TargetGOOS string   `json:"target_goos"`

	CertKeyFiler func() (string, string)

	// BackendPingPeriod if set, defines the period
	// between which the frontend service will check
	// for the liveliness of the backends.
	BackendPingPeriod time.Duration

	// PrefixRouter if set helps route traffic depending on
	// the route prefix e.g
	// {
	//    "/bar": ["http://localhost:7997", "http://localhost:8888"],
	//    "/foo": ["http://localhost:8999", "http://localhost:8877"]
	// }
	// if it gets traffic with a URL prefix "/foo" will distribute traffic
	// between "http://localhost:8999" and "http://localhost:8877".
	PrefixRouter map[string][]string `json:"routing"`
}

var (
	errEmptyDomains  = errors.New("expecting at least one non-empty domain")
	errAlreadyClosed = errors.New("already closed")

	errEmptyProxyAddress = errors.New("expecting a non-empty proxy server address")
)

func (req *Request) hasAtLeastOneProxy() bool {
	if req == nil {
		return false
	}
	if len(req.PrefixRouter) == 0 {
		return otils.FirstNonEmptyString(req.ProxyAddresses...) != ""
	}
	for _, proxyAddresses := range req.PrefixRouter {
		if otils.FirstNonEmptyString(proxyAddresses...) != "" {
			return true
		}
	}
	return false
}

func (req *Request) Validate() error {
	if !req.hasAtLeastOneProxy() {
		return errEmptyProxyAddress
	}
	if req.needsDomains() && strings.TrimSpace(otils.FirstNonEmptyString(req.Domains...)) == "" {
		return errEmptyDomains
	}
	return nil
}

type Server struct {
	Domains []string `json:"domains"`

	ProxyAddresses []string `json:"proxy_addresses"`

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

	if req.CertKeyFiler != nil {
		cert, keyfile := req.CertKeyFiler()
		return http.ListenAndServeTLS(nonHTTPSAddr, cert, keyfile, nil)
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

// The goal is to be able to pass in proxy servers, keep a
// persistent connection to each one of them and use that
// as the weight to figure out which one to send traffic to.
func Listen(req *Request) (*ListenConfirmation, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	// proxyURL, err := url.Parse(req.ProxyAddress)
	// if err != nil {
	// 	return nil, err
	// }

	madeDomains := req.SynthesizeDomains()
	if req.needsDomains() && len(madeDomains) == 0 {
		return nil, errEmptyDomains
	}

	domainsListener := req.DomainsListener
	if domainsListener == nil {
		if !req.HTTP1 {
			domainsListener = autocert.NewListener
		} else {
			listener, err := net.Listen("tcp", req.NonHTTPSAddr)
			if err != nil {
				return nil, err
			}
			domainsListener = func(domains ...string) net.Listener { return listener }
		}
	}
	listener := domainsListener(madeDomains...)

	return req.runAndCreateListener(listener)
}

type livelyProxy struct {
	mu sync.Mutex

	next map[string]int

	cycleFreq time.Duration

	primariesMap   map[string]*lively.Peer
	secondariesMap map[string]map[string]*lively.Peer

	longestPrefixFirst []string

	liveAddresses map[string][]string
}

const defaultCycleFrequence = time.Minute * 3

type cycleFeedback struct {
	cycleNumber uint64
	err         error

	livePeers, nonLivePeers []*lively.Liveliness
}

func (lp *livelyProxy) run() map[string]chan *cycleFeedback {
	lp.mu.Lock()
	freq := lp.cycleFreq
	lp.mu.Unlock()

	if freq <= 0 {
		freq = defaultCycleFrequence
	}

	feedbackChanMap := make(map[string]chan *cycleFeedback)
	for route, primary := range lp.primariesMap {
		feedbackChan := make(chan *cycleFeedback)
		go func(route string, primary *lively.Peer, feedbackChan chan *cycleFeedback) {
			defer close(feedbackChan)
			cycleNumber := uint64(0)

			for {
				cycleNumber += 1
				livePeers, nonLivePeers, err := lp.cycle(route, primary)
				feedbackChan <- &cycleFeedback{
					err:          err,
					cycleNumber:  cycleNumber,
					livePeers:    livePeers,
					nonLivePeers: nonLivePeers,
				}
				<-time.After(freq)
			}
		}(route, primary, feedbackChan)
	}

	return feedbackChanMap
}

func (lp *livelyProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Firstly we need to find a primary match
	var matchedRoute string
	// We need to match by longest prefix first
	// so that cases like
	// * "/"
	// * "/foo"
	// * "/fo"
	// given * "/foo"
	// will always match "/foo" instead of "/" or "/fo"
	// however in the absence of "/foo", "/fo" will match before "/"
	longestPrefixFirst := lp.longestPrefixFirst
	for _, routePrefix := range longestPrefixFirst {
		if strings.HasPrefix(r.URL.Path, routePrefix) {
			matchedRoute = routePrefix
			break
		}
	}

	proxyAddr := lp.roundRobinedAddress(matchedRoute)
	// Now proxy the traffic to that request
	parsedURL, err := url.Parse(proxyAddr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	r.URL.Path = strings.TrimPrefix(r.URL.Path, matchedRoute)
	if !strings.HasPrefix(r.URL.Path, "/") {
		r.URL.Path = "/" + r.URL.Path
	}
	rproxy := httputil.NewSingleHostReverseProxy(parsedURL)
	rproxy.ServeHTTP(w, r)
}

func (lp *livelyProxy) roundRobinedAddress(route string) string {
	lp.mu.Lock()
	defer lp.mu.Unlock()

	liveAddresses := lp.liveAddresses[route]
	if len(liveAddresses) == 0 {
		return ""
	}
	if lp.next[route] >= len(liveAddresses) {
		lp.next[route] = 0
	}
	addr := liveAddresses[lp.next[route]]
	// Now increment it
	lp.next[route] += 1

	return addr
}

func (lp *livelyProxy) cycle(route string, primary *lively.Peer) (livePeers, nonLivePeers []*lively.Liveliness, err error) {
	livePeers, nonLivePeers, err = primary.Liveliness(&lively.LivelyRequest{})

	lp.mu.Lock()
	defer lp.mu.Unlock()

	var liveAddresses []string
	for _, peer := range livePeers {
		liveAddresses = append(liveAddresses, peer.Addr)
	}

	// Now reset the next index.
	lp.next[route] = 0

	// Shuffle the liveAddresses.
	perm := rand.Perm(len(liveAddresses))
	var shuffledAddresses []string
	for _, i := range perm {
		shuffledAddresses = append(shuffledAddresses, liveAddresses[i])
	}
	lp.liveAddresses[route] = shuffledAddresses

	return livePeers, nonLivePeers, err
}

func makeLivelyProxy(cycleFreq time.Duration, pr map[string][]string) *livelyProxy {
	secondariesMap := make(map[string]map[string]*lively.Peer)
	primariesMap := make(map[string]*lively.Peer)
	for prefix, addresses := range pr {
		primary := &lively.Peer{
			ID:      uuid.NewRandom().String(),
			Primary: true,
		}

		peersMap := make(map[string]*lively.Peer)
		for _, addr := range addresses {
			secondary := &lively.Peer{
				Addr: addr,
				ID:   uuid.NewRandom().String(),
			}
			_ = primary.AddPeer(secondary)
			peersMap[secondary.ID] = secondary
		}
		secondariesMap[prefix] = peersMap
		primariesMap[prefix] = primary
	}

	routePrefixes := make([]string, 0, len(pr))
	for routePrefix := range pr {
		routePrefixes = append(routePrefixes, routePrefix)
	}

	sort.Slice(routePrefixes, func(i, j int) bool {
		// Sort in reverse by length
		si, sj := routePrefixes[i], routePrefixes[j]
		return len(si) >= len(sj)
	})
	return &livelyProxy{
		longestPrefixFirst: routePrefixes,
		primariesMap:       primariesMap,
		secondariesMap:     secondariesMap,
		cycleFreq:          cycleFreq,

		next:          make(map[string]int),
		liveAddresses: make(map[string][]string),
	}
}

func (req *Request) runAndCreateListener(listener net.Listener) (*ListenConfirmation, error) {
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

		// Per cycle of liveliness, figure out what is lively
		// what isn't
		lproxy := makeLivelyProxy(req.BackendPingPeriod, req.PrefixRouter)
		go func() {
			feedbackChanMap := lproxy.run()
			for route, feedbackChan := range feedbackChanMap {
				go func(route string, feedbackChan chan *cycleFeedback) {
					for feedback := range feedbackChan {
						if err := feedback.err; err != nil {
							errsChan <- err
						}
					}
				}(route, feedbackChan)
			}
		}()
		errsChan <- http.Serve(listener, lproxy)
	}()

	return lc, nil
}
