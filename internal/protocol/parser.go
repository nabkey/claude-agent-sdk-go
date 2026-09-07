package protocol

import (
	"encoding/json"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ParseMessage converts a raw wire message into a typed Message.
//
// Unrecognized message types return (nil, nil) rather than an error, so a
// newer CLI emitting frames this SDK does not know about never breaks a
// consumer.
func ParseMessage(data map[string]any) (types.Message, error) {
	msgType, ok := data["type"].(string)
	if !ok {
		return nil, errors.NewMessageParseError("Message missing 'type' field", data)
	}

	switch msgType {
	case "user", "user_replay":
		return parseUserMessage(data)
	case "assistant":
		return parseAssistantMessage(data)
	case "system":
		return parseSystemMessage(data)
	case "result":
		return parseResultMessage(data)
	case "stream_event":
		return parseStreamEvent(data)
	case "rate_limit_event":
		return parseRateLimitEvent(data)
	case "channel_message":
		return parseChannelMessage(data)
	case "conversation_reset":
		return parseConversationReset(data)
	default:
		// Forward-compatible: silently skip unknown message types.
		return nil, nil
	}
}

func parseUserMessage(data map[string]any) (*types.UserMessage, error) {
	msg := &types.UserMessage{}

	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}
	if uuid, ok := data["uuid"].(string); ok {
		msg.UUID = &uuid
	}
	if tur, ok := data["tool_use_result"].(map[string]any); ok {
		msg.ToolUseResult = tur
	}
	msg.SessionID = getString(data, "session_id")
	msg.IsSynthetic = getBool(data, "isSynthetic")
	msg.Origin = types.MessageOriginFromAny(data["origin"])

	messageData, ok := data["message"].(map[string]any)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'message' field in user message", data)
	}

	// Content is either a plain string or a list of blocks.
	switch c := messageData["content"].(type) {
	case string:
		msg.Content = c
	case []any:
		blocks, err := parseContentBlocks(c)
		if err != nil {
			return nil, err
		}
		msg.Content = blocks
	default:
		msg.Content = messageData["content"]
	}

	return msg, nil
}

func parseAssistantMessage(data map[string]any) (*types.AssistantMessage, error) {
	msg := &types.AssistantMessage{
		SessionID:        getString(data, "session_id"),
		UUID:             getString(data, "uuid"),
		UserMessageUUID:  getString(data, "user_message_uuid"),
		UserMessageUUIDs: getStrings(data, "user_message_uuids"),
		ContextUsage:     types.SDKContextUsageFromAny(data["context_usage"]),
	}

	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}
	if errStr, ok := data["error"].(string); ok {
		e := types.AssistantMessageError(errStr)
		msg.Error = &e
	}

	messageData, ok := data["message"].(map[string]any)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'message' field in assistant message", data)
	}

	msg.Model = getString(messageData, "model")
	msg.MessageID = getString(messageData, "id")
	msg.StopReason = getString(messageData, "stop_reason")
	if usage, ok := messageData["usage"].(map[string]any); ok {
		msg.Usage = usage
	}

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

func parseSystemMessage(data map[string]any) (types.Message, error) {
	subtype, ok := data["subtype"].(string)
	if !ok {
		return nil, errors.NewMessageParseError("Missing 'subtype' field in system message", data)
	}

	base := types.SystemMessage{Subtype: subtype, Data: data}

	switch subtype {
	case "task_started":
		msg := &types.TaskStartedMessage{
			SystemMessage:  base,
			TaskID:         getString(data, "task_id"),
			Description:    getString(data, "description"),
			UUID:           getString(data, "uuid"),
			SessionID:      getString(data, "session_id"),
			ToolUseID:      getString(data, "tool_use_id"),
			SubagentType:   getString(data, "subagent_type"),
			IsBackgrounded: getBool(data, "is_backgrounded"),
			SpawnDepth:     getInt(data, "spawn_depth"),
			WorkflowName:   getString(data, "workflow_name"),
			Prompt:         getString(data, "prompt"),
			SkipTranscript: getBool(data, "skip_transcript"),
			Ambient:        getBool(data, "ambient"),
		}
		if taskType, ok := data["task_type"].(string); ok {
			msg.TaskType = &taskType
		}
		return msg, nil

	case "task_progress":
		msg := &types.TaskProgressMessage{
			SystemMessage: base,
			TaskID:        getString(data, "task_id"),
			Description:   getString(data, "description"),
			UUID:          getString(data, "uuid"),
			SessionID:     getString(data, "session_id"),
			ToolUseID:     getString(data, "tool_use_id"),
		}
		if lastToolName, ok := data["last_tool_name"].(string); ok {
			msg.LastToolName = &lastToolName
		}
		if usageData, ok := data["usage"].(map[string]any); ok {
			msg.Usage = parseTaskUsage(usageData)
		}
		return msg, nil

	case "task_notification":
		msg := &types.TaskNotificationMessage{
			SystemMessage:  base,
			TaskID:         getString(data, "task_id"),
			Status:         types.TaskNotificationStatus(getString(data, "status")),
			OutputFile:     getString(data, "output_file"),
			Summary:        getString(data, "summary"),
			UUID:           getString(data, "uuid"),
			SessionID:      getString(data, "session_id"),
			ToolUseID:      getString(data, "tool_use_id"),
			ResourceLinks:  types.MCPResourceLinksFromAny(data["resource_links"]),
			SkipTranscript: getBool(data, "skip_transcript"),
			Ambient:        getBool(data, "ambient"),
		}
		if usageData, ok := data["usage"].(map[string]any); ok {
			usage := parseTaskUsage(usageData)
			msg.Usage = &usage
		}
		return msg, nil

	case "task_updated":
		msg := &types.TaskUpdatedMessage{
			SystemMessage: base,
			TaskID:        getString(data, "task_id"),
			UUID:          getString(data, "uuid"),
			SessionID:     getString(data, "session_id"),
		}
		if patch, ok := data["patch"].(map[string]any); ok {
			msg.Patch = patch
			msg.Status = types.TaskUpdatedStatus(getString(patch, "status"))
		}
		return msg, nil

	case "hook_started", "hook_response", "hook_progress":
		// The CLI has used a few names for this field across versions.
		name := getString(data, "hook_event")
		if name == "" {
			name = getString(data, "hook_name")
		}
		if name == "" {
			name = getString(data, "hook_event_name")
		}
		return &types.HookEventMessage{
			SystemMessage: base,
			HookEventName: name,
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	case "mirror_error":
		msg := &types.MirrorErrorMessage{
			SystemMessage: base,
			Error:         getString(data, "error"),
		}
		if key, ok := data["key"].(map[string]any); ok {
			msg.Key = &types.SessionKey{
				ProjectKey: getString(key, "project_key"),
				SessionID:  getString(key, "session_id"),
				Subpath:    getString(key, "subpath"),
			}
		}
		return msg, nil

	case "compact_boundary":
		return &types.CompactBoundaryMessage{
			SystemMessage: base,
			Trigger:       getString(data, "trigger"),
			PreTokens:     getInt(data, "pre_tokens"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	case "session_state_changed":
		return &types.SessionStateChangedMessage{
			SystemMessage: base,
			State:         getString(data, "state"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	case "permission_denied":
		msg := &types.PermissionDeniedMessage{
			SystemMessage: base,
			ToolName:      getString(data, "tool_name"),
			ToolUseID:     getString(data, "tool_use_id"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}
		if input, ok := data["tool_input"].(map[string]any); ok {
			msg.ToolInput = input
		}
		return msg, nil

	case "api_retry":
		msg := &types.APIRetryMessage{
			SystemMessage: base,
			Attempt:       getInt(data, "attempt"),
			MaxRetries:    getInt(data, "max_retries"),
			RetryDelayMS:  getInt(data, "retry_delay_ms"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}
		if status, ok := data["error_status"].(float64); ok {
			s := int(status)
			msg.ErrorStatus = &s
		}
		return msg, nil

	case "status":
		return &types.StatusMessage{
			SystemMessage: base,
			Status:        getString(data, "status"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	case "tool_progress":
		msg := &types.ToolProgressMessage{
			SystemMessage: base,
			ToolUseID:     getString(data, "tool_use_id"),
			ToolName:      getString(data, "tool_name"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}
		if progress, ok := data["progress"].(map[string]any); ok {
			msg.Progress = progress
		}
		return msg, nil

	case "background_tasks_changed":
		return &types.BackgroundTasksChangedMessage{
			SystemMessage: base,
			Tasks:         types.BackgroundTasksFromAny(data["tasks"]),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	case "thinking_tokens":
		return &types.ThinkingTokensMessage{
			SystemMessage:        base,
			EstimatedTokens:      getInt(data, "estimated_tokens"),
			EstimatedTokensDelta: getInt(data, "estimated_tokens_delta"),
			UserMessageUUID:      getString(data, "user_message_uuid"),
			SessionID:            getString(data, "session_id"),
			UUID:                 getString(data, "uuid"),
		}, nil

	case "prompt_suggestion":
		return &types.PromptSuggestionMessage{
			SystemMessage: base,
			Suggestion:    getString(data, "suggestion"),
			SessionID:     getString(data, "session_id"),
			UUID:          getString(data, "uuid"),
		}, nil

	default:
		return &base, nil
	}
}

func parseResultMessage(data map[string]any) (*types.ResultMessage, error) {
	msg := &types.ResultMessage{
		Subtype:          getString(data, "subtype"),
		SessionID:        getString(data, "session_id"),
		UUID:             getString(data, "uuid"),
		DurationMS:       getInt(data, "duration_ms"),
		DurationAPIMS:    getInt(data, "duration_api_ms"),
		NumTurns:         getInt(data, "num_turns"),
		TerminalReason:   types.TerminalReason(getString(data, "terminal_reason")),
		Origin:           types.MessageOriginFromAny(data["origin"]),
		UserMessageUUID:  getString(data, "user_message_uuid"),
		UserMessageUUIDs: getStrings(data, "user_message_uuids"),
	}
	if queued, ok := data["queued_turn_count"].(float64); ok {
		q := int(queued)
		msg.QueuedTurnCount = &q
	}
	msg.IsError, _ = data["is_error"].(bool)

	if cost, ok := data["total_cost_usd"].(float64); ok {
		msg.TotalCostUSD = &cost
	}
	if usage, ok := data["usage"].(map[string]any); ok {
		msg.Usage = usage
	}
	if result, ok := data["result"].(string); ok {
		msg.Result = &result
	}
	if stopReason, ok := data["stop_reason"].(string); ok {
		msg.StopReason = &stopReason
	}
	if status, ok := data["api_error_status"].(float64); ok {
		s := int(status)
		msg.APIErrorStatus = &s
	}
	msg.StructuredOutput = data["structured_output"]
	msg.ModelUsage = types.ModelUsageFromAny(data["modelUsage"])
	msg.PermissionDenials = types.PermissionDenialsFromAny(data["permission_denials"])

	if errs, ok := data["errors"].([]any); ok {
		for _, e := range errs {
			if s, ok := e.(string); ok {
				msg.Errors = append(msg.Errors, s)
			}
		}
	}

	if deferred, ok := data["deferred_tool_use"].(map[string]any); ok {
		use := &types.DeferredToolUse{
			ID:   getString(deferred, "id"),
			Name: getString(deferred, "name"),
		}
		if input, ok := deferred["input"].(map[string]any); ok {
			use.Input = input
		}
		msg.DeferredToolUse = use
	}

	return msg, nil
}

func parseRateLimitEvent(data map[string]any) (*types.RateLimitEvent, error) {
	msg := &types.RateLimitEvent{
		UUID:      getString(data, "uuid"),
		SessionID: getString(data, "session_id"),
	}

	// Rate limit info may be nested under "rate_limit_info" or inlined at the
	// top level, and nested fields use camelCase.
	info := data
	if nested, ok := data["rate_limit_info"].(map[string]any); ok {
		info = nested
	}

	msg.Status = types.RateLimitStatus(getString(info, "status"))

	if resetsAt, ok := getFloat64Any(info, "resets_at", "resetsAt"); ok {
		v := int64(resetsAt)
		msg.ResetsAt = &v
	}
	if rlt, ok := getStringAny(info, "rate_limit_type", "rateLimitType"); ok {
		rt := types.RateLimitType(rlt)
		msg.RateLimitType = &rt
	}
	if util, ok := info["utilization"].(float64); ok {
		msg.Utilization = &util
	}
	if overageStatus, ok := getStringAny(info, "overage_status", "overageStatus"); ok {
		os := types.RateLimitStatus(overageStatus)
		msg.OverageStatus = &os
	}
	if overageResetsAt, ok := getFloat64Any(info, "overage_resets_at", "overageResetsAt"); ok {
		v := int64(overageResetsAt)
		msg.OverageResetsAt = &v
	}
	if reason, ok := getStringAny(info, "overage_disabled_reason", "overageDisabledReason"); ok {
		msg.OverageDisabledReason = &reason
	}
	if v, ok := getBoolAny(info, "is_using_overage", "isUsingOverage", "overageInUse"); ok {
		msg.IsUsingOverage = &v
	}
	if v, ok := getFloat64Any(info, "surpassed_threshold", "surpassedThreshold"); ok {
		msg.SurpassedThreshold = &v
	}
	if v, ok := getStringAny(info, "error_code", "errorCode"); ok {
		msg.ErrorCode = &v
	}
	if v, ok := getBoolAny(info, "can_user_purchase_credits", "canUserPurchaseCredits"); ok {
		msg.CanUserPurchaseCredits = &v
	}
	msg.Raw = info

	return msg, nil
}

func parseStreamEvent(data map[string]any) (*types.StreamEvent, error) {
	msg := &types.StreamEvent{
		UUID:             getString(data, "uuid"),
		SessionID:        getString(data, "session_id"),
		UserMessageUUID:  getString(data, "user_message_uuid"),
		UserMessageUUIDs: getStrings(data, "user_message_uuids"),
	}
	if event, ok := data["event"].(map[string]any); ok {
		msg.Event = event
	}
	if parentID, ok := data["parent_tool_use_id"].(string); ok {
		msg.ParentToolUseID = &parentID
	}
	return msg, nil
}

func parseConversationReset(data map[string]any) (*types.ConversationResetMessage, error) {
	return &types.ConversationResetMessage{
		NewConversationID: getString(data, "new_conversation_id"),
		UUID:              getString(data, "uuid"),
		SessionID:         getString(data, "session_id"),
	}, nil
}

func parseChannelMessage(data map[string]any) (*types.ChannelMessage, error) {
	msg := &types.ChannelMessage{
		ServerName: getString(data, "server_name"),
		Content:    getString(data, "content"),
		UUID:       getString(data, "uuid"),
		SessionID:  getString(data, "session_id"),
	}
	if d, ok := data["data"].(map[string]any); ok {
		msg.Data = d
	}
	return msg, nil
}

// getStringAny returns the first string value found among the given keys.
func getStringAny(data map[string]any, keys ...string) (string, bool) {
	for _, key := range keys {
		if v, ok := data[key].(string); ok {
			return v, true
		}
	}
	return "", false
}

// getFloat64Any returns the first numeric value found among the given keys.
func getFloat64Any(data map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		if v, ok := data[key].(float64); ok {
			return v, true
		}
	}
	return 0, false
}

// getBoolAny returns the first boolean value found among the given keys.
func getBoolAny(data map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if v, ok := data[key].(bool); ok {
			return v, true
		}
	}
	return false, false
}

// parseTaskUsage extracts TaskUsage from a map.
func parseTaskUsage(data map[string]any) types.TaskUsage {
	return types.TaskUsage{
		TotalTokens: getInt(data, "total_tokens"),
		ToolUses:    getInt(data, "tool_uses"),
		DurationMS:  getInt(data, "duration_ms"),
	}
}

func parseContentBlocks(rawBlocks []any) ([]types.ContentBlock, error) {
	blocks := make([]types.ContentBlock, 0, len(rawBlocks))

	for _, raw := range rawBlocks {
		blockData, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		switch getString(blockData, "type") {
		case "text":
			blocks = append(blocks, &types.TextBlock{Text: getString(blockData, "text")})

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

		case "server_tool_use":
			input, _ := blockData["input"].(map[string]any)
			blocks = append(blocks, &types.ServerToolUseBlock{
				ID:    getString(blockData, "id"),
				Name:  types.ServerToolName(getString(blockData, "name")),
				Input: input,
			})

		case "advisor_tool_result", "server_tool_result":
			content, _ := blockData["content"].(map[string]any)
			blocks = append(blocks, &types.ServerToolResultBlock{
				ToolUseID: getString(blockData, "tool_use_id"),
				Content:   content,
			})
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
