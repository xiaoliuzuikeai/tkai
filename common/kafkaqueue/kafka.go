package kafkaqueue

import (
	"GoAI/config"
	messageDAO "GoAI/dao/message"
	"GoAI/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

type MessageEvent struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
	UserName  string `json:"user_name"`
	IsUser    bool   `json:"is_user"`
}

var (
	writer *kafka.Writer
	reader *kafka.Reader
	cancel context.CancelFunc
	mu     sync.RWMutex
)

func Init() error {
	cfg := config.GetConfig().KafkaConfig
	if len(cfg.Brokers) == 0 {
		return errors.New("Kafka brokers are not configured")
	}
	if cfg.MessageTopic == "" {
		return errors.New("Kafka message topic is not configured")
	}
	if cfg.ConsumerGroup == "" {
		return errors.New("Kafka consumer group is not configured")
	}

	ctx, cancelFunc := context.WithCancel(context.Background())

	mu.Lock()
	writer = &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Topic:        cfg.MessageTopic,
		Balancer:     &kafka.Hash{},
		RequiredAcks: kafka.RequireAll,
		Async:        false,
	}
	reader = kafka.NewReader(kafka.ReaderConfig{
		Brokers: cfg.Brokers,
		Topic:   cfg.MessageTopic,
		GroupID: cfg.ConsumerGroup,
	})
	cancel = cancelFunc
	mu.Unlock()

	go consume(ctx)
	return nil
}

func Publish(msg *model.Message) error {
	if msg == nil {
		return errors.New("cannot publish a nil message")
	}

	event := MessageEvent{
		SessionID: msg.SessionID,
		Content:   msg.Content,
		UserName:  msg.UserName,
		IsUser:    msg.IsUser,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal Kafka message: %w", err)
	}

	mu.RLock()
	w := writer
	mu.RUnlock()
	if w == nil {
		return errors.New("Kafka producer is not initialized")
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()

	if err := w.WriteMessages(ctx, kafka.Message{
		Key:   []byte(msg.SessionID),
		Value: payload,
	}); err != nil {
		return fmt.Errorf("publish Kafka message: %w", err)
	}
	return nil
}

func consume(ctx context.Context) {
	mu.RLock()
	r := reader
	mu.RUnlock()
	if r == nil {
		return
	}

	for {
		record, err := r.FetchMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("fetch Kafka message failed: %v", err)
			time.Sleep(time.Second)
			continue
		}

		var event MessageEvent
		if err := json.Unmarshal(record.Value, &event); err != nil {
			log.Printf("discard invalid Kafka message: %v", err)
			if err := r.CommitMessages(ctx, record); err != nil {
				log.Printf("commit invalid Kafka message failed: %v", err)
			}
			continue
		}

		for {
			_, err = messageDAO.CreateMessage(&model.Message{
				SessionID: event.SessionID,
				Content:   event.Content,
				UserName:  event.UserName,
				IsUser:    event.IsUser,
			})
			if err == nil {
				break
			}

			log.Printf("persist Kafka message failed: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}

		if err := r.CommitMessages(ctx, record); err != nil {
			log.Printf("commit Kafka message failed: %v", err)
		}
	}
}

func Close() error {
	mu.Lock()
	defer mu.Unlock()

	if cancel != nil {
		cancel()
		cancel = nil
	}

	var closeErr error
	if reader != nil {
		if err := reader.Close(); err != nil {
			closeErr = err
		}
		reader = nil
	}
	if writer != nil {
		if err := writer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
		writer = nil
	}
	return closeErr
}
