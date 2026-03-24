package mqtt

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

func BuildUsername(authMode, accessKeyID, instanceID string) string {
	return fmt.Sprintf("%s|%s|%s", authMode, accessKeyID, instanceID)
}

func BuildSignaturePassword(clientID, accessKeySecret string) string {
	hash := hmac.New(sha1.New, []byte(accessKeySecret))
	_, _ = hash.Write([]byte(clientID))
	return base64.StdEncoding.EncodeToString(hash.Sum(nil))
}

func BuildClientID(groupID, deviceID string) string {
	return fmt.Sprintf("%s@@@%s", groupID, deviceID)
}

func waitToken(ctx context.Context, timeout time.Duration, token pahomqtt.Token) error {
	if ctx == nil {
		return ErrContextRequired
	}
	if token == nil {
		return ErrOperationTokenRequired
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		if _, hasDeadline := ctx.Deadline(); !hasDeadline {
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	select {
	case <-token.Done():
		return token.Error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func normalizeAliyunAuth(auth *AliyunAuth) (*AliyunAuth, error) {
	if auth == nil {
		return nil, nil
	}

	cloned := *auth
	cloned.Mode = strings.TrimSpace(cloned.Mode)
	cloned.AccessKeyID = strings.TrimSpace(cloned.AccessKeyID)
	cloned.AccessKeySecret = strings.TrimSpace(cloned.AccessKeySecret)
	cloned.InstanceID = strings.TrimSpace(cloned.InstanceID)
	cloned.GroupID = strings.TrimSpace(cloned.GroupID)
	cloned.DeviceID = strings.TrimSpace(cloned.DeviceID)

	switch strings.ToLower(cloned.Mode) {
	case "":
		cloned.Mode = AuthModeSignature
	case strings.ToLower(AuthModeSignature):
		cloned.Mode = AuthModeSignature
	case strings.ToLower(AuthModeToken):
		cloned.Mode = AuthModeToken
	default:
		return nil, fmt.Errorf("%w: %s", ErrInvalidAliyunAuthMode, cloned.Mode)
	}

	return &cloned, nil
}

func trimNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeTopic(topic string) string {
	return strings.TrimSpace(topic)
}

func normalizeTopics(topics []string) ([]string, error) {
	result := make([]string, 0, len(topics))
	seen := make(map[string]struct{}, len(topics))
	for _, topic := range topics {
		topic = normalizeTopic(topic)
		if topic == "" {
			return nil, ErrTopicRequired
		}
		if _, ok := seen[topic]; ok {
			continue
		}
		seen[topic] = struct{}{}
		result = append(result, topic)
	}
	if len(result) == 0 {
		return nil, ErrNoTopics
	}
	return result, nil
}

func normalizeFilters(filters map[string]byte) (map[string]byte, error) {
	if len(filters) == 0 {
		return nil, ErrFiltersRequired
	}

	result := make(map[string]byte, len(filters))
	seen := make(map[string]string, len(filters))
	for topic, qos := range filters {
		normalized := normalizeTopic(topic)
		if normalized == "" {
			return nil, ErrTopicRequired
		}
		if original, ok := seen[normalized]; ok && original != topic {
			return nil, ErrDuplicateFilterTopic
		}
		seen[normalized] = topic
		result[normalized] = qos
	}
	if len(result) == 0 {
		return nil, ErrFiltersRequired
	}
	return result, nil
}
