package gethbridge

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"google.golang.org/protobuf/proto"

	"github.com/waku-org/go-waku/waku/v2/protocol"
	"github.com/waku-org/go-waku/waku/v2/protocol/store"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/status-im/status-go/connection"
	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/wakuv2"
	wakucommon "github.com/status-im/status-go/wakuv2/common"
)

type gethWakuV2Wrapper struct {
	waku *wakuv2.Waku
}

// NewGethWakuWrapper returns an object that wraps Geth's Waku in a types interface
func NewGethWakuV2Wrapper(w *wakuv2.Waku) types.Waku {
	if w == nil {
		panic("waku cannot be nil")
	}

	return &gethWakuV2Wrapper{
		waku: w,
	}
}

// GetGethWhisperFrom retrieves the underlying whisper Whisper struct from a wrapped Whisper interface
func GetGethWakuV2From(m types.Waku) *wakuv2.Waku {
	return m.(*gethWakuV2Wrapper).waku
}

func (w *gethWakuV2Wrapper) PublicWakuAPI() types.PublicWakuAPI {
	return NewGethPublicWakuV2APIWrapper(wakuv2.NewPublicWakuAPI(w.waku))
}

func (w *gethWakuV2Wrapper) Version() uint {
	return 2
}

func (w *gethWakuV2Wrapper) PeerCount() int {
	return w.waku.PeerCount()
}

// DEPRECATED: Not used in WakuV2
func (w *gethWakuV2Wrapper) MinPow() float64 {
	return 0
}

// MaxMessageSize returns the MaxMessageSize set
func (w *gethWakuV2Wrapper) MaxMessageSize() uint32 {
	return w.waku.MaxMessageSize()
}

// DEPRECATED: not used in WakuV2
func (w *gethWakuV2Wrapper) BloomFilter() []byte {
	return nil
}

// GetCurrentTime returns current time.
func (w *gethWakuV2Wrapper) GetCurrentTime() time.Time {
	return w.waku.CurrentTime()
}

func (w *gethWakuV2Wrapper) SubscribeEnvelopeEvents(eventsProxy chan<- types.EnvelopeEvent) types.Subscription {
	events := make(chan wakucommon.EnvelopeEvent, 100) // must be buffered to prevent blocking whisper
	go func() {
		for e := range events {
			eventsProxy <- *NewWakuV2EnvelopeEventWrapper(&e)
		}
	}()

	return NewGethSubscriptionWrapper(w.waku.SubscribeEnvelopeEvents(events))
}

func (w *gethWakuV2Wrapper) GetPrivateKey(id string) (*ecdsa.PrivateKey, error) {
	return w.waku.GetPrivateKey(id)
}

// AddKeyPair imports a asymmetric private key and returns a deterministic identifier.
func (w *gethWakuV2Wrapper) AddKeyPair(key *ecdsa.PrivateKey) (string, error) {
	return w.waku.AddKeyPair(key)
}

// DeleteKeyPair deletes the key with the specified ID if it exists.
func (w *gethWakuV2Wrapper) DeleteKeyPair(keyID string) bool {
	return w.waku.DeleteKeyPair(keyID)
}

func (w *gethWakuV2Wrapper) AddSymKeyDirect(key []byte) (string, error) {
	return w.waku.AddSymKeyDirect(key)
}

func (w *gethWakuV2Wrapper) AddSymKeyFromPassword(password string) (string, error) {
	return w.waku.AddSymKeyFromPassword(password)
}

func (w *gethWakuV2Wrapper) DeleteSymKey(id string) bool {
	return w.waku.DeleteSymKey(id)
}

func (w *gethWakuV2Wrapper) GetSymKey(id string) ([]byte, error) {
	return w.waku.GetSymKey(id)
}

func (w *gethWakuV2Wrapper) Subscribe(opts *types.SubscriptionOptions) (string, error) {
	var (
		err     error
		keyAsym *ecdsa.PrivateKey
		keySym  []byte
	)

	if opts.SymKeyID != "" {
		keySym, err = w.GetSymKey(opts.SymKeyID)
		if err != nil {
			return "", err
		}
	}
	if opts.PrivateKeyID != "" {
		keyAsym, err = w.GetPrivateKey(opts.PrivateKeyID)
		if err != nil {
			return "", err
		}
	}

	f, err := w.createFilterWrapper("", keyAsym, keySym, opts.PubsubTopic, opts.Topics)
	if err != nil {
		return "", err
	}

	id, err := w.waku.Subscribe(GetWakuV2FilterFrom(f))
	if err != nil {
		return "", err
	}

	f.(*wakuV2FilterWrapper).id = id
	return id, nil
}

func (w *gethWakuV2Wrapper) GetStats() types.StatsSummary {
	return w.waku.GetStats()
}

func (w *gethWakuV2Wrapper) GetFilter(id string) types.Filter {
	return NewWakuV2FilterWrapper(w.waku.GetFilter(id), id)
}

func (w *gethWakuV2Wrapper) Unsubscribe(ctx context.Context, id string) error {
	return w.waku.Unsubscribe(ctx, id)
}

func (w *gethWakuV2Wrapper) UnsubscribeMany(ids []string) error {
	return w.waku.UnsubscribeMany(ids)
}

func (w *gethWakuV2Wrapper) createFilterWrapper(id string, keyAsym *ecdsa.PrivateKey, keySym []byte, pubsubTopic string, topics [][]byte) (types.Filter, error) {
	return NewWakuV2FilterWrapper(&wakucommon.Filter{
		KeyAsym:       keyAsym,
		KeySym:        keySym,
		ContentTopics: wakucommon.NewTopicSetFromBytes(topics),
		PubsubTopic:   pubsubTopic,
		Messages:      wakucommon.NewMemoryMessageStore(),
	}, id), nil
}

// DEPRECATED: Not used in waku V2
func (w *gethWakuV2Wrapper) SendMessagesRequest(peerID []byte, r types.MessagesRequest) error {
	return errors.New("DEPRECATED")
}

func (w *gethWakuV2Wrapper) RequestStoreMessages(ctx context.Context, peerIDBytes []byte, r types.MessagesRequest, processEnvelopes bool) (types.StoreRequestCursor, int, error) {
	var options []store.RequestOption

	var peerID peer.ID
	err := peerID.Unmarshal(peerIDBytes)
	if err != nil {
		return nil, 0, err
	}

	options = []store.RequestOption{
		store.WithPaging(false, uint64(r.Limit)),
	}

	var cursor []byte
	if r.StoreCursor != nil {
		cursor = r.StoreCursor
	}

	contentTopics := []string{}
	for _, topic := range r.ContentTopics {
		contentTopics = append(contentTopics, wakucommon.BytesToTopic(topic).ContentTopic())
	}

	query := store.FilterCriteria{
		TimeStart:     proto.Int64(int64(r.From) * int64(time.Second)),
		TimeEnd:       proto.Int64(int64(r.To) * int64(time.Second)),
		ContentFilter: protocol.NewContentFilter(w.waku.GetPubsubTopic(r.PubsubTopic), contentTopics...),
	}

	pbCursor, envelopesCount, err := w.waku.Query(ctx, peerID, query, cursor, options, processEnvelopes)
	if err != nil {
		return nil, 0, err
	}

	if pbCursor != nil {
		return pbCursor, envelopesCount, nil
	}

	return nil, envelopesCount, nil
}

// DEPRECATED: Not used in waku V2
func (w *gethWakuV2Wrapper) RequestHistoricMessagesWithTimeout(peerID []byte, envelope types.Envelope, timeout time.Duration) error {
	return errors.New("DEPRECATED")
}

func (w *gethWakuV2Wrapper) StartDiscV5() error {
	return w.waku.StartDiscV5()
}

func (w *gethWakuV2Wrapper) StopDiscV5() error {
	return w.waku.StopDiscV5()
}

// Subscribe to a pubsub topic, passing an optional public key if the pubsub topic is protected
func (w *gethWakuV2Wrapper) SubscribeToPubsubTopic(topic string, optPublicKey *ecdsa.PublicKey) error {
	return w.waku.SubscribeToPubsubTopic(topic, optPublicKey)
}

func (w *gethWakuV2Wrapper) UnsubscribeFromPubsubTopic(topic string) error {
	return w.waku.UnsubscribeFromPubsubTopic(topic)
}

func (w *gethWakuV2Wrapper) RetrievePubsubTopicKey(topic string) (*ecdsa.PrivateKey, error) {
	return w.waku.RetrievePubsubTopicKey(topic)
}

func (w *gethWakuV2Wrapper) StorePubsubTopicKey(topic string, privKey *ecdsa.PrivateKey) error {
	return w.waku.StorePubsubTopicKey(topic, privKey)
}

func (w *gethWakuV2Wrapper) RemovePubsubTopicKey(topic string) error {
	return w.waku.RemovePubsubTopicKey(topic)
}

func (w *gethWakuV2Wrapper) AddStorePeer(address multiaddr.Multiaddr) (peer.ID, error) {
	return w.waku.AddStorePeer(address)
}

func (w *gethWakuV2Wrapper) AddRelayPeer(address multiaddr.Multiaddr) (peer.ID, error) {
	return w.waku.AddRelayPeer(address)
}

func (w *gethWakuV2Wrapper) Peers() types.PeerStats {
	return w.waku.Peers()
}

func (w *gethWakuV2Wrapper) DialPeer(address multiaddr.Multiaddr) error {
	return w.waku.DialPeer(address)
}

func (w *gethWakuV2Wrapper) DialPeerByID(peerID peer.ID) error {
	return w.waku.DialPeerByID(peerID)
}

func (w *gethWakuV2Wrapper) ListenAddresses() ([]multiaddr.Multiaddr, error) {
	return w.waku.ListenAddresses(), nil
}

func (w *gethWakuV2Wrapper) RelayPeersByTopic(topic string) (*types.PeerList, error) {
	return w.waku.RelayPeersByTopic(topic)
}

func (w *gethWakuV2Wrapper) ENR() (*enode.Node, error) {
	return w.waku.ENR()
}

func (w *gethWakuV2Wrapper) DropPeer(peerID peer.ID) error {
	return w.waku.DropPeer(peerID)
}

func (w *gethWakuV2Wrapper) ProcessingP2PMessages() bool {
	return w.waku.ProcessingP2PMessages()
}

func (w *gethWakuV2Wrapper) MarkP2PMessageAsProcessed(hash common.Hash) {
	w.waku.MarkP2PMessageAsProcessed(hash)
}

func (w *gethWakuV2Wrapper) SubscribeToConnStatusChanges() (*types.ConnStatusSubscription, error) {
	return w.waku.SubscribeToConnStatusChanges(), nil
}

func (w *gethWakuV2Wrapper) SetCriteriaForMissingMessageVerification(peerID peer.ID, pubsubTopic string, contentTopics []types.TopicType) error {
	var cTopics []string
	for _, ct := range contentTopics {
		cTopics = append(cTopics, wakucommon.TopicType(ct).ContentTopic())
	}
	pubsubTopic = w.waku.GetPubsubTopic(pubsubTopic)
	w.waku.SetTopicsToVerifyForMissingMessages(peerID, pubsubTopic, cTopics)

	// No err can be be generated by this function. The function returns an error
	// Just so there's compatibility with GethWakuWrapper from V1
	return nil
}

func (w *gethWakuV2Wrapper) ConnectionChanged(state connection.State) {
	w.waku.ConnectionChanged(state)
}

func (w *gethWakuV2Wrapper) ClearEnvelopesCache() {
	w.waku.ClearEnvelopesCache()
}

type wakuV2FilterWrapper struct {
	filter *wakucommon.Filter
	id     string
}

// NewWakuFilterWrapper returns an object that wraps Geth's Filter in a types interface
func NewWakuV2FilterWrapper(f *wakucommon.Filter, id string) types.Filter {
	if f.Messages == nil {
		panic("Messages should not be nil")
	}

	return &wakuV2FilterWrapper{
		filter: f,
		id:     id,
	}
}

// GetWakuFilterFrom retrieves the underlying whisper Filter struct from a wrapped Filter interface
func GetWakuV2FilterFrom(f types.Filter) *wakucommon.Filter {
	return f.(*wakuV2FilterWrapper).filter
}

// ID returns the filter ID
func (w *wakuV2FilterWrapper) ID() string {
	return w.id
}

func (w *gethWakuV2Wrapper) ConfirmMessageDelivered(hashes []common.Hash) {
	w.waku.ConfirmMessageDelivered(hashes)
}

func (w *gethWakuV2Wrapper) SetStorePeerID(peerID peer.ID) {
	w.waku.SetStorePeerID(peerID)
}

func (w *gethWakuV2Wrapper) PeerID() peer.ID {
	return w.waku.PeerID()
}
