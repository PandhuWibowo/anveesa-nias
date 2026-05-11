package handlers

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	appdb "github.com/anveesa/nias/db"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
)

type KafkaTopicInfo struct {
	Name              string `json:"name"`
	Partitions        int    `json:"partitions"`
	ReplicationFactor int    `json:"replication_factor"`
	LeaderCount       int    `json:"leader_count"`
	Error             string `json:"error,omitempty"`
}

type KafkaGroupInfo struct {
	GroupID      string `json:"group_id"`
	Coordinator  int    `json:"coordinator"`
	ProtocolType string `json:"protocol_type"`
}

type KafkaMessageInfo struct {
	Topic         string               `json:"topic"`
	Partition     int                  `json:"partition"`
	Offset        int64                `json:"offset"`
	HighWaterMark int64                `json:"high_water_mark"`
	Key           string               `json:"key"`
	Value         string               `json:"value"`
	Timestamp     time.Time            `json:"timestamp"`
	Headers       []KafkaMessageHeader `json:"headers"`
}

type KafkaMessageHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type KafkaProduceInput struct {
	Topic   string               `json:"topic"`
	Key     string               `json:"key"`
	Value   string               `json:"value"`
	Headers []KafkaMessageHeader `json:"headers"`
}

type KafkaConsumeInput struct {
	Topic   string `json:"topic"`
	GroupID string `json:"group_id"`
	Limit   int    `json:"limit"`
}

type KafkaConsumeResult struct {
	GroupID  string             `json:"group_id"`
	Topic    string             `json:"topic"`
	Messages []KafkaMessageInfo `json:"messages"`
	Count    int                `json:"count"`
}

type KafkaTopicInput struct {
	Topic             string            `json:"topic"`
	Partitions        int               `json:"partitions"`
	ReplicationFactor int               `json:"replication_factor"`
	Configs           map[string]string `json:"configs"`
}

type KafkaPartitionUpdateInput struct {
	Topic      string `json:"topic"`
	Partitions int    `json:"partitions"`
}

type KafkaGroupDetail struct {
	GroupID  string                      `json:"group_id"`
	State    string                      `json:"state"`
	Members  []KafkaGroupMember          `json:"members"`
	Offsets  []KafkaGroupPartitionOffset `json:"offsets"`
	TotalLag int64                       `json:"total_lag"`
	Error    string                      `json:"error,omitempty"`
}

type KafkaGroupMember struct {
	MemberID    string                       `json:"member_id"`
	ClientID    string                       `json:"client_id"`
	ClientHost  string                       `json:"client_host"`
	Assignments []KafkaGroupTopicAssignments `json:"assignments"`
}

type KafkaGroupTopicAssignments struct {
	Topic      string `json:"topic"`
	Partitions []int  `json:"partitions"`
}

type KafkaGroupPartitionOffset struct {
	Topic           string `json:"topic"`
	Partition       int    `json:"partition"`
	CommittedOffset int64  `json:"committed_offset"`
	LatestOffset    int64  `json:"latest_offset"`
	Lag             int64  `json:"lag"`
	Error           string `json:"error,omitempty"`
}

type KafkaAPIError struct {
	Error       string            `json:"error"`
	Code        string            `json:"code"`
	Operation   string            `json:"operation"`
	Reason      string            `json:"reason"`
	Suggestions []string          `json:"suggestions"`
	Context     map[string]string `json:"context,omitempty"`
	TraceID     string            `json:"trace_id"`
}

func KafkaTopics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "list_topics", "invalid connection id", err, nil)
			return
		}

		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "list_topics", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		topics, err := readKafkaTopics(ctx, in)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "list_topics", "failed to read Kafka topics", err, kafkaErrorContext(connID, in, "", "", nil))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(topics)
	}
}

func KafkaGroups() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "list_groups", "invalid connection id", err, nil)
			return
		}

		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "list_groups", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		groups, err := readKafkaGroups(ctx, in)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "list_groups", "failed to read Kafka groups", err, kafkaErrorContext(connID, in, "", "", nil))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(groups)
	}
}

func KafkaMessages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "read_messages", "invalid connection id", err, nil)
			return
		}
		topic := strings.TrimSpace(r.URL.Query().Get("topic"))
		if topic == "" {
			writeKafkaError(w, r, http.StatusBadRequest, "read_messages", "topic is required", nil, kafkaErrorContext(connID, ConnectionInput{}, topic, "", nil))
			return
		}
		partition := queryInt(r, "partition", -1, -1, 1000000)
		limit := queryInt(r, "limit", 50, 1, 500)

		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "read_messages", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, topic, "", nil))
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		messages, err := readKafkaMessages(ctx, in, topic, partition, limit)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "read_messages", "failed to read Kafka messages", err, kafkaErrorContext(connID, in, topic, "", map[string]string{
				"partition": fmt.Sprintf("%d", partition),
				"limit":     fmt.Sprintf("%d", limit),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(messages)
	}
}

func KafkaProduce() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "produce_message", "invalid connection id", err, nil)
			return
		}
		var payload KafkaProduceInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "produce_message", "invalid JSON body", err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}
		payload.Topic = strings.TrimSpace(payload.Topic)
		if payload.Topic == "" {
			writeKafkaError(w, r, http.StatusBadRequest, "produce_message", "topic is required", nil, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}

		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "produce_message", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, "", nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if err := produceKafkaMessage(ctx, in, payload); err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "produce_message", "failed to produce Kafka message", err, kafkaErrorContext(connID, in, payload.Topic, "", nil))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Message produced"})
	}
}

func KafkaConsumeTest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "consume_test", "invalid connection id", err, nil)
			return
		}
		var payload KafkaConsumeInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "consume_test", "invalid JSON body", err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}
		payload.Topic = strings.TrimSpace(payload.Topic)
		payload.GroupID = strings.TrimSpace(payload.GroupID)
		if payload.Limit <= 0 {
			payload.Limit = 10
		}
		if payload.Limit > 100 {
			payload.Limit = 100
		}
		if payload.Topic == "" || payload.GroupID == "" {
			writeKafkaError(w, r, http.StatusBadRequest, "consume_test", "topic and group_id are required", nil, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, payload.GroupID, nil))
			return
		}

		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "consume_test", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, payload.GroupID, nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		result, err := consumeKafkaTest(ctx, in, payload)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "consume_test", "failed to consume Kafka messages", err, kafkaErrorContext(connID, in, payload.Topic, payload.GroupID, map[string]string{
				"limit": fmt.Sprintf("%d", payload.Limit),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

func KafkaGroupDetailHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "group_detail", "invalid connection id", err, nil)
			return
		}
		groupID := strings.TrimSpace(r.URL.Query().Get("group_id"))
		if groupID == "" {
			writeKafkaError(w, r, http.StatusBadRequest, "group_detail", "group_id is required", nil, kafkaErrorContext(connID, ConnectionInput{}, "", groupID, nil))
			return
		}
		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "group_detail", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, "", groupID, nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		detail, err := readKafkaGroupDetail(ctx, in, groupID)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "group_detail", "failed to read Kafka group detail", err, kafkaErrorContext(connID, in, "", groupID, nil))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(detail)
	}
}

func KafkaCreateTopic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "create_topic", "invalid connection id", err, nil)
			return
		}
		var payload KafkaTopicInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "create_topic", "invalid JSON body", err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}
		payload.Topic = strings.TrimSpace(payload.Topic)
		if payload.Topic == "" || payload.Partitions <= 0 || payload.ReplicationFactor <= 0 {
			writeKafkaError(w, r, http.StatusBadRequest, "create_topic", "topic, partitions, and replication_factor are required", nil, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, "", map[string]string{
				"partitions":         fmt.Sprintf("%d", payload.Partitions),
				"replication_factor": fmt.Sprintf("%d", payload.ReplicationFactor),
			}))
			return
		}
		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "create_topic", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, "", nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if err := createKafkaTopic(ctx, in, payload); err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "create_topic", "failed to create Kafka topic", err, kafkaErrorContext(connID, in, payload.Topic, "", map[string]string{
				"partitions":         fmt.Sprintf("%d", payload.Partitions),
				"replication_factor": fmt.Sprintf("%d", payload.ReplicationFactor),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Topic created"})
	}
}

func KafkaDeleteTopic() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "delete_topic", "invalid connection id", err, nil)
			return
		}
		topic := strings.TrimSpace(r.URL.Query().Get("topic"))
		if topic == "" {
			writeKafkaError(w, r, http.StatusBadRequest, "delete_topic", "topic is required", nil, kafkaErrorContext(connID, ConnectionInput{}, topic, "", nil))
			return
		}
		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "delete_topic", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, topic, "", nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if err := deleteKafkaTopic(ctx, in, topic); err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "delete_topic", "failed to delete Kafka topic", err, kafkaErrorContext(connID, in, topic, "", nil))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Topic deleted"})
	}
}

func KafkaUpdatePartitions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		connID, err := connectionIDFromPath(r.URL.Path)
		if err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "update_partitions", "invalid connection id", err, nil)
			return
		}
		var payload KafkaPartitionUpdateInput
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeKafkaError(w, r, http.StatusBadRequest, "update_partitions", "invalid JSON body", err, kafkaErrorContext(connID, ConnectionInput{}, "", "", nil))
			return
		}
		payload.Topic = strings.TrimSpace(payload.Topic)
		if payload.Topic == "" || payload.Partitions <= 0 {
			writeKafkaError(w, r, http.StatusBadRequest, "update_partitions", "topic and partitions are required", nil, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, "", map[string]string{
				"partitions": fmt.Sprintf("%d", payload.Partitions),
			}))
			return
		}
		in, err := kafkaConnectionInput(connID)
		if err != nil {
			writeKafkaError(w, r, http.StatusNotFound, "update_partitions", err.Error(), err, kafkaErrorContext(connID, ConnectionInput{}, payload.Topic, "", nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
		defer cancel()
		if err := updateKafkaPartitions(ctx, in, payload); err != nil {
			writeKafkaError(w, r, http.StatusBadGateway, "update_partitions", "failed to update Kafka partitions", err, kafkaErrorContext(connID, in, payload.Topic, "", map[string]string{
				"partitions": fmt.Sprintf("%d", payload.Partitions),
			}))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "Partitions updated"})
	}
}

func kafkaConnectionInput(connID int64) (ConnectionInput, error) {
	var in ConnectionInput
	var ssl int
	var encPassword string
	err := appdb.DB.QueryRow(
		appdb.ConvertQuery(`SELECT driver, COALESCE(host,''), COALESCE(port,0), COALESCE(username,''), COALESCE(password,''), ssl FROM connections WHERE id=?`), connID,
	).Scan(&in.Driver, &in.Host, &in.Port, &in.Username, &encPassword, &ssl)
	if err != nil {
		return in, fmt.Errorf("connection not found")
	}
	if in.Driver != "kafka" {
		return in, fmt.Errorf("connection is not Kafka")
	}
	in.SSL = ssl == 1

	password, err := decryptCredential(encPassword)
	if err != nil {
		return in, fmt.Errorf("decryption error")
	}
	in.Password = password
	return in, nil
}

func writeKafkaError(w http.ResponseWriter, r *http.Request, status int, operation, message string, cause error, context map[string]string) {
	raw := strings.TrimSpace(message)
	if cause != nil && !strings.Contains(raw, cause.Error()) {
		raw = strings.TrimSpace(raw + ": " + cause.Error())
	}
	if raw == "" {
		raw = http.StatusText(status)
	}
	diagnostic := kafkaDiagnostic(status, operation, raw, context)
	diagnostic.TraceID = kafkaTraceID(r, operation)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(diagnostic)
	log.Printf("kafka_error trace_id=%s operation=%s code=%s status=%d reason=%q error=%q context=%v", diagnostic.TraceID, diagnostic.Operation, diagnostic.Code, status, diagnostic.Reason, diagnostic.Error, diagnostic.Context)
}

func kafkaDiagnostic(status int, operation, raw string, context map[string]string) KafkaAPIError {
	lower := strings.ToLower(raw)
	out := KafkaAPIError{
		Error:     raw,
		Code:      "kafka_error",
		Operation: operation,
		Reason:    "Kafka returned an error while processing this operation.",
		Context:   context,
		Suggestions: []string{
			"Check the raw error and operation context below.",
			"Verify the selected Kafka connection points to the expected cluster.",
		},
	}

	switch {
	case status == http.StatusBadRequest:
		out.Code = "invalid_request"
		out.Reason = "The request is missing required input or contains invalid values."
		out.Suggestions = []string{
			"Review the selected topic, partition, group, and numeric fields.",
			"Retry after correcting the highlighted input.",
		}
	case strings.Contains(lower, "connection not found"), strings.Contains(lower, "not kafka"), strings.Contains(lower, "decryption"):
		out.Code = "connection_configuration"
		out.Reason = "The saved connection cannot be used as a Kafka connection."
		out.Suggestions = []string{
			"Open Admin / Connections and verify the connection driver, host, port, SSL, username, and password.",
			"If credentials were rotated, save the Kafka connection again.",
		}
	case strings.Contains(lower, "context deadline"), strings.Contains(lower, "deadline exceeded"), strings.Contains(lower, "timeout"), strings.Contains(lower, "i/o timeout"):
		out.Code = "kafka_timeout"
		out.Reason = "The Kafka operation timed out before the broker returned a response."
		out.Suggestions = []string{
			"Confirm the broker is reachable from the NIAS backend host.",
			"Check broker load, network latency, firewall rules, and advertised.listeners.",
			"Try a narrower operation, such as one topic or one partition.",
		}
	case strings.Contains(lower, "connection refused"), strings.Contains(lower, "no such host"), strings.Contains(lower, "dial tcp"), strings.Contains(lower, "network is unreachable"):
		out.Code = "broker_unreachable"
		out.Reason = "The backend could not establish a TCP connection to the Kafka broker."
		out.Suggestions = []string{
			"Verify host and port in the Kafka connection.",
			"Check DNS resolution and firewall or Docker network routing from the backend container or process.",
			"Validate Kafka advertised.listeners matches the address reachable by NIAS.",
		}
	case strings.Contains(lower, "sasl"), strings.Contains(lower, "auth"), strings.Contains(lower, "credential"), strings.Contains(lower, "not authorized"):
		out.Code = "authentication_or_authorization"
		out.Reason = "Kafka rejected the request because authentication or ACL authorization failed."
		out.Suggestions = []string{
			"Verify SASL username and password on the connection.",
			"Check Kafka ACLs for the selected operation, topic, and consumer group.",
			"Confirm the cluster expects the same security protocol configured in NIAS.",
		}
	case strings.Contains(lower, "tls"), strings.Contains(lower, "certificate"), strings.Contains(lower, "handshake"):
		out.Code = "tls_configuration"
		out.Reason = "The TLS handshake failed or the broker certificate could not be accepted."
		out.Suggestions = []string{
			"Confirm SSL is enabled only for TLS listeners.",
			"Verify broker certificates and CA trust from the backend runtime.",
			"Check whether the Kafka listener requires a different security protocol.",
		}
	case strings.Contains(lower, "unknown topic"), strings.Contains(lower, "no such topic"), strings.Contains(lower, "partition") && strings.Contains(lower, "not found"):
		out.Code = "topic_or_partition_not_found"
		out.Reason = "The selected topic or partition does not exist on the cluster metadata returned to NIAS."
		out.Suggestions = []string{
			"Refresh topics and select the topic again.",
			"Verify the topic exists on this exact cluster.",
			"Check whether the topic was recently deleted or recreated.",
		}
	case strings.Contains(lower, "replication factor"), strings.Contains(lower, "replicas"), strings.Contains(lower, "brokers"):
		out.Code = "replication_factor_invalid"
		out.Reason = "The requested replication factor is not valid for the current broker count or cluster policy."
		out.Suggestions = []string{
			"Use a replication factor less than or equal to the number of available brokers.",
			"Check broker health before creating the topic.",
		}
	case strings.Contains(lower, "leader"), strings.Contains(lower, "not coordinator"), strings.Contains(lower, "coordinator"):
		out.Code = "metadata_or_leader_changed"
		out.Reason = "Kafka metadata changed while the operation was running, or the request was sent to the wrong leader/coordinator."
		out.Suggestions = []string{
			"Refresh the Kafka browser and retry.",
			"Check broker/controller health and recent partition leadership changes.",
		}
	}
	return out
}

func kafkaErrorContext(connID int64, in ConnectionInput, topic, group string, extra map[string]string) map[string]string {
	ctx := map[string]string{"connection_id": fmt.Sprintf("%d", connID)}
	if in.Host != "" || in.Port != 0 {
		ctx["brokers"] = strings.Join(kafkaBrokers(in), ",")
		ctx["ssl"] = fmt.Sprintf("%t", in.SSL)
		if in.Username != "" {
			ctx["sasl"] = "true"
		} else {
			ctx["sasl"] = "false"
		}
	}
	if topic != "" {
		ctx["topic"] = topic
	}
	if group != "" {
		ctx["group_id"] = group
	}
	for key, value := range extra {
		ctx[key] = value
	}
	return ctx
}

func kafkaTraceID(r *http.Request, operation string) string {
	if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); id != "" {
		return id
	}
	return fmt.Sprintf("kafka-%s-%d", operation, time.Now().UnixNano())
}

func readKafkaTopics(ctx context.Context, in ConnectionInput) ([]KafkaTopicInfo, error) {
	conn, err := kafkaDialer(in).DialContext(ctx, "tcp", kafkaBrokers(in)[0])
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		return nil, err
	}

	topicsByName := map[string]*KafkaTopicInfo{}
	replicasByTopic := map[string]int{}
	for _, p := range partitions {
		topic := topicsByName[p.Topic]
		if topic == nil {
			topic = &KafkaTopicInfo{Name: p.Topic}
			topicsByName[p.Topic] = topic
		}
		topic.Partitions++
		if p.Leader.ID != 0 || p.Leader.Host != "" {
			topic.LeaderCount++
		}
		if len(p.Replicas) > replicasByTopic[p.Topic] {
			replicasByTopic[p.Topic] = len(p.Replicas)
		}
		if p.Error != nil && topic.Error == "" {
			topic.Error = p.Error.Error()
		}
	}

	topics := make([]KafkaTopicInfo, 0, len(topicsByName))
	for name, topic := range topicsByName {
		topic.ReplicationFactor = replicasByTopic[name]
		topics = append(topics, *topic)
	}
	sort.Slice(topics, func(i, j int) bool { return topics[i].Name < topics[j].Name })
	return topics, nil
}

func readKafkaGroups(ctx context.Context, in ConnectionInput) ([]KafkaGroupInfo, error) {
	client := &kafka.Client{
		Addr:      kafka.TCP(kafkaBrokers(in)...),
		Timeout:   12 * time.Second,
		Transport: kafkaTransport(in),
	}

	res, err := client.ListGroups(ctx, &kafka.ListGroupsRequest{})
	if err != nil {
		return nil, err
	}
	if res.Error != nil {
		return nil, res.Error
	}

	groups := make([]KafkaGroupInfo, 0, len(res.Groups))
	for _, group := range res.Groups {
		groups = append(groups, KafkaGroupInfo{
			GroupID:      group.GroupID,
			Coordinator:  group.Coordinator,
			ProtocolType: group.ProtocolType,
		})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].GroupID < groups[j].GroupID })
	return groups, nil
}

func readKafkaMessages(ctx context.Context, in ConnectionInput, topic string, partition, limit int) ([]KafkaMessageInfo, error) {
	partitions, err := kafkaTopicPartitions(ctx, in, topic)
	if err != nil {
		return nil, err
	}
	if partition >= 0 {
		found := false
		for _, p := range partitions {
			if p == partition {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("partition %d not found", partition)
		}
		partitions = []int{partition}
	}

	messages := make([]KafkaMessageInfo, 0, limit)
	perPartitionLimit := limit
	if partition < 0 && len(partitions) > 0 {
		perPartitionLimit = (limit+len(partitions)-1)/len(partitions) + 2
		if perPartitionLimit < 5 {
			perPartitionLimit = 5
		}
		if perPartitionLimit > limit {
			perPartitionLimit = limit
		}
	}
	var readErrs []string
	for _, p := range partitions {
		partitionMessages, err := readKafkaPartitionMessages(ctx, in, topic, p, perPartitionLimit)
		if err != nil {
			readErrs = append(readErrs, fmt.Sprintf("partition %d: %v", p, err))
			continue
		}
		messages = append(messages, partitionMessages...)
	}
	if len(messages) == 0 && len(readErrs) > 0 {
		return nil, fmt.Errorf("failed to read Kafka partitions: %s", strings.Join(readErrs, "; "))
	}
	if len(readErrs) > 0 {
		log.Printf("kafka_messages_partial topic=%s skipped_partitions=%d errors=%q", topic, len(readErrs), strings.Join(readErrs, "; "))
	}
	sort.Slice(messages, func(i, j int) bool {
		if messages[i].Timestamp.Equal(messages[j].Timestamp) {
			if messages[i].Partition == messages[j].Partition {
				return messages[i].Offset > messages[j].Offset
			}
			return messages[i].Partition < messages[j].Partition
		}
		return messages[i].Timestamp.After(messages[j].Timestamp)
	})
	if len(messages) > limit {
		messages = messages[:limit]
	}
	return messages, nil
}

func readKafkaPartitionMessages(ctx context.Context, in ConnectionInput, topic string, partition, limit int) ([]KafkaMessageInfo, error) {
	conn, err := kafkaDialer(in).DialLeader(ctx, "tcp", kafkaBrokers(in)[0], topic, partition)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	firstOffset, err := conn.ReadFirstOffset()
	if err != nil {
		return nil, err
	}
	lastOffset, err := conn.ReadLastOffset()
	if err != nil {
		return nil, err
	}
	if lastOffset <= firstOffset {
		return []KafkaMessageInfo{}, nil
	}
	startOffset := lastOffset - int64(limit)
	if startOffset < firstOffset {
		startOffset = firstOffset
	}
	if _, err := conn.Seek(startOffset, io.SeekStart); err != nil {
		return nil, err
	}

	messages := make([]KafkaMessageInfo, 0, limit)
	for len(messages) < limit {
		msg, err := conn.ReadMessage(10 << 20)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, kafka.OffsetOutOfRange) {
				break
			}
			return nil, err
		}
		if msg.Offset >= lastOffset {
			break
		}
		messages = append(messages, kafkaMessageInfo(msg))
		if msg.Offset >= lastOffset-1 {
			break
		}
	}
	return messages, nil
}

func produceKafkaMessage(ctx context.Context, in ConnectionInput, payload KafkaProduceInput) error {
	headers := make([]kafka.Header, 0, len(payload.Headers))
	for _, h := range payload.Headers {
		if strings.TrimSpace(h.Key) == "" {
			continue
		}
		headers = append(headers, kafka.Header{Key: strings.TrimSpace(h.Key), Value: []byte(h.Value)})
	}
	writer := kafka.NewWriter(kafka.WriterConfig{
		Brokers:      kafkaBrokers(in),
		Topic:        payload.Topic,
		Dialer:       kafkaDialer(in),
		Balancer:     &kafka.Hash{},
		BatchSize:    1,
		BatchTimeout: 100 * time.Millisecond,
		WriteTimeout: 10 * time.Second,
		ReadTimeout:  10 * time.Second,
	})
	defer writer.Close()
	return writer.WriteMessages(ctx, kafka.Message{
		Key:     []byte(payload.Key),
		Value:   []byte(payload.Value),
		Headers: headers,
		Time:    time.Now(),
	})
}

func consumeKafkaTest(ctx context.Context, in ConnectionInput, payload KafkaConsumeInput) (KafkaConsumeResult, error) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        kafkaBrokers(in),
		GroupID:        payload.GroupID,
		Topic:          payload.Topic,
		Dialer:         kafkaDialer(in),
		MinBytes:       1,
		MaxBytes:       10 << 20,
		MaxWait:        2 * time.Second,
		CommitInterval: 0,
	})
	defer reader.Close()

	result := KafkaConsumeResult{
		GroupID:  payload.GroupID,
		Topic:    payload.Topic,
		Messages: []KafkaMessageInfo{},
	}
	for len(result.Messages) < payload.Limit {
		readCtx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
		msg, err := reader.ReadMessage(readCtx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				break
			}
			return result, err
		}
		result.Messages = append(result.Messages, kafkaMessageInfo(msg))
	}
	result.Count = len(result.Messages)
	return result, nil
}

func readKafkaGroupDetail(ctx context.Context, in ConnectionInput, groupID string) (KafkaGroupDetail, error) {
	client := &kafka.Client{
		Addr:      kafka.TCP(kafkaBrokers(in)...),
		Timeout:   12 * time.Second,
		Transport: kafkaTransport(in),
	}
	detail := KafkaGroupDetail{GroupID: groupID}
	desc, err := client.DescribeGroups(ctx, &kafka.DescribeGroupsRequest{GroupIDs: []string{groupID}})
	if err != nil {
		return detail, err
	}
	if len(desc.Groups) > 0 {
		group := desc.Groups[0]
		detail.State = group.GroupState
		if group.Error != nil {
			detail.Error = group.Error.Error()
		}
		for _, member := range group.Members {
			assignments := make([]KafkaGroupTopicAssignments, 0, len(member.MemberAssignments.Topics))
			for _, topic := range member.MemberAssignments.Topics {
				assignments = append(assignments, KafkaGroupTopicAssignments{
					Topic:      topic.Topic,
					Partitions: topic.Partitions,
				})
			}
			detail.Members = append(detail.Members, KafkaGroupMember{
				MemberID:    member.MemberID,
				ClientID:    member.ClientID,
				ClientHost:  member.ClientHost,
				Assignments: assignments,
			})
		}
	}

	offsets, err := client.OffsetFetch(ctx, &kafka.OffsetFetchRequest{GroupID: groupID})
	if err != nil {
		return detail, err
	}
	if offsets.Error != nil {
		return detail, offsets.Error
	}
	latestReq := map[string][]kafka.OffsetRequest{}
	for topic, partitions := range offsets.Topics {
		for _, partition := range partitions {
			latestReq[topic] = append(latestReq[topic], kafka.LastOffsetOf(partition.Partition))
		}
	}
	latestByTopic := map[string]map[int]int64{}
	if len(latestReq) > 0 {
		latest, err := client.ListOffsets(ctx, &kafka.ListOffsetsRequest{Topics: latestReq})
		if err != nil {
			return detail, err
		}
		for topic, partitions := range latest.Topics {
			latestByTopic[topic] = map[int]int64{}
			for _, partition := range partitions {
				latestByTopic[topic][partition.Partition] = partition.LastOffset
			}
		}
	}
	for topic, partitions := range offsets.Topics {
		for _, partition := range partitions {
			latestOffset := latestByTopic[topic][partition.Partition]
			lag := int64(-1)
			if partition.CommittedOffset >= 0 && latestOffset >= partition.CommittedOffset {
				lag = latestOffset - partition.CommittedOffset
				detail.TotalLag += lag
			}
			item := KafkaGroupPartitionOffset{
				Topic:           topic,
				Partition:       partition.Partition,
				CommittedOffset: partition.CommittedOffset,
				LatestOffset:    latestOffset,
				Lag:             lag,
			}
			if partition.Error != nil {
				item.Error = partition.Error.Error()
			}
			detail.Offsets = append(detail.Offsets, item)
		}
	}
	sort.Slice(detail.Offsets, func(i, j int) bool {
		if detail.Offsets[i].Topic == detail.Offsets[j].Topic {
			return detail.Offsets[i].Partition < detail.Offsets[j].Partition
		}
		return detail.Offsets[i].Topic < detail.Offsets[j].Topic
	})
	return detail, nil
}

func createKafkaTopic(ctx context.Context, in ConnectionInput, payload KafkaTopicInput) error {
	client := &kafka.Client{
		Addr:      kafka.TCP(kafkaBrokers(in)...),
		Timeout:   12 * time.Second,
		Transport: kafkaTransport(in),
	}
	configs := make([]kafka.ConfigEntry, 0, len(payload.Configs))
	for key, value := range payload.Configs {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		configs = append(configs, kafka.ConfigEntry{ConfigName: key, ConfigValue: value})
	}
	res, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{
			Topic:             payload.Topic,
			NumPartitions:     payload.Partitions,
			ReplicationFactor: payload.ReplicationFactor,
			ConfigEntries:     configs,
		}},
	})
	if err != nil {
		return err
	}
	if topicErr := res.Errors[payload.Topic]; topicErr != nil {
		return topicErr
	}
	return nil
}

func deleteKafkaTopic(ctx context.Context, in ConnectionInput, topic string) error {
	client := &kafka.Client{
		Addr:      kafka.TCP(kafkaBrokers(in)...),
		Timeout:   12 * time.Second,
		Transport: kafkaTransport(in),
	}
	res, err := client.DeleteTopics(ctx, &kafka.DeleteTopicsRequest{Topics: []string{topic}})
	if err != nil {
		return err
	}
	if topicErr := res.Errors[topic]; topicErr != nil {
		return topicErr
	}
	return nil
}

func updateKafkaPartitions(ctx context.Context, in ConnectionInput, payload KafkaPartitionUpdateInput) error {
	client := &kafka.Client{
		Addr:      kafka.TCP(kafkaBrokers(in)...),
		Timeout:   12 * time.Second,
		Transport: kafkaTransport(in),
	}
	res, err := client.CreatePartitions(ctx, &kafka.CreatePartitionsRequest{
		Topics: []kafka.TopicPartitionsConfig{{
			Name:  payload.Topic,
			Count: int32(payload.Partitions),
		}},
	})
	if err != nil {
		return err
	}
	if topicErr := res.Errors[payload.Topic]; topicErr != nil {
		return topicErr
	}
	return nil
}

func kafkaTopicPartitions(ctx context.Context, in ConnectionInput, topic string) ([]int, error) {
	conn, err := kafkaDialer(in).DialContext(ctx, "tcp", kafkaBrokers(in)[0])
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	partitions, err := conn.ReadPartitions(topic)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(partitions))
	seen := map[int]bool{}
	for _, partition := range partitions {
		if partition.Topic != topic || seen[partition.ID] {
			continue
		}
		seen[partition.ID] = true
		ids = append(ids, partition.ID)
	}
	sort.Ints(ids)
	return ids, nil
}

func kafkaMessageInfo(msg kafka.Message) KafkaMessageInfo {
	headers := make([]KafkaMessageHeader, 0, len(msg.Headers))
	for _, h := range msg.Headers {
		headers = append(headers, KafkaMessageHeader{Key: h.Key, Value: string(h.Value)})
	}
	return KafkaMessageInfo{
		Topic:         msg.Topic,
		Partition:     msg.Partition,
		Offset:        msg.Offset,
		HighWaterMark: msg.HighWaterMark,
		Key:           string(msg.Key),
		Value:         string(msg.Value),
		Timestamp:     msg.Time,
		Headers:       headers,
	}
}

func kafkaBrokers(in ConnectionInput) []string {
	port := in.Port
	if port == 0 {
		port = 9092
	}
	rawHosts := strings.Split(in.Host, ",")
	brokers := make([]string, 0, len(rawHosts))
	for _, rawHost := range rawHosts {
		host := strings.TrimSpace(rawHost)
		if host == "" {
			continue
		}
		if strings.Contains(host, ":") {
			brokers = append(brokers, host)
			continue
		}
		brokers = append(brokers, fmt.Sprintf("%s:%d", host, port))
	}
	if len(brokers) == 0 {
		brokers = append(brokers, fmt.Sprintf("localhost:%d", port))
	}
	return brokers
}

func kafkaDialer(in ConnectionInput) *kafka.Dialer {
	dialer := &kafka.Dialer{Timeout: 10 * time.Second}
	if in.SSL {
		dialer.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if in.Username != "" {
		dialer.SASLMechanism = plain.Mechanism{
			Username: in.Username,
			Password: in.Password,
		}
	}
	return dialer
}

func kafkaTransport(in ConnectionInput) *kafka.Transport {
	transport := &kafka.Transport{}
	if in.SSL {
		transport.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if in.Username != "" {
		transport.SASL = plain.Mechanism{
			Username: in.Username,
			Password: in.Password,
		}
	}
	return transport
}
