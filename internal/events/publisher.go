package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

var ErrDisabled = errors.New("kafka publisher disabled")

const Source = "iag-quality-control"

type Publisher struct {
	writer  *kafka.Writer
	enabled bool
	topic   string
}

func NewPublisher(brokers []string, topic, clientID string) *Publisher {
	if len(brokers) == 0 || topic == "" {
		return &Publisher{}
	}
	return &Publisher{
		enabled: true,
		topic:   topic,
		writer: &kafka.Writer{
			Addr:     kafka.TCP(brokers...),
			Topic:    topic,
			Balancer: &kafka.LeastBytes{},
			Transport: &kafka.Transport{
				ClientID: clientID,
			},
		},
	}
}

func (p *Publisher) Enabled() bool {
	return p.enabled
}

func (p *Publisher) Close() error {
	if p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

func (p *Publisher) Publish(ctx context.Context, eventType string, data map[string]any) error {
	if !p.enabled {
		return nil
	}
	env := map[string]any{
		"id":     uuid.NewString(),
		"type":   eventType,
		"time":   time.Now().UTC().Format(time.RFC3339),
		"source": Source,
		"data":   data,
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	key := ""
	if bid, ok := data["batch_business_id"].(string); ok {
		key = bid
	}
	if lid, ok := data["lot_business_id"].(string); ok && key == "" {
		key = lid
	}
	return p.writer.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: raw})
}
