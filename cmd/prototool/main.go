package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	internalProto "go-chat-room/internal/proto" // Import your proto package

	pbproto "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func main() {
	mode := flag.String("mode", "encode", "Mode: 'encode' or 'decode'")
	messageType := flag.String("type", "client", "Message type: 'client' (ClientToServerMessage) or 'server' (ChatMessage)")
	inputFormat := flag.String("in", "json", "Input format: 'json', 'hex', 'base64'")
	outputFormat := flag.String("out", "hex", "Output format: 'hex', 'base64', 'json'")
	flag.Parse()

	inputData, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
		os.Exit(1)
	}

	inputStr := strings.TrimSpace(string(inputData))

	switch *mode {
	case "encode":
		encode(inputStr, *messageType, *outputFormat)
	case "decode":
		decode(inputStr, *messageType, *inputFormat)
	default:
		fmt.Fprintf(os.Stderr, "Invalid mode: %s. Use 'encode' or 'decode'.\n", *mode)
		os.Exit(1)
	}
}

// Encodes JSON input to Protobuf binary (Hex or Base64)
func encode(jsonInput string, msgType string, outputFormat string) {
	var msg pbproto.Message
	var err error

	switch msgType {
	case "client":
		var clientMsg internalProto.ClientToServerMessage
		err = json.Unmarshal([]byte(jsonInput), &clientMsg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error unmarshaling JSON to ClientToServerMessage: %v\nInput: %s\n", err, jsonInput)
			os.Exit(1)
		}
		msg = &clientMsg
	case "server":
		// Need a temporary struct to handle timestamp unmarshaling from JSON string
		var serverMsgJSON struct {
			Id             uint64 `json:"id"`
			Content        string `json:"content"`
			SenderId       uint64 `json:"sender_id"`
			ReceiverId     uint64 `json:"receiver_id"`
			CreatedAt      string `json:"created_at"` // Expect ISO 8601 format e.g., "2023-10-27T10:00:00Z"
			SenderUsername string `json:"sender_username"`
			SenderAvatar   string `json:"sender_avatar"`
		}
		err = json.Unmarshal([]byte(jsonInput), &serverMsgJSON)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error unmarshaling JSON to temporary server message struct: %v\nInput: %s\n", err, jsonInput)
			os.Exit(1)
		}
		// Parse timestamp
		ts:= timestamppb.New(parseTimestamp(serverMsgJSON.CreatedAt))
		serverMsg := internalProto.ChatMessage{
			Id:             serverMsgJSON.Id,
			Content:        serverMsgJSON.Content,
			SenderId:       serverMsgJSON.SenderId,
			ReceiverId:     serverMsgJSON.ReceiverId,
			CreatedAt:      ts,
			SenderUsername: serverMsgJSON.SenderUsername,
			SenderAvatar:   serverMsgJSON.SenderAvatar,
		}
		msg = &serverMsg

	default:
		fmt.Fprintf(os.Stderr, "Invalid message type for encoding: %s. Use 'client' or 'server'.\n", msgType)
		os.Exit(1)
	}

	// Marshal to Protobuf binary
	binaryData, err := pbproto.Marshal(msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling to Protobuf: %v\n", err)
		os.Exit(1)
	}

	// Output in requested format
	switch outputFormat {
	case "hex":
		fmt.Println(hex.EncodeToString(binaryData))
	case "base64":
		fmt.Println(base64.StdEncoding.EncodeToString(binaryData))
	default:
		fmt.Fprintf(os.Stderr, "Invalid output format: %s. Use 'hex' or 'base64'.\n", outputFormat)
		os.Exit(1)
	}
}

// Decodes Protobuf binary (Hex or Base64) input to JSON
func decode(input string, msgType string, inputFormat string) {
	var binaryData []byte
	var err error

	// Decode input string to binary
	switch inputFormat {
	case "hex":
		binaryData, err = hex.DecodeString(input)
	case "base64":
		binaryData, err = base64.StdEncoding.DecodeString(input)
	default:
		fmt.Fprintf(os.Stderr, "Invalid input format: %s. Use 'hex' or 'base64'.\n", inputFormat)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding input string (%s): %v\n", inputFormat, err)
		os.Exit(1)
	}

	// Unmarshal Protobuf binary
	var msg pbproto.Message
	switch msgType {
	case "client":
		msg = &internalProto.ClientToServerMessage{}
	case "server":
		msg = &internalProto.ChatMessage{}
	default:
		fmt.Fprintf(os.Stderr, "Invalid message type for decoding: %s. Use 'client' or 'server'.\n", msgType)
		os.Exit(1)
	}

	err = pbproto.Unmarshal(binaryData, msg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error unmarshaling Protobuf: %v\n", err)
		os.Exit(1)
	}

	// Marshal to JSON for output
	// Use custom marshaling for ChatMessage to handle timestamp correctly
	var jsonOutput []byte
	if serverMsg, ok := msg.(*internalProto.ChatMessage); ok {
		jsonOutput, err = marshalChatMessageToJSON(serverMsg)
	} else {
		jsonOutput, err = json.MarshalIndent(msg, "", "  ")
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling to JSON: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(string(jsonOutput))
}

// Helper to parse timestamp string (add more formats if needed)
func parseTimestamp(tsStr string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, tsStr)
	if err == nil {
		return t
	}
	t, err = time.Parse(time.RFC3339, tsStr)
	if err == nil {
		return t
	}
	// Add more formats if necessary
	return time.Time{} // Return zero time on error
}

// Custom JSON marshaler for ChatMessage to format timestamp
func marshalChatMessageToJSON(msg *internalProto.ChatMessage) ([]byte, error) {
	type Alias internalProto.ChatMessage // Avoid recursion
	return json.MarshalIndent(&struct {
		CreatedAt string `json:"created_at"` // Override timestamp format
		*Alias
	}{
		CreatedAt: msg.CreatedAt.AsTime().Format(time.RFC3339Nano), // Format as ISO 8601
		Alias:     (*Alias)(msg),
	}, "", "  ")
}
