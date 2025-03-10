package protocol

import (
	"context"
	"crypto/ecdsa"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/exp/slices"
	"golang.org/x/time/rate"

	"github.com/golang/protobuf/proto"
	"github.com/google/uuid"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"go.uber.org/zap"

	utils "github.com/status-im/status-go/common"

	"github.com/status-im/status-go/account"
	multiaccountscommon "github.com/status-im/status-go/multiaccounts/common"

	"github.com/status-im/status-go/eth-node/crypto"
	"github.com/status-im/status-go/eth-node/types"
	"github.com/status-im/status-go/images"
	"github.com/status-im/status-go/multiaccounts/accounts"
	"github.com/status-im/status-go/protocol/common"
	"github.com/status-im/status-go/protocol/common/shard"
	"github.com/status-im/status-go/protocol/communities"
	"github.com/status-im/status-go/protocol/communities/token"
	"github.com/status-im/status-go/protocol/discord"
	"github.com/status-im/status-go/protocol/encryption"
	"github.com/status-im/status-go/protocol/protobuf"
	"github.com/status-im/status-go/protocol/requests"
	"github.com/status-im/status-go/protocol/transport"
	v1protocol "github.com/status-im/status-go/protocol/v1"
	localnotifications "github.com/status-im/status-go/services/local-notifications"
	"github.com/status-im/status-go/services/wallet/bigint"
	"github.com/status-im/status-go/signal"
)

// 7 days interval
var messageArchiveInterval = 7 * 24 * time.Hour

// 1 day interval
var updateActiveMembersInterval = 24 * time.Hour

// 1 day interval
var grantUpdateInterval = 24 * time.Hour

// 4 hours interval
var grantInvokesProfileDispatchInterval = 4 * time.Hour

const discordTimestampLayout = time.RFC3339

const (
	importSlowRate          = time.Second / 1
	importFastRate          = time.Second / 100
	importMessagesChunkSize = 10
	importInitialDelay      = time.Minute * 5
)

const (
	maxChunkSizeMessages = 1000
	maxChunkSizeBytes    = 1500000
)

const (
	ErrOwnerTokenNeeded                     = "Owner token is needed" // #nosec G101
	ErrMissingCommunityID                   = "CommunityID has to be provided"
	ErrForbiddenProfileOrWatchOnlyAccount   = "Cannot join a community using profile chat or watch-only account"
	ErrSigningJoinRequestForKeycardAccounts = "Signing a joining community request for accounts migrated to keycard must be done with a keycard"
	ErrNotPartOfCommunity                   = "Not part of the community"
	ErrNotAdminOrOwner                      = "Not admin or owner"
	ErrSignerIsNil                          = "Signer can't be nil"
	ErrSyncMessagesSentByNonControlNode     = "Accepted/requested to join sync messages can be send only by the control node"
	ErrReceiverIsNil                        = "Receiver can't be nil"
)

type FetchCommunityRequest struct {
	// CommunityKey should be either a public or a private community key
	CommunityKey    string       `json:"communityKey"`
	Shard           *shard.Shard `json:"shard"`
	TryDatabase     bool         `json:"tryDatabase"`
	WaitForResponse bool         `json:"waitForResponse"`
}

func (r *FetchCommunityRequest) Validate() error {
	if len(r.CommunityKey) <= 2 {
		return fmt.Errorf("community key is too short")
	}
	if _, err := types.DecodeHex(r.CommunityKey); err != nil {
		return fmt.Errorf("invalid community key")
	}
	return nil
}

func (r *FetchCommunityRequest) getCommunityID() string {
	return GetCommunityIDFromKey(r.CommunityKey)
}

func GetCommunityIDFromKey(communityKey string) string {
	// Check if the key is a private key. strip the 0x at the start
	if privateKey, err := crypto.HexToECDSA(communityKey[2:]); err == nil {
		// It is a privateKey
		return types.HexBytes(crypto.CompressPubkey(&privateKey.PublicKey)).String()
	}

	// Not a private key, use the public key
	return communityKey
}

func (m *Messenger) publishOrg(org *communities.Community, shouldRekey bool) error {
	if org == nil {
		return nil
	}

	m.logger.Debug("publishing community",
		zap.Uint64("clock", org.Clock()),
		zap.String("communityID", org.IDString()),
		zap.Any("community", org),
	)

	payload, err := org.MarshaledDescription()

	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Payload: payload,
		Sender:  org.PrivateKey(),
		// we don't want to wrap in an encryption layer message
		SkipEncryptionLayer: true,
		CommunityID:         org.ID(),
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_DESCRIPTION,
		PubsubTopic:         org.PubsubTopic(), // TODO: confirm if it should be sent in community pubsub topic
		Priority:            &common.HighPriority,
	}
	if org.Encrypted() {
		members := org.GetMemberPubkeys()
		if err != nil {
			return err
		}
		rawMessage.CommunityKeyExMsgType = common.KeyExMsgRekey
		// This should be the one that it was used to encrypt this community
		rawMessage.HashRatchetGroupID = org.ID()
		rawMessage.Recipients = members
	}
	messageID, err := m.sender.SendPublic(context.Background(), org.IDString(), rawMessage)
	if err == nil {
		m.logger.Debug("published community",
			zap.String("pubsubTopic", org.PubsubTopic()),
			zap.String("communityID", org.IDString()),
			zap.String("messageID", hexutil.Encode(messageID)),
			zap.Uint64("clock", org.Clock()),
		)
	}
	return err
}

func (m *Messenger) publishCommunityEvents(community *communities.Community, msg *communities.CommunityEventsMessage) error {
	m.logger.Debug("publishing community events", zap.String("admin-id", common.PubkeyToHex(&m.identity.PublicKey)), zap.Any("event", msg))

	payload, err := msg.Marshal()
	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Payload: payload,
		Sender:  m.identity,
		// we don't want to wrap in an encryption layer message
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_EVENTS_MESSAGE,
		PubsubTopic:         community.PubsubTopic(), // TODO: confirm if it should be sent in community pubsub topic
		Priority:            &common.LowPriority,
	}

	// TODO: resend in case of failure?
	_, err = m.sender.SendPublic(context.Background(), types.EncodeHex(msg.CommunityID), rawMessage)
	return err
}

func (m *Messenger) publishCommunityPrivilegedMemberSyncMessage(msg *communities.CommunityPrivilegedMemberSyncMessage) error {
	community, err := m.GetCommunityByID(msg.CommunityPrivilegedUserSyncMessage.CommunityId)
	if err != nil {
		return err
	}

	m.logger.Debug("publishing privileged user sync message",
		zap.Any("receivers", msg.Receivers), zap.Any("type", msg.CommunityPrivilegedUserSyncMessage.Type))

	payload, err := proto.Marshal(msg.CommunityPrivilegedUserSyncMessage)
	if err != nil {
		return err
	}

	rawMessage := &common.RawMessage{
		Payload:             payload,
		Sender:              community.PrivateKey(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_PRIVILEGED_USER_SYNC_MESSAGE,
	}

	for _, receivers := range msg.Receivers {
		_, err = m.sender.SendPrivate(context.Background(), receivers, rawMessage)
	}

	return err
}

func (m *Messenger) handleCommunitiesHistoryArchivesSubscription(c chan *communities.Subscription) {

	go func() {
		for {
			select {
			case sub, more := <-c:
				if !more {
					return
				}

				if sub.CreatingHistoryArchivesSignal != nil {
					m.config.messengerSignalsHandler.CreatingHistoryArchives(sub.CreatingHistoryArchivesSignal.CommunityID)
				}

				if sub.HistoryArchivesCreatedSignal != nil {
					m.config.messengerSignalsHandler.HistoryArchivesCreated(
						sub.HistoryArchivesCreatedSignal.CommunityID,
						sub.HistoryArchivesCreatedSignal.From,
						sub.HistoryArchivesCreatedSignal.To,
					)
				}

				if sub.NoHistoryArchivesCreatedSignal != nil {
					m.config.messengerSignalsHandler.NoHistoryArchivesCreated(
						sub.NoHistoryArchivesCreatedSignal.CommunityID,
						sub.NoHistoryArchivesCreatedSignal.From,
						sub.NoHistoryArchivesCreatedSignal.To,
					)
				}

				if sub.HistoryArchivesSeedingSignal != nil {

					m.config.messengerSignalsHandler.HistoryArchivesSeeding(sub.HistoryArchivesSeedingSignal.CommunityID)

					c, err := m.communitiesManager.GetByIDString(sub.HistoryArchivesSeedingSignal.CommunityID)
					if err != nil {
						m.logger.Debug("failed to retrieve community by id string", zap.Error(err))
					}

					if c.IsControlNode() {
						err := m.dispatchMagnetlinkMessage(sub.HistoryArchivesSeedingSignal.CommunityID)
						if err != nil {
							m.logger.Debug("failed to dispatch magnetlink message", zap.Error(err))
						}
					}
				}

				if sub.HistoryArchivesUnseededSignal != nil {
					m.config.messengerSignalsHandler.HistoryArchivesUnseeded(sub.HistoryArchivesUnseededSignal.CommunityID)
				}

				if sub.HistoryArchiveDownloadedSignal != nil {
					m.config.messengerSignalsHandler.HistoryArchiveDownloaded(
						sub.HistoryArchiveDownloadedSignal.CommunityID,
						sub.HistoryArchiveDownloadedSignal.From,
						sub.HistoryArchiveDownloadedSignal.To,
					)
				}

				if sub.DownloadingHistoryArchivesFinishedSignal != nil {
					m.config.messengerSignalsHandler.DownloadingHistoryArchivesFinished(sub.DownloadingHistoryArchivesFinishedSignal.CommunityID)
				}

				if sub.DownloadingHistoryArchivesStartedSignal != nil {
					m.config.messengerSignalsHandler.DownloadingHistoryArchivesStarted(sub.DownloadingHistoryArchivesStartedSignal.CommunityID)
				}

				if sub.ImportingHistoryArchiveMessagesSignal != nil {
					m.config.messengerSignalsHandler.ImportingHistoryArchiveMessages(sub.ImportingHistoryArchiveMessagesSignal.CommunityID)
				}

			case <-m.quit:
				return
			}
		}
	}()
}

// handleCommunitiesSubscription handles events from communities
func (m *Messenger) handleCommunitiesSubscription(c chan *communities.Subscription) {
	var lastPublished int64
	// We check every 5 minutes if we need to publish
	ticker := time.NewTicker(5 * time.Minute)

	recentlyPublishedOrgs := make(map[string]*communities.Community, 0)

	publishOrgAndDistributeEncryptionKeys := func(community *communities.Community) {
		recentlyPublishedOrg := recentlyPublishedOrgs[community.IDString()]

		if recentlyPublishedOrg != nil && community.Clock() < recentlyPublishedOrg.Clock() {
			return
		}

		// evaluate and distribute encryption keys (if any)
		encryptionKeyActions := communities.EvaluateCommunityEncryptionKeyActions(recentlyPublishedOrg, community)
		err := m.communitiesKeyDistributor.Distribute(community, encryptionKeyActions)
		if err != nil {
			m.logger.Warn("failed to distribute encryption keys", zap.Error(err))
		}

		shouldRekey := encryptionKeyActions.CommunityKeyAction.ActionType == communities.EncryptionKeyRekey
		if community.Encrypted() {
			clock := community.Clock()
			clock++
			userKicked := &protobuf.CommunityUserKicked{
				Clock:       clock,
				CommunityId: community.ID(),
			}

			for pkString := range encryptionKeyActions.CommunityKeyAction.RemovedMembers {
				pk, err := common.HexToPubkey(pkString)
				if err != nil {
					m.logger.Error("failed to decode public key", zap.Error(err), zap.String("pk", pkString))
				}
				payload, err := proto.Marshal(userKicked)
				if err != nil {
					m.logger.Error("failed to marshal user kicked message", zap.Error(err))
					continue
				}

				rawMessage := &common.RawMessage{
					Payload:             payload,
					Sender:              community.PrivateKey(),
					SkipEncryptionLayer: true,
					MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_USER_KICKED,
					PubsubTopic:         shard.DefaultNonProtectedPubsubTopic(),
				}

				_, err = m.sender.SendPrivate(context.Background(), pk, rawMessage)
				if err != nil {
					m.logger.Error("failed to send used kicked message", zap.Error(err))
					continue
				}

			}

		}

		err = m.publishOrg(community, shouldRekey)
		if err != nil {
			m.logger.Warn("failed to publish org", zap.Error(err))
			return
		}
		m.logger.Debug("published org")

		// publish shard information
		err = m.sendPublicCommunityShardInfo(community)
		if err != nil {
			m.logger.Warn("failed to publish public shard info", zap.Error(err))
			return
		}

		// signal client with published community
		if m.config.messengerSignalsHandler != nil {
			if recentlyPublishedOrg == nil || community.Clock() > recentlyPublishedOrg.Clock() {
				response := &MessengerResponse{}
				response.AddCommunity(community)
				m.config.messengerSignalsHandler.MessengerResponse(response)
			}
		}

		recentlyPublishedOrgs[community.IDString()] = community.CreateDeepCopy()
	}

	go func() {
		for {
			select {
			case sub, more := <-c:
				if !more {
					return
				}
				if sub.Community != nil {
					if sub.Community == nil {
						continue
					}
					// NOTE: because we use a pointer here, there's a race condition where the community would be updated before it's compared to the previous one.
					// This results in keys not being propagated as the copy would not see any changes
					communityCopy := sub.Community.CreateDeepCopy()

					publishOrgAndDistributeEncryptionKeys(communityCopy)
				}

				if sub.CommunityEventsMessage != nil {
					err := m.publishCommunityEvents(sub.Community, sub.CommunityEventsMessage)
					if err != nil {
						m.logger.Warn("failed to publish community events", zap.Error(err))
					}
				}

				if sub.AcceptedRequestsToJoin != nil {
					for _, requestID := range sub.AcceptedRequestsToJoin {
						accept := &requests.AcceptRequestToJoinCommunity{
							ID: requestID,
						}
						response, err := m.AcceptRequestToJoinCommunity(accept)
						if err != nil {
							m.logger.Warn("failed to accept request to join ", zap.Error(err))
						}
						if m.config.messengerSignalsHandler != nil {
							m.config.messengerSignalsHandler.MessengerResponse(response)
						}
					}
				}

				if sub.RejectedRequestsToJoin != nil {
					for _, requestID := range sub.RejectedRequestsToJoin {
						reject := &requests.DeclineRequestToJoinCommunity{
							ID: requestID,
						}
						response, err := m.DeclineRequestToJoinCommunity(reject)
						if err != nil {
							m.logger.Warn("failed to decline request to join ", zap.Error(err))
						}
						if m.config.messengerSignalsHandler != nil {
							m.config.messengerSignalsHandler.MessengerResponse(response)
						}
					}
				}

				if sub.CommunityPrivilegedMemberSyncMessage != nil {
					if err := m.publishCommunityPrivilegedMemberSyncMessage(sub.CommunityPrivilegedMemberSyncMessage); err != nil {
						m.logger.Warn("failed to publish community private members sync message", zap.Error(err))
					}
				}
				if sub.TokenCommunityValidated != nil {
					state := m.buildMessageState()
					communityResponse := sub.TokenCommunityValidated

					err := m.handleCommunityResponse(state, communityResponse)
					if err != nil {
						m.logger.Error("failed to handle community response", zap.Error(err))
					}

					m.processCommunityChanges(state)

					_, err = m.saveDataAndPrepareResponse(state)
					if err != nil {
						m.logger.Error("failed to save data and prepare response")
					}

					if m.config.messengerSignalsHandler != nil {
						m.config.messengerSignalsHandler.MessengerResponse(state.Response)
					}
				}

			case <-ticker.C:
				// If we are not online, we don't even try
				if !m.Online() {
					continue
				}

				// If not enough time has passed since last advertisement, we skip this
				if time.Now().Unix()-lastPublished < communityAdvertiseIntervalSecond {
					continue
				}

				controlledCommunities, err := m.communitiesManager.Controlled()
				if err != nil {
					m.logger.Warn("failed to retrieve orgs", zap.Error(err))
				}

				for idx := range controlledCommunities {
					org := controlledCommunities[idx]
					_, beingImported := m.importingCommunities[org.IDString()]
					if !beingImported {
						publishOrgAndDistributeEncryptionKeys(org)
					}
				}

				// set lastPublished
				lastPublished = time.Now().Unix()

			case <-m.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *Messenger) updateCommunitiesActiveMembersPeriodically() {
	communitiesLastUpdated := make(map[string]int64)

	// We check every 5 minutes if we need to update
	ticker := time.NewTicker(5 * time.Minute)

	go func() {
		for {
			select {
			case <-ticker.C:
				controlledCommunities, err := m.communitiesManager.Controlled()
				if err != nil {
					m.logger.Error("failed to update community active members count", zap.Error(err))
				}

				for _, community := range controlledCommunities {
					lastUpdated, ok := communitiesLastUpdated[community.IDString()]
					if !ok {
						lastUpdated = 0
					}

					// If not enough time has passed since last update, we skip this
					if time.Now().Unix()-lastUpdated < int64(updateActiveMembersInterval.Seconds()) {
						continue
					}

					if err := m.updateCommunityActiveMembers(community.IDString()); err == nil {
						communitiesLastUpdated[community.IDString()] = time.Now().Unix()

						// Perf: ensure `updateCommunityActiveMembers` is not called few times in a row
						// Next communities will be handled in subsequent ticks
						break
					} else {
						m.logger.Error("failed to update community active members count", zap.Error(err))
					}
				}

			case <-m.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *Messenger) HandleCommunityUpdateGrant(state *ReceivedMessageState, message *protobuf.CommunityUpdateGrant, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	grant, err := m.encryptor.DecryptCommunityGrant(m.identity, state.CurrentMessageState.PublicKey, message.Grants)
	if err != nil {
		return err
	}

	return m.handleCommunityGrant(community, grant, message.Timestamp)
}

func (m *Messenger) HandleCommunityEncryptionKeysRequest(state *ReceivedMessageState, message *protobuf.CommunityEncryptionKeysRequest, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	if !community.IsControlNode() {
		return communities.ErrNotControlNode
	}
	signer := state.CurrentMessageState.PublicKey
	return m.handleCommunityEncryptionKeysRequest(community, message.ChatIds, signer)
}

func (m *Messenger) HandleCommunitySharedAddressesRequest(state *ReceivedMessageState, message *protobuf.CommunitySharedAddressesRequest, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	if !community.IsControlNode() {
		return communities.ErrNotControlNode
	}
	signer := state.CurrentMessageState.PublicKey
	return m.handleCommunitySharedAddressesRequest(state, community, signer)
}

func (m *Messenger) HandleCommunitySharedAddressesResponse(state *ReceivedMessageState, message *protobuf.CommunitySharedAddressesResponse, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	signer := state.CurrentMessageState.PublicKey
	return m.handleCommunitySharedAddressesResponse(state, community, signer, message.RevealedAccounts)
}

func (m *Messenger) HandleCommunityTokenAction(state *ReceivedMessageState, message *protobuf.CommunityTokenAction, statusMessage *v1protocol.StatusMessage) error {
	return m.communityTokensService.ProcessCommunityTokenAction(message)
}

func (m *Messenger) handleCommunityEncryptionKeysRequest(community *communities.Community, channelIDs []string, signer *ecdsa.PublicKey) error {
	if !community.HasMember(signer) {
		return communities.ErrMemberNotFound
	}

	keyActions := &communities.EncryptionKeyActions{
		CommunityKeyAction: communities.EncryptionKeyAction{},
		ChannelKeysActions: map[string]communities.EncryptionKeyAction{},
	}

	pkStr := common.PubkeyToHex(signer)
	members := make(map[string]*protobuf.CommunityMember)
	members[pkStr] = community.GetMember(signer)

	if community.Encrypted() {
		keyActions.CommunityKeyAction = communities.EncryptionKeyAction{
			ActionType: communities.EncryptionKeySendToMembers,
			Members:    members,
		}
	}

	requestedChannelIDs := map[string]bool{}
	for _, channelID := range channelIDs {
		requestedChannelIDs[channelID] = true
	}

	for channelID, channel := range community.Chats() {
		// Skip channels that weren't requested
		if len(requestedChannelIDs) > 0 && !requestedChannelIDs[channelID] {
			continue
		}

		channelMembers := channel.GetMembers()
		member, exists := channelMembers[pkStr]
		if exists && community.ChannelEncrypted(channelID) {
			members[pkStr] = member
			keyActions.ChannelKeysActions[channelID] = communities.EncryptionKeyAction{
				ActionType: communities.EncryptionKeySendToMembers,
				Members:    members,
			}
		}
	}

	err := m.communitiesKeyDistributor.Distribute(community, keyActions)
	if err != nil {
		m.logger.Error("failed to send community keys", zap.Error(err), zap.String("community ID", community.IDString()))
	}

	return nil
}

func (m *Messenger) handleCommunitySharedAddressesRequest(state *ReceivedMessageState, community *communities.Community, signer *ecdsa.PublicKey) error {
	if !community.HasMember(signer) {
		return communities.ErrMemberNotFound
	}

	pkStr := common.PubkeyToHex(signer)

	revealedAccounts, err := m.communitiesManager.GetRevealedAddresses(community.ID(), pkStr)
	if err != nil {
		return err
	}

	usersSharedAddressesProto := &protobuf.CommunitySharedAddressesResponse{
		CommunityId:      community.ID(),
		RevealedAccounts: revealedAccounts,
	}

	payload, err := proto.Marshal(usersSharedAddressesProto)
	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		Sender:              community.PrivateKey(),
		CommunityID:         community.ID(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_SHARED_ADDRESSES_RESPONSE,
		PubsubTopic:         shard.DefaultNonProtectedPubsubTopic(),
		ResendType:          common.ResendTypeRawMessage,
		ResendMethod:        common.ResendMethodSendPrivate,
		Recipients:          []*ecdsa.PublicKey{signer},
	}

	_, err = m.sender.SendPrivate(context.Background(), signer, &rawMessage)
	if err != nil {
		return err
	}

	if community.IsPrivilegedMember(signer) {
		memberRole := community.MemberRole(signer)
		newPrivilegedMember := make(map[protobuf.CommunityMember_Roles][]*ecdsa.PublicKey)
		newPrivilegedMember[memberRole] = []*ecdsa.PublicKey{signer}
		if err = m.communitiesManager.ShareRequestsToJoinWithPrivilegedMembers(community, newPrivilegedMember); err != nil {
			return err
		}
	}

	return nil
}

func (m *Messenger) handleCommunitySharedAddressesResponse(state *ReceivedMessageState, community *communities.Community, signer *ecdsa.PublicKey, revealedAccounts []*protobuf.RevealedAccount) error {
	isControlNodeMsg := common.IsPubKeyEqual(community.ControlNode(), signer)
	if !isControlNodeMsg {
		return errors.New(ErrSyncMessagesSentByNonControlNode)
	}

	requestID := communities.CalculateRequestID(common.PubkeyToHex(&m.identity.PublicKey), community.ID())
	err := m.communitiesManager.SaveRequestToJoinRevealedAddresses(requestID, revealedAccounts)
	if err != nil {
		return nil
	}

	requestsToJoin, err := m.communitiesManager.GetCommunityRequestsToJoinWithRevealedAddresses(community.ID())
	if err != nil {
		return nil
	}

	state.Response.AddRequestsToJoinCommunity(requestsToJoin)

	return nil
}

func (m *Messenger) handleCommunityGrant(community *communities.Community, grant []byte, clock uint64) error {
	difference, err := m.communitiesManager.HandleCommunityGrant(community, grant, clock)
	if err == communities.ErrGrantOlder || err == communities.ErrGrantExpired {
		// Don't log an error for these cases
		return nil
	}
	if err != nil {
		return err
	}

	// if grant is significantly newer than the one we have, we should check the profile showcase
	if time.Duration(difference)*time.Millisecond > grantInvokesProfileDispatchInterval {
		err = m.UpdateProfileShowcaseCommunity(community)
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Messenger) publishGroupGrantMessage(community *communities.Community, timestamp uint64, recipientGrants map[*ecdsa.PublicKey][]byte) error {
	grants, err := m.encryptor.EncryptCommunityGrants(community.PrivateKey(), recipientGrants)
	if err != nil {
		return err
	}

	message := &protobuf.CommunityUpdateGrant{
		Timestamp:   timestamp,
		CommunityId: community.ID(),
		Grants:      grants,
	}

	payload, err := proto.Marshal(message)
	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		Sender:              community.PrivateKey(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_UPDATE_GRANT,
		PubsubTopic:         community.PubsubTopic(),
		Priority:            &common.LowPriority,
	}

	_, err = m.sender.SendPublic(context.Background(), community.IDString(), rawMessage)
	return err
}

func (m *Messenger) updateGrantsForControlledCommunities() {
	controlledCommunities, err := m.communitiesManager.Controlled()
	if err != nil {
		m.logger.Error("failed fetch controlled communities for grants update", zap.Error(err))
	}

	for _, community := range controlledCommunities {
		// Skip unencrypted communities
		if !community.Encrypted() {
			continue
		}

		memberGrants := map[*ecdsa.PublicKey][]byte{}
		for memberKey := range community.Members() {
			if memberKey == m.IdentityPublicKeyString() {
				grant, err := community.BuildGrant(m.IdentityPublicKey(), "")
				if err != nil {
					m.logger.Error("can't build own grant for controlled community", zap.Error(err))
				}

				err = m.handleCommunityGrant(community, grant, uint64(time.Now().UnixMilli()))
				if err != nil {
					m.logger.Error("error handling grant for controlled community", zap.Error(err))
				}
			} else {
				memberPubKey, err := common.HexToPubkey(memberKey)
				if err != nil {
					m.logger.Error("Pubkey decode ", zap.Error(err))
				}

				grant, err := community.BuildGrant(memberPubKey, "")
				if err != nil {
					m.logger.Error("can't build member's grant for controlled community", zap.Error(err))
				}

				memberGrants[memberPubKey] = grant
			}
		}
		err = m.publishGroupGrantMessage(community, uint64(time.Now().UnixMilli()), memberGrants)
		if err != nil {
			m.logger.Error("failed to update grant for community members", zap.Error(err))
		}
	}
}

func (m *Messenger) schedulePublishGrantsForControlledCommunities() {
	// Send once immediately
	m.updateGrantsForControlledCommunities()

	ticker := time.NewTicker(grantUpdateInterval)

	go func() {
		for {
			select {
			case <-ticker.C:
				m.updateGrantsForControlledCommunities()
			case <-m.quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func (m *Messenger) CheckCommunitiesToUnmute() (*MessengerResponse, error) {
	m.logger.Debug("watching communities to unmute")
	response := &MessengerResponse{}
	communities, err := m.communitiesManager.All()
	if err != nil {
		return nil, fmt.Errorf("couldn't get all communities: %v", err)
	}
	for _, community := range communities {
		communityMuteTill := community.MuteTill().Truncate(time.Second)
		currTime := time.Now().Truncate(time.Second)

		if currTime.After(communityMuteTill) && !communityMuteTill.Equal(time.Time{}) && community.Muted() {
			err := m.communitiesManager.SetMuted(community.ID(), false)
			if err != nil {
				m.logger.Info("CheckCommunitiesToUnmute err", zap.Any("Couldn't unmute community", err))
				break
			}

			err = m.MuteCommunityTill(community.ID(), time.Time{})
			if err != nil {
				m.logger.Info("MuteCommunityTill err", zap.Any("Could not set mute community till", err))
				break
			}

			unmutedCommunity, err := m.communitiesManager.GetByID(community.ID())
			if err != nil {
				return nil, err
			}
			response.AddCommunity(unmutedCommunity)

		}

	}

	return response, nil
}

func (m *Messenger) updateCommunityActiveMembers(communityID string) error {
	lastWeek := time.Now().AddDate(0, 0, -7).Unix()
	count, err := m.persistence.CountActiveChattersInCommunity(communityID, lastWeek)
	if err != nil {
		return err
	}

	if err = m.communitiesManager.SetCommunityActiveMembersCount(communityID, uint64(count)); err != nil {
		return err
	}

	m.logger.Debug("community active members updated", zap.String("communityID", communityID), zap.Uint("count", count))
	return nil
}

func (m *Messenger) Communities() ([]*communities.Community, error) {
	return m.communitiesManager.All()
}

func (m *Messenger) ControlledCommunities() ([]*communities.Community, error) {
	return m.communitiesManager.Controlled()
}

func (m *Messenger) JoinedCommunities() ([]*communities.Community, error) {
	return m.communitiesManager.Joined()
}

func (m *Messenger) IsDisplayNameDupeOfCommunityMember(name string) (bool, error) {
	controlled, err := m.communitiesManager.Controlled()
	if err != nil {
		return false, err
	}

	joined, err := m.communitiesManager.Joined()
	if err != nil {
		return false, err
	}

	for _, community := range append(controlled, joined...) {
		for memberKey := range community.Members() {
			contact := m.GetContactByID(memberKey)
			if contact == nil {
				continue
			}
			if strings.Compare(contact.DisplayName, name) == 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *Messenger) CommunityUpdateLastOpenedAt(communityID string) (int64, error) {
	id, err := hexutil.Decode(communityID)
	if err != nil {
		return 0, err
	}
	currentTime := time.Now().Unix()
	updatedCommunity, err := m.communitiesManager.CommunityUpdateLastOpenedAt(id, currentTime)
	if err != nil {
		return 0, err
	}
	err = m.syncCommunity(context.Background(), updatedCommunity, m.dispatchMessage)
	if err != nil {
		return 0, err
	}
	return currentTime, nil
}

func (m *Messenger) SpectatedCommunities() ([]*communities.Community, error) {
	return m.communitiesManager.Spectated()
}

func (m *Messenger) initCommunityChats(community *communities.Community) ([]*Chat, error) {
	logger := m.logger.Named("initCommunityChats")
	publicFiltersToInit := m.DefaultFilters(community)

	chats := CreateCommunityChats(community, m.getTimesource())

	for _, chat := range chats {
		publicFiltersToInit = append(publicFiltersToInit, transport.FiltersToInitialize{ChatID: chat.ID, PubsubTopic: community.PubsubTopic()})

	}

	// Load transport filters
	filters, err := m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		logger.Debug("m.transport.InitPublicFilters error", zap.Error(err))
		return nil, err
	}

	if community.IsControlNode() {
		// Init the community filter so we can receive messages on the community

		communityFilters, err := m.InitCommunityFilters([]transport.CommunityFilterToInitialize{{
			Shard:   community.Shard(),
			PrivKey: community.PrivateKey(),
		}})

		if err != nil {
			return nil, err
		}
		filters = append(filters, communityFilters...)
	}

	willSync, err := m.scheduleSyncFilters(filters)
	if err != nil {
		logger.Debug("m.scheduleSyncFilters error", zap.Error(err))
		return nil, err
	}

	if !willSync {
		defaultSyncPeriod, err := m.settings.GetDefaultSyncPeriod()
		if err != nil {
			logger.Debug("m.settings.GetDefaultSyncPeriod error", zap.Error(err))
			return nil, err
		}

		timestamp := uint32(m.getTimesource().GetCurrentTime()/1000) - defaultSyncPeriod
		for idx := range chats {
			chats[idx].SyncedTo = timestamp
			chats[idx].SyncedFrom = timestamp
		}
	}

	if err = m.saveChats(chats); err != nil {
		logger.Debug("m.saveChats error", zap.Error(err))
		return nil, err
	}

	return chats, nil
}

func (m *Messenger) initCommunitySettings(communityID types.HexBytes) (*communities.CommunitySettings, error) {
	communitySettings, err := m.communitiesManager.GetCommunitySettingsByID(communityID)
	if err != nil {
		return nil, err
	}
	if communitySettings != nil {
		return communitySettings, nil
	}

	communitySettings = &communities.CommunitySettings{
		CommunityID:                  communityID.String(),
		HistoryArchiveSupportEnabled: true,
	}

	if err := m.communitiesManager.SaveCommunitySettings(*communitySettings); err != nil {
		return nil, err
	}

	return communitySettings, nil
}

func (m *Messenger) JoinCommunity(ctx context.Context, communityID types.HexBytes, forceJoin bool) (*MessengerResponse, error) {
	mr, err := m.joinCommunity(ctx, communityID, forceJoin)
	if err != nil {
		return nil, err
	}

	if com, ok := mr.communities[communityID.String()]; ok {
		err = m.syncCommunity(context.Background(), com, m.dispatchMessage)
		if err != nil {
			return nil, err
		}
	}

	return mr, nil
}

func (m *Messenger) subscribeToCommunityShard(communityID []byte, shard *shard.Shard) error {
	if m.transport.WakuVersion() != 2 {
		return nil
	}

	// TODO: this should probably be moved completely to transport once pubsub topic logic is implemented
	pubsubTopic := shard.PubsubTopic()

	privK, err := m.transport.RetrievePubsubTopicKey(pubsubTopic)
	if err != nil {
		return err
	}

	var pubK *ecdsa.PublicKey
	if privK != nil {
		pubK = &privK.PublicKey
	}

	return m.transport.SubscribeToPubsubTopic(pubsubTopic, pubK)
}

func (m *Messenger) unsubscribeFromShard(shard *shard.Shard) error {
	if m.transport.WakuVersion() != 2 {
		return nil
	}

	// TODO: this should probably be moved completely to transport once pubsub topic logic is implemented

	return m.transport.UnsubscribeFromPubsubTopic(shard.PubsubTopic())
}

func (m *Messenger) joinCommunity(ctx context.Context, communityID types.HexBytes, forceJoin bool) (*MessengerResponse, error) {
	logger := m.logger.Named("joinCommunity")
	response := &MessengerResponse{}
	community, _ := m.communitiesManager.GetByID(communityID)
	isCommunityMember := community.Joined()

	community, err := m.communitiesManager.JoinCommunity(communityID, forceJoin)
	if err != nil {
		logger.Debug("m.communitiesManager.JoinCommunity error", zap.Error(err))
		return nil, err
	}

	// chats and settings are already initialized for spectated communities
	if !community.Spectated() {
		chats, err := m.initCommunityChats(community)
		if err != nil {
			return nil, err
		}
		response.AddChats(chats)

		if _, err = m.initCommunitySettings(communityID); err != nil {
			return nil, err
		}

		if err = m.subscribeToCommunityShard(community.ID(), community.Shard()); err != nil {
			return nil, err
		}
	}

	communitySettings, err := m.communitiesManager.GetCommunitySettingsByID(communityID)
	if err != nil {
		return nil, err
	}

	response.AddCommunity(community)
	response.AddCommunitySettings(communitySettings)

	if err = m.reregisterForPushNotifications(); err != nil {
		return nil, err
	}

	if err = m.sendCurrentUserStatusToCommunity(ctx, community); err != nil {
		logger.Debug("m.sendCurrentUserStatusToCommunity error", zap.Error(err))
		return nil, err
	}

	if err = m.PublishIdentityImage(); err != nil {
		return nil, err
	}

	// Was applicant not a member and successfully joined?
	if !isCommunityMember && community.Joined() {
		joinedNotification := &localnotifications.Notification{
			ID:       gethcommon.Hash(types.BytesToHash([]byte(`you-joined-` + communityID.String()))),
			Title:    community.Name(),
			Message:  community.Name(),
			BodyType: localnotifications.CategoryCommunityJoined,
			Category: localnotifications.CategoryCommunityJoined,
			Deeplink: "status-app://cr/" + community.IDString(),
			Image:    "",
		}
		response.AddNotification(joinedNotification)

		// Activity Center notification
		requestID := communities.CalculateRequestID(common.PubkeyToHex(&m.identity.PublicKey), communityID)
		notification, err := m.persistence.GetActivityCenterNotificationByID(requestID)
		if err != nil {
			return nil, err
		}

		if notification != nil && notification.MembershipStatus != ActivityCenterMembershipStatusAccepted {
			notification.MembershipStatus = ActivityCenterMembershipStatusAccepted
			notification.Read = false
			notification.Deleted = false

			notification.UpdatedAt = m.GetCurrentTimeInMillis()
			err = m.addActivityCenterNotification(response, notification, nil)
			if err != nil {
				m.logger.Error("failed to update request to join accepted notification", zap.Error(err))
				return nil, err
			}
		}
	}

	return response, nil
}

func (m *Messenger) SpectateCommunity(communityID types.HexBytes) (*MessengerResponse, error) {
	logger := m.logger.Named("SpectateCommunity")

	response := &MessengerResponse{}

	community, err := m.communitiesManager.SpectateCommunity(communityID)
	if err != nil {
		logger.Debug("SpectateCommunity error", zap.Error(err))
		return nil, err
	}

	chats, err := m.initCommunityChats(community)
	if err != nil {
		return nil, err
	}
	response.AddChats(chats)

	settings, err := m.initCommunitySettings(communityID)
	if err != nil {
		return nil, err
	}
	response.AddCommunitySettings(settings)

	response.AddCommunity(community)

	if err = m.subscribeToCommunityShard(community.ID(), community.Shard()); err != nil {
		return nil, err
	}

	// sync community
	m.asyncRequestAllHistoricMessages()

	return response, nil
}

func (m *Messenger) MuteDuration(mutedType requests.MutingVariation) (time.Time, error) {
	var MuteTill time.Time

	switch mutedType {
	case MuteTill1Min:
		MuteTill = time.Now().Add(MuteFor1MinDuration)
	case MuteFor15Min:
		MuteTill = time.Now().Add(MuteFor15MinsDuration)
	case MuteFor1Hr:
		MuteTill = time.Now().Add(MuteFor1HrsDuration)
	case MuteFor8Hr:
		MuteTill = time.Now().Add(MuteFor8HrsDuration)
	case MuteFor1Week:
		MuteTill = time.Now().Add(MuteFor1WeekDuration)
	default:
		MuteTill = time.Time{}
	}

	muteTillTimeRemoveMs, err := time.Parse(time.RFC3339, MuteTill.Format(time.RFC3339))
	if err != nil {
		return time.Time{}, err
	}

	return muteTillTimeRemoveMs, nil
}

func (m *Messenger) SetMuted(request *requests.MuteCommunity) error {
	if err := request.Validate(); err != nil {
		return err
	}

	if request.MutedType == Unmuted {
		return m.communitiesManager.SetMuted(request.CommunityID, false)
	}

	return m.communitiesManager.SetMuted(request.CommunityID, true)
}

func (m *Messenger) MuteCommunityTill(communityID []byte, muteTill time.Time) error {
	return m.communitiesManager.MuteCommunityTill(communityID, muteTill)
}

func (m *Messenger) MuteAllCommunityChats(request *requests.MuteCommunity) (time.Time, error) {
	return m.UpdateMuteCommunityStatus(request.CommunityID.String(), true, request.MutedType)
}

func (m *Messenger) UnMuteAllCommunityChats(communityID string) (time.Time, error) {
	return m.UpdateMuteCommunityStatus(communityID, false, Unmuted)
}

func (m *Messenger) UpdateMuteCommunityStatus(communityID string, muted bool, mutedType requests.MutingVariation) (time.Time, error) {
	community, err := m.communitiesManager.GetByIDString(communityID)
	if err != nil {
		return time.Time{}, err
	}

	request := &requests.MuteCommunity{
		CommunityID: community.ID(),
		MutedType:   mutedType,
	}

	err = m.SetMuted(request)
	if err != nil {
		return time.Time{}, err
	}

	muteTill, err := m.MuteDuration(mutedType)
	if err != nil {
		return time.Time{}, err
	}

	err = m.MuteCommunityTill(community.ID(), muteTill)

	for _, chatID := range community.CommunityChatsIDs() {
		if muted {
			_, err := m.MuteChat(&requests.MuteChat{ChatID: communityID + chatID, MutedType: mutedType})
			if err != nil {
				return time.Time{}, err
			}

		} else {
			err = m.UnmuteChat(communityID + chatID)
			if err != nil {
				return time.Time{}, err
			}

		}

		if err != nil {
			return time.Time{}, err
		}
	}

	return muteTill, err
}

func (m *Messenger) SetMutePropertyOnChatsByCategory(request *requests.MuteCategory, muted bool) error {
	if err := request.Validate(); err != nil {
		return err
	}
	community, err := m.communitiesManager.GetByIDString(request.CommunityID)
	if err != nil {
		return err
	}

	for _, chatID := range community.ChatsByCategoryID(request.CategoryID) {
		if muted {
			_, err = m.MuteChat(&requests.MuteChat{ChatID: request.CommunityID + chatID, MutedType: request.MutedType})
		} else {
			err = m.UnmuteChat(request.CommunityID + chatID)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

// Generates a single hash for each address that needs to be revealed to a community.
// Each hash needs to be signed.
// The order of retuned hashes corresponds to the order of addresses in addressesToReveal.
func (m *Messenger) generateCommunityRequestsForSigning(memberPubKey string, communityID types.HexBytes, addressesToReveal []string, isEdit bool) ([]account.SignParams, error) {
	walletAccounts, err := m.settings.GetActiveAccounts()
	if err != nil {
		return nil, err
	}

	containsAddress := func(addresses []string, targetAddress string) bool {
		for _, address := range addresses {
			if types.HexToAddress(address) == types.HexToAddress(targetAddress) {
				return true
			}
		}
		return false
	}

	msgsToSign := make([]account.SignParams, 0)
	for _, walletAccount := range walletAccounts {
		if walletAccount.Chat || walletAccount.Type == accounts.AccountTypeWatch {
			continue
		}

		if len(addressesToReveal) > 0 && !containsAddress(addressesToReveal, walletAccount.Address.Hex()) {
			continue
		}

		requestID := []byte{}
		if !isEdit {
			requestID = communities.CalculateRequestID(memberPubKey, communityID)
		}
		msgsToSign = append(msgsToSign, account.SignParams{
			Data:    types.EncodeHex(crypto.Keccak256(m.IdentityPublicKeyCompressed(), communityID, requestID)),
			Address: walletAccount.Address.Hex(),
		})
	}

	return msgsToSign, nil
}

func (m *Messenger) GenerateJoiningCommunityRequestsForSigning(memberPubKey string, communityID types.HexBytes, addressesToReveal []string) ([]account.SignParams, error) {
	if len(communityID) == 0 {
		return nil, errors.New(ErrMissingCommunityID)
	}
	return m.generateCommunityRequestsForSigning(memberPubKey, communityID, addressesToReveal, false)
}

func (m *Messenger) GenerateEditCommunityRequestsForSigning(memberPubKey string, communityID types.HexBytes, addressesToReveal []string) ([]account.SignParams, error) {
	return m.generateCommunityRequestsForSigning(memberPubKey, communityID, addressesToReveal, true)
}

// Signs the provided messages with the provided accounts and password.
// Provided accounts must not belong to a keypair that is migrated to a keycard.
// Otherwise, the signing will fail, cause such accounts should be signed with a keycard.
func (m *Messenger) SignData(signParams []account.SignParams) ([]string, error) {
	signatures := make([]string, len(signParams))
	for i, param := range signParams {
		if err := param.Validate(true); err != nil {
			return nil, err
		}

		account, err := m.settings.GetAccountByAddress(types.HexToAddress(param.Address))
		if err != nil {
			return nil, err
		}

		if account.Chat || account.Type == accounts.AccountTypeWatch {
			return nil, errors.New(ErrForbiddenProfileOrWatchOnlyAccount)
		}

		keypair, err := m.settings.GetKeypairByKeyUID(account.KeyUID)
		if err != nil {
			return nil, err
		}

		if keypair.MigratedToKeycard() {
			return nil, errors.New(ErrSigningJoinRequestForKeycardAccounts)
		}

		verifiedAccount, err := m.accountsManager.GetVerifiedWalletAccount(m.settings, param.Address, param.Password)
		if err != nil {
			return nil, err
		}

		signature, err := m.accountsManager.Sign(param, verifiedAccount)
		if err != nil {
			return nil, err
		}

		signatures[i] = types.EncodeHex(signature)
	}

	return signatures, nil
}

func (m *Messenger) RequestToJoinCommunity(request *requests.RequestToJoinCommunity) (*MessengerResponse, error) {
	// TODO: Because of changes that need to be done in tests, calling this function and providing `request` without `AddressesToReveal`
	//       is not an error, but it should be.
	logger := m.logger.Named("RequestToJoinCommunity")
	logger.Debug("Addresses to reveal", zap.Any("Addresses:", request.AddressesToReveal))

	if err := request.Validate(); err != nil {
		logger.Debug("request failed to validate", zap.Error(err), zap.Any("request", request))
		return nil, err
	}

	community, err := m.communitiesManager.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	// We don't allow requesting access if already joined
	if community.Joined() {
		return nil, communities.ErrAlreadyJoined
	}

	requestToJoin := m.communitiesManager.CreateRequestToJoin(request, m.account.GetCustomizationColor())

	if len(request.AddressesToReveal) > 0 {
		revealedAddresses := make([]gethcommon.Address, 0)
		for _, addr := range request.AddressesToReveal {
			revealedAddresses = append(revealedAddresses, gethcommon.HexToAddress(addr))
		}

		permissions, err := m.communitiesManager.CheckPermissionToJoin(request.CommunityID, revealedAddresses)
		if err != nil {
			return nil, err
		}
		if !permissions.Satisfied {
			return nil, communities.ErrPermissionToJoinNotSatisfied
		}

		for _, accountAndChainIDs := range permissions.ValidCombinations {
			for i := range requestToJoin.RevealedAccounts {
				if gethcommon.HexToAddress(requestToJoin.RevealedAccounts[i].Address) == accountAndChainIDs.Address {
					requestToJoin.RevealedAccounts[i].ChainIds = accountAndChainIDs.ChainIDs
				}
			}
		}
	}

	displayName, err := m.settings.DisplayName()
	if err != nil {
		return nil, err
	}

	requestToJoinProto := &protobuf.CommunityRequestToJoin{
		Clock:              requestToJoin.Clock,
		EnsName:            requestToJoin.ENSName,
		DisplayName:        displayName,
		CommunityId:        request.CommunityID,
		RevealedAccounts:   requestToJoin.RevealedAccounts,
		CustomizationColor: multiaccountscommon.ColorToIDFallbackToBlue(requestToJoin.CustomizationColor),
	}

	community, _, err = m.communitiesManager.SaveRequestToJoinAndCommunity(requestToJoin, community)
	if err != nil {
		return nil, err
	}
	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	payload, err := proto.Marshal(requestToJoinProto)
	if err != nil {
		return nil, err
	}

	rawMessage := &common.RawMessage{
		Payload:             payload,
		CommunityID:         community.ID(),
		ResendType:          common.ResendTypeRawMessage,
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_REQUEST_TO_JOIN,
		PubsubTopic:         shard.DefaultNonProtectedPubsubTopic(),
		Priority:            &common.HighPriority,
	}

	_, err = m.SendMessageToControlNode(community, rawMessage)
	if err != nil {
		return nil, err
	}

	if _, err = m.AddRawMessageToWatch(rawMessage); err != nil {
		return nil, err
	}

	if !community.AutoAccept() {
		privilegedMembersSorted := community.GetFilteredPrivilegedMembers(map[string]struct{}{m.IdentityPublicKeyString(): {}})
		privMembersArray := []*ecdsa.PublicKey{}

		if rawMessage.ResendMethod != common.ResendMethodSendPrivate {
			privMembersArray = append(privMembersArray, privilegedMembersSorted[protobuf.CommunityMember_ROLE_OWNER]...)
		}

		privMembersArray = append(privMembersArray, privilegedMembersSorted[protobuf.CommunityMember_ROLE_TOKEN_MASTER]...)
		privMembersArray = append(privMembersArray, privilegedMembersSorted[protobuf.CommunityMember_ROLE_ADMIN]...)

		rawMessage.ResendMethod = common.ResendMethodSendPrivate
		rawMessage.ID = ""
		rawMessage.Recipients = privMembersArray

		// don't send revealed addresses to privileged members
		// tokenMaster and owner without community private key will receive them from control node
		requestToJoinProto.RevealedAccounts = make([]*protobuf.RevealedAccount, 0)
		payload, err = proto.Marshal(requestToJoinProto)
		if err != nil {
			return nil, err
		}
		rawMessage.Payload = payload

		for _, member := range rawMessage.Recipients {
			_, err := m.sender.SendPrivate(context.Background(), member, rawMessage)
			if err != nil {
				return nil, err
			}
		}

		if len(rawMessage.Recipients) > 0 {
			if _, err = m.AddRawMessageToWatch(rawMessage); err != nil {
				return nil, err
			}
		}
	}

	response := &MessengerResponse{}
	response.AddRequestToJoinCommunity(requestToJoin)
	response.AddCommunity(community)

	// We send a push notification in the background
	go func() {
		if m.pushNotificationClient != nil {
			pks, err := community.CanManageUsersPublicKeys()
			if err != nil {
				m.logger.Error("failed to get pks", zap.Error(err))
				return
			}
			for _, publicKey := range pks {
				pkString := common.PubkeyToHex(publicKey)
				_, err = m.pushNotificationClient.SendNotification(publicKey, nil, requestToJoin.ID, pkString, protobuf.PushNotification_REQUEST_TO_JOIN_COMMUNITY)
				if err != nil {
					m.logger.Error("error sending notification", zap.Error(err))
					return
				}
			}
		}
	}()

	// Activity center notification
	notification := &ActivityCenterNotification{
		ID:               types.FromHex(requestToJoin.ID.String()),
		Type:             ActivityCenterNotificationTypeCommunityRequest,
		Timestamp:        m.getTimesource().GetCurrentTime(),
		CommunityID:      community.IDString(),
		MembershipStatus: ActivityCenterMembershipStatusPending,
		Read:             true,
		Deleted:          false,
		UpdatedAt:        m.GetCurrentTimeInMillis(),
	}

	err = m.addActivityCenterNotification(response, notification, nil)
	if err != nil {
		m.logger.Error("failed to save notification", zap.Error(err))
		return nil, err
	}

	for _, account := range requestToJoin.RevealedAccounts {
		err := m.settings.AddressWasShown(types.HexToAddress(account.Address))
		if err != nil {
			return nil, err
		}
	}

	return response, nil
}

func (m *Messenger) EditSharedAddressesForCommunity(request *requests.EditSharedAddresses) (*MessengerResponse, error) {
	logger := m.logger.Named("EditSharedAddressesForCommunity")
	if err := request.Validate(); err != nil {
		logger.Debug("request failed to validate", zap.Error(err), zap.Any("request", request))
		return nil, err
	}

	community, err := m.communitiesManager.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	if !community.HasMember(m.IdentityPublicKey()) {
		return nil, errors.New(ErrNotPartOfCommunity)
	}

	revealedAddresses := make([]gethcommon.Address, 0)
	for _, addr := range request.AddressesToReveal {
		revealedAddresses = append(revealedAddresses, gethcommon.HexToAddress(addr))
	}

	checkPermissionResponse, err := m.communitiesManager.CheckPermissionToJoin(community.ID(), revealedAddresses)
	if err != nil {
		return nil, err
	}

	member := community.GetMember(m.IdentityPublicKey())

	requestToEditRevealedAccountsProto := &protobuf.CommunityEditSharedAddresses{
		Clock:            member.LastUpdateClock + 1,
		CommunityId:      community.ID(),
		RevealedAccounts: make([]*protobuf.RevealedAccount, 0),
	}

	for i := range request.AddressesToReveal {
		revealedAcc := &protobuf.RevealedAccount{
			Address:          request.AddressesToReveal[i],
			IsAirdropAddress: types.HexToAddress(request.AddressesToReveal[i]) == types.HexToAddress(request.AirdropAddress),
			Signature:        request.Signatures[i],
		}

		for _, accountAndChainIDs := range checkPermissionResponse.ValidCombinations {
			if accountAndChainIDs.Address == gethcommon.HexToAddress(request.AddressesToReveal[i]) {
				revealedAcc.ChainIds = accountAndChainIDs.ChainIDs
				break
			}
		}

		requestToEditRevealedAccountsProto.RevealedAccounts = append(requestToEditRevealedAccountsProto.RevealedAccounts, revealedAcc)
	}

	requestID := communities.CalculateRequestID(common.PubkeyToHex(&m.identity.PublicKey), request.CommunityID)
	err = m.communitiesManager.RemoveRequestToJoinRevealedAddresses(requestID)
	if err != nil {
		return nil, err
	}
	err = m.communitiesManager.SaveRequestToJoinRevealedAddresses(requestID, requestToEditRevealedAccountsProto.RevealedAccounts)
	if err != nil {
		return nil, err
	}

	payload, err := proto.Marshal(requestToEditRevealedAccountsProto)
	if err != nil {
		return nil, err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		CommunityID:         community.ID(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_EDIT_SHARED_ADDRESSES,
		PubsubTopic:         community.PubsubTopic(), // TODO: confirm if it should be sent in community pubsub topic
		ResendType:          common.ResendTypeRawMessage,
	}

	_, err = m.SendMessageToControlNode(community, &rawMessage)
	if err != nil {
		return nil, err
	}

	if _, err = m.AddRawMessageToWatch(&rawMessage); err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)

	return response, nil
}

func (m *Messenger) PublishTokenActionToPrivilegedMembers(communityID []byte, chainID uint64, contractAddress string, actionType protobuf.CommunityTokenAction_ActionType) error {

	community, err := m.communitiesManager.GetByID(communityID)
	if err != nil {
		return err
	}

	tokenActionProto := &protobuf.CommunityTokenAction{
		ChainId:         chainID,
		ContractAddress: contractAddress,
		ActionType:      actionType,
	}

	payload, err := proto.Marshal(tokenActionProto)
	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Payload:      payload,
		CommunityID:  community.ID(),
		ResendType:   common.ResendTypeRawMessage,
		ResendMethod: common.ResendMethodSendPrivate,
		MessageType:  protobuf.ApplicationMetadataMessage_COMMUNITY_TOKEN_ACTION,
		PubsubTopic:  community.PubsubTopic(),
	}

	skipMembers := make(map[string]struct{})
	skipMembers[common.PubkeyToHex(&m.identity.PublicKey)] = struct{}{}
	privilegedMembers := community.GetFilteredPrivilegedMembers(skipMembers)

	allRecipients := privilegedMembers[protobuf.CommunityMember_ROLE_OWNER]
	allRecipients = append(allRecipients, privilegedMembers[protobuf.CommunityMember_ROLE_TOKEN_MASTER]...)
	rawMessage.Recipients = allRecipients

	for _, recipient := range rawMessage.Recipients {
		_, err := m.sender.SendPrivate(context.Background(), recipient, &rawMessage)
		if err != nil {
			return err
		}
	}

	if len(allRecipients) > 0 {
		if _, err = m.AddRawMessageToWatch(&rawMessage); err != nil {
			return err
		}
	}

	return nil
}

func (m *Messenger) GetRevealedAccounts(communityID types.HexBytes, memberPk string) ([]*protobuf.RevealedAccount, error) {
	return m.communitiesManager.GetRevealedAddresses(communityID, memberPk)
}

func (m *Messenger) GetRevealedAccountsForAllMembers(communityID types.HexBytes) (map[string][]*protobuf.RevealedAccount, error) {
	community, err := m.communitiesManager.GetByID(communityID)
	if err != nil {
		return nil, err
	}
	membersRevealedAccounts := map[string][]*protobuf.RevealedAccount{}
	for _, memberPubKey := range community.GetMemberPubkeys() {
		memberPubKeyStr := common.PubkeyToHex(memberPubKey)
		accounts, err := m.communitiesManager.GetRevealedAddresses(communityID, memberPubKeyStr)
		if err != nil {
			return nil, err
		}
		membersRevealedAccounts[memberPubKeyStr] = accounts
	}
	return membersRevealedAccounts, nil
}

func (m *Messenger) CreateCommunityCategory(request *requests.CreateCommunityCategory) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	var response MessengerResponse
	_, changes, err := m.communitiesManager.CreateCategory(request, true)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(changes.Community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return &response, nil
}

func (m *Messenger) EditCommunityCategory(request *requests.EditCommunityCategory) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	var response MessengerResponse
	community, changes, err := m.communitiesManager.EditCategory(request)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return &response, nil
}

func (m *Messenger) ReorderCommunityCategories(request *requests.ReorderCommunityCategories) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	var response MessengerResponse
	community, changes, err := m.communitiesManager.ReorderCategories(request)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return &response, nil
}

func (m *Messenger) ReorderCommunityChat(request *requests.ReorderCommunityChat) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	var response MessengerResponse
	community, changes, err := m.communitiesManager.ReorderChat(request)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return &response, nil
}

func (m *Messenger) DeleteCommunityCategory(request *requests.DeleteCommunityCategory) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	var response MessengerResponse
	community, changes, err := m.communitiesManager.DeleteCategory(request)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return &response, nil
}

func (m *Messenger) CancelRequestToJoinCommunity(ctx context.Context, request *requests.CancelRequestToJoinCommunity) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	requestToJoin, community, err := m.communitiesManager.CancelRequestToJoin(request)
	if err != nil {
		return nil, err
	}

	displayName, err := m.settings.DisplayName()
	if err != nil {
		return nil, err
	}

	cancelRequestToJoinProto := &protobuf.CommunityCancelRequestToJoin{
		Clock:              community.Clock(),
		EnsName:            requestToJoin.ENSName,
		DisplayName:        displayName,
		CommunityId:        community.ID(),
		CustomizationColor: multiaccountscommon.ColorToIDFallbackToBlue(requestToJoin.CustomizationColor),
	}

	payload, err := proto.Marshal(cancelRequestToJoinProto)
	if err != nil {
		return nil, err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		CommunityID:         community.ID(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_CANCEL_REQUEST_TO_JOIN,
		PubsubTopic:         shard.DefaultNonProtectedPubsubTopic(),
		ResendType:          common.ResendTypeRawMessage,
		Priority:            &common.HighPriority,
	}

	_, err = m.SendMessageToControlNode(community, &rawMessage)
	if err != nil {
		return nil, err
	}

	// NOTE: rawMessage.ID is generated from payload + sender + messageType
	// rawMessage.ID will be the same for control node and privileged members, but for
	// community without owner token resend type is different
	// in order not to override msg to control node by message for privileged members,
	// we skip storing the same message for privileged members
	avoidDuplicateWatchingForPrivilegedMembers := community.AutoAccept() || rawMessage.ResendMethod != common.ResendMethodSendPrivate
	if avoidDuplicateWatchingForPrivilegedMembers {
		if _, err = m.AddRawMessageToWatch(&rawMessage); err != nil {
			return nil, err
		}
	}

	if !community.AutoAccept() {
		// send cancelation to community admins also
		rawMessage.Payload = payload
		rawMessage.ResendMethod = common.ResendMethodSendPrivate

		privilegedMembersSorted := community.GetFilteredPrivilegedMembers(map[string]struct{}{m.IdentityPublicKeyString(): {}})
		privMembersArray := privilegedMembersSorted[protobuf.CommunityMember_ROLE_TOKEN_MASTER]
		privMembersArray = append(privMembersArray, privilegedMembersSorted[protobuf.CommunityMember_ROLE_ADMIN]...)

		if !avoidDuplicateWatchingForPrivilegedMembers {
			// control node was added to the recipients during 'SendMessageToControlNode'
			rawMessage.Recipients = append(rawMessage.Recipients, privMembersArray...)
		} else {
			privMembersArray = append(privMembersArray, privilegedMembersSorted[protobuf.CommunityMember_ROLE_OWNER]...)
			rawMessage.Recipients = privMembersArray
		}

		for _, privilegedMember := range privMembersArray {
			_, err := m.sender.SendPrivate(context.Background(), privilegedMember, &rawMessage)
			if err != nil {
				return nil, err
			}
		}

		if !avoidDuplicateWatchingForPrivilegedMembers {
			if _, err = m.AddRawMessageToWatch(&rawMessage); err != nil {
				return nil, err
			}
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.AddRequestToJoinCommunity(requestToJoin)

	// delete activity center notification
	notification, err := m.persistence.GetActivityCenterNotificationByID(requestToJoin.ID)
	if err != nil {
		return nil, err
	}

	if notification != nil {
		notification.IncrementUpdatedAt(m.getTimesource())
		err = m.persistence.DeleteActivityCenterNotificationByID(types.FromHex(requestToJoin.ID.String()), notification.UpdatedAt)
		if err != nil {
			m.logger.Error("failed to delete notification from Activity Center", zap.Error(err))
			return nil, err
		}

		// set notification as deleted, so that the client will remove the activity center notification from UI
		notification.Deleted = true
		err = m.syncActivityCenterDeletedByIDs(ctx, []types.HexBytes{notification.ID}, notification.UpdatedAt)
		if err != nil {
			m.logger.Error("CancelRequestToJoinCommunity, failed to sync activity center notification as deleted", zap.Error(err))
			return nil, err
		}
		response.AddActivityCenterNotification(notification)
	}

	return response, nil
}

func (m *Messenger) acceptRequestToJoinCommunity(requestToJoin *communities.RequestToJoin) (*MessengerResponse, error) {
	community, err := m.communitiesManager.AcceptRequestToJoin(requestToJoin)
	if err != nil {
		return nil, err
	}

	if community.IsControlNode() {
		// If we are the control node, we send the response to the user
		pk, err := common.HexToPubkey(requestToJoin.PublicKey)
		if err != nil {
			return nil, err
		}

		grant, err := community.BuildGrant(pk, "")
		if err != nil {
			return nil, err
		}

		var key *ecdsa.PrivateKey
		if m.transport.WakuVersion() == 2 {
			key, err = m.transport.RetrievePubsubTopicKey(community.PubsubTopic())
			if err != nil {
				return nil, err
			}
		}

		encryptedDescription, err := community.EncryptedDescription()
		if err != nil {
			return nil, err
		}

		requestToJoinResponseProto := &protobuf.CommunityRequestToJoinResponse{
			Clock:                    community.Clock(),
			Accepted:                 true,
			CommunityId:              community.ID(),
			Community:                encryptedDescription,
			Grant:                    grant,
			ProtectedTopicPrivateKey: crypto.FromECDSA(key),
			Shard:                    community.Shard().Protobuffer(),
		}

		// The purpose of this torrent code is to get the 'magnetlink' to populate 'requestToJoinResponseProto.MagnetUri'
		if m.archiveManager.IsReady() && m.archiveManager.TorrentFileExists(community.IDString()) {
			magnetlink, err := m.archiveManager.GetHistoryArchiveMagnetlink(community.ID())
			if err != nil {
				m.logger.Warn("couldn't get magnet link for community", zap.Error(err))
				return nil, err
			}
			requestToJoinResponseProto.MagnetUri = magnetlink
		}

		payload, err := proto.Marshal(requestToJoinResponseProto)
		if err != nil {
			return nil, err
		}

		rawMessage := &common.RawMessage{
			Payload:             payload,
			Sender:              community.PrivateKey(),
			CommunityID:         community.ID(),
			SkipEncryptionLayer: true,
			MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_REQUEST_TO_JOIN_RESPONSE,
			PubsubTopic:         shard.DefaultNonProtectedPubsubTopic(),
			ResendType:          common.ResendTypeRawMessage,
			ResendMethod:        common.ResendMethodSendPrivate,
			Recipients:          []*ecdsa.PublicKey{pk},
			Priority:            &common.HighPriority,
		}

		if community.Encrypted() {
			rawMessage.HashRatchetGroupID = community.ID()
			rawMessage.CommunityKeyExMsgType = common.KeyExMsgReuse
		}

		_, err = m.sender.SendPrivate(context.Background(), pk, rawMessage)
		if err != nil {
			return nil, err
		}

		if _, err = m.AddRawMessageToWatch(rawMessage); err != nil {
			return nil, err
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.AddRequestToJoinCommunity(requestToJoin)

	// Update existing notification
	notification, err := m.persistence.GetActivityCenterNotificationByID(requestToJoin.ID)
	if err != nil {
		return nil, err
	}
	if notification != nil {
		notification.MembershipStatus = ActivityCenterMembershipStatusAccepted
		if community.HasPermissionToSendCommunityEvents() {
			notification.MembershipStatus = ActivityCenterMembershipStatusAcceptedPending
		}
		notification.Read = true
		notification.Accepted = true
		notification.IncrementUpdatedAt(m.getTimesource())

		err = m.addActivityCenterNotification(response, notification, m.syncActivityCenterCommunityRequestDecisionAdapter)
		if err != nil {
			m.logger.Error("failed to save notification", zap.Error(err))
			return nil, err
		}
	}

	return response, nil
}

func (m *Messenger) AcceptRequestToJoinCommunity(request *requests.AcceptRequestToJoinCommunity) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	requestToJoin, err := m.communitiesManager.GetRequestToJoin(request.ID)
	if err != nil {
		return nil, err
	}

	return m.acceptRequestToJoinCommunity(requestToJoin)
}

func (m *Messenger) declineRequestToJoinCommunity(requestToJoin *communities.RequestToJoin) (*MessengerResponse, error) {
	community, err := m.communitiesManager.DeclineRequestToJoin(requestToJoin)
	if err != nil {
		return nil, err
	}

	if community.IsControlNode() {
		// Notify privileged members that request to join was rejected
		// Send request to join without revealed addresses
		requestToJoin.RevealedAccounts = make([]*protobuf.RevealedAccount, 0)
		declinedRequestsToJoin := make(map[string]*protobuf.CommunityRequestToJoin)
		declinedRequestsToJoin[requestToJoin.PublicKey] = requestToJoin.ToCommunityRequestToJoinProtobuf()

		syncMsg := &protobuf.CommunityPrivilegedUserSyncMessage{
			Type:          protobuf.CommunityPrivilegedUserSyncMessage_CONTROL_NODE_REJECT_REQUEST_TO_JOIN,
			CommunityId:   community.ID(),
			RequestToJoin: declinedRequestsToJoin,
		}

		payloadSyncMsg, err := proto.Marshal(syncMsg)
		if err != nil {
			return nil, err
		}

		rawSyncMessage := &common.RawMessage{
			Payload:             payloadSyncMsg,
			Sender:              community.PrivateKey(),
			SkipEncryptionLayer: true,
			MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_PRIVILEGED_USER_SYNC_MESSAGE,
		}

		privilegedMembers := community.GetPrivilegedMembers()
		for _, privilegedMember := range privilegedMembers {
			if privilegedMember.Equal(&m.identity.PublicKey) {
				continue
			}
			_, err := m.sender.SendPrivate(context.Background(), privilegedMember, rawSyncMessage)
			if err != nil {
				return nil, err
			}
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.AddRequestToJoinCommunity(requestToJoin)

	// Update existing notification
	notification, err := m.persistence.GetActivityCenterNotificationByID(requestToJoin.ID)
	if err != nil {
		return nil, err
	}
	if notification != nil {
		notification.MembershipStatus = ActivityCenterMembershipStatusDeclined
		if community.HasPermissionToSendCommunityEvents() {
			notification.MembershipStatus = ActivityCenterMembershipStatusDeclinedPending
		}
		notification.Read = true
		notification.Dismissed = true
		notification.IncrementUpdatedAt(m.getTimesource())

		err = m.addActivityCenterNotification(response, notification, m.syncActivityCenterCommunityRequestDecisionAdapter)
		if err != nil {
			m.logger.Error("failed to save notification", zap.Error(err))
			return nil, err
		}
	}

	return response, nil
}

func (m *Messenger) DeclineRequestToJoinCommunity(request *requests.DeclineRequestToJoinCommunity) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	requestToJoin, err := m.communitiesManager.GetRequestToJoin(request.ID)
	if err != nil {
		return nil, err
	}

	return m.declineRequestToJoinCommunity(requestToJoin)
}

func (m *Messenger) LeaveCommunity(communityID types.HexBytes) (*MessengerResponse, error) {
	_, err := m.persistence.DismissAllActivityCenterNotificationsFromCommunity(communityID.String(), m.GetCurrentTimeInMillis())
	if err != nil {
		return nil, err
	}

	mr, err := m.leaveCommunity(communityID)
	if err != nil {
		return nil, err
	}

	community, ok := mr.communities[communityID.String()]
	if !ok {
		return nil, communities.ErrOrgNotFound
	}

	err = m.communitiesManager.DeleteCommunitySettings(communityID)
	if err != nil {
		return nil, err
	}

	m.archiveManager.StopHistoryArchiveTasksInterval(communityID)

	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	if !community.IsControlNode() {
		requestToLeaveProto := &protobuf.CommunityRequestToLeave{
			Clock:       uint64(time.Now().Unix()),
			CommunityId: communityID,
		}

		payload, err := proto.Marshal(requestToLeaveProto)
		if err != nil {
			return nil, err
		}

		community, err := m.communitiesManager.GetByID(communityID)
		if err != nil {
			return nil, err
		}

		rawMessage := common.RawMessage{
			Payload:             payload,
			CommunityID:         communityID,
			SkipEncryptionLayer: true,
			MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_REQUEST_TO_LEAVE,
			PubsubTopic:         community.PubsubTopic(), // TODO: confirm if it should be sent in the community pubsub topic
			ResendType:          common.ResendTypeRawMessage,
			Priority:            &common.HighPriority,
		}

		_, err = m.SendMessageToControlNode(community, &rawMessage)
		if err != nil {
			return nil, err
		}

		if _, err = m.AddRawMessageToWatch(&rawMessage); err != nil {
			return nil, err
		}
	}

	return mr, nil
}

func (m *Messenger) leaveCommunity(communityID types.HexBytes) (*MessengerResponse, error) {
	response := &MessengerResponse{}

	community, err := m.communitiesManager.LeaveCommunity(communityID)
	if err != nil {
		return nil, err
	}

	// Make chat inactive
	for chatID := range community.Chats() {
		communityChatID := communityID.String() + chatID
		response.AddRemovedChat(communityChatID)

		_, err = m.deactivateChat(communityChatID, 0, false, false)
		if err != nil {
			return nil, err
		}
		_, err = m.transport.RemoveFilterByChatID(communityChatID)
		if err != nil {
			return nil, err
		}
	}

	err = m.DeleteProfileShowcaseCommunity(community)
	if err != nil {
		return nil, err
	}

	_, err = m.transport.RemoveFilterByChatID(communityID.String())
	if err != nil {
		return nil, err
	}

	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) kickedOutOfCommunity(communityID types.HexBytes, spectateMode bool) (*MessengerResponse, error) {
	response := &MessengerResponse{}

	community, err := m.communitiesManager.KickedOutOfCommunity(communityID, spectateMode)
	if err != nil {
		return nil, err
	}

	if !spectateMode {
		err = m.DeleteProfileShowcaseCommunity(community)
		if err != nil {
			return nil, err
		}
	}

	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) CheckAndDeletePendingRequestToJoinCommunity(ctx context.Context, sendResponse bool) (*MessengerResponse, error) {
	sendSignal := false

	pendingRequestsToJoin, err := m.communitiesManager.PendingRequestsToJoin()
	if err != nil {
		m.logger.Error("failed to fetch pending request to join", zap.Error(err))
		return nil, err
	}

	if len(pendingRequestsToJoin) == 0 {
		return nil, nil
	}

	response := &MessengerResponse{}
	timeNow := uint64(time.Now().Unix())

	for _, requestToJoin := range pendingRequestsToJoin {
		requestTimeOutClock, err := communities.AddTimeoutToRequestToJoinClock(requestToJoin.Clock)
		if err != nil {
			return nil, err
		}

		if timeNow >= requestTimeOutClock {
			err := m.communitiesManager.DeletePendingRequestToJoin(requestToJoin)
			if err != nil {
				m.logger.Error("failed to delete pending request to join", zap.String("req-id", requestToJoin.ID.String()), zap.Error(err))
				return nil, err
			}

			requestToJoin.Deleted = true
			response.AddRequestToJoinCommunity(requestToJoin)

			notification, err := m.persistence.GetActivityCenterNotificationByID(requestToJoin.ID)
			if err != nil {
				m.logger.Error("failed to fetch pending request to join", zap.Error(err))
				return nil, err
			}

			if notification != nil {
				// Delete activity centre notification for community admin
				if notification.Type == ActivityCenterNotificationTypeCommunityMembershipRequest {
					response2, err := m.MarkActivityCenterNotificationsDeleted(ctx, []types.HexBytes{notification.ID}, m.GetCurrentTimeInMillis(), true)
					if err != nil {
						m.logger.Error("[CheckAndDeletePendingRequestToJoinCommunity] failed to mark notification as deleted", zap.Error(err))
						return nil, err
					}
					response.AddActivityCenterNotifications(response2.ActivityCenterNotifications())
					response.SetActivityCenterState(response2.ActivityCenterState())
				}
				// Update activity centre notification for requester
				if notification.Type == ActivityCenterNotificationTypeCommunityRequest {
					notification.MembershipStatus = ActivityCenterMembershipStatusIdle
					notification.Read = false
					notification.Deleted = false
					notification.IncrementUpdatedAt(m.getTimesource())
					err = m.addActivityCenterNotification(response, notification, m.syncActivityCenterUnreadByIDs)
					if err != nil {
						m.logger.Error("failed to update notification in activity center", zap.Error(err))
						return nil, err
					}
				}
			}

			sendSignal = true
		}
	}

	if sendSignal && !sendResponse {
		signal.SendNewMessages(response)
	}

	if sendResponse {
		return response, nil
	}

	return nil, nil
}

func (m *Messenger) CreateCommunityChat(communityID types.HexBytes, c *protobuf.CommunityChat) (*MessengerResponse, error) {
	var response MessengerResponse

	c.Identity.FirstMessageTimestamp = FirstMessageTimestampNoMessage
	changes, err := m.communitiesManager.CreateChat(communityID, c, true, "")
	if err != nil {
		return nil, err
	}
	response.AddCommunity(changes.Community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	var chats []*Chat
	var publicFiltersToInit []transport.FiltersToInitialize
	for chatID, chat := range changes.ChatsAdded {
		c := CreateCommunityChat(changes.Community.IDString(), chatID, chat, m.getTimesource())
		chats = append(chats, c)
		publicFiltersToInit = append(publicFiltersToInit, transport.FiltersToInitialize{ChatID: c.ID, PubsubTopic: changes.Community.PubsubTopic()})

		response.AddChat(c)
	}

	// Load filters
	filters, err := m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		return nil, err
	}
	_, err = m.scheduleSyncFilters(filters)
	if err != nil {
		return nil, err
	}

	err = m.saveChats(chats)
	if err != nil {
		return nil, err
	}

	err = m.reregisterForPushNotifications()
	if err != nil {
		return nil, err
	}

	return &response, nil
}

func (m *Messenger) EditCommunityChat(communityID types.HexBytes, chatID string, c *protobuf.CommunityChat) (*MessengerResponse, error) {
	var response MessengerResponse
	community, changes, err := m.communitiesManager.EditChat(communityID, chatID, c)
	if err != nil {
		return nil, err
	}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	var chats []*Chat
	var publicFiltersToInit []transport.FiltersToInitialize
	for chatID, change := range changes.ChatsModified {
		c := CreateCommunityChat(community.IDString(), chatID, change.ChatModified, m.getTimesource())
		chats = append(chats, c)
		publicFiltersToInit = append(publicFiltersToInit, transport.FiltersToInitialize{ChatID: c.ID, PubsubTopic: community.PubsubTopic()})
		response.AddChat(c)
	}

	// Load filters
	filters, err := m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		return nil, err
	}
	_, err = m.scheduleSyncFilters(filters)
	if err != nil {
		return nil, err
	}

	return &response, m.saveChats(chats)
}

func (m *Messenger) DeleteCommunityChat(communityID types.HexBytes, chatID string) (*MessengerResponse, error) {
	response := &MessengerResponse{}

	community, _, err := m.communitiesManager.DeleteChat(communityID, chatID)
	if err != nil {
		return nil, err
	}
	err = m.deleteChat(chatID)
	if err != nil {
		return nil, err
	}
	response.AddRemovedChat(chatID)

	_, err = m.transport.RemoveFilterByChatID(chatID)
	if err != nil {
		return nil, err
	}

	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) InitCommunityFilters(communityFiltersToInitialize []transport.CommunityFilterToInitialize) ([]*transport.Filter, error) {
	return m.transport.InitCommunityFilters(communityFiltersToInitialize)
}

func (m *Messenger) DefaultFilters(o *communities.Community) []transport.FiltersToInitialize {
	cID := o.IDString()
	uncompressedPubKey := common.PubkeyToHex(o.PublicKey())[2:]
	updatesChannelID := o.StatusUpdatesChannelID()
	mlChannelID := o.MagnetlinkMessageChannelID()
	memberUpdateChannelID := o.MemberUpdateChannelID()

	communityPubsubTopic := o.PubsubTopic()

	filters := []transport.FiltersToInitialize{
		{ChatID: cID, PubsubTopic: communityPubsubTopic},
		{ChatID: updatesChannelID, PubsubTopic: communityPubsubTopic},
		{ChatID: mlChannelID, PubsubTopic: communityPubsubTopic},
		{ChatID: memberUpdateChannelID, PubsubTopic: communityPubsubTopic},
		{ChatID: uncompressedPubKey, PubsubTopic: shard.DefaultNonProtectedPubsubTopic()},
	}

	return filters
}

func (m *Messenger) CreateCommunity(request *requests.CreateCommunity, createDefaultChannel bool) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	response := &MessengerResponse{}

	community, err := m.communitiesManager.CreateCommunity(request, true)
	if err != nil {
		return nil, err
	}

	communitySettings := communities.CommunitySettings{
		CommunityID:                  community.IDString(),
		HistoryArchiveSupportEnabled: request.HistoryArchiveSupportEnabled,
	}
	err = m.communitiesManager.SaveCommunitySettings(communitySettings)
	if err != nil {
		return nil, err
	}

	if err = m.subscribeToCommunityShard(community.ID(), community.Shard()); err != nil {
		return nil, err
	}

	// Init the community filter so we can receive messages on the community
	_, err = m.InitCommunityFilters([]transport.CommunityFilterToInitialize{{
		Shard:   community.Shard(),
		PrivKey: community.PrivateKey(),
	}})
	if err != nil {
		return nil, err
	}

	// Init the default community filters
	_, err = m.transport.InitPublicFilters(m.DefaultFilters(community))
	if err != nil {
		return nil, err
	}

	if createDefaultChannel {
		chatResponse, err := m.CreateCommunityChat(community.ID(), &protobuf.CommunityChat{
			Identity: &protobuf.ChatIdentity{
				DisplayName:           "general",
				Description:           "General channel for the community",
				Color:                 community.Description().Identity.Color,
				FirstMessageTimestamp: FirstMessageTimestampNoMessage,
			},
			Permissions: &protobuf.CommunityPermissions{
				Access: protobuf.CommunityPermissions_AUTO_ACCEPT,
			},
		})
		if err != nil {
			return nil, err
		}

		// updating community so it contains the general chat
		community = chatResponse.Communities()[0]
		response.AddChat(chatResponse.Chats()[0])
	}

	response.AddCommunity(community)
	response.AddCommunitySettings(&communitySettings)
	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	if m.config.torrentConfig != nil && m.config.torrentConfig.Enabled && communitySettings.HistoryArchiveSupportEnabled {
		go m.archiveManager.StartHistoryArchiveTasksInterval(community, messageArchiveInterval)
	}

	return response, nil
}

func (m *Messenger) SetCommunityShard(request *requests.SetCommunityShard) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, err := m.communitiesManager.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	if !community.IsControlNode() {
		return nil, errors.New(ErrNotAdminOrOwner)
	}

	// Reset the community private key
	community.SetPubsubTopicPrivateKey(nil)

	// Removing the private key (if it exist)
	err = m.RemovePubsubTopicPrivateKey(community.PubsubTopic())
	if err != nil {
		return nil, err
	}

	// Unsubscribing from existing shard
	if community.Shard() != nil {
		err := m.unsubscribeFromShard(community.Shard())
		if err != nil {
			return nil, err
		}
	}

	community, err = m.communitiesManager.SetShard(request.CommunityID, request.Shard)
	if err != nil {
		return nil, err
	}

	if request.Shard != nil {
		var topicPrivKey *ecdsa.PrivateKey
		if request.PrivateKey != nil {
			topicPrivKey, err = crypto.ToECDSA(*request.PrivateKey)
		} else {
			topicPrivKey, err = crypto.GenerateKey()
		}
		if err != nil {
			return nil, err
		}

		community.SetPubsubTopicPrivateKey(topicPrivKey)

		err = m.communitiesManager.UpdatePubsubTopicPrivateKey(community.PubsubTopic(), topicPrivKey)
		if err != nil {
			return nil, err
		}
	}

	// TODO: Check
	err = m.UpdateCommunityFilters(community)
	if err != nil {
		return nil, err
	}

	err = m.SendCommunityShardKey(community, community.GetMemberPubkeys())
	if err != nil {
		return nil, err
	}

	err = m.sendPublicCommunityShardInfo(community)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)

	return response, nil
}

func (m *Messenger) RemovePubsubTopicPrivateKey(topic string) error {
	return m.transport.RemovePubsubTopicKey(topic)
}

func (m *Messenger) SetCommunityStorenodes(request *requests.SetCommunityStorenodes) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	community, err := m.communitiesManager.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}
	if !community.IsControlNode() {
		return nil, errors.New(ErrNotAdminOrOwner)
	}

	if err := m.communityStorenodes.UpdateStorenodesInDB(request.CommunityID, request.Storenodes, 0); err != nil {
		return nil, err
	}
	err = m.sendCommunityPublicStorenodesInfo(community, request.Storenodes)
	if err != nil {
		return nil, err
	}
	response := &MessengerResponse{
		CommunityStorenodes: request.Storenodes,
	}
	return response, nil
}

func (m *Messenger) GetCommunityStorenodes(communityID types.HexBytes) (*MessengerResponse, error) {
	community, err := m.communitiesManager.GetByID(communityID)
	if err != nil {
		return nil, err
	}
	if community == nil {
		return nil, communities.ErrOrgNotFound
	}

	snodes, err := m.communityStorenodes.GetStorenodesFromDB(communityID)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{
		CommunityStorenodes: snodes,
	}
	return response, nil
}

func (m *Messenger) UpdateCommunityFilters(community *communities.Community) error {
	defaultFilters := m.DefaultFilters(community)
	publicFiltersToInit := make([]transport.FiltersToInitialize, 0, len(defaultFilters)+len(community.Chats()))

	publicFiltersToInit = append(publicFiltersToInit, defaultFilters...)

	for chatID := range community.Chats() {
		communityChatID := community.IDString() + chatID
		_, err := m.transport.RemoveFilterByChatID(communityChatID)
		if err != nil {
			return err
		}
		publicFiltersToInit = append(publicFiltersToInit, transport.FiltersToInitialize{ChatID: communityChatID, PubsubTopic: community.PubsubTopic()})
	}

	_, err := m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		return err
	}

	// Init the community filter so we can receive messages on the community
	_, err = m.InitCommunityFilters([]transport.CommunityFilterToInitialize{{
		Shard:   community.Shard(),
		PrivKey: community.PrivateKey(),
	}})
	if err != nil {
		return err
	}

	// Init the default community filters
	_, err = m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		return err
	}

	if err = m.subscribeToCommunityShard(community.ID(), community.Shard()); err != nil {
		return err
	}

	return nil
}

func (m *Messenger) CreateCommunityTokenPermission(request *requests.CreateCommunityTokenPermission) (*MessengerResponse, error) {
	request.FillDeprecatedAmount()

	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, changes, err := m.communitiesManager.CreateCommunityTokenPermission(request)
	if err != nil {
		return nil, err
	}

	if community.IsControlNode() {
		err = m.communitiesManager.ScheduleMembersReevaluation(community.ID())
		if err != nil {
			return nil, err
		}
	}

	// ensure HRkeys are synced
	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return response, nil
}

func (m *Messenger) EditCommunityTokenPermission(request *requests.EditCommunityTokenPermission) (*MessengerResponse, error) {
	request.FillDeprecatedAmount()

	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, changes, err := m.communitiesManager.EditCommunityTokenPermission(request)
	if err != nil {
		return nil, err
	}

	// check if members still fulfill the token criteria of all
	// BECOME_MEMBER permissions and kick them if necessary
	//
	// We do this in a separate routine to not block this function
	if community.IsControlNode() {
		err = m.communitiesManager.ScheduleMembersReevaluation(community.ID())
		if err != nil {
			return nil, err
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return response, nil
}

func (m *Messenger) DeleteCommunityTokenPermission(request *requests.DeleteCommunityTokenPermission) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, changes, err := m.communitiesManager.DeleteCommunityTokenPermission(request)
	if err != nil {
		return nil, err
	}

	// check if members still fulfill the token criteria
	// We do this in a separate routine to not block this function
	if community.IsControlNode() {
		err = m.communitiesManager.ScheduleMembersReevaluation(community.ID())
		if err != nil {
			return nil, err
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	return response, nil
}

func (m *Messenger) HandleCommunityReevaluatePermissionsRequest(state *ReceivedMessageState, request *protobuf.CommunityReevaluatePermissionsRequest, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(request.CommunityId)
	if err != nil {
		return err
	}

	if !community.IsControlNode() {
		return communities.ErrNotControlNode
	}

	if !community.IsMemberTokenMaster(statusMessage.SigPubKey()) {
		return communities.ErrNotAuthorized
	}

	return m.communitiesManager.ScheduleMembersReevaluation(request.CommunityId)
}

func (m *Messenger) ReevaluateCommunityMembersPermissions(request *requests.ReevaluateCommunityMembersPermissions) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, err := m.communitiesManager.GetByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	if community.IsControlNode() {
		err = m.communitiesManager.ScheduleMembersReevaluation(request.CommunityID)
		if err != nil {
			return nil, err
		}
	} else if community.IsTokenMaster() {
		reevaluateRequest := &protobuf.CommunityReevaluatePermissionsRequest{
			CommunityId: request.CommunityID,
		}

		encodedMessage, err := proto.Marshal(reevaluateRequest)
		if err != nil {
			return nil, err
		}

		rawMessage := common.RawMessage{
			Payload:             encodedMessage,
			CommunityID:         request.CommunityID,
			SkipEncryptionLayer: true,
			MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_REEVALUATE_PERMISSIONS_REQUEST,
			PubsubTopic:         community.PubsubTopic(),
		}
		_, err = m.SendMessageToControlNode(community, &rawMessage)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, communities.ErrNotAuthorized
	}

	return &MessengerResponse{}, nil
}

func (m *Messenger) EditCommunity(request *requests.EditCommunity) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, err := m.communitiesManager.EditCommunity(request)
	if err != nil {
		return nil, err
	}

	communitySettings := communities.CommunitySettings{
		CommunityID:                  community.IDString(),
		HistoryArchiveSupportEnabled: request.HistoryArchiveSupportEnabled,
	}
	err = m.communitiesManager.UpdateCommunitySettings(communitySettings)
	if err != nil {
		return nil, err
	}

	id := community.ID()

	if m.archiveManager.IsReady() {
		if !communitySettings.HistoryArchiveSupportEnabled {
			m.archiveManager.StopHistoryArchiveTasksInterval(id)
		} else if !m.archiveManager.IsSeedingHistoryArchiveTorrent(id) {
			var communities []*communities.Community
			communities = append(communities, community)
			go m.InitHistoryArchiveTasks(communities)
		}
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	response.AddCommunitySettings(&communitySettings)
	err = m.SyncCommunitySettings(context.Background(), &communitySettings)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func (m *Messenger) RemovePrivateKey(id types.HexBytes) (*MessengerResponse, error) {
	community, err := m.communitiesManager.RemovePrivateKey(id)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)

	return response, nil
}

func (m *Messenger) ExportCommunity(id types.HexBytes) (*ecdsa.PrivateKey, error) {
	return m.communitiesManager.ExportCommunity(id)
}

func (m *Messenger) ImportCommunity(ctx context.Context, key *ecdsa.PrivateKey) (*MessengerResponse, error) {
	clock, _ := m.getLastClockWithRelatedChat()

	community, err := m.communitiesManager.ImportCommunity(key, clock)
	if err != nil {
		return nil, err
	}

	// Load filters
	_, err = m.transport.InitPublicFilters(m.DefaultFilters(community))
	if err != nil {
		return nil, err
	}

	if err != nil {
		return nil, err
	}

	_, err = m.FetchCommunity(&FetchCommunityRequest{
		CommunityKey:    community.IDString(),
		Shard:           community.Shard(),
		TryDatabase:     false,
		WaitForResponse: true,
	})
	if err != nil {
		// TODO In the future we should add a mechanism to re-apply next steps (adding owner, joining)
		// if there is no connection with mailserver. Otherwise changes will be overwritten.
		// Do not return error to make tests pass.
		m.logger.Error("Can't request community info from mailserver")
	}

	// We add ourselves
	community, err = m.communitiesManager.AddMemberOwnerToCommunity(community.ID(), &m.identity.PublicKey)
	if err != nil {
		return nil, err
	}

	response, err := m.JoinCommunity(ctx, community.ID(), true)
	if err != nil {
		return nil, err
	}

	// Notify other clients we are the control node now
	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	if m.archiveManager.IsReady() {
		var communities []*communities.Community
		communities = append(communities, community)
		go m.InitHistoryArchiveTasks(communities)
	}
	return response, nil
}

func (m *Messenger) GetCommunityByID(communityID types.HexBytes) (*communities.Community, error) {
	return m.communitiesManager.GetByID(communityID)
}

func (m *Messenger) ShareCommunity(request *requests.ShareCommunity) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	community, err := m.GetCommunityByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	communityURL, err := m.ShareCommunityURLWithData(request.CommunityID)
	if err != nil {
		return nil, err
	}

	var statusLinkPreview common.StatusLinkPreview
	statusCommunityLinkPreview, err := community.ToStatusLinkPreview()
	if err != nil {
		return nil, err
	}

	statusLinkPreview.URL = communityURL
	statusLinkPreview.Community = statusCommunityLinkPreview
	var messages []*common.Message
	for _, pk := range request.Users {
		message := common.NewMessage()
		message.StatusLinkPreviews = []common.StatusLinkPreview{statusLinkPreview}
		message.ChatId = pk.String()
		message.Shard = community.Shard().Protobuffer()
		message.ContentType = protobuf.ChatMessage_TEXT_PLAIN
		message.Text = communityURL
		if request.InviteMessage != "" {
			message.Text = fmt.Sprintf("%s\n%s", request.InviteMessage, communityURL)
		}
		messages = append(messages, message)
		r, err := m.CreateOneToOneChat(&requests.CreateOneToOneChat{ID: pk})
		if err != nil {
			return nil, err
		}

		if err := response.Merge(r); err != nil {
			return nil, err
		}
	}

	sendMessagesResponse, err := m.SendChatMessages(context.Background(), messages)
	if err != nil {
		return nil, err
	}

	if err := response.Merge(sendMessagesResponse); err != nil {
		return nil, err
	}

	return response, nil
}

func (m *Messenger) MyCanceledRequestsToJoin() ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.CanceledRequestsToJoinForUser(&m.identity.PublicKey)
}

func (m *Messenger) MyPendingRequestsToJoin() ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.PendingRequestsToJoinForUser(&m.identity.PublicKey)
}

func (m *Messenger) LatestRequestToJoinForCommunity(communityID types.HexBytes) (*communities.RequestToJoin, error) {
	return m.communitiesManager.GetCommunityRequestToJoinWithRevealedAddresses(m.myHexIdentity(), communityID)
}

func (m *Messenger) PendingRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.PendingRequestsToJoinForCommunity(id)
}

func (m *Messenger) DeclinedRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.DeclinedRequestsToJoinForCommunity(id)
}

func (m *Messenger) CanceledRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.CanceledRequestsToJoinForCommunity(id)
}

func (m *Messenger) AcceptedRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.AcceptedRequestsToJoinForCommunity(id)
}

func (m *Messenger) AcceptedPendingRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.AcceptedPendingRequestsToJoinForCommunity(id)
}

func (m *Messenger) DeclinedPendingRequestsToJoinForCommunity(id types.HexBytes) ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.DeclinedPendingRequestsToJoinForCommunity(id)
}

func (m *Messenger) AllNonApprovedCommunitiesRequestsToJoin() ([]*communities.RequestToJoin, error) {
	return m.communitiesManager.AllNonApprovedCommunitiesRequestsToJoin()
}

func (m *Messenger) RemoveUserFromCommunity(id types.HexBytes, pkString string) (*MessengerResponse, error) {
	publicKey, err := common.HexToPubkey(pkString)
	if err != nil {
		return nil, err
	}

	community, err := m.communitiesManager.RemoveUserFromCommunity(id, publicKey)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) SendCommunityShardKey(community *communities.Community, pubkeys []*ecdsa.PublicKey) error {
	if m.transport.WakuVersion() != 2 {
		return nil
	}

	if !community.IsControlNode() {
		return nil
	}

	keyBytes := make([]byte, 0)
	key := community.PubsubTopicPrivateKey()
	if key != nil {
		keyBytes = crypto.FromECDSA(key)
	}

	communityShardKey := &protobuf.CommunityShardKey{
		Clock:       community.Clock(),
		CommunityId: community.ID(),
		PrivateKey:  keyBytes,
		Shard:       community.Shard().Protobuffer(),
	}

	encodedMessage, err := proto.Marshal(communityShardKey)
	if err != nil {
		return err
	}

	rawMessage := common.RawMessage{
		Recipients:  pubkeys,
		ResendType:  common.ResendTypeDataSync,
		MessageType: protobuf.ApplicationMetadataMessage_COMMUNITY_SHARD_KEY,
		Payload:     encodedMessage,
	}

	_, err = m.sender.SendPubsubTopicKey(context.Background(), &rawMessage)

	return err
}

func (m *Messenger) UnbanUserFromCommunity(request *requests.UnbanUserFromCommunity) (*MessengerResponse, error) {
	community, err := m.communitiesManager.UnbanUserFromCommunity(request)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) BanUserFromCommunity(ctx context.Context, request *requests.BanUserFromCommunity) (*MessengerResponse, error) {
	community, err := m.communitiesManager.BanUserFromCommunity(request)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response, err = m.DeclineAllPendingGroupInvitesFromUser(ctx, response, request.User.String())
	if err != nil {
		return nil, err
	}

	response.AddCommunity(community)

	if request.DeleteAllMessages && community.IsControlNode() {
		deleteMessagesResponse, err := m.deleteCommunityMemberMessages(request.User.String(), request.CommunityID.String(), []*protobuf.DeleteCommunityMemberMessage{})
		if err != nil {
			return nil, err
		}

		err = response.Merge(deleteMessagesResponse)
		if err != nil {
			return nil, err
		}

		// signal client with community and messages changes
		if m.config.messengerSignalsHandler != nil {
			m.config.messengerSignalsHandler.MessengerResponse(deleteMessagesResponse)
		}
	}

	return response, nil
}

func (m *Messenger) AddRoleToMember(request *requests.AddRoleToMember) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	community, err := m.communitiesManager.AddRoleToMember(request)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) RemoveRoleFromMember(request *requests.RemoveRoleFromMember) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	community, err := m.communitiesManager.RemoveRoleFromMember(request)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}
	response.AddCommunity(community)
	return response, nil
}

func (m *Messenger) FindCommunityInfoFromDB(communityID string) (*communities.Community, error) {
	id, err := hexutil.Decode(communityID)
	if err != nil {
		return nil, err
	}

	var community *communities.Community
	community, err = m.GetCommunityByID(id)
	if err != nil {
		return nil, err
	}
	return community, nil
}

// FetchCommunity installs filter for community and requests its details
// from mailserver.
//
// If `request.TryDatabase` is true, it first looks for community in database,
// and requests from mailserver only if it wasn't found locally.
// If `request.WaitForResponse` is true, it waits until it has the community before returning it.
// If `request.WaitForResponse` is false, it installs filter for community and requests its details
// from mailserver. When response received it will be passed through signals handler.
func (m *Messenger) FetchCommunity(request *FetchCommunityRequest) (*communities.Community, error) {
	if err := request.Validate(); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	communityID := request.getCommunityID()

	if request.TryDatabase {
		community, err := m.FindCommunityInfoFromDB(communityID)
		if err != nil && err != communities.ErrOrgNotFound {
			return nil, err
		}
		if community != nil {
			if !request.WaitForResponse {
				m.config.messengerSignalsHandler.CommunityInfoFound(community)
			}
			return community, nil
		}
	}

	communityAddress := communities.CommunityShard{
		CommunityID: communityID,
		Shard:       request.Shard,
	}

	options := []StoreNodeRequestOption{
		WithWaitForResponseOption(request.WaitForResponse),
	}

	community, _, err := m.storeNodeRequestsManager.FetchCommunity(communityAddress, options)

	return community, err
}

// fetchCommunities installs filter for community and requests its details from store node.
// When response received it will be passed through signals handler.
func (m *Messenger) fetchCommunities(communities []communities.CommunityShard) error {
	return m.storeNodeRequestsManager.FetchCommunities(communities, []StoreNodeRequestOption{})
}

// passStoredCommunityInfoToSignalHandler calls signal handler with community info
func (m *Messenger) passStoredCommunityInfoToSignalHandler(community *communities.Community) {
	if m.config.messengerSignalsHandler == nil {
		return
	}
	m.config.messengerSignalsHandler.CommunityInfoFound(community)
}

// handleCommunityDescription handles an community description
func (m *Messenger) handleCommunityDescription(state *ReceivedMessageState, signer *ecdsa.PublicKey, description *protobuf.CommunityDescription, rawPayload []byte, verifiedOwner *ecdsa.PublicKey, shard *protobuf.Shard) error {
	communityResponse, err := m.communitiesManager.HandleCommunityDescriptionMessage(signer, description, rawPayload, verifiedOwner, shard)
	if err != nil {
		return err
	}

	// If response is nil, but not error, it will be processed async
	if communityResponse == nil {
		return nil
	}

	if len(communityResponse.FailedToDecrypt) != 0 {
		for _, r := range communityResponse.FailedToDecrypt {
			if state.CurrentMessageState != nil && state.CurrentMessageState.StatusMessage != nil {
				err := m.persistence.SaveHashRatchetMessage(r.GroupID, r.KeyID, state.CurrentMessageState.StatusMessage.TransportLayer.Message)
				m.logger.Info("saving failed to decrypt community description", zap.String("hash", types.Bytes2Hex(state.CurrentMessageState.StatusMessage.TransportLayer.Message.Hash)))
				if err != nil {
					m.logger.Warn("failed to save waku message")
				}
			}

		}
		// We stop here if we could not decrypt the community main metadata
		if communityResponse.Community == nil {
			return nil
		}
	}

	return m.handleCommunityResponse(state, communityResponse)
}

func (m *Messenger) handleCommunityResponse(state *ReceivedMessageState, communityResponse *communities.CommunityResponse) error {
	community := communityResponse.Community

	if len(communityResponse.Changes.MembersBanned) > 0 {
		for memberID, deleteAllMessages := range communityResponse.Changes.MembersBanned {
			if deleteAllMessages {
				response, err := m.deleteCommunityMemberMessages(memberID, community.IDString(), []*protobuf.DeleteCommunityMemberMessage{})
				if err != nil {
					return err
				}

				if err = state.Response.Merge(response); err != nil {
					return err
				}
			}
		}
	}

	state.Response.AddCommunity(community)
	state.Response.CommunityChanges = append(state.Response.CommunityChanges, communityResponse.Changes)
	state.Response.AddRequestsToJoinCommunity(communityResponse.RequestsToJoin)

	// If we haven't joined/spectated the org, nothing to do
	if !community.Joined() && !community.Spectated() {
		return nil
	}

	removedChatIDs := make([]string, 0)
	for id := range communityResponse.Changes.ChatsRemoved {
		chatID := community.ChatID(id)
		_, ok := state.AllChats.Load(chatID)
		if ok {
			removedChatIDs = append(removedChatIDs, chatID)
			state.AllChats.Delete(chatID)
			err := m.DeleteChat(chatID)
			if err != nil {
				m.logger.Error("couldn't delete chat", zap.Error(err))
			}
		}
	}

	// Check if we have been removed from a chat (ie no longer have access)
	for channelID, changes := range communityResponse.Changes.ChatsModified {
		if _, ok := changes.MembersRemoved[common.PubkeyToHex(&m.identity.PublicKey)]; ok {
			chatID := community.ChatID(channelID)

			if chat, ok := state.AllChats.Load(chatID); ok {
				// Reset the chat's message counts
				chat.UnviewedMessagesCount = 0
				chat.UnviewedMentionsCount = 0
				err := m.saveChat(chat)
				if err != nil {
					return err
				}
				state.Response.AddChat(chat)
			}
		}
	}

	// Update relevant chats names and add new ones
	// Currently removal is not supported
	chats := CreateCommunityChats(community, state.Timesource)
	var publicFiltersToInit []transport.FiltersToInitialize
	for i, chat := range chats {

		oldChat, ok := state.AllChats.Load(chat.ID)
		if !ok {
			// Beware, don't use the reference in the range (i.e chat) as it's a shallow copy
			state.AllChats.Store(chat.ID, chats[i])

			state.Response.AddChat(chat)
			publicFiltersToInit = append(publicFiltersToInit, transport.FiltersToInitialize{
				ChatID:      chat.ID,
				PubsubTopic: community.PubsubTopic(),
			})
			// Update name, currently is the only field is mutable
		} else if oldChat.Name != chat.Name ||
			oldChat.Description != chat.Description ||
			oldChat.Emoji != chat.Emoji ||
			oldChat.Color != chat.Color ||
			oldChat.HideIfPermissionsNotMet != chat.HideIfPermissionsNotMet ||
			oldChat.UpdateFirstMessageTimestamp(chat.FirstMessageTimestamp) {
			oldChat.Name = chat.Name
			oldChat.Description = chat.Description
			oldChat.Emoji = chat.Emoji
			oldChat.Color = chat.Color
			oldChat.HideIfPermissionsNotMet = chat.HideIfPermissionsNotMet
			// TODO(samyoul) remove storing of an updated reference pointer?
			state.AllChats.Store(chat.ID, oldChat)
			state.Response.AddChat(chat)
		}
	}

	for _, chatID := range removedChatIDs {
		_, err := m.transport.RemoveFilterByChatID(chatID)
		if err != nil {
			m.logger.Error("couldn't remove filter", zap.Error(err))
		}
	}

	// Load transport filters
	filters, err := m.transport.InitPublicFilters(publicFiltersToInit)
	if err != nil {
		return err
	}
	_, err = m.scheduleSyncFilters(filters)
	if err != nil {
		return err
	}

	for _, requestToJoin := range communityResponse.RequestsToJoin {
		// Activity Center notification
		notification, err := m.persistence.GetActivityCenterNotificationByID(requestToJoin.ID)
		if err != nil {
			return err
		}

		if notification != nil {
			notification.MembershipStatus = ActivityCenterMembershipStatusAccepted
			switch requestToJoin.State {
			case communities.RequestToJoinStateDeclined:
				notification.MembershipStatus = ActivityCenterMembershipStatusDeclined
			case communities.RequestToJoinStateAccepted:
				notification.MembershipStatus = ActivityCenterMembershipStatusAccepted
			case communities.RequestToJoinStateAcceptedPending:
				notification.MembershipStatus = ActivityCenterMembershipStatusAcceptedPending
			case communities.RequestToJoinStateDeclinedPending:
				notification.MembershipStatus = ActivityCenterMembershipStatusDeclinedPending
			case communities.RequestToJoinStateAwaitingAddresses:
				notification.MembershipStatus = ActivityCenterMembershipOwnershipChanged
			default:
				notification.MembershipStatus = ActivityCenterMembershipStatusPending

			}

			notification.Read = true
			notification.Accepted = true
			notification.IncrementUpdatedAt(m.getTimesource())

			err = m.addActivityCenterNotification(state.Response, notification, nil)
			if err != nil {
				m.logger.Error("failed to save notification", zap.Error(err))
				return err
			}
		}
	}

	return nil
}

func (m *Messenger) HandleCommunityUserKicked(state *ReceivedMessageState, message *protobuf.CommunityUserKicked, statusMessage *v1protocol.StatusMessage) error {
	// TODO: validate the user can be removed checking the signer
	if len(message.CommunityId) == 0 {
		return nil
	}
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}
	if community == nil || !community.Joined() {
		return nil
	}
	if community.Clock() > message.Clock {
		return nil
	}

	response, err := m.kickedOutOfCommunity(community.ID(), false)
	if err != nil {
		m.logger.Error("cannot leave community", zap.Error(err))
		return err
	}

	if err := state.Response.Merge(response); err != nil {
		m.logger.Error("cannot merge join community response", zap.Error(err))
		return err
	}

	return nil
}

func (m *Messenger) HandleCommunityEventsMessage(state *ReceivedMessageState, message *protobuf.CommunityEventsMessage, statusMessage *v1protocol.StatusMessage) error {
	signer := state.CurrentMessageState.PublicKey
	communityResponse, err := m.communitiesManager.HandleCommunityEventsMessage(signer, message)
	if err != nil {
		return err
	}

	return m.handleCommunityResponse(state, communityResponse)
}

// HandleCommunityShardKey handles the private keys for the community shards
func (m *Messenger) HandleCommunityShardKey(state *ReceivedMessageState, message *protobuf.CommunityShardKey, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	// If we haven't joined the community, nothing to do
	if !community.Joined() {
		return nil
	}

	signer := state.CurrentMessageState.PublicKey
	if signer == nil {
		return errors.New(ErrReceiverIsNil)
	}

	err = m.handleCommunityShardAndFiltersFromProto(community, message)
	if err != nil {
		return err
	}

	state.Response.AddCommunity(community)

	return nil
}

func (m *Messenger) handleCommunityShardAndFiltersFromProto(community *communities.Community, message *protobuf.CommunityShardKey) error {
	err := m.communitiesManager.UpdateShard(community, shard.FromProtobuff(message.Shard), message.Clock)
	if err != nil {
		return err
	}

	var privKey *ecdsa.PrivateKey = nil
	if message.Shard != nil {
		if message.PrivateKey != nil {
			privKey, err = crypto.ToECDSA(message.PrivateKey)
			if err != nil {
				return err
			}
		}
	}

	// Removing the existing private key (if any)
	err = m.RemovePubsubTopicPrivateKey(community.PubsubTopic())
	if err != nil {
		return err
	}

	// Unsubscribing from existing shard
	if community.Shard() != nil && community.Shard() != shard.FromProtobuff(message.GetShard()) {
		err := m.unsubscribeFromShard(community.Shard())
		if err != nil {
			return err
		}
	}

	community.SetPubsubTopicPrivateKey(privKey)

	err = m.communitiesManager.UpdatePubsubTopicPrivateKey(community.PubsubTopic(), privKey)
	if err != nil {
		return err
	}
	// Update community filters in case of change of shard
	if community.Shard() != shard.FromProtobuff(message.GetShard()) {
		err = m.UpdateCommunityFilters(community)
		if err != nil {
			return err
		}

	}
	return nil
}

func (m *Messenger) handleCommunityPrivilegedUserSyncMessage(state *ReceivedMessageState, signer *ecdsa.PublicKey, message *protobuf.CommunityPrivilegedUserSyncMessage) error {
	if signer == nil {
		return errors.New(ErrSignerIsNil)
	}

	community, err := m.communitiesManager.GetByID(message.CommunityId)
	if err != nil {
		return err
	}

	if community.IsControlNode() {
		return nil
	}

	// Currently this type of msg coming from the control node.
	// If it will change in the future, check that events types starting from
	// CONTROL_NODE were sent by a control node
	isControlNodeMsg := common.IsPubKeyEqual(community.ControlNode(), signer)
	if !isControlNodeMsg {
		return errors.New(ErrSyncMessagesSentByNonControlNode)
	}

	err = m.communitiesManager.ValidateCommunityPrivilegedUserSyncMessage(message)
	if err != nil {
		return err
	}

	switch message.Type {
	case protobuf.CommunityPrivilegedUserSyncMessage_CONTROL_NODE_ACCEPT_REQUEST_TO_JOIN:
		fallthrough
	case protobuf.CommunityPrivilegedUserSyncMessage_CONTROL_NODE_REJECT_REQUEST_TO_JOIN:
		requestsToJoin, err := m.communitiesManager.HandleRequestToJoinPrivilegedUserSyncMessage(message, community)
		if err != nil {
			return err
		}
		state.Response.AddRequestsToJoinCommunity(requestsToJoin)

	case protobuf.CommunityPrivilegedUserSyncMessage_CONTROL_NODE_ALL_SYNC_REQUESTS_TO_JOIN:
		nonAcceptedRequestsToJoin, err := m.communitiesManager.HandleSyncAllRequestToJoinForNewPrivilegedMember(message, community)
		if err != nil {
			return err
		}
		state.Response.AddRequestsToJoinCommunity(nonAcceptedRequestsToJoin)
	case protobuf.CommunityPrivilegedUserSyncMessage_CONTROL_NODE_MEMBER_EDIT_SHARED_ADDRESSES:
		err = m.communitiesManager.HandleEditSharedAddressesPrivilegedUserSyncMessage(message, community)
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *Messenger) HandleCommunityPrivilegedUserSyncMessage(state *ReceivedMessageState, message *protobuf.CommunityPrivilegedUserSyncMessage, statusMessage *v1protocol.StatusMessage) error {
	signer := state.CurrentMessageState.PublicKey
	return m.handleCommunityPrivilegedUserSyncMessage(state, signer, message)
}

func (m *Messenger) sendSharedAddressToControlNode(receiver *ecdsa.PublicKey, community *communities.Community) (*communities.RequestToJoin, error) {
	if receiver == nil {
		return nil, errors.New(ErrReceiverIsNil)
	}

	if community == nil {
		return nil, communities.ErrOrgNotFound
	}

	m.logger.Info("share address to the new owner ", zap.String("community id", community.IDString()))

	pk := common.PubkeyToHex(&m.identity.PublicKey)

	requestToJoin, err := m.communitiesManager.GetCommunityRequestToJoinWithRevealedAddresses(pk, community.ID())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, communities.ErrRevealedAccountsAbsent
		}
		return nil, err
	}

	if len(requestToJoin.RevealedAccounts) == 0 {
		return nil, communities.ErrRevealedAccountsAbsent
	}

	// check if at least one account is signed
	// old community users can not keep locally the signature of their revealed accounts in the DB
	revealedAccountSigned := false
	for _, account := range requestToJoin.RevealedAccounts {
		revealedAccountSigned = len(account.Signature) > 0
		if revealedAccountSigned {
			break
		}
	}

	if !revealedAccountSigned {
		return nil, communities.ErrNoRevealedAccountsSignature
	}

	requestToJoin.Clock = uint64(time.Now().Unix())
	requestToJoin.State = communities.RequestToJoinStateAwaitingAddresses
	payload, err := proto.Marshal(requestToJoin.ToCommunityRequestToJoinProtobuf())
	if err != nil {
		return nil, err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		CommunityID:         community.ID(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_REQUEST_TO_JOIN,
		PubsubTopic:         community.PubsubTopic(), // TODO: confirm if it should be sent in community pubsub topic
		ResendType:          common.ResendTypeRawMessage,
		ResendMethod:        common.ResendMethodSendPrivate,
		Recipients:          []*ecdsa.PublicKey{receiver},
	}

	if err = m.communitiesManager.SaveRequestToJoin(requestToJoin); err != nil {
		return nil, err
	}

	_, err = m.sender.SendPrivate(context.Background(), receiver, &rawMessage)
	if err != nil {
		return nil, err
	}

	_, err = m.AddRawMessageToWatch(&rawMessage)

	return requestToJoin, err
}

func (m *Messenger) HandleSyncInstallationCommunity(messageState *ReceivedMessageState, syncCommunity *protobuf.SyncInstallationCommunity, statusMessage *v1protocol.StatusMessage) error {
	return m.handleSyncInstallationCommunity(messageState, syncCommunity)
}

func (m *Messenger) handleSyncInstallationCommunity(messageState *ReceivedMessageState, syncCommunity *protobuf.SyncInstallationCommunity) error {
	logger := m.logger.Named("handleSyncInstallationCommunity")

	// Should handle community
	shouldHandle, err := m.communitiesManager.ShouldHandleSyncCommunity(syncCommunity)
	if err != nil {
		logger.Debug("m.communitiesManager.ShouldHandleSyncCommunity error", zap.Error(err))
		return err
	}
	logger.Debug("ShouldHandleSyncCommunity result", zap.Bool("shouldHandle", shouldHandle))
	if !shouldHandle {
		return nil
	}

	// Handle deprecated community keys
	if len(syncCommunity.EncryptionKeysV1) != 0 {
		//  We pass nil,nil as private key/public key as they won't be encrypted
		_, err := m.encryptor.HandleHashRatchetKeysPayload(syncCommunity.Id, syncCommunity.EncryptionKeysV1, nil, nil)
		if err != nil {
			return err
		}
	}

	// Handle community and channel keys
	if len(syncCommunity.EncryptionKeysV2) != 0 {
		err := m.encryptor.HandleHashRatchetHeadersPayload(syncCommunity.EncryptionKeysV2)
		if err != nil {
			return err
		}
	}

	// Handle any community requests to join.
	// MUST BE HANDLED BEFORE DESCRIPTION!
	pending := false
	for _, rtj := range syncCommunity.RequestsToJoin {
		req := new(communities.RequestToJoin)
		req.InitFromSyncProtobuf(rtj)

		if req.State == communities.RequestToJoinStatePending {
			pending = true
		}

		err = m.communitiesManager.SaveRequestToJoin(req)
		if err != nil && err != communities.ErrOldRequestToJoin {
			logger.Debug("m.communitiesManager.SaveRequestToJoin error", zap.Error(err))
			return err
		}
	}
	logger.Debug("community requests to join pending state", zap.Bool("pending", pending))

	// Don't use the public key of the private key, uncompress the community id
	orgPubKey, err := crypto.DecompressPubkey(syncCommunity.Id)
	if err != nil {
		logger.Debug("crypto.DecompressPubkey error", zap.Error(err))
		return err
	}
	logger.Debug("crypto.DecompressPubkey result", zap.Any("orgPubKey", orgPubKey))

	var amm protobuf.ApplicationMetadataMessage
	err = proto.Unmarshal(syncCommunity.Description, &amm)
	if err != nil {
		logger.Debug("proto.Unmarshal protobuf.ApplicationMetadataMessage error", zap.Error(err))
		return err
	}

	var cd protobuf.CommunityDescription
	err = proto.Unmarshal(amm.Payload, &cd)
	if err != nil {
		logger.Debug("proto.Unmarshal protobuf.CommunityDescription error", zap.Error(err))
		return err
	}

	// This is our own message, so we can trust the set community owner
	// This is good to do so that we don't have to queue all the actions done after the handled community description.
	// `signer` is `communityID` for a community with no owner token and `owner public key` otherwise
	signer, err := utils.RecoverKey(&amm)
	if err != nil {
		logger.Debug("failed to recover community description signer", zap.Error(err))
		return err
	}

	// Passing shard as nil so that defaultProtected shard 32 is considered
	err = m.handleCommunityDescription(messageState, signer, &cd, syncCommunity.Description, signer, nil)
	// Even if the Description is outdated we should proceed in order to sync settings and joined state
	if err != nil && err != communities.ErrInvalidCommunityDescriptionClockOutdated {
		logger.Debug("m.handleCommunityDescription error", zap.Error(err))
		return err
	}
	descriptionOutdated := err == communities.ErrInvalidCommunityDescriptionClockOutdated

	if syncCommunity.Settings != nil {
		err = m.HandleSyncCommunitySettings(messageState, syncCommunity.Settings, nil)
		if err != nil {
			logger.Debug("m.handleSyncCommunitySettings error", zap.Error(err))
			return err
		}
	}

	if syncCommunity.ControlNode != nil {
		err = m.communitiesManager.SetSyncControlNode(syncCommunity.Id, syncCommunity.ControlNode)
		if err != nil {
			logger.Debug("m.SetSyncControlNode", zap.Error(err))
			return err
		}
	}

	// Handle community last updated
	if syncCommunity.LastOpenedAt > 0 {
		_, err = m.communitiesManager.CommunityUpdateLastOpenedAt(syncCommunity.Id, syncCommunity.LastOpenedAt)
		if err != nil {
			logger.Debug("m.CommunityUpdateLastOpenedAt", zap.Error(err))
			return err
		}
	}

	// if we are not waiting for approval, join or leave the community
	if !pending && !descriptionOutdated {
		var mr *MessengerResponse
		if syncCommunity.Joined {
			mr, err = m.joinCommunity(context.Background(), syncCommunity.Id, false)
			if err != nil && err != communities.ErrOrgAlreadyJoined {
				logger.Debug("m.joinCommunity error", zap.Error(err))
				return err
			}
		} else {
			mr, err = m.leaveCommunity(syncCommunity.Id)
			if err != nil {
				logger.Debug("m.leaveCommunity error", zap.Error(err))
				return err
			}
		}
		if mr != nil {
			err = messageState.Response.Merge(mr)
			if err != nil {
				logger.Debug("messageState.Response.Merge error", zap.Error(err))
				return err
			}
		}
	}

	// update the clock value
	err = m.communitiesManager.SetSyncClock(syncCommunity.Id, syncCommunity.Clock)
	if err != nil {
		logger.Debug("m.communitiesManager.SetSyncClock", zap.Error(err))
		return err
	}

	return nil
}

func (m *Messenger) HandleSyncCommunitySettings(messageState *ReceivedMessageState, syncCommunitySettings *protobuf.SyncCommunitySettings, statusMessage *v1protocol.StatusMessage) error {
	shouldHandle, err := m.communitiesManager.ShouldHandleSyncCommunitySettings(syncCommunitySettings)
	if err != nil {
		m.logger.Debug("m.communitiesManager.ShouldHandleSyncCommunitySettings error", zap.Error(err))
		return err
	}
	m.logger.Debug("ShouldHandleSyncCommunity result", zap.Bool("shouldHandle", shouldHandle))
	if !shouldHandle {
		return nil
	}

	communitySettings, err := m.communitiesManager.HandleSyncCommunitySettings(syncCommunitySettings)
	if err != nil {
		return err
	}

	messageState.Response.AddCommunitySettings(communitySettings)
	return nil
}

func (m *Messenger) InitHistoryArchiveTasks(communities []*communities.Community) {

	m.logger.Debug("initializing history archive tasks")

	for _, c := range communities {

		if c.Joined() {
			settings, err := m.communitiesManager.GetCommunitySettingsByID(c.ID())
			if err != nil {
				m.logger.Error("failed to get community settings", zap.Error(err))
				continue
			}
			if !settings.HistoryArchiveSupportEnabled {
				m.logger.Debug("history archive support disabled for community", zap.String("id", c.IDString()))
				continue
			}

			// Check if there's already a torrent file for this community and seed it
			if m.archiveManager.TorrentFileExists(c.IDString()) {
				err = m.archiveManager.SeedHistoryArchiveTorrent(c.ID())
				if err != nil {
					m.logger.Error("failed to seed history archive", zap.Error(err))
				}
			}

			filters, err := m.archiveManager.GetCommunityChatsFilters(c.ID())
			if err != nil {
				m.logger.Error("failed to get community chats filters for community", zap.Error(err))
				continue
			}

			if len(filters) == 0 {
				m.logger.Debug("no filters or chats for this community starting interval", zap.String("id", c.IDString()))
				go m.archiveManager.StartHistoryArchiveTasksInterval(c, messageArchiveInterval)
				continue
			}

			topics := []types.TopicType{}

			for _, filter := range filters {
				topics = append(topics, filter.ContentTopic)
			}

			// First we need to know the timestamp of the latest waku message
			// we've received for this community, so we can request messages we've
			// possibly missed since then
			latestWakuMessageTimestamp, err := m.communitiesManager.GetLatestWakuMessageTimestamp(topics)
			if err != nil {
				m.logger.Error("failed to get Latest waku message timestamp", zap.Error(err))
				continue
			}

			if latestWakuMessageTimestamp == 0 {
				// This means we don't have any waku messages for this community
				// yet, either because no messages were sent in the community so far,
				// or because messages haven't reached this node
				//
				// In this case we default to requesting messages from the store nodes
				// for the past 30 days
				latestWakuMessageTimestamp = uint64(time.Now().AddDate(0, 0, -30).Unix())
			}

			// Request possibly missed waku messages for community
			ms := m.getActiveMailserver(c.ID().String())
			_, err = m.syncFiltersFrom(*ms, filters, uint32(latestWakuMessageTimestamp))
			if err != nil {
				m.logger.Error("failed to request missing messages", zap.Error(err))
				continue
			}

			// We figure out the end date of the last created archive and schedule
			// the interval for creating future archives
			// If the last end date is at least `interval` ago, we create an archive immediately first
			lastArchiveEndDateTimestamp, err := m.archiveManager.GetHistoryArchivePartitionStartTimestamp(c.ID())
			if err != nil {
				m.logger.Error("failed to get archive partition start timestamp", zap.Error(err))
				continue
			}

			to := time.Now()
			lastArchiveEndDate := time.Unix(int64(lastArchiveEndDateTimestamp), 0)
			durationSinceLastArchive := to.Sub(lastArchiveEndDate)

			if lastArchiveEndDateTimestamp == 0 {
				// No prior messages to be archived, so we just kick off the archive creation loop
				// for future archives
				go m.archiveManager.StartHistoryArchiveTasksInterval(c, messageArchiveInterval)
			} else if durationSinceLastArchive < messageArchiveInterval {
				// Last archive is less than `interval` old, wait until `interval` is complete,
				// then create archive and kick off archive creation loop for future archives
				// Seed current archive in the meantime
				err := m.archiveManager.SeedHistoryArchiveTorrent(c.ID())
				if err != nil {
					m.logger.Error("failed to seed history archive", zap.Error(err))
				}
				timeToNextInterval := messageArchiveInterval - durationSinceLastArchive

				m.logger.Debug("starting history archive tasks interval in", zap.Any("timeLeft", timeToNextInterval))
				time.AfterFunc(timeToNextInterval, func() {
					err := m.archiveManager.CreateAndSeedHistoryArchive(c.ID(), topics, lastArchiveEndDate, to.Add(timeToNextInterval), messageArchiveInterval, c.Encrypted())
					if err != nil {
						m.logger.Error("failed to get create and seed history archive", zap.Error(err))
					}
					go m.archiveManager.StartHistoryArchiveTasksInterval(c, messageArchiveInterval)
				})
			} else {
				// Looks like the last archive was generated more than `interval`
				// ago, so lets create a new archive now and then schedule the archive
				// creation loop
				err := m.archiveManager.CreateAndSeedHistoryArchive(c.ID(), topics, lastArchiveEndDate, to, messageArchiveInterval, c.Encrypted())
				if err != nil {
					m.logger.Error("failed to get create and seed history archive", zap.Error(err))
				}

				go m.archiveManager.StartHistoryArchiveTasksInterval(c, messageArchiveInterval)
			}
		}
	}
}

func (m *Messenger) enableHistoryArchivesImportAfterDelay() {
	go func() {
		time.Sleep(importInitialDelay)
		m.importDelayer.once.Do(func() {
			close(m.importDelayer.wait)
		})
	}()
}

func (m *Messenger) checkIfIMemberOfCommunity(communityID types.HexBytes) error {
	community, err := m.communitiesManager.GetByID(communityID)
	if err != nil {
		m.logger.Error("couldn't get community to import archives", zap.Error(err))
		return err
	}

	if !community.HasMember(&m.identity.PublicKey) {
		m.logger.Error("can't import archives when user not a member of community")
		return ErrUserNotMember
	}

	return nil
}

func (m *Messenger) resumeHistoryArchivesImport(communityID types.HexBytes) error {
	archiveIDsToImport, err := m.archiveManager.GetMessageArchiveIDsToImport(communityID)
	if err != nil {
		return err
	}

	if len(archiveIDsToImport) == 0 {
		return nil
	}

	err = m.checkIfIMemberOfCommunity(communityID)
	if err != nil {
		return err
	}

	currentTask := m.archiveManager.GetHistoryArchiveDownloadTask(communityID.String())
	// no need to resume imports if there's already a task ongoing
	if currentTask != nil {
		return nil
	}

	// Create new task
	task := &communities.HistoryArchiveDownloadTask{
		CancelChan: make(chan struct{}),
		Waiter:     *new(sync.WaitGroup),
		Cancelled:  false,
	}

	m.archiveManager.AddHistoryArchiveDownloadTask(communityID.String(), task)

	// this wait groups tracks the ongoing task for a particular community
	task.Waiter.Add(1)

	go func() {
		defer task.Waiter.Done()
		err := m.importHistoryArchives(communityID, task.CancelChan)
		if err != nil {
			m.logger.Error("failed to import history archives", zap.Error(err))
		}
		m.config.messengerSignalsHandler.DownloadingHistoryArchivesFinished(types.EncodeHex(communityID))
	}()
	return nil
}

func (m *Messenger) SpeedupArchivesImport() {
	m.importRateLimiter.SetLimit(rate.Every(importFastRate))
}

func (m *Messenger) SlowdownArchivesImport() {
	m.importRateLimiter.SetLimit(rate.Every(importSlowRate))
}

func (m *Messenger) importHistoryArchives(communityID types.HexBytes, cancel chan struct{}) error {
	importTicker := time.NewTicker(100 * time.Millisecond)
	defer importTicker.Stop()

	ctx, cancelFunc := context.WithCancel(context.Background())
	go func() {
		<-cancel
		cancelFunc()
	}()

	// don't proceed until initial import delay has passed
	select {
	case <-m.importDelayer.wait:
	case <-ctx.Done():
		return nil
	}

	delayImport := false

importMessageArchivesLoop:
	for {
		if delayImport {
			select {
			case <-ctx.Done():
				m.logger.Debug("interrupted importing history archive messages")
				return nil
			case <-time.After(1 * time.Hour):
				delayImport = false
			}
		}

		select {
		case <-ctx.Done():
			m.logger.Debug("interrupted importing history archive messages")
			return nil
		case <-importTicker.C:
			err := m.checkIfIMemberOfCommunity(communityID)
			if err != nil {
				break importMessageArchivesLoop
			}
			archiveIDsToImport, err := m.archiveManager.GetMessageArchiveIDsToImport(communityID)
			if err != nil {
				m.logger.Error("couldn't get message archive IDs to import", zap.Error(err))
				return err
			}

			if len(archiveIDsToImport) == 0 {
				m.logger.Debug("no message archives to import")
				break importMessageArchivesLoop
			}

			m.logger.Info("importing message archive", zap.Int("left", len(archiveIDsToImport)))

			// only process one archive at a time, so in case of cancel we don't
			// wait for all archives to be processed first
			downloadedArchiveID := archiveIDsToImport[0]

			archiveMessages, err := m.archiveManager.ExtractMessagesFromHistoryArchive(communityID, downloadedArchiveID)
			if err != nil {
				if errors.Is(err, encryption.ErrHashRatchetGroupIDNotFound) {
					// In case we're missing hash ratchet keys, best we can do is
					// to wait for them to be received and try import again.
					delayImport = true
					continue
				}
				m.logger.Error("failed to extract history archive messages", zap.Error(err))
				continue
			}

			m.config.messengerSignalsHandler.ImportingHistoryArchiveMessages(types.EncodeHex(communityID))

			for _, messagesChunk := range chunkSlice(archiveMessages, importMessagesChunkSize) {
				if err := m.importRateLimiter.Wait(ctx); err != nil {
					if !errors.Is(err, context.Canceled) {
						m.logger.Error("rate limiter error when handling archive messages", zap.Error(err))
					}
					continue importMessageArchivesLoop
				}

				response, err := m.handleArchiveMessages(messagesChunk)
				if err != nil {
					m.logger.Error("failed to handle archive messages", zap.Error(err))
					continue importMessageArchivesLoop
				}

				if !response.IsEmpty() {
					notifications := response.Notifications()
					response.ClearNotifications()
					signal.SendNewMessages(response)
					localnotifications.PushMessages(notifications)
				}
			}

			err = m.archiveManager.SetMessageArchiveIDImported(communityID, downloadedArchiveID, true)
			if err != nil {
				m.logger.Error("failed to mark history message archive as imported", zap.Error(err))
				continue
			}
		}
	}
	return nil
}

func (m *Messenger) dispatchMagnetlinkMessage(communityID string) error {

	community, err := m.communitiesManager.GetByIDString(communityID)
	if err != nil {
		return err
	}

	magnetlink, err := m.archiveManager.GetHistoryArchiveMagnetlink(community.ID())
	if err != nil {
		return err
	}

	magnetLinkMessage := &protobuf.CommunityMessageArchiveMagnetlink{
		Clock:     m.getTimesource().GetCurrentTime(),
		MagnetUri: magnetlink,
	}

	encodedMessage, err := proto.Marshal(magnetLinkMessage)
	if err != nil {
		return err
	}

	chatID := community.MagnetlinkMessageChannelID()
	rawMessage := common.RawMessage{
		LocalChatID:          chatID,
		Sender:               community.PrivateKey(),
		Payload:              encodedMessage,
		MessageType:          protobuf.ApplicationMetadataMessage_COMMUNITY_MESSAGE_ARCHIVE_MAGNETLINK,
		SkipGroupMessageWrap: true,
		PubsubTopic:          community.PubsubTopic(),
		Priority:             &common.LowPriority,
	}

	_, err = m.sender.SendPublic(context.Background(), chatID, rawMessage)
	if err != nil {
		return err
	}

	err = m.communitiesManager.UpdateCommunityDescriptionMagnetlinkMessageClock(community.ID(), magnetLinkMessage.Clock)
	if err != nil {
		return err
	}
	return m.communitiesManager.UpdateMagnetlinkMessageClock(community.ID(), magnetLinkMessage.Clock)
}

func (m *Messenger) EnableCommunityHistoryArchiveProtocol() error {
	nodeConfig, err := m.settings.GetNodeConfig()
	if err != nil {
		return err
	}

	if nodeConfig.TorrentConfig.Enabled {
		return nil
	}

	nodeConfig.TorrentConfig.Enabled = true
	err = m.settings.SaveSetting("node-config", nodeConfig)
	if err != nil {
		return err
	}

	m.config.torrentConfig = &nodeConfig.TorrentConfig
	m.archiveManager.SetTorrentConfig(&nodeConfig.TorrentConfig)
	err = m.archiveManager.StartTorrentClient()
	if err != nil {
		return err
	}

	controlledCommunities, err := m.communitiesManager.Controlled()
	if err != nil {
		return err
	}

	if len(controlledCommunities) > 0 {
		go m.InitHistoryArchiveTasks(controlledCommunities)
	}
	if m.config.messengerSignalsHandler != nil {
		m.config.messengerSignalsHandler.HistoryArchivesProtocolEnabled()
	}
	return nil
}

func (m *Messenger) DisableCommunityHistoryArchiveProtocol() error {

	nodeConfig, err := m.settings.GetNodeConfig()
	if err != nil {
		return err
	}
	if !nodeConfig.TorrentConfig.Enabled {
		return nil
	}

	err = m.archiveManager.Stop()
	if err != nil {
		m.logger.Error("failed to stop torrent manager", zap.Error(err))
	}

	nodeConfig.TorrentConfig.Enabled = false
	err = m.settings.SaveSetting("node-config", nodeConfig)
	m.config.torrentConfig = &nodeConfig.TorrentConfig
	m.archiveManager.SetTorrentConfig(&nodeConfig.TorrentConfig)
	if err != nil {
		return err
	}
	if m.config.messengerSignalsHandler != nil {
		m.config.messengerSignalsHandler.HistoryArchivesProtocolDisabled()
	}
	return nil
}

func (m *Messenger) GetCommunitiesSettings() ([]communities.CommunitySettings, error) {
	settings, err := m.communitiesManager.GetCommunitiesSettings()
	if err != nil {
		return nil, err
	}
	return settings, nil
}

func (m *Messenger) SyncCommunitySettings(ctx context.Context, settings *communities.CommunitySettings) error {

	if !m.hasPairedDevices() {
		return nil
	}

	clock, chat := m.getLastClockWithRelatedChat()

	syncMessage := &protobuf.SyncCommunitySettings{
		Clock:                        clock,
		CommunityId:                  settings.CommunityID,
		HistoryArchiveSupportEnabled: settings.HistoryArchiveSupportEnabled,
	}
	encodedMessage, err := proto.Marshal(syncMessage)
	if err != nil {
		return err
	}

	_, err = m.dispatchMessage(ctx, common.RawMessage{
		LocalChatID: chat.ID,
		Payload:     encodedMessage,
		MessageType: protobuf.ApplicationMetadataMessage_SYNC_COMMUNITY_SETTINGS,
		ResendType:  common.ResendTypeDataSync,
	})
	if err != nil {
		return err
	}

	chat.LastClockValue = clock
	return m.saveChat(chat)
}

func (m *Messenger) generateSystemPinnedMessage(pinMessage *common.PinMessage, channel *Chat, clockAndTimestamp uint64, pinnedMessageID string) (*common.Message, *discord.ImportError) {
	id, err := generatePinMessageNotificationID(&m.identity.PublicKey, pinMessage, channel)
	if err != nil {
		m.logger.Warn("failed to generate pin message notification ID",
			zap.String("PinMessageId", pinMessage.ID))
		return nil, discord.Warning(err.Error())
	}
	systemMessage := &common.Message{
		ChatMessage: &protobuf.ChatMessage{
			Clock:       pinMessage.Clock,
			Timestamp:   clockAndTimestamp,
			ChatId:      channel.ID,
			MessageType: pinMessage.MessageType,
			ResponseTo:  pinnedMessageID,
			ContentType: protobuf.ChatMessage_SYSTEM_MESSAGE_PINNED_MESSAGE,
		},
		WhisperTimestamp: clockAndTimestamp,
		ID:               id,
		LocalChatID:      channel.ID,
		From:             pinMessage.From,
		Seen:             true,
	}

	return systemMessage, nil
}

func (m *Messenger) pinMessagesToWakuMessages(pinMessages []*common.PinMessage, c *communities.Community) ([]*types.Message, error) {
	wakuMessages := make([]*types.Message, 0)
	for _, msg := range pinMessages {

		filter := m.transport.FilterByChatID(msg.LocalChatID)
		encodedPayload, err := proto.Marshal(msg.GetProtobuf())
		if err != nil {
			return nil, err
		}
		wrappedPayload, err := v1protocol.WrapMessageV1(encodedPayload, protobuf.ApplicationMetadataMessage_PIN_MESSAGE, c.PrivateKey())
		if err != nil {
			return nil, err
		}

		hash := crypto.Keccak256Hash(append([]byte(c.IDString()), wrappedPayload...))
		wakuMessage := &types.Message{
			Sig:          crypto.FromECDSAPub(&c.PrivateKey().PublicKey),
			Timestamp:    uint32(msg.WhisperTimestamp / 1000),
			Topic:        filter.ContentTopic,
			Payload:      wrappedPayload,
			Padding:      []byte{1},
			Hash:         hash[:],
			ThirdPartyID: msg.ID, // CommunityID + DiscordMessageID
		}
		wakuMessages = append(wakuMessages, wakuMessage)
	}

	return wakuMessages, nil
}

func (m *Messenger) chatMessagesToWakuMessages(chatMessages []*common.Message, c *communities.Community) ([]*types.Message, error) {
	wakuMessages := make([]*types.Message, 0)
	for _, msg := range chatMessages {

		filter := m.transport.FilterByChatID(msg.LocalChatID)
		encodedPayload, err := proto.Marshal(msg.GetProtobuf())
		if err != nil {
			return nil, err
		}

		wrappedPayload, err := v1protocol.WrapMessageV1(encodedPayload, protobuf.ApplicationMetadataMessage_CHAT_MESSAGE, c.PrivateKey())
		if err != nil {
			return nil, err
		}

		hash := crypto.Keccak256Hash([]byte(msg.ID))
		wakuMessage := &types.Message{
			Sig:          crypto.FromECDSAPub(&c.PrivateKey().PublicKey),
			Timestamp:    uint32(msg.WhisperTimestamp / 1000),
			Topic:        filter.ContentTopic,
			Payload:      wrappedPayload,
			Padding:      []byte{1},
			Hash:         hash[:],
			ThirdPartyID: msg.ID, // CommunityID + DiscordMessageID
		}
		wakuMessages = append(wakuMessages, wakuMessage)
	}

	return wakuMessages, nil
}

func (m *Messenger) GetCommunityToken(communityID string, chainID int, address string) (*token.CommunityToken, error) {
	return m.communitiesManager.GetCommunityToken(communityID, chainID, address)
}

func (m *Messenger) GetCommunityTokenByChainAndAddress(chainID int, address string) (*token.CommunityToken, error) {
	return m.communitiesManager.GetCommunityTokenByChainAndAddress(chainID, address)
}

func (m *Messenger) GetCommunityTokens(communityID string) ([]*token.CommunityToken, error) {
	return m.communitiesManager.GetCommunityTokens(communityID)
}

func (m *Messenger) GetCommunityPermissionedBalances(request *requests.GetPermissionedBalances) (map[gethcommon.Address][]communities.PermissionedBalance, error) {
	err := request.Validate()
	if err != nil {
		return nil, err
	}

	accountAddresses, err := m.settings.GetWalletAddresses()
	if err != nil {
		return nil, err
	}

	gethAddresses := make([]gethcommon.Address, 0, len(accountAddresses))
	for _, address := range accountAddresses {
		gethAddresses = append(gethAddresses, gethcommon.HexToAddress(address.Hex()))
	}

	return m.communitiesManager.GetPermissionedBalances(
		context.Background(),
		request.CommunityID,
		gethAddresses,
	)
}

func (m *Messenger) GetAllCommunityTokens() ([]*token.CommunityToken, error) {
	return m.communitiesManager.GetAllCommunityTokens()
}

func (m *Messenger) SaveCommunityToken(token *token.CommunityToken, croppedImage *images.CroppedImage) (*token.CommunityToken, error) {
	return m.communitiesManager.SaveCommunityToken(token, croppedImage)
}

func (m *Messenger) AddCommunityToken(communityID string, chainID int, address string) error {
	communityToken, err := m.communitiesManager.GetCommunityToken(communityID, chainID, address)
	if err != nil {
		return err
	}

	clock, _ := m.getLastClockWithRelatedChat()
	community, err := m.communitiesManager.AddCommunityToken(communityToken, clock)
	if err != nil {
		return err
	}

	err = m.syncCommunity(context.Background(), community, m.dispatchMessage)
	if err != nil {
		return err
	}

	return nil
}

func (m *Messenger) UpdateCommunityTokenState(chainID int, contractAddress string, deployState token.DeployState) error {
	return m.communitiesManager.UpdateCommunityTokenState(chainID, contractAddress, deployState)
}

func (m *Messenger) UpdateCommunityTokenAddress(chainID int, oldContractAddress string, newContractAddress string) error {
	return m.communitiesManager.UpdateCommunityTokenAddress(chainID, oldContractAddress, newContractAddress)
}

func (m *Messenger) UpdateCommunityTokenSupply(chainID int, contractAddress string, supply *bigint.BigInt) error {
	return m.communitiesManager.UpdateCommunityTokenSupply(chainID, contractAddress, supply)
}

func (m *Messenger) RemoveCommunityToken(chainID int, contractAddress string) error {
	return m.communitiesManager.RemoveCommunityToken(chainID, contractAddress)
}

func (m *Messenger) CheckPermissionsToJoinCommunity(request *requests.CheckPermissionToJoinCommunity) (*communities.CheckPermissionToJoinResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	var addresses []gethcommon.Address

	if len(request.Addresses) == 0 {
		accounts, err := m.settings.GetActiveAccounts()
		if err != nil {
			return nil, err
		}

		for _, a := range accounts {
			if a.IsWalletNonWatchOnlyAccount() {
				addresses = append(addresses, gethcommon.HexToAddress(a.Address.Hex()))
			}
		}
	} else {
		for _, v := range request.Addresses {
			addresses = append(addresses, gethcommon.HexToAddress(v))
		}
	}

	return m.communitiesManager.CheckPermissionToJoin(request.CommunityID, addresses)
}

func (m *Messenger) getSharedAddresses(communityID types.HexBytes, requestAddresses []string) ([]gethcommon.Address, error) {
	addressesMap := make(map[string]struct{})

	for _, v := range requestAddresses {
		addressesMap[v] = struct{}{}
	}

	if len(requestAddresses) == 0 {
		sharedAddresses, err := m.GetRevealedAccounts(communityID, common.PubkeyToHex(&m.identity.PublicKey))
		if err != nil {
			return nil, err
		}

		for _, v := range sharedAddresses {
			addressesMap[v.Address] = struct{}{}
		}
	}

	if len(addressesMap) == 0 {
		accounts, err := m.settings.GetActiveAccounts()
		if err != nil {
			return nil, err
		}

		for _, a := range accounts {
			addressesMap[a.Address.Hex()] = struct{}{}
		}
	}

	var addresses []gethcommon.Address
	for addr := range addressesMap {
		addresses = append(addresses, gethcommon.HexToAddress(addr))
	}

	return addresses, nil
}

func (m *Messenger) CheckCommunityChannelPermissions(request *requests.CheckCommunityChannelPermissions) (*communities.CheckChannelPermissionsResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	addresses, err := m.getSharedAddresses(request.CommunityID, request.Addresses)
	if err != nil {
		return nil, err
	}

	return m.communitiesManager.CheckChannelPermissions(request.CommunityID, request.ChatID, addresses)
}

func (m *Messenger) CheckAllCommunityChannelsPermissions(request *requests.CheckAllCommunityChannelsPermissions) (*communities.CheckAllChannelsPermissionsResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	addresses, err := m.getSharedAddresses(request.CommunityID, request.Addresses)
	if err != nil {
		return nil, err
	}

	return m.communitiesManager.CheckAllChannelsPermissions(request.CommunityID, addresses)
}

func (m *Messenger) GetCommunityCheckChannelPermissionResponses(communityID types.HexBytes) (*communities.CheckAllChannelsPermissionsResponse, error) {
	return m.communitiesManager.GetCheckChannelPermissionResponses(communityID)
}

func chunkSlice[T comparable](slice []T, chunkSize int) [][]T {
	var chunks [][]T
	for i := 0; i < len(slice); i += chunkSize {
		end := i + chunkSize

		// necessary check to avoid slicing beyond
		// slice capacity
		if end > len(slice) {
			end = len(slice)
		}

		chunks = append(chunks, slice[i:end])
	}

	return chunks
}

func chunkAttachmentsByByteSize(slice []*protobuf.DiscordMessageAttachment, maxFileSizeBytes uint64) [][]*protobuf.DiscordMessageAttachment {
	var chunks [][]*protobuf.DiscordMessageAttachment

	currentChunkSize := uint64(0)
	currentChunk := make([]*protobuf.DiscordMessageAttachment, 0)

	for i, attachment := range slice {
		payloadBytes := attachment.GetFileSizeBytes()
		if currentChunkSize+payloadBytes > maxFileSizeBytes && len(currentChunk) > 0 {
			chunks = append(chunks, currentChunk)
			currentChunk = make([]*protobuf.DiscordMessageAttachment, 0)
			currentChunkSize = uint64(0)
		}
		currentChunk = append(currentChunk, attachment)
		currentChunkSize = currentChunkSize + payloadBytes
		if i == len(slice)-1 {
			chunks = append(chunks, currentChunk)
		}
	}
	return chunks
}

// startCommunityRekeyLoop creates a 5-minute ticker and starts a routine that attempts to rekey every community every tick
func (m *Messenger) startCommunityRekeyLoop() {
	logger := m.logger.Named("CommunityRekeyLoop")
	var d time.Duration
	if m.communitiesManager.RekeyInterval != 0 {
		if m.communitiesManager.RekeyInterval < 10 {
			d = time.Nanosecond
		} else {
			d = m.communitiesManager.RekeyInterval / 10
		}
	} else {
		d = 5 * time.Minute
	}

	ticker := time.NewTicker(d)
	go func() {
		for {
			select {
			case <-ticker.C:
				m.rekeyCommunities(logger)
			case <-m.quit:
				ticker.Stop()
				logger.Debug("CommunityRekeyLoop stopped")
				return
			}
		}
	}()
}

// rekeyCommunities loops over controlled communities and rekeys if rekey interval elapsed
func (m *Messenger) rekeyCommunities(logger *zap.Logger) {
	// TODO in future have a community level rki rather than a global rki
	var rekeyInterval time.Duration
	if m.communitiesManager.RekeyInterval == 0 {
		rekeyInterval = 48 * time.Hour
	} else {
		rekeyInterval = m.communitiesManager.RekeyInterval
	}

	shouldRekey := func(hashRatchetGroupID []byte) bool {
		key, err := m.sender.GetCurrentKeyForGroup(hashRatchetGroupID)
		if err != nil {
			logger.Error("failed to get current hash ratchet key", zap.Error(err))
			return false
		}

		keyDistributedAt := time.UnixMilli(int64(key.Timestamp))
		return time.Now().After(keyDistributedAt.Add(rekeyInterval))
	}

	controlledCommunities, err := m.ControlledCommunities()
	if err != nil {
		logger.Error("error getting communities", zap.Error(err))
		return
	}

	for _, c := range controlledCommunities {
		keyActions := &communities.EncryptionKeyActions{
			CommunityKeyAction: communities.EncryptionKeyAction{},
			ChannelKeysActions: map[string]communities.EncryptionKeyAction{},
		}

		if c.Encrypted() && shouldRekey(c.ID()) {
			keyActions.CommunityKeyAction = communities.EncryptionKeyAction{
				ActionType: communities.EncryptionKeyRekey,
				Members:    c.Members(),
			}
		}

		for channelID, channel := range c.Chats() {
			if c.ChannelEncrypted(channelID) && shouldRekey([]byte(c.IDString()+channelID)) {
				keyActions.ChannelKeysActions[channelID] = communities.EncryptionKeyAction{
					ActionType: communities.EncryptionKeyRekey,
					Members:    channel.Members,
				}
			}
		}

		err = m.communitiesKeyDistributor.Distribute(c, keyActions)
		if err != nil {
			logger.Error("failed to rekey community", zap.Error(err), zap.String("community ID", c.IDString()))
			continue
		}
	}
}

func (m *Messenger) GetCommunityMembersForWalletAddresses(communityID types.HexBytes, chainID uint64) (map[string]*Contact, error) {
	community, err := m.communitiesManager.GetByID(communityID)
	if err != nil {
		return nil, err
	}

	membersForAddresses := map[string]*Contact{}

	for _, memberPubKey := range community.GetMemberPubkeys() {
		memberPubKeyStr := common.PubkeyToHex(memberPubKey)
		revealedAccounts, err := m.communitiesManager.GetRevealedAddresses(communityID, memberPubKeyStr)
		if err != nil {
			return nil, err
		}
		for _, revealedAccount := range revealedAccounts {
			if !slices.Contains(revealedAccount.ChainIds, chainID) {
				continue
			}

			contact, ok := m.allContacts.Load(memberPubKeyStr)
			if ok {
				membersForAddresses[revealedAccount.Address] = contact
			} else {
				m.logger.Error("community member is not a contact", zap.String("contact ID", memberPubKeyStr))
			}
		}
	}

	return membersForAddresses, nil
}

func (m *Messenger) processCommunityChanges(messageState *ReceivedMessageState) {
	// Process any community changes
	pkString := common.PubkeyToHex(&m.identity.PublicKey)
	for _, changes := range messageState.Response.CommunityChanges {
		if changes.ShouldMemberJoin {
			response, err := m.joinCommunity(context.TODO(), changes.Community.ID(), false)
			if err != nil {
				m.logger.Error("cannot join community", zap.Error(err))
				continue
			}

			if err := messageState.Response.Merge(response); err != nil {
				m.logger.Error("cannot merge join community response", zap.Error(err))
				continue
			}
		} else if changes.MemberSoftKicked {
			m.leaveCommunityOnSoftKick(changes.Community, messageState.Response)
			m.shareRevealedAccountsOnSoftKick(changes.Community, messageState.Response)

		} else if changes.MemberKicked {
			notificationType := ActivityCenterNotificationTypeCommunityKicked
			if changes.IsMemberBanned(pkString) {
				notificationType = ActivityCenterNotificationTypeCommunityBanned
			}
			m.leaveCommunityDueToKickOrBan(changes, notificationType, messageState.Response)
		} else if changes.IsMemberUnbanned(pkString) {
			m.AddActivityCenterNotificationToResponse(changes.Community.IDString(), ActivityCenterNotificationTypeCommunityUnbanned, messageState.Response)
		}
	}
	// Clean up as not used by clients currently
	messageState.Response.CommunityChanges = nil
}

func (m *Messenger) PromoteSelfToControlNode(communityID types.HexBytes) (*MessengerResponse, error) {
	clock, _ := m.getLastClockWithRelatedChat()

	community, err := m.FetchCommunity(&FetchCommunityRequest{
		CommunityKey:    types.EncodeHex(communityID),
		Shard:           nil,
		TryDatabase:     true,
		WaitForResponse: true,
	})

	if err != nil {
		return nil, err
	}

	if !communities.HasTokenOwnership(community.Description()) {
		return nil, errors.New(ErrOwnerTokenNeeded)
	}

	changes, err := m.communitiesManager.PromoteSelfToControlNode(community, clock)
	if err != nil {
		return nil, err
	}

	var response MessengerResponse

	if len(changes.MembersRemoved) > 0 {
		requestsToJoin, err := m.communitiesManager.GenerateRequestsToJoinForAutoApprovalOnNewOwnership(changes.Community.ID(), changes.MembersRemoved)
		if err != nil {
			return nil, err
		}
		response.AddRequestsToJoinCommunity(requestsToJoin)
	}

	err = m.syncCommunity(context.Background(), changes.Community, m.dispatchMessage)
	if err != nil {
		return nil, err
	}

	response.AddCommunity(changes.Community)
	response.CommunityChanges = []*communities.CommunityChanges{changes}

	if m.config.messengerSignalsHandler != nil {
		m.config.messengerSignalsHandler.MessengerResponse(&response)
	}

	m.communitiesManager.StartMembersReevaluationLoop(community.ID(), false)

	return &response, nil
}

func (m *Messenger) CreateResponseWithACNotification(communityID string, acType ActivityCenterType, isRead bool, tokenDataJSON string) (*MessengerResponse, error) {
	tokenData := ActivityTokenData{}
	err := json.Unmarshal([]byte(tokenDataJSON), &tokenData)
	if len(tokenDataJSON) > 0 && err != nil {
		// Only return error when activityDataString is not empty
		return nil, err
	}
	// Activity center notification
	notification := &ActivityCenterNotification{
		ID:          types.FromHex(uuid.New().String()),
		Type:        acType,
		Timestamp:   m.getTimesource().GetCurrentTime(),
		CommunityID: communityID,
		Read:        isRead,
		Deleted:     false,
		UpdatedAt:   m.GetCurrentTimeInMillis(),
		TokenData:   &tokenData,
	}

	err = m.prepareTokenData(notification.TokenData, m.httpServer)
	if err != nil {
		return nil, err
	}

	response := &MessengerResponse{}

	err = m.addActivityCenterNotification(response, notification, nil)
	if err != nil {
		m.logger.Error("failed to save notification", zap.Error(err))
		return response, err
	}

	return response, nil
}

// SendMessageToControlNode sends a message to the control node of the community.
// use pointer to rawMessage to get the message ID and other updated properties.
func (m *Messenger) SendMessageToControlNode(community *communities.Community, rawMessage *common.RawMessage) ([]byte, error) {
	if !community.PublicKey().Equal(community.ControlNode()) {
		rawMessage.ResendMethod = common.ResendMethodSendPrivate
		rawMessage.Recipients = append(rawMessage.Recipients, community.ControlNode())
		return m.sender.SendPrivate(context.Background(), community.ControlNode(), rawMessage)
	}
	rawMessage.ResendMethod = common.ResendMethodSendCommunityMessage
	// Note: There are multiple instances where SendMessageToControlNode is invoked throughout the codebase.
	// Additionally, some callers may invoke SendPrivate before SendMessageToControlNode. This could potentially
	// lead to a situation where the same raw message is sent using different methods, which, from a code perspective,
	// seems erroneous when implementing raw message resending. However, this behavior is intentional and is not considered
	// an issue. For a detailed explanation, refer https://github.com/status-im/status-go/pull/4969#issuecomment-2040891184
	return m.sender.SendCommunityMessage(context.Background(), rawMessage)
}

func (m *Messenger) AddActivityCenterNotificationToResponse(communityID string, acType ActivityCenterType, response *MessengerResponse) {
	// Activity Center notification
	notification := &ActivityCenterNotification{
		ID:          types.FromHex(uuid.New().String()),
		Type:        acType,
		Timestamp:   m.getTimesource().GetCurrentTime(),
		CommunityID: communityID,
		Read:        false,
		Deleted:     false,
		UpdatedAt:   m.GetCurrentTimeInMillis(),
	}

	err := m.addActivityCenterNotification(response, notification, nil)
	if err != nil {
		m.logger.Error("failed to save notification", zap.Error(err))
	}
}

func (m *Messenger) leaveCommunityDueToKickOrBan(changes *communities.CommunityChanges, acType ActivityCenterType, stateResponse *MessengerResponse) {
	response, err := m.kickedOutOfCommunity(changes.Community.ID(), false)
	if err != nil {
		m.logger.Error("cannot leave community", zap.Error(err))
		return
	}

	// Activity Center notification
	notification := &ActivityCenterNotification{
		ID:          types.FromHex(uuid.New().String()),
		Type:        acType,
		Timestamp:   m.getTimesource().GetCurrentTime(),
		CommunityID: changes.Community.IDString(),
		Read:        false,
		UpdatedAt:   m.GetCurrentTimeInMillis(),
	}

	err = m.addActivityCenterNotification(response, notification, nil)
	if err != nil {
		m.logger.Error("failed to save notification", zap.Error(err))
		return
	}

	if err := stateResponse.Merge(response); err != nil {
		m.logger.Error("cannot merge leave and notification response", zap.Error(err))
	}
}

func (m *Messenger) GetCommunityMemberAllMessages(request *requests.CommunityMemberMessages) ([]*common.Message, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	messages, err := m.persistence.GetCommunityMemberAllMessages(request.MemberPublicKey, request.CommunityID)
	if err != nil {
		return nil, err
	}

	for _, message := range messages {
		updatedMessages, err := m.persistence.MessagesByResponseTo(message.ID)
		if err != nil {
			return nil, err
		}

		messages = append(messages, updatedMessages...)
	}
	err = m.prepareMessagesList(messages)
	if err != nil {
		return nil, err
	}
	return messages, nil

}

func (m *Messenger) DeleteCommunityMemberMessages(request *requests.DeleteCommunityMemberMessages) (*MessengerResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}

	community, err := m.GetCommunityByID(request.CommunityID)
	if err != nil {
		return nil, err
	}

	if community == nil {
		return nil, communities.ErrOrgNotFound
	}

	if !community.IsControlNode() && !community.IsPrivilegedMember(m.IdentityPublicKey()) {
		return nil, communities.ErrNotEnoughPermissions
	}

	memberPubKey, err := common.HexToPubkey(request.MemberPubKey)
	if err != nil {
		return nil, err
	}

	if community.IsMemberOwner(memberPubKey) && !m.IdentityPublicKey().Equal(memberPubKey) {
		return nil, communities.ErrNotOwner
	}

	deleteMessagesResponse, err := m.deleteCommunityMemberMessages(request.MemberPubKey, request.CommunityID.String(), request.Messages)
	if err != nil {
		return nil, err
	}

	deletedMessages := &protobuf.DeleteCommunityMemberMessages{
		Clock:       uint64(time.Now().Unix()),
		CommunityId: community.ID(),
		MemberId:    request.MemberPubKey,
		Messages:    request.Messages,
	}

	payload, err := proto.Marshal(deletedMessages)
	if err != nil {
		return nil, err
	}

	rawMessage := common.RawMessage{
		Payload:             payload,
		Sender:              community.PrivateKey(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_DELETE_COMMUNITY_MEMBER_MESSAGES,
		PubsubTopic:         community.PubsubTopic(),
	}

	_, err = m.sender.SendPublic(context.Background(), community.IDString(), rawMessage)

	return deleteMessagesResponse, err
}

func (m *Messenger) HandleDeleteCommunityMemberMessages(state *ReceivedMessageState, request *protobuf.DeleteCommunityMemberMessages, statusMessage *v1protocol.StatusMessage) error {
	community, err := m.communitiesManager.GetByID(request.CommunityId)
	if err != nil {
		return err
	}

	if community == nil {
		return communities.ErrOrgNotFound
	}

	if !community.ControlNode().Equal(state.CurrentMessageState.PublicKey) && !community.IsPrivilegedMember(state.CurrentMessageState.PublicKey) {
		return communities.ErrNotAuthorized
	}

	deleteMessagesResponse, err := m.deleteCommunityMemberMessages(request.MemberId, community.IDString(), request.Messages)
	if err != nil {
		return err
	}

	return state.Response.Merge(deleteMessagesResponse)
}

func (m *Messenger) leaveCommunityOnSoftKick(community *communities.Community, messengerResponse *MessengerResponse) {
	response, err := m.kickedOutOfCommunity(community.ID(), true)
	if err != nil {
		m.logger.Error("member soft kick error", zap.String("communityID", types.EncodeHex(community.ID())), zap.Error(err))
	}

	if err := messengerResponse.Merge(response); err != nil {
		m.logger.Error("cannot merge leaveCommunityOnSoftKick response", zap.String("communityID", types.EncodeHex(community.ID())), zap.Error(err))
	}
}

func (m *Messenger) shareRevealedAccountsOnSoftKick(community *communities.Community, messengerResponse *MessengerResponse) {
	requestToJoin, err := m.sendSharedAddressToControlNode(community.ControlNode(), community)
	if err != nil {
		m.logger.Error("share address to control node failed", zap.String("id", types.EncodeHex(community.ID())), zap.Error(err))

		if err == communities.ErrRevealedAccountsAbsent || err == communities.ErrNoRevealedAccountsSignature {
			m.AddActivityCenterNotificationToResponse(community.IDString(), ActivityCenterNotificationTypeShareAccounts, messengerResponse)
		}
	} else {
		messengerResponse.AddRequestToJoinCommunity(requestToJoin)
	}
}

func (m *Messenger) requestCommunityEncryptionKeys(community *communities.Community, channelIDs []string) error {
	m.logger.Debug("request community encryption keys",
		zap.String("communityID", community.IDString()),
		zap.Strings("channels", channelIDs))

	request := &protobuf.CommunityEncryptionKeysRequest{
		CommunityId: community.ID(),
		ChatIds:     channelIDs,
	}

	payload, err := proto.Marshal(request)
	if err != nil {
		return err
	}

	rawMessage := &common.RawMessage{
		Payload:             payload,
		Sender:              m.identity,
		CommunityID:         community.ID(),
		SkipEncryptionLayer: true,
		MessageType:         protobuf.ApplicationMetadataMessage_COMMUNITY_ENCRYPTION_KEYS_REQUEST,
	}

	_, err = m.SendMessageToControlNode(community, rawMessage)
	return err
}

func (m *Messenger) startRequestMissingCommunityChannelsHRKeysLoop() {
	logger := m.logger.Named("requestMissingCommunityChannelsHRKeysLoop")

	go func() {
		for {
			select {
			case <-time.After(5 * time.Minute):
				communitiesChannels, err := m.communitiesManager.DetermineChannelsForHRKeysRequest()
				if err != nil {
					logger.Error("failed to determine channels for encryption keys request", zap.Error(err))
					continue
				}

				for _, cc := range communitiesChannels {
					err := m.requestCommunityEncryptionKeys(cc.Community, cc.ChannelIDs)
					if err != nil {
						logger.Error("failed to request channels' encryption keys",
							zap.String("communityID", cc.Community.IDString()),
							zap.Strings("channelIDs", cc.ChannelIDs),
							zap.Error(err))
						continue
					}

					err = m.communitiesManager.UpdateEncryptionKeysRequests(cc.Community.ID(), cc.ChannelIDs)
					if err != nil {
						logger.Error("failed to update channels' encryption keys requests",
							zap.String("communityID", cc.Community.IDString()),
							zap.Strings("channelIDs", cc.ChannelIDs),
							zap.Error(err))
					}
				}

			case <-m.quit:
				return
			}
		}
	}()
}
