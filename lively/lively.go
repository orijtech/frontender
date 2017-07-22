package lively

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/orijtech/otils"

	"github.com/odeke-em/semalim"
)

type Peer struct {
	Addr string `json:"addr"`
	ID   string `json:"id"`

	Primary bool `json:"primary"`

	Peers map[string]*Peer `json:"peers"`

	mu sync.RWMutex
	rt http.RoundTripper
}

type Ping struct {
	PeerID string `json:"id"`
	Clock  int64  `json:"clock"`
}

func (e *Peer) ping(other *Peer) (*Ping, error) {
	blob, err := json.Marshal(&Ping{PeerID: e.ID, Clock: time.Now().Unix()})
	if err != nil {
		return nil, err
	}

	addr := fmt.Sprintf("%s/ping", other.Addr)
	body := bytes.NewReader(blob)
	req, err := http.NewRequest("POST", addr, body)
	if err != nil {
		return nil, err
	}
	res, err := e.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if !otils.StatusOK(res.StatusCode) {
		return nil, errors.New(res.Status)
	}
	slurp, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	recv := new(Ping)
	if err := json.Unmarshal(slurp, recv); err != nil {
		return nil, err
	}
	return recv, nil
}

func (e *Peer) httpClient() *http.Client {
	e.mu.RLock()
	rt := e.rt
	e.mu.RUnlock()

	if rt == nil {
		rt = http.DefaultTransport
	}
	return &http.Client{Transport: rt}
}

func (e *Peer) Consesus() error {
	return nil
}

var errBlankPeerID = errors.New("peer has a blank ID")

func (p *Peer) AddPeer(other *Peer) error {
	otherID := strings.TrimSpace(other.ID)
	if otherID == "" {
		return errBlankPeerID
	}

	p.mu.RLock()
	if p.Peers == nil {
		p.Peers = make(map[string]*Peer)
	}
	p.Peers[otherID] = other
	p.mu.RUnlock()

	return nil
}

func (p *Peer) SetHTTPRoundTripper(rt http.RoundTripper) {
	p.mu.Lock()
	p.rt = rt
	p.mu.Unlock()
}

type Liveliness struct {
	PeerID string `json:"peer_id"`
	Ping   *Ping  `json:"ping"`
	Err    error  `json:"error"`
}

type LivelyRequest struct {
	ConcurrentPings int
}

func (p *Peer) Liveliness(llv *LivelyRequest) (livePeers, nonLivePeers []*Liveliness, err error) {
	p.mu.RLock()
	curPeers := make(map[string]*Peer, len(p.Peers))
	for id, curPeer := range p.Peers {
		curPeers[id] = curPeer
	}
	p.mu.RUnlock()

	jobsBench := make(chan semalim.Job)
	go func() {
		defer close(jobsBench)

		for _, curPeer := range curPeers {
			jobsBench <- &peerPing{id: curPeer.ID, peer: curPeer, self: p}
		}
	}()

	concurrentPings := 5
	if llv != nil && llv.ConcurrentPings > 0 {
		concurrentPings = llv.ConcurrentPings
	}
	resChan := semalim.Run(jobsBench, uint64(concurrentPings))
	for res := range resChan {
		pping := res.Value().(*Ping)
		peerID := res.Id().(string)
		err := res.Err()
		ptr := &nonLivePeers
		if err == nil && pping != nil {
			ptr = &livePeers
		}
		*ptr = append(*ptr, &Liveliness{Err: err, PeerID: peerID, Ping: pping})
	}

	return livePeers, nonLivePeers, nil
}

type peerPing struct {
	id   string
	peer *Peer
	self *Peer
}

var _ semalim.Job = (*peerPing)(nil)

func (pp *peerPing) Id() interface{} {
	return pp.id
}

func (pp *peerPing) Do() (interface{}, error) {
	return pp.self.ping(pp.peer)
}