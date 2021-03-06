package lively_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/orijtech/frontender/lively"

	"github.com/odeke-em/go-uuid"
)

func TestLiveliness(t *testing.T) {
	baseAddr := "http://192.168.1.68"
	peers := nPeers(64, baseAddr)

	// Arbitrarily pick the first as the primary
	primaryIndex := 0

	for i := 0; i < 20; i++ {
		blockedIndices := rand.Perm(len(peers) / 2)
		blockedMap := make(map[string]bool)
		for _, blockedIndex := range blockedIndices {
			blockedPeer := peers[blockedIndex]
			blockedMap[blockedPeer.ID] = true
		}

		primary := peers[primaryIndex]
		primary.Primary = true

		// Next make the rest the peers for the primary
		secondaries := append(peers[:primaryIndex], peers[primaryIndex+1:]...)
		for _, secondary := range secondaries {
			primary.AddPeer(secondary)
			secondary.AddPeer(primary)
			secondary.Primary = false
		}

		var updatedPeers []*lively.Peer
		for _, peer := range peers {
			peer.SetHTTPRoundTripper(&backend{id: peer.ID, blocked: blockedMap})
			updatedPeers = append(updatedPeers, peer)
		}
		doneChan := make(chan *peerDesc)
		waitCount := 0
		// Now asynchronously ping each one of them
		for i, peer := range peers {
			waitCount += 1
			go func(id int) {
				_, _, err := peer.Liveliness(nil)
				doneChan <- &peerDesc{i: id, err: err}
			}(i)
		}

		for i := 0; i < waitCount; i++ {
			desc := <-doneChan
			if err := desc.err; err != nil {
				t.Errorf("#%d: err: %v", i, err)
			}
		}
	}
}

type peerDesc struct {
	i   int
	err error
}

// backend is the test roundTripper that mimicks a
// backend that performs a request and responds with a result.
type backend struct {
	blocked map[string]bool

	id string
}

var blankPtrPing = new(lively.Ping)

func (b *backend) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.Method != "POST" {
		return makeResp(`expecting "POST" as a method`, http.StatusBadRequest, nil), nil
	}
	// Expecting a ping frame
	slurp, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return makeResp(err.Error(), http.StatusBadRequest, nil), nil
	}
	recv := new(lively.Ping)
	if err := json.Unmarshal(slurp, recv); err != nil {
		return makeResp(err.Error(), http.StatusBadRequest, nil), nil
	}
	if reflect.DeepEqual(recv, blankPtrPing) {
		return makeResp(`expecting a non-blank "ping"`, http.StatusBadRequest, nil), nil
	}
	if _, blocked := b.blocked[recv.PeerID]; blocked {
		return makeResp(`purposefully not responding`, http.StatusInternalServerError, nil), nil
	}

	// Otherwise now respond
	blob, err := json.Marshal(&lively.Ping{PeerID: b.id, Clock: time.Now().Unix()})
	if err != nil {
		return makeResp(err.Error(), http.StatusBadRequest, nil), nil
	}
	prc, pwc := io.Pipe()
	go func() {
		defer pwc.Close()
		pwc.Write(blob)
	}()
	return makeResp("200 OK", http.StatusOK, prc), nil
}

func nPeers(n int, baseAddr string) (peers []*lively.Peer) {
	for i := 0; i < n; i++ {
		peers = append(peers, &lively.Peer{
			ID:   uuid.NewRandom().String(),
			Addr: fmt.Sprintf("%s:%d", baseAddr, i+1000),
		})
	}
	return peers
}

func makeResp(status string, code int, body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: code,
		Status:     status,
		Body:       body,
		Header:     make(http.Header),
	}
}

type closeCheck struct {
	once       sync.Once
	rc         io.ReadCloser
	closedChan chan interface{}
}

var closedSentinel = "it has been closed"

func (c *closeCheck) Close() error {
	var err error = errAlreadyClosed
	c.once.Do(func() {
		err = c.rc.Close()
		c.closedChan <- closedSentinel
	})
	return err
}

func (c *closeCheck) Read(b []byte) (int, error) {
	return c.rc.Read(b)
}

func TestEnsurePingClosesBodyIfNonNil(t *testing.T) {
	baseAddr := "http://192.168.1.68"
	peers := nPeers(2, baseAddr)
	primary := peers[0]
	primary.Primary = true
	secondary := peers[1]
	primary.AddPeer(secondary)

	statuses := [...]int{
		0: http.StatusOK,
		1: http.StatusInternalServerError,
		2: http.StatusBadRequest,
		3: http.StatusTemporaryRedirect,
	}

	for i, statusCode := range statuses {
		ccheck := newCloseCheck()
		primary.SetHTTPRoundTripper(&closeRoundTripper{body: ccheck, statusCode: statusCode})
		_, _, err := primary.Liveliness(nil)
		if err != nil {
			t.Errorf("#%d: liveliness err=(%v)", i, err)
			continue
		}
		closedRecv := <-ccheck.closedChan
		if got, want := closedRecv, closedSentinel; got != want {
			t.Errorf("#%d: got=(%v) want=(%v)", i, got, want)
		}
	}
}

var errAlreadyClosed = errors.New("already closed")

func newCloseCheck() *closeCheck {
	prc, pwc := io.Pipe()
	go func() {
		_, _ = pwc.Write([]byte(`{}`))
		_ = pwc.Close()
	}()

	return &closeCheck{
		rc: prc,

		closedChan: make(chan interface{}, 1),
	}
}

type closeRoundTripper struct {
	body       *closeCheck
	statusCode int
}

func (cr *closeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Replace the body with a custom closer to
	// ensure we can see if the body gets closed!
	resp := makeResp(`Foo OK`, cr.statusCode, cr.body)
	return resp, nil
}
