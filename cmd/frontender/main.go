package main

import (
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"github.com/orijtech/frontender"
	"github.com/orijtech/namespace"
)

func main() {
	var http1 bool
	var csvBackendAddresses string
	var nonHTTPSAddr string
	var backendPingPeriodStr string
	var csvDomains string
	var noAutoWWW bool
	var nonHTTPSRedirectURL string
	var routeFile string

	flag.StringVar(&csvBackendAddresses, "csv-backends", "", "the comma separated addresses of the backend servers")
	flag.StringVar(&csvDomains, "domains", "", "the comma separated domains that the frontend will be representing")
	flag.BoolVar(&http1, "http1", false, "if true signals that the server should run as an http1 server locally")
	flag.StringVar(&nonHTTPSAddr, "non-https-addr", ":8877", "the non-https address")
	flag.StringVar(&nonHTTPSRedirectURL, "non-https-redirect", "", "the URL to which all non-HTTPS traffic will be redirected")
	flag.BoolVar(&noAutoWWW, "no-auto-www", false, "if set, explicits tells the frontend service NOT to make equivalent www CNAMEs of domains, if the www CNAMEs haven't yet been set")
	flag.StringVar(&backendPingPeriodStr, "backend-ping-period", "3m", `the period for which the frontend should ping the backend servers. Please enter this value with the form <DIGIT><UNIT> where <UNIT> could be  "ns", "us" (or "µs"), "ms", "s", "m", "h"`)
	flag.StringVar(&routeFile, "route-file", "", "the file containing the routing")
	flag.Parse()
	f, err := os.Open(routeFile)
	if err != nil && false {
		log.Fatalf("route-file: %v\n", err)
	}
	if f != nil {
		defer f.Close()
	}

	ns, err := namespace.ParseWithHeaderDelimiter(f, ",")
	if err != nil {
		log.Fatalf("namespace: %v", err)
	}

	var pingPeriod time.Duration
	if t, err := time.ParseDuration(backendPingPeriodStr); err == nil {
		pingPeriod = t
	}

	proxyAddresses := splitAndTrimAddresses(csvBackendAddresses)
	if len(ns) == 0 {
		for _, addr := range proxyAddresses {
			ns[namespace.GlobalNamespaceKey] = append(ns[namespace.GlobalNamespaceKey], addr)
		}
	}

	fReq := &frontender.Request{
		HTTP1:   http1,
		Domains: splitAndTrimAddresses(csvDomains),

		NoAutoWWW:           noAutoWWW,
		NonHTTPSAddr:        nonHTTPSAddr,
		NonHTTPSRedirectURL: nonHTTPSRedirectURL,

		BackendPingPeriod: pingPeriod,
		PrefixRouter:      (map[string][]string)(ns),
		ProxyAddresses:    proxyAddresses,
	}

	confirmation, err := frontender.Listen(fReq)
	if err != nil {
		log.Fatal(err)
	}
	defer confirmation.Close()

	if err := confirmation.Wait(); err != nil {
		log.Fatal(err)
	}
}

func splitAndTrimAddresses(csvOfAddresses string) []string {
	splits := strings.Split(csvOfAddresses, ",")
	var trimmed []string
	for _, split := range splits {
		trimmed = append(trimmed, strings.TrimSpace(split))
	}
	return trimmed
}
