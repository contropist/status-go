package localnotifications

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/status-im/status-go/signal"

	messagebus "github.com/vardius/message-bus"

	"github.com/ethereum/go-ethereum/common"
)

type messagePayload interface{}

// MessageType - Enum defining types of message handled by the bus
type MessageType string

type PushCategory string

type notificationBody struct {
	State string `json:"state"`
	From  string `json:"from"`
	To    string `json:"to"`
	Value string `json:"value"`
}

type Notification struct {
	ID            string           `json:"id"`
	Platform      float32          `json:"platform,omitempty"`
	Body          notificationBody `json:"body"`
	Category      PushCategory     `json:"category,omitempty"`
	Image         string           `json:"imageUrl,omitempty"`
	IsScheduled   bool             `json:"isScheduled,omitempty"`
	ScheduledTime string           `json:"scheduleTime,omitempty"`
}

// TransactionEvent - structure used to pass messages from wallet to bus
type TransactionEvent struct {
	Type                      string
	BlockNumber               *big.Int
	Accounts                  []common.Address
	NewTransactionsPerAccount map[common.Address]int
	ERC20                     bool
}

const (
	// Transaction - Ethereum transaction
	Transaction MessageType = "transaction"

	// Message - Waku message
	Message MessageType = "message"

	// Custom - User defined notifications
	Custom MessageType = "custom"
)

const topic = "local-notifications"

// Broker keeps the state of message bus
type Broker struct {
	bus messagebus.MessageBus
	ctx context.Context
}

// InitializeBus - Initialize MessageBus which will handle generation of notifications
func InitializeBus(queueSize int) *Broker {
	return &Broker{
		bus: messagebus.New(queueSize),
		ctx: context.Background(),
	}
}

func pushMessage(notification *Notification) {
	// Send signal with this notification
	jsonBody, _ := json.Marshal(notification)
	fmt.Println(string(jsonBody))

	signal.SendLocalNotifications(notification)
}

func buildTransactionNotification(payload messagePayload) *Notification {
	transaction := payload.(TransactionEvent)
	body := notificationBody{
		State: transaction.Type,
		From:  transaction.Type,
		To:    transaction.Type,
		Value: transaction.Type,
	}

	for i, s := range transaction.Accounts {
		fmt.Println(i, s)
	}

	return &Notification{
		ID:       string(transaction.Type),
		Body:     body,
		Category: "transaction",
	}
}

// TransactionsHandler - Handles new transaction messages
func TransactionsHandler(ctx context.Context, payload messagePayload) {
	n := buildTransactionNotification(payload)
	pushMessage(n)
}

// NotificationWorker - Worker which processes all incoming messages
func (s *Broker) NotificationWorker() error {

	fmt.Println("NotificationWorker")

	if err := s.bus.Subscribe(string(Transaction), TransactionsHandler); err != nil {
		return err
	}

	return nil
}

// PublishMessage - Send new message to be processed into a notification
func (s *Broker) PublishMessage(messageType MessageType, payload interface{}) {
	s.bus.Publish(string(messageType), s.ctx, messagePayload(payload))
	fmt.Println("Messag published", string(messageType))
}
