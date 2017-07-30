package main

import (
	"flag"
	"log"
	"strings"
	"time"

	"github.com/orijtech/frontender"
)

func main() {
	var http1 bool
	var csvBackendAddresses string
	var nonHTTPSAddr string
	var backendPingPeriodStr string
	var csvDomains string
	var noAutoWWW bool
	var nonHTTPSRedirectURL string

	flag.StringVar(&csvBackendAddresses, "csv-backends", "", "the comma separated addresses of the backend servers")
	flag.StringVar(&csvDomains, "domains", "", "the comma separated domains that the frontend will be representing")
	flag.BoolVar(&http1, "http1", false, "if true signals that the server should run as an http1 server locally")
	flag.StringVar(&nonHTTPSAddr, "non-https-addr", ":8877", "the non-https address")
	flag.StringVar(&nonHTTPSRedirectURL, "non-https-redirect", "", "the URL to which all non-HTTPS traffic will be redirected")
	flag.BoolVar(&noAutoWWW, "no-auto-www", false, "if set, explicits tells the frontend service NOT to make equivalent www CNAMEs of domains, if the www CNAMEs haven't yet been set")
	flag.StringVar(&backendPingPeriodStr, "backend-ping-period", "3m", `the period for which the frontend should ping the backend servers. Please enter this value with the form <DIGIT><UNIT> where <UNIT> could be  "ns", "us" (or "µs"), "ms", "s", "m", "h"`)
	flag.Parse()

	var pingPeriod time.Duration
	if t, err := time.ParseDuration(backendPingPeriodStr); err == nil {
		pingPeriod = t
	}

	fReq := &frontender.Request{
		HTTP1:   http1,
		Domains: splitAndTrimAddresses(csvDomains),

		NoAutoWWW:           noAutoWWW,
		NonHTTPSAddr:        nonHTTPSAddr,
		NonHTTPSRedirectURL: nonHTTPSRedirectURL,

		ProxyAddresses:    splitAndTrimAddresses(csvBackendAddresses),
		BackendPingPeriod: pingPeriod,
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
