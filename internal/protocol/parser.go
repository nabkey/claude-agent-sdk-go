package protocol

import (
	"encoding/json"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ParseMessage parses a raw message map into a typed Message.
func ParseMessage(data map[string]any) (types.Message, error) {
	msgType, ok := data["type"].(string)
	if !ok {
		return nil, errors.NewMessageParseError("Message missing 'type' field", data)
	}

	switch msgType {
	case "user":
		return parseUserMessage(data)
	case "assistant":
		return parseAssistantMessage(data)
	case "system":
		return parseSystemMessage(data)
	case "result":
		return parseResultMessage(data)
	case "stream_event":
		return parseStreamEvent(data)
	default:
		return nil, errors.NewMessageParseError("Unknown message type: "+msgType, data)
	}
}

func parseUserMessage(data map[string]any) (*types.UserMessage, error) {
	msg := &types.UserMessage{}

	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}

	messageData, ok := data["message"].(map[string]any)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'message' field in user message", data)
	}

	content := messageData["content"]

	// Content can be a string or a list of blocks
	switch c := content.(type) {
	case string:
		msg.Content = c
	case []any:
		blocks, err := parseContentBlocks(c)
		if err != nil {
			return nil, err
		}
		msg.Content = blocks
	default:
		msg.Content = content
	}

	return msg, nil
}

func parseAssistantMessage(data map[string]any) (*types.AssistantMessage, error) {
	msg := &types.AssistantMessage{}

	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}

	messageData, ok := data["message"].(map[string]any)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'message' field in assistant message", data)
	}

	msg.Model, _ = messageData["model"].(string)

	contentRaw, ok := messageData["content"].([]any)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'content' field in assistant message", data)
	}

	blocks, err := parseContentBlocks(contentRaw)
	if err != nil {
		return nil, err
	}
	msg.Content = blocks

	return msg, nil
}

func parseSystemMessage(data map[string]any) (*types.SystemMessage, error) {
	msg := &types.SystemMessage{
		Data: data,
	}

	subtype, ok := data["subtype"].(string)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'subtype' field in system message", data)
	}
	msg.Subtype = subtype

	return msg, nil
}

func parseResultMessage(data map[string]any) (*types.ResultMessage, error) {
	msg := &types.ResultMessage{}

	msg.Subtype, _ = data["subtype"].(string)
	msg.SessionID, _ = data["session_id"].(string)
	msg.IsError, _ = data["is_error"].(bool)

	if dms, ok := data["duration_ms"].(float64); ok {
		msg.DurationMS = int(dms)
	}
	if dapi, ok := data["duration_api_ms"].(float64); ok {
		msg.DurationAPIMS = int(dapi)
	}
	if nt, ok := data["num_turns"].(float64); ok {
		msg.NumTurns = int(nt)
	}
	if cost, ok := data["total_cost_usd"].(float64); ok {
		msg.TotalCostUSD = &cost
	}
	if usage, ok := data["usage"].(map[string]any); ok {
		msg.Usage = usage
	}
	if result, ok := data["result"].(string); ok {
		msg.Result = &result
	}
	msg.StructuredOutput = data["structured_output"]

	return msg, nil
}

func parseStreamEvent(data map[string]any) (*types.StreamEvent, error) {
	msg := &types.StreamEvent{}

	msg.UUID, _ = data["uuid"].(string)
	msg.SessionID, _ = data["session_id"].(string)

	if event, ok := data["event"].(map[string]any); ok {
		msg.Event = event
	}

	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}

	return msg, nil
}

func parseContentBlocks(rawBlocks []any) ([]types.ContentBlock, error) {
	blocks := make([]types.ContentBlock, 0, len(rawBlocks))

	for _, raw := range rawBlocks {
		blockData, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		blockType, _ := blockData["type"].(string)

		switch blockType {
		case "text":
			text, _ := blockData["text"].(string)
			blocks = append(blocks, &types.TextBlock{Text: text})

		case "thinking":
			blocks = append(blocks, &types.ThinkingBlock{
				Thinking:  getString(blockData, "thinking"),
				Signature: getString(blockData, "signature"),
			})

		case "tool_use":
			input, _ := blockData["input"].(map[string]any)
			blocks = append(blocks, &types.ToolUseBlock{
				ID:    getString(blockData, "id"),
				Name:  getString(blockData, "name"),
				Input: input,
			})

		case "tool_result":
			block := &types.ToolResultBlock{
				ToolUseID: getString(blockData, "tool_use_id"),
				Content:   blockData["content"],
			}
			if isErr, ok := blockData["is_error"].(bool); ok {
				block.IsError = &isErr
			}
			blocks = append(blocks, block)
		}
	}

	return blocks, nil
}

// MarshalUserInput creates a user input message for streaming mode.
func MarshalUserInput(prompt string, sessionID string) ([]byte, error) {
	msg := types.UserInputMessage{
		Type: "user",
		Message: types.UserInputInner{
			Role:    "user",
			Content: prompt,
		},
		SessionID: sessionID,
	}
	return json.Marshal(msg)
}
